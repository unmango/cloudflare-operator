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
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	cfclient "github.com/unmango/cloudflare-operator/internal/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	cloudflareTunnelFinalizer = "cloudflaretunnel.cloudflare.unmango.dev/finalizer"
)

// retryAfterFailedCreate is how long to wait before creating the tunnel again
// after a failure, whether the Cloudflare API rejected it or the spec named a
// source that could not be read.
const retryAfterFailedCreate = time.Minute

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

	// Sources reads the Secret or ConfigMap named by spec.tunnelSecret. It
	// bypasses the manager cache so the operator needs only get on those
	// resources, rather than the cluster-wide list and watch a cached read
	// would require. Access is granted separately from the manager role, so
	// reads through it fail with Forbidden on a default install.
	Sources client.Reader
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
					return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
				}
			}
		}

		if tunnel.Status.Id == nil {
			// The tunnel was never created on the Cloudflare side, so there is
			// nothing to clean up. Release the finalizer or the object is stuck.
			log.Info("No tunnel id, releasing the finalizer")
			return ctrl.Result{}, patch(ctx, r, tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
				_ = controllerutil.RemoveFinalizer(obj, cloudflareTunnelFinalizer)
			})
		}

		log.V(2).Info("Deleting tunnel from the cloudflare API")
		if err := r.deleteTunnel(ctx, *tunnel.Status.Id, tunnel); err != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
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
				Reason:  reasonReconciling,
				Message: msgStartingReconciliation,
			})
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeDegradedCloudflareTunnel,
				Status:  metav1.ConditionUnknown,
				Reason:  reasonReconciling,
				Message: msgStartingReconciliation,
			})
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeProgressingCloudflareTunnel,
				Status:  metav1.ConditionUnknown,
				Reason:  reasonReconciling,
				Message: msgStartingReconciliation,
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !controllerutil.ContainsFinalizer(tunnel, cloudflareTunnelFinalizer) {
		log.V(2).Info("Adding finalizer to CloudflareTunnel")
		if err := patch(ctx, r, tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
			_ = controllerutil.AddFinalizer(tunnel, cloudflareTunnelFinalizer)
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	var tunnelId string
	if id := tunnel.Status.Id; id == nil {
		log.V(2).Info("Creating cloudflare tunnel", "name", req.Name)
		if err := r.createTunnel(ctx, tunnel); err != nil {
			log.Error(err, "Failed to create new cloudflare tunnel", "name", tunnel.Name)
			// Nothing else enqueues the tunnel: no controller watches the Secret
			// or ConfigMap a tunnel secret can name, so without this the tunnel
			// stays Degraded until its own spec changes.
			return ctrl.Result{RequeueAfter: retryAfterFailedCreate}, nil
		}

		log.Info("Created cloudflare tunnel")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	} else {
		tunnelId = *id
	}

	log.V(2).Info("Updating existing cloudflare tunnel", "id", tunnelId)
	if err := r.updateTunnel(ctx, tunnelId, tunnel); err != nil {
		log.Error(err, "Failed to update existing cloudflare tunnel", "id", tunnelId)
		return ctrl.Result{}, nil
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
				Reason:  reasonReconciling,
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
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

// effectiveName is the name the tunnel is given on the Cloudflare side. The API
// requires one, so an unset spec.name falls back to the name of the object.
func effectiveName(tunnel *cfv1alpha1.CloudflareTunnel) string {
	if tunnel.Spec.Name != "" {
		return tunnel.Spec.Name
	}

	return tunnel.Name
}

// degrade records a user-fixable problem with the spec on the resource.
func (r *CloudflareTunnelReconciler) degrade(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel, message string) error {
	return patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeDegradedCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  reasonInvalidSpec,
			Message: message,
		})
	})
}

// tunnelSecret resolves spec.tunnelSecret. The second return reports whether a
// secret was configured at all; without one Cloudflare generates its own.
func (r *CloudflareTunnelReconciler) tunnelSecret(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel) (string, bool, error) {
	value, err := resolveTunnelSecret(ctx, r.Sources, tunnel.Namespace, tunnel.Spec.TunnelSecret)
	if errors.Is(err, errValueUnset) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	// Cloudflare rejects anything shorter, and the failure it reports is much
	// less specific than this one.
	if raw, err := base64.StdEncoding.DecodeString(value); err != nil {
		return "", false, fmt.Errorf("tunnel secret is not valid base64: %w", err)
	} else if len(raw) < 32 {
		return "", false, fmt.Errorf("tunnel secret decodes to %d bytes, at least 32 are required", len(raw))
	}

	return value, true, nil
}

