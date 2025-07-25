package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CloudflaredTemplateSpec describes the data a cloudflared should have when created from a template
type CloudflaredTemplateSpec struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the cloudflared.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	//
	// +optional
	Spec CloudflaredSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// These types are currently unused. I have this janky idea to spawn a job in the CRD namespace
// to perform cloudflare API operations. It would allow specifying the API token on the CRD
// rather than the controller and (potentially) improve isolation of secrets since the controller
// would never need to read the secret, only the pod spawned by the job.
//
// This is absolutely overkill and we can get away with passing the API token to the controller
// via environment variables for now.

// CloudflareApiTokenReference defines a reference to either a ConfigMap or Secret with a
// key containing the API token to use.
type CloudflareApiTokenReference struct {
	// ConfigMapKeyRef selects a key from an existing ConfigMap containing the API token.
	//
	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// SecretKeyRef selects a key from an existing Secret containing the API token.
	//
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

// CloudflareApiToken defines the API token used for authenticating requests to the Cloudflare API.
//
// +kubebuilder:validation:MaxProperties:=1
// +kubebuilder:validation:MinProperties:=1
type CloudflareApiToken struct {
	// An inline API token value. Consider storing the secret in a Kubernetes Secret and
	// using valueFrom.secretKeyRef instead.
	//
	// +optional
	Value *string `json:"value,omitempty"`

	// ValueFrom defines an existing source in the cluster to pull the API token from.
	//
	// +optional
	ValueFrom *CloudflareApiTokenReference `json:"valueFrom,omitempty"`
}
