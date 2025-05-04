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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	cfclient "github.com/unmango/cloudflare-operator/internal/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	cloudflareTunnelFinalizer = "cloudflaretunnel.unmango.dev/finalizer"
)

const (
	typeAvailableCloudflareTunnel = "Available"
	typeErrorCloudflareTunnel     = "Error"
)

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
		log.V(1).Info("Not found, ignoring")
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

	if !tunnel.DeletionTimestamp.IsZero() {
		if err := r.deleteTunnel(ctx, tunnel.Status.Id, tunnel); err != nil {
			log.Error(err, "Failed to delete cloudflare tunnel")
			return ctrl.Result{}, err
		} else {
			log.Info("Successfully deleted cloudflare tunnel")
			return ctrl.Result{}, nil
		}
	}

	if id := tunnel.Status.Id; id != "" {
		log.Info("Fetching existing cloudflare tunnel", "id", id)
		if err := r.updateTunnel(ctx, id, tunnel); err != nil {
			log.Error(err, "Failed to update existing cloudflare tunnel", "id", id)
			return ctrl.Result{}, err
		}
	} else {
		log.Info("Creating cloudflare tunnel", "name", req.Name)
		if err := r.createTunnel(ctx, tunnel); err != nil {
			log.Error(err, "Failed to create new cloudflare tunnel", "name", tunnel.Name)
			return ctrl.Result{}, err
		}
	}

	if !controllerutil.ContainsFinalizer(tunnel, cloudflareTunnelFinalizer) {
		if err := patch(ctx, r, tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = controllerutil.AddFinalizer(tunnel, cloudflareTunnelFinalizer)
		}); err != nil {
			log.Error(err, "Failed to add finalizer to CloudflareTunnel")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *CloudflareTunnelReconciler) createTunnel(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)

	res, err := r.Cloudflare.CreateTunnel(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID:    cloudflare.F(tunnel.Spec.AccountId),
		Name:         cloudflare.F(tunnel.Spec.Name),
		ConfigSrc:    cloudflare.F(r.mapConfigSrc(tunnel.Spec.ConfigSource)),
		TunnelSecret: cloudflare.Null[string](),
	})
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
			return err // TODO: Include inner error somehow
		}

		return err
	}

	if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeAvailableCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: "Successfully created cloudflare tunnel",
		})
		obj.Status.AccountTag = res.AccountTag
		obj.Status.Id = res.ID
		obj.Status.RemoteConfig = res.RemoteConfig
		obj.Status.Status = cfv1alpha1.CloudflareTunnelHealth(res.Status)
		obj.Status.CreatedAt = metav1.NewTime(res.CreatedAt)
	}); err != nil {
		log.Error(err, "Failed to update cloudflare tunnel status")
		return err
	}

	return nil
}

func (r *CloudflareTunnelReconciler) updateTunnel(ctx context.Context, id string, tunnel *cfv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)

	res, err := r.Cloudflare.GetTunnel(ctx, id, zero_trust.TunnelCloudflaredGetParams{
		AccountID: cloudflare.F(tunnel.Spec.AccountId),
	})
	if err != nil {
		if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeErrorCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  "Reconciling",
				Message: "Get tunnel request to Cloudflare failed",
			})
		}); err != nil {
			log.Error(err, "Failed to update cloudflare tunnel status conditions")
			return err // TODO: Include inner error somehow
		}

		return err
	}

	if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeAvailableCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: "Found existing tunnel matching tunnel id",
		})
		obj.Status.AccountTag = res.AccountTag
		obj.Status.CreatedAt = metav1.NewTime(res.CreatedAt)
		obj.Status.Id = res.ID
		obj.Status.RemoteConfig = res.RemoteConfig
		obj.Status.Status = cfv1alpha1.CloudflareTunnelHealth(res.Status)
	}); err != nil {
		log.Error(err, "Failed to update cloudflare tunnel status", "id", id)
		return err
	} else {
		return nil
	}
}

func (r *CloudflareTunnelReconciler) deleteTunnel(ctx context.Context, id string, tunnel *cfv1alpha1.CloudflareTunnel) error {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(tunnel, cloudflareTunnelFinalizer) {
		log.Info("No finalizer, nothing to do")
		return nil
	}

	if id != "" {
		log.Info("Deleting cloudflare tunnel", "id", id)
		_, err := r.Cloudflare.DeleteTunnel(ctx, id, zero_trust.TunnelCloudflaredDeleteParams{
			AccountID: cloudflare.F(tunnel.Spec.AccountId),
		})
		if err != nil {
			if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
				_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
					Type:    typeErrorCloudflareTunnel,
					Status:  metav1.ConditionTrue,
					Reason:  "Reconciling",
					Message: "Get tunnel request to Cloudflare failed",
				})
			}); err != nil {
				log.Error(err, "Failed to update cloudflare tunnel status conditions")
				return err // TODO: Include inner error somehow
			}

			return err
		}
	}

	log.Info("Removing finalizer", "id", id)
	if err := patch(ctx, r, tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = controllerutil.RemoveFinalizer(tunnel, cloudflareTunnelFinalizer)
	}); err != nil {
		log.Error(err, "Failed to remove finanlizer")
		return err
	} else {
		return nil
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
