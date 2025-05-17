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

// CloudflareTunnelCloudflared describes how cloudflared should run this tunnel. If template is
// provided the labels must match selector. The spec.config.tunnelId and spec.config.accountId fields
// will be managed by the CloudflareTunnel controller. If template is nil the controller will begin
// managing the spec.config.tunnelId and spec.config.accountId fields on all Cloudflared resources
// matched by selector.
type CloudflareTunnelCloudflared struct {
	// Selector defines which Cloudflared resource(s) is/are responsible for running the tunnel.
	//
	// +required
	Selector *metav1.LabelSelector `json:"selector"`

	// Template describes the cloudflareds that will be created.
	//
	// +optional
	Template *CloudflaredTemplateSpec `json:"template,omitempty"`
}

// For all L7 requests to this hostname, cloudflared will validate each request's Cf-Access-Jwt-Assertion request header.
type CloudflareTunnelOriginRequestAccess struct {
	// Access applications that are allowed to reach this hostname for this Tunnel. Audience tags can be identified in the dashboard or via the List Access policies API.
	AudTag []string `json:"audTag"`

	// (default: "Your Zero Trust organization name.")
	TeamName string `json:"teamName"`

	// Deny traffic that has not fulfilled Access authorization.
	//
	// +optional
	Required bool `json:"required,omitempty"`
}

// Configuration parameters for the public hostname specific connection settings between cloudflared and origin server.
type CloudflareTunnelOriginRequest struct {
	// For all L7 requests to this hostname, cloudflared will validate each request's Cf-Access-Jwt-Assertion request header.
	//
	// +optional
	Access CloudflareTunnelOriginRequestAccess `json:"access,omitempty"`

	// Path to the certificate authority (CA) for the certificate of your origin.
	// This option should be used only if your certificate is not signed by Cloudflare.
	//
	// +ptional
	CaPool string `json:"caPool"`

	// Timeout for establishing a new TCP connection to your origin server.
	// This excludes the time taken to establish TLS, which is controlled by tlsTimeout.
	//
	// +kubebuilder:default:=10
	// +optional
	ConnectTimeout int64 `json:"connectTimeout"`

	// Disables chunked transfer encoding.
	// Useful if you are running a WSGI server.
	//
	// +optional
	DisableChunkedEncoding bool `json:"diableChunkedEncoding,omitempty"`

	// Attempt to connect to origin using HTTP2.
	// Origin must be configured as https.
	//
	// +optional
	Http2Origin bool `json:"http2Origin,omitempty"`

	// Sets the HTTP Host header on requests sent to the local service.
	//
	// +optional
	HttpHostHeader string `json:"httpHostHeader,omitempty"`

	// Maximum number of idle keepalive connections between Tunnel and your origin.
	// This does not restrict the total number of concurrent connections.
	//
	// +kubebuilder:default:=100
	// +optional
	KeepAliveConnections int64 `json:"keepAliveConnections"`

	// Timeout after which an idle keepalive connection can be discarded.
	//
	// +kubebuilder:default:=90
	// +optional
	KeepAliveTimeout int64 `json:"keepAliveTimeout"`

	// Disable the “happy eyeballs” algorithm for IPv4/IPv6 fallback if your local network has misconfigured one of the protocols.
	//
	// +optional
	NoHappyEyeballs bool `json:"noHappyEyeballs,omitempty"`

	// Disables TLS verification of the certificate presented by your origin.
	// Will allow any certificate from the origin to be accepted.
	//
	// +optional
	NoTlsVerify bool `json:"noTlsVerify,omitempty"`

	// Hostname that cloudflared should expect from your origin server certificate.
	//
	// +optional
	OriginServerName string `json:"originServerName,omitempty"`

	// cloudflared starts a proxy server to translate HTTP traffic into TCP when proxying, for example, SSH or RDP.
	// This configures what type of proxy will be started.
	// Valid options are: "" for the regular proxy and "socks" for a SOCKS5 proxy.
	//
	// +optional
	// +kubebuilder:validation:Enum=;socks
	ProxyType string `json:"proxyType,omitempty"`

	// The timeout after which a TCP keepalive packet is sent on a connection between Tunnel and the origin server.
	//
	// +kubebuilder:default:=30
	// +optional
	TcpKeepAlive int64 `json:"tcpKeepAlive"`

	// Timeout for completing a TLS handshake to your origin server, if you have chosen to connect Tunnel to an HTTPS server.
	//
	// +kubebuilder:default:=10
	// +optional
	TlsTimeout int64 `json:"tlsTimeout"`
}

type CloudflareTunnelConfigIngress struct {
	// Public hostname for this service.
	//
	// +required
	Hostname string `json:"hostname"`

	// Protocol and address of destination server.
	// Supported protocols: http://, https://, unix://, tcp://, ssh://, rdp://, unix+tls://, smb://.
	// Alternatively can return a HTTP status code http_status:[code] e.g. 'http_status:404'.
	//
	// +required
	Service string `json:"service"`

	// Configuration parameters for the public hostname specific connection settings between cloudflared and origin server.
	//
	// +optional
	OriginRequest CloudflareTunnelOriginRequest `json:"originRequest,omitempty"`

	// Requests with this path route to this public hostname.
	//
	// +optional
	Path string `json:"path,omitempty"`
}

// Enable private network access from WARP users to private network routes.
// This is enabled if the tunnel has an assigned route.
type CloudflareTunnelWarpRouting struct {
	Enable bool `json:"enable"`
}

type CloudflareTunnelConfig struct {
	// List of public hostname definitions.
	// At least one ingress rule needs to be defined for the tunnel.
	Ingress []CloudflareTunnelConfigIngress `json:"ingress,omitempty"`

	// Configuration parameters for the public hostname specific connection settings between cloudflared and origin server.
	//
	// +optional
	OriginRequest CloudflareTunnelOriginRequest `json:"originRequest,omitempty"`

	// Enable private network access from WARP users to private network routes.
	// This is enabled if the tunnel has an assigned route.
	//
	// +optional
	WarpRouting *CloudflareTunnelWarpRouting `json:"warpRouting,omitempty"`
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

	// The tunnel configuration and ingress rules.
	//
	// +optional
	Config *CloudflareTunnelConfig `json:"config,omitempty"`

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
