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
	DaemonSet  CloudflaredKind = "DaemonSet"
	Deployment CloudflaredKind = "Deployment"
)

// CloudflaredConfigReference defines a reference to either a ConfigMap or Secret with a
// key containing a config.yml file to mount in the cloudflared container.
type CloudflaredConfigReference struct {
	// ConfigMapKeyRef selects a key from an existing ConfigMap containing the cloudflared config.
	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// SecretKeyRef selects a key from an existing Secret containing the cloudflared config.
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

// CloudflaredConfig defines the configuration for the `cloudflared` instance.
type CloudflaredConfig struct {
	// ValueFrom defines an existing source in the cluster to pull the cloudflared config from.
	// +optional
	ValueFrom *CloudflaredConfigReference `json:"valueFrom,omitempty"`
}

// CloudflaredSpec defines the desired state of Cloudflared.
type CloudflaredSpec struct {
	// Config describes how to configure the cloudflared instance.
	// +optional
	Config *CloudflaredConfig `json:"config,omitempty"`

	// Kind describes the kind of kubernetes resource that will run the cloudflared instance.
	// Available options are Deployment or Daemonset, default is DaemonSet.
	// +kubebuilder:default:=DaemonSet
	Kind CloudflaredKind `json:"kind,omitempty"`

	// Template allows customizing the PodTemplateSpec used by the resource specified in Kind.
	// To customize the cloudflared container, add a container named `cloudflared` to containers.
	// Currently, the only supported `cloudflared` customization is `image`.
	// +optional
	Template *corev1.PodTemplateSpec `json:"template,omitempty"`
}

// CloudflaredStatus defines the observed state of Cloudflared.
type CloudflaredStatus struct {
	// +optional
	State string `json:"state"`

	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=".status.state",name=State,type=string

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
