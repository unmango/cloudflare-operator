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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// CloudflareGatewayConfigSpec defines the desired state of CloudflareGatewayConfig.
//
// At most one of tunnelRef and template is set. tunnelRef attaches every Gateway
// of the class to one existing tunnel; template gives each Gateway its own.
// With neither, a Gateway is served by its Envoy proxy inside the cluster only,
// which suits a private Gateway and the Gateway API conformance suite.
//
// +kubebuilder:validation:XValidation:rule="!(has(self.tunnelRef) && has(self.template))",message="at most one of spec.tunnelRef and spec.template may be set"
type CloudflareGatewayConfigSpec struct {
	// TunnelRef locates the CloudflareTunnel that Gateways of this class attach to.
	//
	// +optional
	TunnelRef *CloudflareGatewayTunnelReference `json:"tunnelRef,omitempty"`

	// Template describes the CloudflareTunnel provisioned for each Gateway of this
	// class, in the Gateway's namespace and named after it.
	//
	// +optional
	Template *CloudflareGatewayTunnelTemplate `json:"template,omitempty"`

	// Envoy configures the proxy that serves each Gateway of this class.
	//
	// +optional
	Envoy *CloudflareGatewayEnvoy `json:"envoy,omitempty"`
}

// CloudflareGatewayEnvoy configures the Envoy proxy provisioned for each Gateway.
//
// Envoy does the routing: cloudflared forwards every request it receives for the
// Gateway to it, and the operator programs it over xDS. The proxy runs in the
// Gateway's namespace, named after the Gateway.
type CloudflareGatewayEnvoy struct {
	// Image is the Envoy container image. It must match the xDS API the operator
	// speaks, so override it only to mirror the default into another registry.
	//
	// +optional
	Image string `json:"image,omitempty"`

	// Replicas is the number of Envoy pods.
	//
	// +kubebuilder:validation:Minimum:=0
	// +kubebuilder:default:=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// ServiceType is the type of the Service in front of the proxy. cloudflared
	// reaches it from inside the cluster, so ClusterIP is enough behind a tunnel.
	// LoadBalancer gives the Gateway an address outside the cluster as well.
	//
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +kubebuilder:default:=ClusterIP
	// +optional
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// Resources of the Envoy container. Defaults to a 50m CPU and 64Mi memory
	// request with a 512Mi memory limit.
	//
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// CloudflareGatewayTunnelReference locates an existing CloudflareTunnel.
type CloudflareGatewayTunnelReference struct {
	// The name of a CloudflareTunnel resource.
	//
	// +required
	Name string `json:"name"`

	// The namespace of the CloudflareTunnel resource. A GatewayClass is cluster
	// scoped, so it carries no namespace of its own to default to.
	//
	// +required
	Namespace string `json:"namespace"`
}

// CloudflareGatewayTunnelTemplate describes a CloudflareTunnel to provision.
type CloudflareGatewayTunnelTemplate struct {
	// Metadata applied to the provisioned tunnel. Its name and namespace come from
	// the Gateway and cannot be set here.
	//
	// +optional
	ObjectMeta CloudflareGatewayTunnelTemplateMeta `json:"metadata,omitempty"`

	// Spec of the provisioned tunnel. Its configuration is always pushed to
	// Cloudflare, whatever configSource says, because the operator writes the
	// rules that send the Gateway's traffic to Envoy. Rules in spec.config are
	// kept ahead of them, and a catch-all among them is replaced.
	//
	// +required
	Spec CloudflareTunnelSpec `json:"spec"`
}

// CloudflareGatewayTunnelTemplateMeta is the subset of object metadata a template
// may set.
type CloudflareGatewayTunnelTemplateMeta struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// CloudflareGatewayConfigStatus defines the observed state of CloudflareGatewayConfig.
type CloudflareGatewayConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the CloudflareGatewayConfig resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CloudflareGatewayConfig is the Schema for the cloudflaregatewayconfigs API
type CloudflareGatewayConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CloudflareGatewayConfig
	// +required
	Spec CloudflareGatewayConfigSpec `json:"spec"`

	// status defines the observed state of CloudflareGatewayConfig
	// +optional
	Status CloudflareGatewayConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CloudflareGatewayConfigList contains a list of CloudflareGatewayConfig
type CloudflareGatewayConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CloudflareGatewayConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &CloudflareGatewayConfig{}, &CloudflareGatewayConfigList{})
		return nil
	})
}