func (r *CloudflareTunnelReconciler) createTunnel(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel) error {
	value, ok, err := r.tunnelSecret(ctx, tunnel)
	if err != nil {
		return errors.Join(err, r.degrade(ctx, tunnel, fmt.Sprintf("Resolving tunnel secret: %s", err)))
	}

	secret := cloudflare.Null[string]()
	if ok {
		secret = cloudflare.F(value)
	}

	res, err := r.Cloudflare.CreateTunnel(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID:    cloudflare.F(tunnel.Spec.AccountId),
		Name:         cloudflare.F(effectiveName(tunnel)),
		ConfigSrc:    cloudflare.F(r.mapConfigSrc(tunnel.Spec.ConfigSource)),
		TunnelSecret: secret,
	})
	if err != nil {
		return cfclient.IgnoreConflict(err)
	}

	if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeProgressingCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  reasonReconciling,
			Message: "Successfully created cloudflare tunnel",
		})
		obj.Status.Name = res.Name
		obj.Status.AccountTag = res.AccountTag
		obj.Status.Id = &res.ID
		obj.Status.RemoteConfig = res.ConfigSrc == shared.CloudflareTunnelConfigSrcCloudflare
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
	res, err := r.Cloudflare.GetTunnel(ctx, id, zero_trust.TunnelCloudflaredGetParams{
		AccountID: cloudflare.F(tunnel.Spec.AccountId),
	})
	if err != nil {
		return err
	}

	if name := effectiveName(tunnel); name != res.Name {
		edited, err := r.Cloudflare.EditTunnel(ctx, id, zero_trust.TunnelCloudflaredEditParams{
			// TODO: AccountId should probably come from the status, not the spec
			AccountID: cloudflare.F(tunnel.Spec.AccountId),
			Name:      cloudflare.F(name),
		})
		if err != nil {
			return err
		}
		if edited != nil {
			res = edited
		}
	}

	// Which side owns the configuration is fixed when the tunnel is created, so
	// what the API reports is authoritative over the spec. Cloudflare keeps no
	// configuration for a locally-managed tunnel; it comes from a file on the
	// origin machine instead.
	remoteConfig := res.ConfigSrc == shared.CloudflareTunnelConfigSrcCloudflare
	configConflict := tunnel.Spec.Config != nil && !remoteConfig

	if config := tunnel.Spec.Config; config != nil && remoteConfig {
		c := cfclient.CloudflareTunnelConfig(*config)
		_, err := r.Cloudflare.UpdateConfiguration(ctx, id, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
			// TODO: AccountId should probably come from the status, not the spec
			AccountID: cloudflare.F(tunnel.Spec.AccountId),
			Config:    cloudflare.F(c.UpdateParams()),
		})
		if err != nil {
			return err
		}
	}

	if err := patchSubResource(ctx, r.Status(), tunnel, func(obj *cfv1alpha1.CloudflareTunnel) {
		_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type:    typeProgressingCloudflareTunnel,
			Status:  metav1.ConditionTrue,
			Reason:  reasonReconciling,
			Message: "Tunnel status updated",
		})
		if configConflict {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeDegradedCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  reasonInvalidSpec,
				Message: "spec.config is set on a locally managed tunnel and cannot be pushed to Cloudflare",
			})
		} else {
			_ = meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
				Type:    typeDegradedCloudflareTunnel,
				Status:  metav1.ConditionFalse,
				Reason:  reasonReconciling,
				Message: "Tunnel status updated",
			})
		}
		obj.Status.Name = res.Name
		obj.Status.AccountTag = res.AccountTag
		obj.Status.CreatedAt = metav1.NewTime(res.CreatedAt)
		obj.Status.ConnectionsActiveAt = metav1.NewTime(res.ConnsActiveAt)
		obj.Status.ConnectionsInactiveAt = metav1.NewTime(res.ConnsInactiveAt)
		obj.Status.Id = &res.ID
		obj.Status.RemoteConfig = remoteConfig
		obj.Status.Status = cfv1alpha1.CloudflareTunnelHealth(res.Status)
		obj.Status.Type = cfv1alpha1.CloudflareTunnelType(res.TunType)
	}); err != nil {
		return err
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
				Type:    typeDegradedCloudflareTunnel,
				Status:  metav1.ConditionTrue,
				Reason:  reasonReconciling,
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
			Reason:  reasonReconciling,
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
