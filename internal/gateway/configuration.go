// Package gateway holds the Gateway API logic the controllers share: class
// parameters, listener policy, route translation, and rule assembly.
package gateway

const (
	// DefaultClassName is the GatewayClass name the chart ships.
	DefaultClassName = "cloudflare"

	// ControllerName identifies this implementation in GatewayClass.spec.controllerName
	// and in the route and Gateway statuses it writes.
	ControllerName = "cloudflare.unmango.dev/gateway-controller"
)
