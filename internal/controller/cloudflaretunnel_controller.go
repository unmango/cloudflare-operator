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
	"os"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
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
	cloudflareTunnelFinalizer = "cloudflaretunnel.cloudflare.unmango.dev/finalizer"
)

const (
	typeAvailableCloudflareTunnel   = "Available"
	typeDegradedCloudflareTunnel    = "Degraded"
	typeProgressingCloudflareTunnel = "Progressing"
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
		log.V(1).Info("CloudflareTunnel resource not found, ignoring")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if os.Getenv("CLOUDFLARE_API_TOKEN") == "" {
		log.Info("No CLOUDFLARE_API_TOKEN provided, API calls will likely fail")
	}

	if !tunnel.DeletionTimestamp.IsZero() {
		if tunnel.Spec.Cloudflared != nil {
			log.Info("Listing cloudflareds")
			cloudflareds, err := r.listCloudflareds(ctx, tunnel)
			if err != nil {
				return ctrl.Result{}, err
			}

			log.Info("Found cloudflareds", "items", cloudflareds.Items)
			if len(cloudflareds.Items) > 0 {
				if err := r.deleteCloudflareds(ctx, tunnel, cloudflareds); err != nil {
					return ctrl.Result{}, err
				} else {
					log.Info("Successfully deleted owned Cloudflareds")
					return ctrl.Result{Requeue: true}, nil
				}
			}
		}

		if tunnel.Status.Id == nil {
			log.Info("No tunnel id, nothing to do")
			return ctrl.Result{}, nil
		}

		log.V(2).Info("Deleting tunnel from the cloudflare API")
		if err := r.deleteTunnel(ctx, *tunnel.Status.Id, tunnel); err != nil {
			return ctrl.Result{Requeue: true}, nil
		}

		if err := patch(ctx, r, tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = controllerutil.RemoveFinalizer(tunnel, cloudflareTunnelFinalizer)
		}); err != nil {
			return ctrl.Result{}, err
		}

		log.Info("Successfully deleted cloudflare tunnel")
		return ctrl.Result{}, nil
	}

	if len(tunnel.Status.Conditions) == 0 {
		if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeAvailableCloudflareTunnel,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation",
			})
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeDegradedCloudflareTunnel,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation",
			})
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeProgressingCloudflareTunnel,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !controllerutil.ContainsFinalizer(tunnel, cloudflareTunnelFinalizer) {
		if err := patch(ctx, r, tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = controllerutil.AddFinalizer(tunnel, cloudflareTunnelFinalizer)
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	if tunnel.Status.Id == nil {
		if err := r.createTunnel(ctx, tunnel); err != nil {
			log.Error(err, "Failed to create new cloudflare tunnel", "name", tunnel.Name)
			return ctrl.Result{}, nil
		}

		log.Info("Successfully created cloudflare tunnel")
		return ctrl.Result{Requeue: true}, nil
	}

	tunnelId := *tunnel.Status.Id
	res, err := r.Cloudflare.GetTunnel(ctx, tunnelId, zero_trust.TunnelCloudflaredGetParams{
		AccountID: cloudflare.F(tunnel.Spec.AccountId),
	})
	if err != nil {
		log.Error(err, "Failed to read tunnel from Cloudflare API")
		return ctrl.Result{}, nil
	}
	if err = patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeProgressingCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: "Selecting cloudflared resources",
		})
		obj.Status.AccountTag = res.AccountTag
		obj.Status.ConnectionsActiveAt = metav1.NewTime(res.ConnsActiveAt)
		obj.Status.ConnectionsInactiveAt = metav1.NewTime(res.ConnsInactiveAt)
		obj.Status.CreatedAt = metav1.NewTime(res.CreatedAt)
		obj.Status.Id = &res.ID
		obj.Status.Name = res.Name
		obj.Status.RemoteConfig = res.RemoteConfig
		obj.Status.Status = cfv1alpha1.CloudflareTunnelHealth(res.Status)
		obj.Status.Type = cfv1alpha1.CloudflareTunnelType(res.TunType)
	}); err != nil {
		return ctrl.Result{}, err
	}

	if tunnel.Status.Name != tunnel.Spec.Name {
		res, err := r.Cloudflare.EditTunnel(ctx, tunnelId, zero_trust.TunnelCloudflaredEditParams{
			AccountID: cloudflare.F(tunnel.Spec.AccountId),
			Name:      cloudflare.F(tunnel.Spec.Name),
		})
		if err != nil {
			log.Error(err, "Failed to update tunnel in Cloudflare API")
			return ctrl.Result{}, nil
		}

		if err = patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			obj.Status.Name = res.Name
		}); err != nil {
			return ctrl.Result{}, err
		}

		log.Info("Successfully updated tunnel in Cloudflare API")
		return ctrl.Result{}, nil
	}

	if config := tunnel.Spec.Config; config != nil {
		c := cfclient.CloudflareTunnelConfig(*config)
		_, err := r.Cloudflare.UpdateConfiguration(ctx, tunnelId, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
			AccountID: cloudflare.F(tunnel.Spec.AccountId),
			Config:    cloudflare.F(c.UpdateParams()),
		})
		if err != nil {
			log.Error(err, "Failed to update tunnel configuration in Cloudflare API")
			return ctrl.Result{}, nil
		}
	}

	if cf := tunnel.Spec.Cloudflared; cf != nil {
		log.V(2).Info("Listing selected Cloudflared resources")
		cloudflareds, err := r.listCloudflareds(ctx, tunnel)
		if err != nil {
			return ctrl.Result{}, err
		}

		if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeProgressingCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  "Reconciling",
				Message: "Selecting cloudflared resources",
			})
			obj.Status.Instances = int32(len(cloudflareds.Items))
		}); err != nil {
			return ctrl.Result{}, err
		}

		var count int
		for _, c := range cloudflareds.Items {
			c.Spec.Config = &cfv1alpha1.CloudflaredConfig{
				CloudflaredConfigInline: cfv1alpha1.CloudflaredConfigInline{
					TunnelId:  &tunnelId,
					AccountId: &tunnel.Status.AccountTag,
				},
			}

			log.V(2).Info("Applying tunnel id to Cloudflared", "name", c.Name, "id", tunnelId)
			if err := r.Update(ctx, &c); err != nil {
				log.Error(err, "Failed to update Cloudflared")
				return ctrl.Result{}, nil
			} else {
				count++
				log.Info("Applied config to Cloudflared",
					"name", c.Name,
					"id", tunnelId,
					"account", tunnel.Spec.AccountId,
				)
			}
		}

		if cf.Template != nil && count == 0 {
			selector, err := metav1.LabelSelectorAsSelector(cf.Selector)
			if err != nil {
				log.Error(err, "Failed to convert LabelSelector to selector")
				return ctrl.Result{}, nil
			}

			if !selector.Matches(labels.Set(cf.Template.Labels)) {
				log.Info("Given label selector does not match Cloudflared template labels",
					"selector", selector,
					"labels", cf.Template.Labels,
				)
				return ctrl.Result{}, nil
			}

			cloudflared := &cfv1alpha1.Cloudflared{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tunnel.Name,
					Namespace: tunnel.Namespace,
					Labels:    cf.Template.Labels,
				},
				Spec: cf.Template.Spec,
			}

			cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
				CloudflaredConfigInline: cfv1alpha1.CloudflaredConfigInline{
					TunnelId:  &tunnelId,
					AccountId: &tunnel.Status.AccountTag,
				},
			}

			if err := controllerutil.SetControllerReference(tunnel, cloudflared, r.Scheme); err != nil {
				log.Error(err, "Failed to set controller reference")
				return ctrl.Result{}, nil
			}

			if err := r.Create(ctx, cloudflared); err != nil {
				log.Error(err, "Failed to create Cloudflared")
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	if err = patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeAvailableCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: "Finished reconciling",
		})
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeDegradedCloudflareTunnel,
			Status:  metav1.ConditionFalse,
			Reason:  "Reconciling",
			Message: "Finished reconciling",
		})
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeProgressingCloudflareTunnel,
			Status:  metav1.ConditionFalse,
			Reason:  "Reconciling",
			Message: "Finished reconciling",
		})
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *CloudflareTunnelReconciler) createTunnel(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel) error {
	name := tunnel.Spec.Name
	if name == "" {
		name = tunnel.Name
	}

	res, err := r.Cloudflare.CreateTunnel(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID:    cloudflare.F(tunnel.Spec.AccountId),
		Name:         cloudflare.F(name),
		ConfigSrc:    cloudflare.F(r.mapConfigSrc(tunnel.Spec.ConfigSource)),
		TunnelSecret: cloudflare.Null[string](),
	})
	if err != nil {
		return cfclient.IgnoreConflict(err)
	}

	if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeProgressingCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: "Successfully created cloudflare tunnel",
		})
		obj.Status.Name = res.Name
		obj.Status.AccountTag = res.AccountTag
		obj.Status.Id = &res.ID
		obj.Status.RemoteConfig = res.RemoteConfig
		obj.Status.Status = cfv1alpha1.CloudflareTunnelHealth(res.Status)
		obj.Status.CreatedAt = metav1.NewTime(res.CreatedAt)
		obj.Status.ConnectionsActiveAt = metav1.NewTime(res.ConnsActiveAt)
		obj.Status.ConnectionsInactiveAt = metav1.NewTime(res.ConnsInactiveAt)
		obj.Status.Type = cfv1alpha1.CloudflareTunnelType(res.TunType)
	}); err != nil {
		return err
	}

	return nil
}

