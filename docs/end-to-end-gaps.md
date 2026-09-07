# End-to-end tunnel gaps

What stands between the current operator and "apply one manifest, get a working Cloudflare tunnel".

## Where things stand

The hand-written path works.
A `CloudflareTunnel` with `spec.accountId`, `spec.configSource: cloudflare` and `spec.config.ingress[]` creates the tunnel through the API and pushes the ingress rules remotely.
Its `spec.cloudflared.template` produces a `Cloudflared`, which fetches the tunnel token and runs `cloudflared tunnel run <id>` as a DaemonSet or Deployment.
A `DnsRecord` of type CNAME pointing at `<tunnel-id>.cfargotunnel.com` completes the path.

The operator does not connect those three pieces.
The user supplies the tunnel id to the DNS record by hand, and the Ingress controller creates a tunnel shell that carries no routing.

Gaps are grouped into four phases.
Each phase is independently shippable and leaves the operator in a working state.
Land one focused PR per item.

## Phase 1: correctness fixes

Small, self-contained, no API changes.
These are bugs in code that already ships.

### 1.1 Empty `spec.name` renames the tunnel to the empty string

`createTunnel` falls back to `metadata.name` when `spec.Name` is empty (`internal/controller/cloudflaretunnel_controller.go:263`), but `updateTunnel` compares `tunnel.Spec.Name` against the remote name (`:309`) and calls `EditTunnel` with the empty spec value on every reconcile.

Fix: resolve the effective name once, in a helper both paths call, and compare that.
Test: create a tunnel with no `spec.name`, reconcile twice, assert `EditTunnel` is never called.

### 1.2 `spec.tunnelSecret` is silently ignored

`createTunnel` always sends `TunnelSecret: cloudflare.Null[string]()` (`:272`), so `CloudflareTunnelSecret` in the API and the `ingress.cloudflare.unmango.dev/tunnelSecret` annotation both do nothing.

Fix: resolve `spec.tunnelSecret` (inline `value`, or `valueFrom` secret/configmap key) and pass it.
Cloudflare expects a base64-encoded 32-byte secret; decide whether the operator validates or passes through, and document it on the field.
Test: inline value reaches `CreateTunnel`; a secret ref is read from the referenced Secret; absent field still sends null.

### 1.3 Tunnel config is pushed even for `configSource: local`

`updateTunnel` calls `UpdateConfiguration` whenever `spec.config != nil` (`:320`), regardless of config source.
For a locally-managed tunnel that write is wrong, and the API may reject it.

Fix: only push remote configuration when `spec.configSource` is `cloudflare`; surface a condition when `spec.config` is set on a local tunnel.

### 1.4 API errors are swallowed as successful reconciles

Several paths log an error and return `ctrl.Result{}, nil` (`cloudflaretunnel_controller.go:158`, `:204`, `dnsrecord_controller.go:100`, `:120`), so a failed create never retries and never surfaces in status.

Fix: return the error (controller-runtime backs off) or requeue explicitly, and set the `Degraded` condition with the reason.
Keep the deliberate swallows where the failure is terminal, and comment them.

## Phase 2: tunnel to DNS

Close the manual step: make a hostname routed by a tunnel get its CNAME automatically.

### 2.1 Add `spec.config.ingress[].dns` (or a tunnel-level `spec.dns`)

New optional field carrying at minimum `zoneId`, plus `proxied` (default true) and `ttl`.
Requires `make manifests generate` and `make helm`.

### 2.2 Reconcile a `DnsRecord` per routed hostname

In the `CloudflareTunnel` reconciler, once `status.id` is set, create or update an owned `DnsRecord` per ingress entry that has DNS config: type CNAME, name the hostname, content `<status.id>.cfargotunnel.com`, `proxied: true`.
Set the controller reference so deletion cascades, and name the records deterministically (`<tunnel>-<hostname-hash>`) so repeated reconciles converge.

Catch-all ingress entries (no hostname, the required trailing `http_status:404` rule) must be skipped.

