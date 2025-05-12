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

// CloudflareTunnelConfigSource describes whether the tunnel is locally or remotely managed.
//
// +kubebuilder:validation:Enum=local;cloudflare
type CloudflareTunnelConfigSource string

const (
	LocalCloudflareTunnelConfigSource      CloudflareTunnelConfigSource = "local"
	CloudflareCloudflareTunnelConfigSource CloudflareTunnelConfigSource = "cloudflare"
)

// The status of the tunnel. Valid values are `inactive` (tunnel has never been run),
// `degraded` (tunnel is active and able to serve traffic but in an unhealthy state),
// `healthy` (tunnel is active and able to serve traffic), or
// `down` (tunnel can not serve traffic as it has no connections to the Cloudflare Edge).
//
// +kubebuilder:validation:Enum=healthy;degraded;inactive;down
type CloudflareTunnelHealth string

const (
	// Tunnel is active and able to serve traffic.
	HealthyCloudflareTunnelHealth CloudflareTunnelHealth = "healthy"

	// Tunnel is active and able to serve traffic but in an unhealthy state.
	DegradedCloudflareTunnelHealth CloudflareTunnelHealth = "degraded"

	// Tunnel has never been run.
	InactiveCloudflareTunnelHealth CloudflareTunnelHealth = "inactive"

	// Tunnel can not serve traffic as it has no connections to the Cloudflare Edge.
	DownCloudflareTunnelHealth CloudflareTunnelHealth = "down"
)

// The type of tunnel.
//
// +kubebuilder:validation:Enum=cfd_tunnel;warp_connector;warp;magic;ip_sec;gre;cni
type CloudflareTunnelType string

const (
	CfdTunnelCloudflareTunnelType     CloudflareTunnelType = "cfd_tunnel"
	WarpConnectorCloudflareTunnelType CloudflareTunnelType = "warp_connector"
	WarpCloudflareTunnelType          CloudflareTunnelType = "warp"
	MagicCloudflareTunnelType         CloudflareTunnelType = "magic"
	IpSecCloudflareTunnelType         CloudflareTunnelType = "ip_sec"
	GreCloudflareTunnelType           CloudflareTunnelType = "gre"
	CniCloudflareTunnelType           CloudflareTunnelType = "cni"
)

// CloudflaredConfigReference defines a reference to either a ConfigMap or Secret with a
// key containing the tunnel secret to use.
type CloudflareTunnelSecretReference struct {
	// ConfigMapKeyRef selects a key from an existing ConfigMap containing the tunnel secret.
	//
	// +optional
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// SecretKeyRef selects a key from an existing Secret containing the tunnel secret.
	//
	// +optional
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
}

// CloudflareTunnelSecret defines the tunnel secret used to run a locally-managed tunnel.
//
// +kubebuilder:validation:MaxProperties:=1
// +kubebuilder:validation:MinProperties:=1
type CloudflareTunnelSecret struct {
	// An inline secret value. Consider storing the secret in a Kubernetes Secret and
	// using valueFrom.secretKeyRef instead.
	//
	// +optional
	Value *string `json:"value,omitempty"`

	// ValueFrom defines an existing source in the cluster to pull the tunnel secret from.
	//
	// +optional
	ValueFrom *CloudflareTunnelSecretReference `json:"valueFrom,omitempty"`
}

