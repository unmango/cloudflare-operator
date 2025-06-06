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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/ingress"
	"github.com/unmango/cloudflare-operator/internal/ingress/annotation"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultIngressClassName = ingress.DefaultClassName
	ingressControllerName   = ingress.ControllerName
)

// IngressReconciler reconciles a Ingress object
type IngressReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/finalizers,verbs=update
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaretunnels,verbs=get;list;create;update;patch;delete

func (r *IngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ingress := &networkingv1.Ingress{}
	if err := r.Get(ctx, req.NamespacedName, ingress); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	class := r.ingressClassName(ingress)
	if class != nil && *class != defaultIngressClassName {
		log.V(1).Info("Ignoring ingress with a different class", "class", class)
		return ctrl.Result{}, nil
	}

	tunnel := &cfv1alpha1.CloudflareTunnel{}
	if err := r.Get(ctx, req.NamespacedName, tunnel); err != nil {
		if errors.IsNotFound(err) {
			return r.createTunnel(ctx, ingress)
		} else {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *IngressReconciler) createTunnel(ctx context.Context, ingress *networkingv1.Ingress) (ctrl.Result, error) {
	tunnel := &cfv1alpha1.CloudflareTunnel{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingress.Name,
			Namespace: ingress.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(ingress, tunnel, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	annotations := annotation.Parse(ingress)
	if accountId, err := annotations.AccountId(); err == nil {
		tunnel.Spec.AccountId = accountId
	} else {
		logf.FromContext(ctx).Info("Missing account id")
		return ctrl.Result{}, nil
	}
	_ = annotations.Cloudflared.UnmarshalYAML(tunnel.Spec.Cloudflared)
	if cs, err := annotations.ConfigSource(); err == nil {
		tunnel.Spec.ConfigSource = cfv1alpha1.CloudflareTunnelConfigSource(cs)
	}
	if name, err := annotations.Name(); err == nil {
		tunnel.Spec.Name = name
	}
	if err := annotations.TunnelSecret.UnmarshalYAML(tunnel.Spec.TunnelSecret); err != nil {
		if secret, err := annotations.TunnelSecret(); err == nil {
			tunnel.Spec.TunnelSecret = &cfv1alpha1.CloudflareTunnelSecret{
				Value: &secret,
			}
		}
	}
	if err := r.Create(ctx, tunnel); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *IngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Named("ingress").
		Complete(r)
}

func (r *IngressReconciler) ingressClassName(ingress *networkingv1.Ingress) *string {
	if class := ingress.Spec.IngressClassName; class != nil {
		return class
	}
	if class, ok := annotation.Kubernetes.Lookup(ingress, "ingress.class"); ok {
		return &class
	}

	return nil
}

// func (r *IngressReconciler) getIngressClass(ctx context.Context, ingress *networkingv1.Ingress) (*networkingv1.IngressClass, error) {
// 	if ingress.Spec.IngressClassName == nil {
// 		return nil, fmt.Errorf("ingressClassName not declared in ingress spec")
// 	}

// 	class := &networkingv1.IngressClass{}
// 	key := client.ObjectKey{Name: *ingress.Spec.IngressClassName}
// 	if err := r.Get(ctx, key, class); err != nil {
// 		return nil, err
// 	} else {
// 		return class, nil
// 	}
// }
