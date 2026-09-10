---
name: code-review
description: Review changes to the cloudflare-operator for repo-specific correctness, covering regeneration drift, reconcile and finalizer conventions, kubebuilder marker style, and envtest spec hygiene. Use when reviewing a pull request or diff in this repository.
---

# Reviewing cloudflare-operator changes

`AGENTS.md` at the repository root describes how the operator is built.
This skill lists what to check in a diff, ordered by how much a miss costs.
Report findings as checks against a named file, not as prose.

## 1. Generated-file drift

Drift is the one thing CI gates with an explicit diff.
The `lint` job in `.github/workflows/ci.yml` runs:

```sh
nix develop -c make helm
git diff --exit-code -- dist/chart PROJECT Makefile
```

Regenerated CRDs reach `dist/chart` through kustomize, so stale `config/crd/bases` output surfaces as a diff in the chart.
`make lint` and `make test` fail on their own terms and need no help from this section.

Flag a diff that changes a source without its generated output:

- `api/v1alpha1/*_types.go` or any `+kubebuilder:` marker, without `config/crd/bases/*`, `**/zz_generated.deepcopy.go`, and `dist/chart`. Fix: `make manifests generate` then `make helm`.
- Anything under `config/`, without `dist/chart`. Fix: `make helm`.
- A new method on the `Client` interface in `internal/client/client.go`, without `internal/testing/client.go`. Fix: `go generate ./...`, driven by the `//go:generate mockgen` directive in that file.
- `go.mod`, without `gomod2nix.toml`. Fix: `make tidy`. The Nix build fails when these disagree.

Flag hand-edits to generated files: `config/crd/bases/*`, `config/rbac/role.yaml`, `**/zz_generated.*.go`, `internal/testing/client.go`, `PROJECT`, and everything under `dist/chart` except `Chart.yaml`, `values.yaml`, and `templates/ingress-class/`.

Flag any deleted `// +kubebuilder:scaffold:*` comment.
The CLI injects code at those markers.

## 2. Reconciler conventions

`internal/controller/clientutil.go` defines `patch` and `patchSubResource`.

- Spec and metadata mutations go through `patch`, status through `patchSubResource`. A raw `Update` or `Status().Update()` in controller code is a finding.
- Both helpers snapshot the original with `DeepCopy`, call the closure, and patch with `MergeFrom(original)`. The mutation must land on the instance being patched. Writing to the closure's `obj` parameter guarantees that; a captured variable only works while it aliases the same object, so prefer the parameter and flag a closure that mutates anything else.
- The deletion branch, guarded by `!obj.DeletionTimestamp.IsZero()`, comes before status work. The finalizer is released even when creation never succeeded, or the object is stuck.
- Finalizer constants are `<kind>.cloudflare.unmango.dev/finalizer`, as in `dnsrecord_controller.go` and `cloudflaretunnel_controller.go`. `cloudflaredFinalizer` in `cloudflared_controller.go` is `cloudflared.unmango.dev/finalizer` and is unused, so do not cite it as precedent.
- The requeue interval is `5 * time.Second` throughout. A different value needs a stated reason.
- Cloudflare API errors are logged and swallowed with `return ctrl.Result{}, nil`, so a bad token does not hot-loop the workqueue. Kubernetes API errors and patch errors are returned.
- Use `cfclient.IgnoreNotFound` and `cfclient.IgnoreConflict` from `internal/client/errors.go` for Cloudflare errors, and `client.IgnoreNotFound` for Kubernetes ones. Raw status-code checks in a controller are a finding.
- The Cloudflare SDK stays behind `internal/client.Client`. SDK types must not appear in `api/v1alpha1` or in the controllers, and `cloudflare.F(...)` wrapping belongs in `internal/client/map.go`.
- Reconcilers converge over several passes rather than doing everything in one.
- `internal/client` interface methods are alphabetical, and the implementations follow the same order.

Style:

- `log := logf.FromContext(ctx)` at the top of `Reconcile`, never a package-level logger.
- `log.Info` for state transitions, `log.V(1).Info` for ignorable conditions, `log.V(2).Info` for step tracing.
- Messages are capitalized fragments with no trailing punctuation and no `fmt.Sprintf`. Context goes in structured key/value pairs. Failures use `log.Error(err, "Failed to ...")`.
- Wrapped errors use a lowercase verb phrase: `fmt.Errorf("lookup tunnel: %w", err)`.
- `internal/client` is imported as `cfclient` so `client` stays the controller-runtime package. `revive`'s `import-shadowing` rule is enabled.

## 3. API types

- Doc comment, blank `//` line, markers, then the field.
- Every field carries an explicit `+optional` or `+required`. Fields with a `+kubebuilder:default` carry neither.
- Cloudflare nouns keep non-Go initialisms: `Id`, `ZoneId`, `AccountId`, `Ttl`, `Ipv4Only`. Do not propose renaming them.
- Enums are `type X string` with `+kubebuilder:validation:Enum=` on the type and constants named `<Value><TypeName>`.
- Union types set `+kubebuilder:validation:MaxProperties:=1` and `MinProperties:=1` with all-pointer members.
- A new status `Conditions` field copies the marker block verbatim from `api/v1alpha1/cloudflared_types.go`: `+listType=map`, `+listMapKey=type`, `+patchStrategy=merge`, `+patchMergeKey=type`, `+optional`, and the protobuf tag.
- Shared condition reasons and messages belong in `internal/controller/reason.go`, names of managed objects in `internal/controller/names.go`, not inline at the call site.
- Scheme registration is a per-file `init()` calling `SchemeBuilder.Register`.
- `api/v1alpha1` depends only on `k8s.io` packages.

## 4. Tests

- envtest runs no controllers and no garbage collector. Every spec that creates an object needs a matching `deleteIfExists` call in `AfterEach`. Without it the finalizer is never removed, the object outlives the spec, and it collides with the next one.
- Fixtures go in `testNamespace`, defined in `internal/controller/suite_test.go`, rather than a namespace invented by the spec.
- Assertions read the object through the spec's `observed()` helper, not the variable passed to `Create`.
- A spec asserting on a converged state reconciles more than once.
- Reconciliation coverage belongs in the envtest suites. `test/e2e` covers only that the image starts in a real cluster and that the API server accepts the published CRDs.
- gomock controllers and mocks are constructed fresh in `BeforeEach`. Use `gomock.Eq` with the full parameter struct when the point of the spec is spec-to-API translation. An expectation deliberately left unset carries a comment saying so.
- Spec naming: `Describe("<Kind> Controller")`, `Context("When reconciling a resource")`, nested `Context("and ...")`, `It("should ...")`.
- `ginkgolinter` is enabled: `Expect(err).NotTo(HaveOccurred())`, never `Expect(err).To(BeNil())`.
- Environment variables are set with `GinkgoT().Setenv`, never `os.Setenv`.

## 5. Confirming a finding

Report these commands rather than running them.
Every tool comes from the Nix dev shell, so prefix with `nix develop -c` when the shell is not loaded.

```sh
# Wider than the CI gate: this also catches stale config/crd/bases directly.
make manifests generate helm && git diff --exit-code -- config dist/chart PROJECT Makefile
make lint-config lint helm-lint
make test
```

## 6. Running this review by hand

Outside a pull request, apply the checks above to `git diff main...HEAD` together with any uncommitted changes.
