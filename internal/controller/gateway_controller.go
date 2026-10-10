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
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/gateway"
	"github.com/unmango/cloudflare-operator/internal/xds"
)

// retryXDSAddress is how long to wait before looking for the xDS Service again.
// Nothing watched changes when it appears, so the retry is on a timer.
const retryXDSAddress = 30 * time.Second

// errNotControlled reports an object that already exists under the name the
// operator would give it, and that some other owner, or none, controls.
var errNotControlled = errors.New("exists and is not controlled by this Gateway")

// XDSAddressResolver works out where Envoy reaches the xDS server.
type XDSAddressResolver interface {
	Resolve(ctx context.Context) (xds.Address, error)
}

// GatewayReconciler provisions the Envoy proxy and the Cloudflare tunnel for each
// Gateway of a class this controller owns, and reports their state on the
// Gateway's status. GatewayXDSReconciler programs the proxy.
type GatewayReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	XDS      XDSAddressResolver
	Features gateway.Features
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaretunnels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=cloudflaregatewayconfigs,verbs=get;list;watch

func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	gw := &gatewayv1.Gateway{}
	if err := r.Get(ctx, req.NamespacedName, gw); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	class, ours, err := gatewayClassFor(ctx, r.Client, gw)
	if err != nil || !ours {
		return ctrl.Result{}, err
	}

	// Everything provisioned for the Gateway carries an owner reference to it,
	// so deletion cascades without a finalizer.
	if !gw.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	log.Info(msgStartingReconciliation)

	model, _, err := buildGateway(ctx, r.Client, gw, r.Features)
	if err != nil {
		return ctrl.Result{}, err
	}
	status := newGatewayStatus(model)

	config, accepted := r.accept(ctx, gw, class, status)
	if !accepted {
		return ctrl.Result{}, r.patchStatus(ctx, gw, status)
	}

	settings := resolveEnvoySettings(config)
	result := ctrl.Result{}

	addr, err := r.XDS.Resolve(ctx)
	if err != nil {
		log.Info("Waiting for the xDS server address", "reason", err.Error())
		status.programmed(metav1.ConditionFalse, gatewayv1.GatewayReasonPending,
			fmt.Sprintf("Waiting for the xDS server address: %s", err))
		result.RequeueAfter = retryXDSAddress
	} else if err := r.provisionEnvoy(ctx, gw, settings, addr, status); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.provisionTunnel(ctx, gw, config, status); err != nil {
		return ctrl.Result{}, err
	}

	return result, r.patchStatus(ctx, gw, status)
}

// accept decides the Gateway's Accepted condition. It reports false when the
// Gateway cannot be served at all, and leaves Programmed false with it.
func (r *GatewayReconciler) accept(ctx context.Context, gw *gatewayv1.Gateway, class *gatewayv1.GatewayClass, status *gatewayStatus) (*cfv1alpha1.CloudflareGatewayConfig, bool) {
	reject := func(reason gatewayv1.GatewayConditionReason, message string) (*cfv1alpha1.CloudflareGatewayConfig, bool) {
		status.accepted(metav1.ConditionFalse, reason, message)
		status.programmed(metav1.ConditionFalse, gatewayv1.GatewayReasonInvalid, message)
		return nil, false
	}

	if !meta.IsStatusConditionTrue(class.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted)) {
		return reject(gatewayv1.GatewayReasonPending, fmt.Sprintf("GatewayClass %s is not accepted", class.Name))
	}

	config, err := gateway.Resolve(ctx, r.Client, class)
	if err != nil {
		return reject(gatewayv1.GatewayReasonInvalidParameters, err.Error())
	}

	if len(gw.Spec.Addresses) > 0 {
		return reject(gatewayv1.GatewayReasonUnsupportedAddress, "spec.addresses is not supported")
	}

	if status.validListeners() == 0 {
		return reject(gatewayv1.GatewayReasonListenersNotValid, "No listener is valid")
	}

	if n := status.validListeners(); n < len(gw.Spec.Listeners) {
		status.accepted(metav1.ConditionTrue, gatewayv1.GatewayReasonListenersNotValid,
			fmt.Sprintf("%d of %d listeners are invalid", len(gw.Spec.Listeners)-n, len(gw.Spec.Listeners)))
		return config, true
	}

	status.accepted(metav1.ConditionTrue, gatewayv1.GatewayReasonAccepted, "Gateway accepted")
	return config, true
}

