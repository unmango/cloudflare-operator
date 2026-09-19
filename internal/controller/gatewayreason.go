package controller

// Condition reasons the Gateway API controllers set beyond the ones upstream
// defines. Gateway API allows implementation-specific reasons.
const (
	// The GatewayClass parametersRef is absent, names the wrong kind, or points
	// at an object that is not there.
	reasonInvalidParameters = "InvalidParameters"
)
