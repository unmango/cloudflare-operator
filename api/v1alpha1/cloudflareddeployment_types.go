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
type CloudflaredDeploymentKind string

const (
	DaemonSet  CloudflaredDeploymentKind = "DaemonSet"
	Deployment CloudflaredDeploymentKind = "Deployment"
)

// CloudflaredDeploymentSpec defines the desired state of CloudflaredDeployment.
type CloudflaredDeploymentSpec struct {
	//+kubebuilder:default:=DaemonSet
	Kind CloudflaredDeploymentKind `json:"kind,omitempty"`

	// +optional
	Template *corev1.PodTemplateSpec `json:"template,omitempty"`
}

// CloudflaredDeploymentStatus defines the observed state of CloudflaredDeployment.
type CloudflaredDeploymentStatus struct {
	// +optional
	State string `json:"state"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CloudflaredDeployment is the Schema for the cloudflareddeployments API.
type CloudflaredDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec   CloudflaredDeploymentSpec   `json:"spec,omitzero"`
	Status CloudflaredDeploymentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CloudflaredDeploymentList contains a list of CloudflaredDeployment.
type CloudflaredDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CloudflaredDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudflaredDeployment{}, &CloudflaredDeploymentList{})
}
