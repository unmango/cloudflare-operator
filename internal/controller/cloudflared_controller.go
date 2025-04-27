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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultCloudflaredImage = "docker.io/cloudflare/cloudflared:latest"
	cloudflaredFinalizer    = "cloudflared.unmango.dev/finalizer"
)

const (
	typeAvailableCloudflared = "Available"
	typeDegradedCloudflared  = "Degraded"
)

// CloudflaredReconciler reconciles a Cloudflared object
type CloudflaredReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflareds/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=daemonsets;deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *CloudflaredReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cloudflared := &cfv1alpha1.Cloudflared{}
	if err := r.Get(ctx, req.NamespacedName, cloudflared); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("Cloudflared resource not found, ignoring")
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "Failed to get cloudflared")
			return ctrl.Result{}, err
		}
	}

	if len(cloudflared.Status.Conditions) == 0 {
		if err := r.setStatus(ctx, cloudflared, metav1.Condition{
			Type:    typeAvailableCloudflared,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Starting reconciliation",
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !r.containsFinalizer(cloudflared) {
		r.addFinalizer(ctx, cloudflared)
	}

	return ctrl.Result{}, nil
}

func (r *CloudflaredReconciler) setStatus(
	ctx context.Context,
	cloudflared *cfv1alpha1.Cloudflared,
	condition metav1.Condition,
) error {
	log := logf.FromContext(ctx)

	_ = meta.SetStatusCondition(&cloudflared.Status.Conditions, condition)
	if err := r.Status().Update(ctx, cloudflared); err != nil {
		log.Error(err, "Failed to update Cloudflared status")
		return err
	}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: cloudflared.Namespace,
		Name:      cloudflared.Name,
	}, cloudflared); err != nil {
		log.Error(err, "Failed to re-fetch Cloudflared")
		return err
	}

	return nil
}

func (r *CloudflaredReconciler) containsFinalizer(cloudflared *cfv1alpha1.Cloudflared) bool {
	return controllerutil.ContainsFinalizer(cloudflared, cloudflaredFinalizer)
}

func (r *CloudflaredReconciler) addFinalizer(ctx context.Context, cloudflared *cfv1alpha1.Cloudflared) error {
	log := logf.FromContext(ctx)

	if ok := controllerutil.AddFinalizer(cloudflared, cloudflaredFinalizer); !ok {
		err := fmt.Errorf("finalizer for cloudflared was not added")
		log.Error(err, "Failed to add finalizer for Cloudflared")
		return err
	}

	if err := r.Update(ctx, cloudflared); err != nil {
		log.Error(err, "Failed to update custom resource to add finalizer")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudflaredReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cfv1alpha1.Cloudflared{}).
		Named("cloudflared").
		Complete(r)
}