// provisionEnvoy creates or updates the proxy and the Service in front of it,
// and records whether they are ready to serve.
func (r *GatewayReconciler) provisionEnvoy(ctx context.Context, gw *gatewayv1.Gateway, settings envoySettings, addr xds.Address, status *gatewayStatus) error {
	bootstrap, err := xds.Bootstrap(gateway.NodeID(gw.Namespace, gw.Name), addr.Host, addr.Port)
	if err != nil {
		return err
	}

	name := gateway.EnvoyObjectName(gw)

	deploy := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gw.Namespace}}
	if err := r.ensureControlled(ctx, gw, deploy, func() {
		mutateEnvoyDeployment(deploy, gw, settings, bootstrap)
	}); errors.Is(err, errNotControlled) {
		status.programmed(metav1.ConditionFalse, gatewayv1.GatewayReasonInvalid,
			fmt.Sprintf("Deployment %s %s", name, err))
		return nil
	} else if err != nil {
		return err
	}

	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gw.Namespace}}
	if err := r.ensureControlled(ctx, gw, svc, func() {
		mutateEnvoyService(svc, gw, settings, envoyServicePorts(status.listeners))
	}); errors.Is(err, errNotControlled) {
		status.programmed(metav1.ConditionFalse, gatewayv1.GatewayReasonInvalid,
			fmt.Sprintf("Service %s %s", name, err))
		return nil
	} else if err != nil {
		return err
	}

	status.addresses = serviceAddresses(svc)

	switch {
	case settings.Replicas == 0:
		status.programmed(metav1.ConditionFalse, gatewayv1.GatewayReasonNoResources,
			"spec.envoy.replicas of the CloudflareGatewayConfig is 0")
	case deploy.Status.AvailableReplicas == 0:
		status.programmed(metav1.ConditionFalse, gatewayv1.GatewayReasonPending,
			fmt.Sprintf("Waiting for Deployment %s to become available", name))
	case len(status.addresses) == 0:
		status.programmed(metav1.ConditionFalse, gatewayv1.GatewayReasonAddressNotAssigned,
			fmt.Sprintf("Waiting for Service %s to be assigned an address", name))
	default:
		status.programmed(metav1.ConditionTrue, gatewayv1.GatewayReasonProgrammed, "Envoy is serving the Gateway")
	}

	return nil
}

// provisionTunnel creates or updates the tunnel a template asks for, and records
// the tunnel condition for whichever way the class reaches Cloudflare.
func (r *GatewayReconciler) provisionTunnel(ctx context.Context, gw *gatewayv1.Gateway, config *cfv1alpha1.CloudflareGatewayConfig, status *gatewayStatus) error {
	envoyHost := gateway.EnvoyObjectName(gw) + "." + gw.Namespace + ".svc"

	switch {
	case config.Spec.Template != nil:
	case config.Spec.TunnelRef != nil:
		ref := config.Spec.TunnelRef
		status.tunnel = &metav1.Condition{
			Type:   conditionTunnelProgrammed,
			Status: metav1.ConditionFalse,
			Reason: reasonTunnelNotImplemented,
			Message: fmt.Sprintf("The operator does not yet write rules into the shared tunnel %s/%s; "+
				"route its traffic to http://%s by hand", ref.Namespace, ref.Name, envoyHost),
		}
		return nil
	default:
		// Served inside the cluster only.
		return nil
	}

	template := config.Spec.Template
	tunnel := &cfv1alpha1.CloudflareTunnel{ObjectMeta: metav1.ObjectMeta{Name: gw.Name, Namespace: gw.Namespace}}
	err := r.ensureControlled(ctx, gw, tunnel, func() {
		tunnel.Labels = mergeOwned(tunnel.Labels, template.ObjectMeta.Labels)
		tunnel.Annotations = mergeOwned(tunnel.Annotations, template.ObjectMeta.Annotations)

		spec := template.Spec.DeepCopy()
		// The operator writes the rules that reach Envoy, which only works when
		// Cloudflare holds the configuration.
		spec.ConfigSource = cfv1alpha1.CloudflareCloudflareTunnelConfigSource
		var handwritten []cfv1alpha1.CloudflareTunnelConfigIngress
		if spec.Config == nil {
			spec.Config = &cfv1alpha1.CloudflareTunnelConfig{}
		} else {
			handwritten = spec.Config.Ingress
		}
		spec.Config.Ingress = gateway.TunnelIngress(handwritten, status.listeners, envoyHost)
		tunnel.Spec = *spec
	})
	if errors.Is(err, errNotControlled) {
		status.tunnel = &metav1.Condition{
			Type:    conditionTunnelProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  reasonTunnelConflict,
			Message: fmt.Sprintf("CloudflareTunnel %s %s", tunnel.Name, err),
		}
		return nil
	}
	if err != nil {
		return err
	}

	status.tunnel = tunnelCondition(tunnel)
	return nil
}

