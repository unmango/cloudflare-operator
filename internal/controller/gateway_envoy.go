package controller

import (
	"fmt"
	"maps"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/gateway"
)

const envoyContainerName = "envoy"

// defaultEnvoyResources sizes a proxy that has not been sized by hand. The
// memory limit leaves room for the configuration to grow with routes.
var defaultEnvoyResources = corev1.ResourceRequirements{
	Requests: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("50m"),
		corev1.ResourceMemory: resource.MustParse("64Mi"),
	},
	Limits: corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("512Mi"),
	},
}

// envoySettings are the CloudflareGatewayConfig's envoy fields with defaults
// applied, for a config written before they existed or a defaulter that did not
// run.
type envoySettings struct {
	Image       string
	Replicas    int32
	ServiceType corev1.ServiceType
	Resources   corev1.ResourceRequirements
}

func resolveEnvoySettings(config *cfv1alpha1.CloudflareGatewayConfig) envoySettings {
	settings := envoySettings{
		Image:       gateway.DefaultEnvoyImage,
		Replicas:    1,
		ServiceType: corev1.ServiceTypeClusterIP,
		Resources:   *defaultEnvoyResources.DeepCopy(),
	}

	envoy := config.Spec.Envoy
	if envoy == nil {
		return settings
	}
	if envoy.Image != "" {
		settings.Image = envoy.Image
	}
	if envoy.Replicas != nil {
		settings.Replicas = *envoy.Replicas
	}
	if envoy.ServiceType != "" {
		settings.ServiceType = envoy.ServiceType
	}
	if envoy.Resources != nil {
		settings.Resources = *envoy.Resources.DeepCopy()
	}

	return settings
}

// infrastructureMeta returns the labels and annotations spec.infrastructure asks
// for on generated objects, under the operator's own labels.
func infrastructureMeta(gw *gatewayv1.Gateway) (map[string]string, map[string]string) {
	labels := map[string]string{}
	annotations := map[string]string{}
	if infra := gw.Spec.Infrastructure; infra != nil {
		for k, v := range infra.Labels {
			labels[string(k)] = string(v)
		}
		for k, v := range infra.Annotations {
			annotations[string(k)] = string(v)
		}
	}
	maps.Copy(labels, gateway.Labels(gw))

	return labels, annotations
}

// mutateEnvoyDeployment writes the desired state of the proxy serving gw into
// deploy, leaving fields the API server or other controllers own alone.
func mutateEnvoyDeployment(deploy *appsv1.Deployment, gw *gatewayv1.Gateway, settings envoySettings, bootstrap string) {
	labels, annotations := infrastructureMeta(gw)

	deploy.Labels = mergeOwned(deploy.Labels, labels)
	deploy.Annotations = mergeOwned(deploy.Annotations, annotations)

	deploy.Spec.Replicas = new(settings.Replicas)
	// The selector is immutable, so it is set only on create. It uses the UID,
	// which never changes for the life of the Gateway.
	if deploy.Spec.Selector == nil {
		deploy.Spec.Selector = &metav1.LabelSelector{MatchLabels: gateway.SelectorLabels(gw)}
	}

	// Merged rather than replaced, so an annotation such as the one kubectl
	// rollout restart adds does not get reverted.
	deploy.Spec.Template.Labels = mergeOwned(deploy.Spec.Template.Labels, labels)
	deploy.Spec.Template.Annotations = mergeOwned(deploy.Spec.Template.Annotations, annotations)
	deploy.Spec.Template.Spec = corev1.PodSpec{
		// Envoy talks to the xDS server, never to the Kubernetes API.
		AutomountServiceAccountToken: new(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: new(true),
			// The distroless image runs as this user, and the kubelet cannot
			// check runAsNonRoot against a user it is not told the id of.
			RunAsUser: new(int64(65532)),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		Containers: []corev1.Container{{
			Name:  envoyContainerName,
			Image: settings.Image,
			Args: []string{
				"--config-yaml", bootstrap,
				// Hot restart needs shared memory the read-only root denies, and
				// Kubernetes replaces pods rather than restarting them in place.
				"--disable-hot-restart",
				"--log-level", "info",
			},
			Ports: []corev1.ContainerPort{{
				Name:          "readiness",
				ContainerPort: gateway.EnvoyReadinessPort,
				Protocol:      corev1.ProtocolTCP,
			}},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/ready",
						Port: intstr.FromInt32(gateway.EnvoyReadinessPort),
					},
				},
				PeriodSeconds:    5,
				FailureThreshold: 3,
			},
			Resources: settings.Resources,
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: new(false),
				ReadOnlyRootFilesystem:   new(true),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			},
		}},
	}
}

// envoyServicePorts lists one port per distinct port and transport among the
// valid listeners. A port's name carries its protocol, which is how tools that
// read appProtocol conventions from names tell HTTP from the rest.
func envoyServicePorts(listeners []gateway.Listener) []corev1.ServicePort {
	var ports []corev1.ServicePort
	seen := map[servicePortKey]bool{}
	for _, l := range listeners {
		protocol := corev1.ProtocolTCP
		if l.Protocol == gatewayv1.UDPProtocolType {
			protocol = corev1.ProtocolUDP
		}
		key := servicePortKey{port: l.Port, protocol: protocol}
		if !l.Valid || seen[key] {
			continue
		}
		seen[key] = true

		ports = append(ports, corev1.ServicePort{
			Name:       fmt.Sprintf("%s-%d", strings.ToLower(string(l.Protocol)), l.Port),
			Protocol:   protocol,
			Port:       l.Port,
			TargetPort: intstr.FromInt32(gateway.ContainerPort(l.Port)),
		})
	}

	return ports
}

// servicePortKey identifies a Service port: TCP and UDP may share a number.
type servicePortKey struct {
	port     int32
	protocol corev1.Protocol
}

// mutateEnvoyService writes the desired state of the Service in front of the
// proxy into svc.
func mutateEnvoyService(svc *corev1.Service, gw *gatewayv1.Gateway, settings envoySettings, ports []corev1.ServicePort) {
	labels, annotations := infrastructureMeta(gw)

	svc.Labels = mergeOwned(svc.Labels, labels)
	svc.Annotations = mergeOwned(svc.Annotations, annotations)
	svc.Spec.Selector = gateway.SelectorLabels(gw)
	svc.Spec.Type = settings.ServiceType

	// A node port is allocated by the API server. Writing ports without the one
	// it already allocated would release it and allocate another on every
	// update, so existing allocations are carried over.
	allocated := map[servicePortKey]int32{}
	for _, p := range svc.Spec.Ports {
		allocated[servicePortKey{port: p.Port, protocol: p.Protocol}] = p.NodePort
	}
	desired := make([]corev1.ServicePort, len(ports))
	for i, p := range ports {
		if settings.ServiceType != corev1.ServiceTypeClusterIP {
			p.NodePort = allocated[servicePortKey{port: p.Port, protocol: p.Protocol}]
		}
		desired[i] = p
	}
	svc.Spec.Ports = desired
}

// mergeOwned overlays owned onto existing, so labels and annotations other
// controllers add survive.
func mergeOwned(existing, owned map[string]string) map[string]string {
	if existing == nil {
		existing = map[string]string{}
	}
	maps.Copy(existing, owned)

	return existing
}
