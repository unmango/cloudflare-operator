package controller

// Condition types and reasons the Gateway API controllers set beyond the ones
// upstream defines. Gateway API allows implementation-specific ones, and a
// condition type of our own carries the domain prefix it requires.
const (
	// The GatewayClass parametersRef is absent, names the wrong kind, or points
	// at an object that is not there.
	reasonInvalidParameters = "InvalidParameters"

	// conditionTunnelProgrammed reports whether the Cloudflare tunnel in front of
	// a Gateway sends its traffic to Envoy. Programmed covers Envoy alone, so a
	// Gateway can serve inside the cluster while its tunnel is still pending.
	conditionTunnelProgrammed = "cloudflare.unmango.dev/TunnelProgrammed"

	// The tunnel exists on the Cloudflare side and its rules are pushed.
	reasonTunnelProgrammed = "Programmed"

	// The tunnel has not been created yet, or reports a problem.
	reasonTunnelPending = "Pending"

	// Another object already holds the name the tunnel would be given.
	reasonTunnelConflict = "Conflict"

	// The class attaches Gateways to a shared tunnel, whose rules the operator
	// does not write yet.
	reasonTunnelNotImplemented = "NotImplemented"
)
