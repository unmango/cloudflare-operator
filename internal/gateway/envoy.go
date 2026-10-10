package gateway

import (
	"crypto/sha256"
	"encoding/hex"

	"k8s.io/apimachinery/pkg/util/validation"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// DefaultEnvoyImage is the Envoy the operator provisions unless a
// CloudflareGatewayConfig names another. Its minor version tracks the
// go-control-plane envoy module in go.mod, which defines the xDS API the
// operator speaks.
const DefaultEnvoyImage = "docker.io/envoyproxy/envoy:distroless-v1.39.3"

// Labels the operator puts on everything it provisions for a Gateway. The
// selector uses the Gateway's UID rather than its name: a UID always fits a
// label value, and a Gateway recreated under the same name gets new objects
// rather than adopting the old ones.
const (
	LabelName      = "app.kubernetes.io/name"
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelGateway   = "cloudflare.unmango.dev/gateway-uid"

	// LabelGatewayName is the label Gateway API reserves for naming the Gateway
	// a generated object belongs to. It is set only when the name fits.
	LabelGatewayName = "gateway.networking.k8s.io/gateway-name"

	EnvoyName = "envoy"
	ManagedBy = "cloudflare-operator"
)

// Ports Envoy uses besides the Gateway's listeners. Admin binds to loopback;
// readiness is a static listener that forwards /ready to it, so the probe
// reaches only that path.
const (
	EnvoyAdminPort     = 19000
	EnvoyReadinessPort = 19001
)

// privilegedPortOffset moves a listener port below 1024 to one Envoy can bind
// without privileges. The Service still exposes the listener's own port.
const privilegedPortOffset = 10000

// ContainerPort is the port Envoy binds for a listener port.
func ContainerPort(port gatewayv1.PortNumber) int32 {
	if port < 1024 {
		return port + privilegedPortOffset
	}

	return port
}

// EnvoyObjectName names the Deployment and Service serving gw. Both live in the
// Gateway's namespace. A Service name must be a DNS-1035 label, which a Gateway
// name need not be, so a name that would not fit is replaced by a hash of it.
func EnvoyObjectName(gw *gatewayv1.Gateway) string {
	name := gw.Name + "-" + EnvoyName
	if len(validation.IsDNS1035Label(name)) == 0 {
		return name
	}

	sum := sha256.Sum256([]byte(gw.Name))
	return EnvoyName + "-" + hex.EncodeToString(sum[:])[:16]
}

// NodeID is the Envoy node id of the proxy serving gw, and the key of its
// snapshot in the xDS cache.
func NodeID(namespace, name string) string {
	return namespace + "/" + name
}

// SelectorLabels select the Envoy pods serving gw.
func SelectorLabels(gw *gatewayv1.Gateway) map[string]string {
	return map[string]string{
		LabelName:      EnvoyName,
		LabelManagedBy: ManagedBy,
		LabelGateway:   string(gw.UID),
	}
}

// Labels are the selector labels plus the gateway-name label when the name fits
// in a label value.
func Labels(gw *gatewayv1.Gateway) map[string]string {
	labels := SelectorLabels(gw)
	if len(validation.IsValidLabelValue(gw.Name)) == 0 {
		labels[LabelGatewayName] = gw.Name
	}

	return labels
}
