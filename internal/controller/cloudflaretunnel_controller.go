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
	"os"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	cfclient "github.com/unmango/cloudflare-operator/pkg/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	typeAvailableCloudflareTunnel = "Available"
	typeErrorCloudflareTunnel     = "Error"
)

var apiTokenSet = os.Getenv("CLOUDFLARE_API_TOKEN") != ""

// CloudflareTunnelReconciler reconciles a CloudflareTunnel object
type CloudflareTunnelReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Cloudflare cfclient.Client
}

// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaretunnels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaretunnels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaretunnels/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *CloudflareTunnelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	tunnel := &cfv1alpha1.CloudflareTunnel{}
	if err := r.Get(ctx, req.NamespacedName, tunnel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if len(tunnel.Status.Conditions) == 0 {
		if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeAvailableCloudflareTunnel,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation",
			})
		}); err != nil {
			log.Error(err, "Failed to update cloudflare tunnel status conditions")
			return ctrl.Result{}, err
		}
	}

	if os.Getenv("CLOUDFLARE_API_TOKEN") == "" {
		if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeErrorCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  "Reconciling",
				Message: "Cloudflare API token not provided to controller",
			})
		}); err != nil {
			log.Error(err, "Failed to update cloudflare tunnel status conditions")
			return ctrl.Result{}, err
		} else {
			log.Info("Unable to create tunnel, no API token provided")
			return ctrl.Result{}, nil
		}
	}

	log.Info("Creating cloudflare tunnel", "name", req.Name)
	createResponse, err := r.Cloudflare.CreateTunnel(ctx, r.createParams(tunnel))
	if err != nil {
		if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeErrorCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  "Reconciling",
				Message: "New tunnel request to Cloudflare failed",
			})
		}); err != nil {
			log.Error(err, "Failed to update cloudflare tunnel status conditions")
			return ctrl.Result{}, err
		}

		log.Error(err, "Failed to create new cloudflare tunnel", "name", req.Name)
		return ctrl.Result{}, err
	}
	if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeAvailableCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: "Successfully created cloudflare tunnel",
		})
		obj.Status.AccountTag = createResponse.AccountTag
		obj.Status.Id = createResponse.ID
		obj.Status.RemoteConfig = createResponse.RemoteConfig
		obj.Status.Status = createResponse.Status
		obj.Status.CreatedAt = createResponse.CreatedAt.String()
	}); err != nil {
		log.Error(err, "Failed to update cloudflare tunnel status conditions")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CloudflareTunnelReconciler) createParams(tunnel *cfv1alpha1.CloudflareTunnel) zero_trust.TunnelCloudflaredNewParams {
	return zero_trust.TunnelCloudflaredNewParams{
		AccountID:    cloudflare.F(tunnel.Spec.AccountId),
		Name:         cloudflare.F(tunnel.Spec.Name),
		ConfigSrc:    cloudflare.F(r.mapConfigSrc(tunnel.Spec.ConfigSource)),
		TunnelSecret: cloudflare.Null[string](),
	}
}

func (r *CloudflareTunnelReconciler) mapConfigSrc(src cfv1alpha1.CloudflareTunnelConfigSource) zero_trust.TunnelCloudflaredNewParamsConfigSrc {
	switch src {
	case cfv1alpha1.CloudflareCloudflareTunnelConfigSource:
		return zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare
	case cfv1alpha1.LocalCloudflareTunnelConfigSource:
		return zero_trust.TunnelCloudflaredNewParamsConfigSrcLocal
	default:
		panic("unrecognized config source: " + src)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudflareTunnelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cfv1alpha1.CloudflareTunnel{}).
		Named("cloudflaretunnel").
		Complete(r)
}
