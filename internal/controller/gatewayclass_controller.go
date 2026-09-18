/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

const gatewayClassFinalizer = "gatewayclass.cloudflare.unmango.dev/finalizer"

// GatewayClassReconciler reconciles a GatewayClass object
type GatewayClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaregatewayconfigs,verbs=get;list;watch

func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	class := &gatewayv1.GatewayClass{}
	if err := r.Get(ctx, req.NamespacedName, class); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Another implementation owns this class. Writing status to it would claim a
	// class that is not ours.
	if string(class.Spec.ControllerName) != gateway.ControllerName {
		log.V(1).Info("Ignoring GatewayClass owned by another controller",
			"controllerName", class.Spec.ControllerName,
		)
		return ctrl.Result{}, nil
	}

	log.Info(msgStartingReconciliation)

	if !class.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, class)
	}

	if controllerutil.AddFinalizer(class, gatewayClassFinalizer) {
		if err := r.Update(ctx, class); err != nil {
			return ctrl.Result{}, err
		}
	}

	accepted := metav1.Condition{
		Type:    string(gatewayv1.GatewayClassConditionStatusAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.GatewayClassReasonAccepted),
		Message: "Parameters resolved",
	}
	if _, err := gateway.Resolve(ctx, r.Client, class); err != nil {
		if !gateway.IsParametersError(err) {
			return ctrl.Result{}, err
		}

		accepted = metav1.Condition{
			Type:    string(gatewayv1.GatewayClassConditionStatusAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  reasonInvalidParameters,
			Message: err.Error(),
		}
	}

	return ctrl.Result{}, r.patchStatus(ctx, class, accepted)
}

// finalize releases the finalizer once no Gateway references the class.
// A GatewayClass is cluster scoped, so it cannot be garbage collected through an
// owner reference and has to be held open by hand.
func (r *GatewayClassReconciler) finalize(ctx context.Context, class *gatewayv1.GatewayClass) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	gateways := &gatewayv1.GatewayList{}
	if err := r.List(ctx, gateways); err != nil {
		return ctrl.Result{}, err
	}

	// Filtered in memory rather than through an index: the list is served from
	// the cache and a class is deleted rarely.
	for _, gw := range gateways.Items {
		if string(gw.Spec.GatewayClassName) == class.Name {
			log.Info("Holding the GatewayClass finalizer", "gateway", client.ObjectKeyFromObject(&gw))
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	if controllerutil.RemoveFinalizer(class, gatewayClassFinalizer) {
		return ctrl.Result{}, r.Update(ctx, class)
	}

	return ctrl.Result{}, nil
}

func (r *GatewayClassReconciler) patchStatus(ctx context.Context, class *gatewayv1.GatewayClass, conditions ...metav1.Condition) error {
	return patchSubResource(ctx, r.Status(), class, func(obj *gatewayv1.GatewayClass) {
		for _, c := range conditions {
			c.ObservedGeneration = obj.Generation
			_ = meta.SetStatusCondition(&obj.Status.Conditions, c)
		}

		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:               string(gatewayv1.GatewayClassConditionStatusSupportedVersion),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.GatewayClassReasonSupportedVersion),
			Message:            "Gateway API v1 is supported",
			ObservedGeneration: obj.Generation,
		})
	})
}

func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{}).
		Watches(&gatewayv1.Gateway{}, gatewayClassHandler()).
		Named("gatewayclass").
		Complete(r)
}