func (r *CloudflareTunnelReconciler) updateTunnel(ctx context.Context, id string, tunnel *cfv1alpha1.CloudflareTunnel) error {
	if config := tunnel.Spec.Config; config != nil {
		c := cfclient.CloudflareTunnelConfig(*config)
		_, err := r.Cloudflare.UpdateConfiguration(ctx, id, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
			AccountID: cloudflare.F(tunnel.Spec.AccountId),
			Config:    cloudflare.F(c.UpdateParams()),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *CloudflareTunnelReconciler) listCloudflareds(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel) (*cfv1alpha1.CloudflaredList, error) {
	selector, err := metav1.LabelSelectorAsSelector(tunnel.Spec.Cloudflared.Selector)
	if err != nil {
		return nil, fmt.Errorf("converting label selector into label: %w", err)
	}

	cloudflareds := &cfv1alpha1.CloudflaredList{}
	if err := r.List(ctx, cloudflareds, &client.ListOptions{
		Namespace:     tunnel.Namespace,
		LabelSelector: selector,
	}); err != nil {
		return nil, fmt.Errorf("listing cloudflareds: %w", err)
	} else {
		return cloudflareds, nil
	}
}

func (r *CloudflareTunnelReconciler) deleteCloudflareds(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel, cloudflareds *cfv1alpha1.CloudflaredList) error {
	if len(cloudflareds.Items) > 0 {
		if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeAvailableCloudflareTunnel,
				Status:  metav1.ConditionFalse,
				Reason:  "Reconciling",
				Message: "Deleting owned Cloudflared instances",
			})
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeDegradedCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  "Reconciling",
				Message: "Deleting owned Cloudflared instances",
			})
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeProgressingCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  "Reconciling",
				Message: "Deleting owned Cloudflared instances",
			})
		}); err != nil {
			return err
		}
	}

	for _, c := range cloudflareds.Items {
		hasOwnerRef, err := controllerutil.HasOwnerReference(c.OwnerReferences, tunnel, r.Scheme)
		if err != nil {
			return err
		}
		if !hasOwnerRef {
			return nil
		}
		if err = r.Delete(ctx, &c); err != nil {
			return err
		}
	}

	return nil
}

func (r *CloudflareTunnelReconciler) deleteTunnel(ctx context.Context, id string, tunnel *cfv1alpha1.CloudflareTunnel) error {
	if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeDegradedCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  "Reconciling",
			Message: "Deleting tunnel from Cloudflare API",
		})
	}); err != nil {
		return err
	}

	_, err := r.Cloudflare.DeleteTunnel(ctx, id, zero_trust.TunnelCloudflaredDeleteParams{
		AccountID: cloudflare.F(tunnel.Status.AccountTag),
	})

	return cfclient.IgnoreNotFound(err)
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
