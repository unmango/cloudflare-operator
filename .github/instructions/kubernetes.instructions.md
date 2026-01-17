---
applyTo: "internal/controller/**/*.go,api/**/*_types.go"
---

# Kubernetes Controller and API Instructions

## Controller Development

- Use controller-runtime patterns for reconciliation
- Implement the `Reconcile` method to be idempotent
- Return `ctrl.Result{}` with appropriate requeue settings
- Update resource status separately from spec updates
- Use finalizers for cleanup of external resources
- Handle resource deletion gracefully
- Log important events with structured logging

## Reconciliation Logic

- Check if the resource is being deleted first (check for deletion timestamp)
- Handle finalizers before allowing deletion
- Fetch the latest version of the resource at the start
- Update status conditions to reflect reconciliation progress
- Return early on errors with appropriate error handling
- Use exponential backoff for retries (built into controller-runtime)

## CRD Type Definitions (api/v1alpha1/)

- Use kubebuilder markers for CRD generation
- Include `+kubebuilder:` comments for validation, defaults, and documentation
- Define clear status conditions
- Use proper JSON/YAML tags
- Include omitempty where appropriate
- Define subresources (status, scale) using markers
- Follow Kubernetes API conventions for naming and structure

## Status Updates

- Always update status as a separate operation from spec
- Use status conditions to communicate resource state
- Include relevant information in status fields
- Handle conflicts when updating status (retry on conflict)
- Use appropriate condition types and reasons

## RBAC

- Document required RBAC permissions with `+kubebuilder:rbac` markers
- Grant minimal permissions necessary
- Include permissions for both core resources and external resources
- Document why each permission is needed

## External Resource Management

- Check if external resource exists before creating
- Clean up external resources in finalizers
- Handle API rate limiting gracefully
- Support running without CLOUDFLARE_API_TOKEN for cluster-only resources
- Update status to reflect external resource state

## Testing Controllers

- Use envtest for controller testing
- Test reconciliation logic thoroughly
- Mock external API calls
- Test finalizer logic
- Test status updates
- Test error handling and retries