### 2.3 Reflect DNS state on the tunnel

Add `status.hostnames[]` or a count plus a condition, so `kubectl get cloudflaretunnel` shows whether routing is live.

Tests: envtest, asserting the owned `DnsRecord` objects rather than the Cloudflare API; the `DnsRecord` controller already covers the API call.
Remember envtest runs no garbage collector, so use `deleteIfExists`.

## Phase 3: a working Ingress path

`internal/controller/ingress_controller.go` currently creates a bare `CloudflareTunnel` from annotations and then returns early forever.
This phase makes the Ingress class actually route traffic.

### 3.1 Map `ingress.spec.rules` into `spec.config.ingress`

For each rule host and HTTP path, emit a `CloudflareTunnelConfigIngress` whose `hostname` is the rule host, `path` is the path (respecting `pathType`), and `service` is the in-cluster URL of the backend Service: `http://<svc>.<ns>.svc.cluster.local:<port>`.

Resolve the backend port: `backend.service.port.number` directly, `port.name` by reading the Service.
Reject or condition-flag `backend.resource` backends, which have no counterpart.

Append the terminal `http_status:404` catch-all, which Cloudflare requires as the last rule.

### 3.2 Reconcile updates, not just creation

Replace the early return at `:69` with a converge step that recomputes the desired spec from the Ingress and patches the existing tunnel.
Ingress edits, added rules, and removed rules must all propagate.

### 3.3 Zone id for DNS

The Ingress path needs a zone to create records in.
Add an `ingress.cloudflare.unmango.dev/zoneId` annotation and thread it into the Phase 2 DNS fields.
Consider resolving the zone from the hostname through the Cloudflare API as a follow-up, which needs a new `ListZones` method on `internal/client.Client` and its regenerated mock.

### 3.4 Publish `ingress.status.loadBalancer`

Report the tunnel hostname back on the Ingress so `kubectl get ingress` is informative.

### 3.5 Watch what it owns

`SetupWithManager` watches only `Ingress` (`:122`).
Add `Owns(&cfv1alpha1.CloudflareTunnel{})` so tunnel status changes re-trigger the Ingress reconcile.
The same applies to the `CloudflareTunnel` reconciler, which should own `Cloudflared` and `DnsRecord`.

### 3.6 Ship an `IngressClass`

`dist/chart/templates/ingress-class/` is hand-owned; confirm it matches `ControllerName` in `internal/ingress/configuration.go` and that `config/` installs an equivalent.

## Phase 4: operational polish

### 4.1 Drift detection on the remote tunnel config

`updateTunnel` writes the configuration on every reconcile without reading it back.
Fetch the current configuration and skip the write when it matches, to stop burning API quota.
Needs a `GetConfiguration` method on `internal/client.Client`.

### 4.2 Per-resource credentials

The API token comes only from the process environment (`cloudflaretunnel_controller.go:73`).
A secret reference per `CloudflareTunnel` would let one operator serve several accounts.
Design decision, not just plumbing: it changes how `internal/client.Client` is constructed, since the client is currently a single instance injected at startup.

### 4.3 Readiness that reflects reality

`Cloudflared` sets `Available=True` as soon as the app object is created, not when pods are ready.
The commented-out `appReady` helper (`cloudflared_controller.go:623`) is the intended fix.
Wire it in and gate the condition on it.

### 4.4 e2e coverage for the full path

The e2e suite covers only that the image starts and the CRDs are accepted.
An e2e test with a real account is not viable in CI, but a fake Cloudflare API server backing `internal/client.Client` would let the envtest suites assert the whole chain: Ingress in, tunnel plus cloudflared plus DNS record out.

## Sequencing notes

Phase 1 is independent and can land in any order.
Phase 2 must land before Phase 3.3 has anywhere to write.
Phase 3.1 is the largest single item and deserves its own design pass on port resolution and path mapping before implementation.

After anything touching `*_types.go` or a kubebuilder marker, run `make manifests generate` and `make helm`, and commit the result: CI regenerates both and fails on a diff.
