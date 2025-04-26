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

// CloudflaredSpec defines the desired state of Cloudflared.
type CloudflaredSpec struct {
	//+kubebuilder:default:=DaemonSet
	Kind CloudflaredKind `json:"kind,omitempty"`

	// +optional
	Template *corev1.PodTemplateSpec `json:"template,omitempty"`
}

// CloudflaredStatus defines the observed state of Cloudflared.
type CloudflaredStatus struct {
	// +optional
	State string `json:"state"`
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
