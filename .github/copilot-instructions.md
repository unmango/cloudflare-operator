# Cloudflare Operator - GitHub Copilot Instructions

This is a Kubernetes operator for managing Cloudflare infrastructure using the operator pattern.

## Project Overview

- **Language**: Go 1.24.4+
- **Framework**: Kubebuilder v4
- **Purpose**: Manage Cloudflare Tunnels, DNS records, and cloudflared deployments via Kubernetes Custom Resource Definitions (CRDs)
- **API Version**: cloudflare.unmango.dev/v1alpha1

## Development Guidelines

### Code Style

- Follow standard Go conventions and idioms
- Use golangci-lint for linting (configuration in `.golangci.yml`)
- Run `make fmt` to format code
- Run `make vet` to check for common mistakes
- Run `make lint` to run the full linter suite

### Building and Testing

- Build with `make build`
- Run tests with `make test`
- Run end-to-end tests with `make test-e2e`
- Generate manifests with `make manifests`
- Generate code (DeepCopy methods, mocks) with `make generate`

### Project Structure

- `api/v1alpha1/` - CRD type definitions (Cloudflared, CloudflareTunnel, DnsRecord)
- `internal/` - Internal packages including controllers and client code
- `cmd/` - Main application entry points
- `config/` - Kubernetes manifests, CRDs, RBAC, and deployment configs
- `test/` - Test utilities and e2e tests
- `hack/` - Build scripts and utilities

### Kubernetes Controller Patterns

- Use controller-runtime for reconciliation loops
- Follow the operator pattern for managing external Cloudflare resources
- Ensure proper status updates on custom resources
- Handle both cluster-only resources (like Cloudflared) and API-backed resources (like CloudflareTunnel, DnsRecord)
- Support running without CLOUDFLARE_API_TOKEN for cluster-only resources

### Testing

- Use Ginkgo v2 and Gomega for testing
- Mock external dependencies using go.uber.org/mock
- Write table-driven tests where appropriate
- Test both successful and error cases
- Use the testing utilities in `internal/testing/`

### Dependencies

- Use `go.mod` for dependency management
- Install development tools via `go tool` directives in go.mod
- Key dependencies: controller-runtime, cloudflare-go/v4, Ginkgo/Gomega

### API Design

- Follow Kubernetes API conventions for CRD design
- Use proper status subresources
- Include meaningful conditions in status
- Support both imperative and declarative configurations
- Design for idempotency in reconciliation loops
