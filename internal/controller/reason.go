package controller

// Condition reasons and log messages shared across the controllers.
const (
	reasonReconciling = "Reconciling"

	// The spec cannot be acted on as written, and reconciling again will not
	// change that. The user has to edit the resource.
	reasonInvalidSpec = "InvalidSpec"

	msgStartingReconciliation = "Starting reconciliation"
)