// tunnelCondition reads a provisioned tunnel's own status.
func tunnelCondition(tunnel *cfv1alpha1.CloudflareTunnel) *metav1.Condition {
	if degraded := meta.FindStatusCondition(tunnel.Status.Conditions, typeDegradedCloudflareTunnel); degraded != nil && degraded.Status == metav1.ConditionTrue {
		return &metav1.Condition{
			Type:    conditionTunnelProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  reasonTunnelPending,
			Message: fmt.Sprintf("CloudflareTunnel %s is degraded: %s", tunnel.Name, degraded.Message),
		}
	}
	if tunnel.Status.Id == nil {
		return &metav1.Condition{
			Type:    conditionTunnelProgrammed,
			Status:  metav1.ConditionFalse,
			Reason:  reasonTunnelPending,
			Message: fmt.Sprintf("Waiting for CloudflareTunnel %s to be created", tunnel.Name),
		}
	}

	return &metav1.Condition{
		Type:    conditionTunnelProgrammed,
		Status:  metav1.ConditionTrue,
		Reason:  reasonTunnelProgrammed,
		Message: fmt.Sprintf("CloudflareTunnel %s routes to Envoy", tunnel.Name),
	}
}

// ensureControlled creates obj, or updates it when the Gateway controls it.
// An existing object the Gateway does not control is left untouched.
func (r *GatewayReconciler) ensureControlled(ctx context.Context, gw *gatewayv1.Gateway, obj client.Object, mutate func()) error {
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if obj.GetResourceVersion() != "" && !metav1.IsControlledBy(obj, gw) {
			return errNotControlled
		}

		mutate()
		return controllerutil.SetControllerReference(gw, obj, r.Scheme)
	})

	return err
}

func (r *GatewayReconciler) patchStatus(ctx context.Context, gw *gatewayv1.Gateway, status *gatewayStatus) error {
	return patchSubResource(ctx, r.Status(), gw, status.apply)
}

// gatewayClassFor fetches the class gw names and reports whether this
// controller owns it. A class that does not exist belongs to nobody yet.
func gatewayClassFor(ctx context.Context, reader client.Reader, gw *gatewayv1.Gateway) (*gatewayv1.GatewayClass, bool, error) {
	class := &gatewayv1.GatewayClass{}
	if err := reader.Get(ctx, client.ObjectKey{Name: string(gw.Spec.GatewayClassName)}, class); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	return class, string(class.Spec.ControllerName) == gateway.ControllerName, nil
}

// serviceAddresses are the addresses a Service is reachable at: the load
// balancer's when it has one, and the cluster IP otherwise.
func serviceAddresses(svc *corev1.Service) []gatewayv1.GatewayStatusAddress {
	var addresses []gatewayv1.GatewayStatusAddress

	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			switch {
			case ingress.IP != "":
				addresses = append(addresses, gatewayv1.GatewayStatusAddress{
					Type:  new(gatewayv1.IPAddressType),
					Value: ingress.IP,
				})
			case ingress.Hostname != "":
				addresses = append(addresses, gatewayv1.GatewayStatusAddress{
					Type:  new(gatewayv1.HostnameAddressType),
					Value: ingress.Hostname,
				})
			}
		}

		return addresses
	}

	for _, ip := range svc.Spec.ClusterIPs {
		if ip == "" || ip == corev1.ClusterIPNone {
			continue
		}
		addresses = append(addresses, gatewayv1.GatewayStatusAddress{
			Type:  new(gatewayv1.IPAddressType),
			Value: ip,
		})
	}

	return addresses
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&cfv1alpha1.CloudflareTunnel{}).
		// Acceptance depends on the class and its parameters, which change
		// without any edit to the Gateway.
		Watches(&gatewayv1.GatewayClass{}, gatewaysForClassHandler(mgr.GetClient())).
		Watches(&cfv1alpha1.CloudflareGatewayConfig{}, gatewaysForConfigHandler(mgr.GetClient()))

	return watchGatewayInputs(b, mgr.GetClient(), r.Features, false).
		Named("gateway").
		Complete(r)
}
