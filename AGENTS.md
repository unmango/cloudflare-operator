# AGENTS.md

This file provides guidance to AI agents working with code in this repository.

## What This Is

A Kubernetes operator that manages Cloudflare infrastructure via custom resources:
- **Cloudflared** — deploys cloudflared tunnel agents (DaemonSet or Deployment)
- **CloudflareTunnel** — creates/manages Cloudflare Tunnels via the Cloudflare API
- **DnsRecord** — manages Cloudflare DNS records via the Cloudflare API
- **Ingress** — integrates with standard Kubernetes Ingress resources

## Commands

```sh
make build          # Build manager binary to bin/manager
make run            # Run controller locally against current kubeconfig
make test           # Run unit/integration tests (excludes e2e)
make test-e2e       # Run e2e tests (requires Kind cluster, see below)
make lint           # Run golangci-lint
make lint-fix       # Run golangci-lint with auto-fix
make fmt            # Run go fmt
make vet            # Run go vet
make manifests      # Regenerate CRD, RBAC, and Webhook manifests from markers
make generate       # Regenerate DeepCopy methods and mocks
make install        # Install CRDs into current cluster
make deploy IMG=<image>  # Deploy controller to cluster
make kind-cluster   # Create local Kind cluster named "cloudflare-operator"
make delete-cluster # Tear down Kind cluster
```

### Running a single test

```sh
# Unit/integration (Ginkgo)
go test ./internal/controller/... -run TestControllers
# or with Ginkgo label/focus
go test ./internal/controller/... -v

# E2E (requires Kind cluster and operator deployed)
go test ./test/e2e/... -v

# E2E without Cloudflare credentials (cloud-gated tests are skipped automatically)
make docker-build IMG=<image>   # must be done before running e2e
go test ./test/e2e/... -v -- --label-filter='!cloudflare-api'
```

## Architecture

```
cmd/main.go                # Controller manager entrypoint
api/v1alpha1/              # CRD type definitions (kubebuilder markers here)
internal/
  controller/              # Reconcilers: cloudflared, cloudflaretunnel, dnsrecord, ingress
  client/                  # cfclient.Client interface wrapping cloudflare-go/v4
  ingress/                 # Ingress-specific logic
  annotation/              # Annotation parsing helpers
  config/                  # Configuration structures
  testing/                 # Mocks (go.uber.org/mock) for cfclient.Client
config/                    # Kustomize manifests for CRDs, RBAC, manager, Helm chart
test/e2e/                  # End-to-end tests (Kind + real or mocked Cloudflare API)
```

### Key patterns

- **Reconcilers** follow standard controller-runtime shape: `Reconcile(ctx, req) (ctrl.Result, error)`
- **Finalizers** handle cleanup when resources are deleted (e.g., `cloudflared.unmango.dev/finalizer`)
- **Status conditions** use `metav1.Condition` with types `Available`, `Degraded`, `Progressing`
- **cfclient.Client** is a mockable interface over cloudflare-go/v4; unit tests mock this, never hit the real API
- **envtest** bootstraps a real API server for controller tests; CRDs are loaded from `config/crd/bases/`

### Code generation

After changing kubebuilder markers (`+kubebuilder:...`) in `api/v1alpha1/` or reconciler RBAC comments:

```sh
make manifests   # regenerate CRD/RBAC/Webhook configs
make generate    # regenerate DeepCopy methods and mocks
```

Both must be re-run before the changes take effect in tests or deployment.

## Gotchas

- **E2E image pre-step**: `make test-e2e` does not build the image; run `make docker-build IMG=<image>` first.
- **Cloudflare API tests**: labeled `cloudflare-api` and skip automatically when credentials are absent.
- **Cloudflared hello-world mode**: omit `spec.config` entirely to create a DaemonSet that runs `--hello-world` with no API calls — useful for tests and local dev.
- **`createTunnel` error handling**: if the Cloudflare API call fails, the reconciler logs the error and drops it silently (`ctrl.Result{}, nil`). The `Available` condition stays `Unknown`; `Degraded` is **not** set. Don't assert `Degraded=True` after a failed create.
- **Webhooks are inactive**: cert-manager is not required; the webhook config is commented out in `config/default/kustomization.yaml`.

## Environment Variables

| Variable | Purpose |
|---|---|
| `CLOUDFLARE_API_TOKEN` | Required for real Cloudflare API operations |
| `CLOUDFLARE_ACCOUNT_ID` | Required for real Cloudflare API operations |
| `IMG` | Container image (default: `ghcr.io/unmango/cloudflare-operator:v0.0.4`) |
| `KUBEBUILDER_ASSETS` | Set automatically by envtest during `make test` |
