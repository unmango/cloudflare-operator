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
)

// CloudflaredDeploymentKind describes the kind of deployment to create.
// +kubebuilder:validation:Enum=DaemonSet;Deployment
type CloudflaredKind string

const (
	DaemonSetCloudflaredKind  CloudflaredKind = "DaemonSet"
	DeploymentCloudflaredKind CloudflaredKind = "Deployment"
)

// CloudflaredConfigReference defines a reference to either a ConfigMap or Secret with a
// key containing a config.yml file to mount in the cloudflared container.
type CloudflaredConfigReference struct {
	// ConfigMapKeyRef selects a key from an existing ConfigMap containing the cloudflared config.
	//
	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// SecretKeyRef selects a key from an existing Secret containing the cloudflared config.
	//
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

// CloudflaredTunnelReference defines the minimum required information to
// locate a CloudflareTunnel resource.
type CloudflaredTunnelReference struct {
	// The name of a CloudflareTunnel resource in the same namespace.
	//
	// +required
	Name string `json:"name"`
}

// CloudflaredConfig defines the configuration for the `cloudflared` instance.
// Only one of tunnelId, tunnelRef, or valueFrom may be defined.
//
// +kubebuilder:validation:MaxProperties:=1
// +kubebuilder:validation:MinProperties:=1
type CloudflaredConfig struct {
	// The id of an existing Cloudflare tunnel to run.
	//
	// +optional
	TunnelId *string `json:"tunnelId,omitempty"`

	// A reference to an existing object containing the id of the Cloudflare tunnel to run.
	//
	// +optional
	TunnelRef *CloudflaredTunnelReference `json:"tunnelRef,omitempty"`

	// ValueFrom defines an existing source in the cluster to pull the cloudflared config from.
	//
	// +optional
	ValueFrom *CloudflaredConfigReference `json:"valueFrom,omitempty"`
}

// CloudflaredSpec defines the desired state of Cloudflared.
type CloudflaredSpec struct {
	// Config describes how to configure the cloudflared instance.
	//
	// +optional
	Config *CloudflaredConfig `json:"config,omitempty"`

	// Kind describes the kind of kubernetes resource that will run the cloudflared instance.
	// Available options are Deployment or Daemonset, default is DaemonSet.
	//
	// +kubebuilder:default:=DaemonSet
	Kind CloudflaredKind `json:"kind,omitempty"`

	// When Kind is "Deployment", specifies the desired number of replicas.
	// Ignored when Kind is "DaemonSet".
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Template allows customizing the PodTemplateSpec used by the resource specified in Kind.
	// To customize the cloudflared container, add a container named `cloudflared` to containers.
	// Currently, the only supported `cloudflared` customization is `image`.
	//
	// +optional
	Template *corev1.PodTemplateSpec `json:"template,omitempty"`

	// Version defines the version of cloudflared to deploy. Available versions can be found at
	// https://github.com/cloudflare/cloudflared/releases.
	//
	// +kubebuilder:default:=latest
	Version string `json:"version,omitempty"`
}

// CloudflaredStatus defines the observed state of Cloudflared.
type CloudflaredStatus struct {
	// The Kind of the app managing the cloudflared instance.
	//
	// +optional
	Kind CloudflaredKind `json:"kind,omitempty"`

	// The id of the tunnel currently being run by this cloudflared instance.
	//
	// +optional
	TunnelId *string `json:"tunnelId,omitEmpty"`

	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=".status.kind",name=Kind,type=string
// +kubebuilder:printcolumn:JSONPath=".status.tunnelId",name=Tunnel ID,type=string

// Cloudflared is the Schema for the cloudflareds API.
type Cloudflared struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudflaredSpec   `json:"spec,omitempty"`
	Status CloudflaredStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudflaredList contains a list of Cloudflared.
type CloudflaredList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cloudflared `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cloudflared{}, &CloudflaredList{})
}