type CloudflareTunnelCloudflared struct {
	// Selector defines which Cloudflared resource(s) is/are responsible for running the tunnel.
	//
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// CloudflareTunnelSpec defines the desired state of CloudflareTunnel.
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/methods/create/
type CloudflareTunnelSpec struct {
	// Cloudflare account ID.
	//
	// +kubebuilder:validation:MaxLength:=32
	// +required
	AccountId string `json:"accountId"`

	// The preferred authorization scheme for interacting with the Cloudflare API. [Create a token].
	//
	// [Create a token]: https://developers.cloudflare.com/fundamentals/api/get-started/create-token/
	//
	// +required
	// ApiToken CloudflareApiToken `json:"apiToken"`

	// Cloudflared defines how the tunnel will interact with the cloudflared instance running it.
	// If undefined the user is responsible for configuring the cloudflared instance and managing
	// its lifecycle.
	//
	// +optional
	Cloudflared *CloudflareTunnelCloudflared `json:"cloudflared,omitempty"`

	// Indicates if this is a locally or remotely configured tunnel. If `local`, manage
	// the tunnel using a YAML file on the origin machine. If `cloudflare`, manage the
	// tunnel on the Zero Trust dashboard.
	//
	// +kubebuilder:default:=local
	ConfigSource CloudflareTunnelConfigSource `json:"configSource"`

	// A user-friendly name for a tunnel.
	//
	// +optional
	Name string `json:"name,omitempty"`

	// Sets the password required to run a locally-managed tunnel. Must be at least 32 bytes and
	// encoded as a bas64 string.
	//
	// +optional
	TunnelSecret *CloudflareTunnelSecret `json:"tunnelSecret,omitempty"`

	// The type of tunnel.
	//
	// +optional
	Type CloudflareTunnelType `json:"type,omitempty"`
}

// CloudflareTunnelStatus defines the observed state of CloudflareTunnel.
type CloudflareTunnelStatus struct {
	// Cloudflare account ID
	//
	// +optional
	AccountTag string `json:"accountTag,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" protobuf:"bytes,1,rep,name=conditions"`

	// Timestamp of when the tunnel established at least one connection to Cloudflare's edge.
	// If null, the tunnel is inactive.
	//
	// +optional
	ConnectionsActiveAt metav1.Time `json:"connsActiveAt,omitempty"`

	// Timestamp of when the tunnel became inactive (no connections to Cloudflare's edge).
	// If null, the tunnel is active.
	//
	// +optional
	ConnectionsInactiveAt metav1.Time `json:"connsInactiveAt,omitempty"`

	// Timestamp of when the resource was created.
	//
	// +optional
	CreatedAt metav1.Time `json:"createdAt,omitempty"`

	// UUID of the tunnel.
	//
	// +optional
	Id *string `json:"id,omitempty" format:"uuid"`

	// Total number of cloudflared instances targeted by this tunnel (their labels match the selector).
	//
	// +optional
	Instances int32 `json:"instances,omitempty"`

	// A user-friendly name for a tunnel.
	//
	// +optional
	Name string `json:"name,omitempty"`

	// If `true`, the tunnel can be configured remotely from the Zero Trust dashboard.
	// If `false`, the tunnel must be configured locally on the origin machine.
	//
	// +optional
	RemoteConfig bool `json:"remoteConfig,omitempty"`

	// The status of the tunnel. Valid values are `inactive` (tunnel has never been
	// run), `degraded` (tunnel is active and able to serve traffic but in an unhealthy
	// state), `healthy` (tunnel is active and able to serve traffic), or `down`
	// (tunnel can not serve traffic as it has no connections to the Cloudflare Edge).
	//
	// +optional
	Status CloudflareTunnelHealth `json:"status,omitempty"`

	// The type of tunnel.
	//
	// +optional
	Type CloudflareTunnelType `json:"type,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="ID",type=string,JSONPath=".status.id"
// +kubebuilder:printcolumn:name="Account",type=string,JSONPath=".status.accountTag"
// +kubebuilder:printcolumn:name="Remote",type=boolean,JSONPath=".status.remoteConfig"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.status"
// +kubebuilder:printcolumn:name="Created",type=string,JSONPath=".status.createdAt"
// +kubebuilder:printcolumn:name="Active At",type=string,JSONPath=".status.connsActiveAt"
// +kubebuilder:printcolumn:name="Inactive At",type=string,JSONPath=".status.connsInactiveAt"
// +kubebuilder:printcolumn:name="Instances",type=string,JSONPath=".status.instances"

// CloudflareTunnel is the Schema for the cloudflaretunnels API.
type CloudflareTunnel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudflareTunnelSpec   `json:"spec,omitempty"`
	Status CloudflareTunnelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudflareTunnelList contains a list of CloudflareTunnel.
type CloudflareTunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudflareTunnel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudflareTunnel{}, &CloudflareTunnelList{})
}
