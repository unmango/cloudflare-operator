# Every tool below comes from the nix dev shell. Run `direnv allow` once, or
# prefix invocations with `nix develop -c` when the shell is not loaded.
GO         ?= go
GINKGO     ?= ginkgo
GOMOD2NIX  ?= gomod2nix
CONTROLLER_GEN ?= controller-gen
GOLANGCI_LINT  ?= golangci-lint
KIND       ?= kind
KUBECTL    ?= kubectl
KUSTOMIZE  ?= kustomize

GO_SRC ?= $(shell find . -name '*.go')

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: ## Generate DeepCopy implementations and mocks.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
	$(GO) generate ./...

.PHONY: fmt format
fmt format: ## Run nix fmt against code.
	nix fmt

.PHONY: vet
vet: ## Run go vet against code.
	$(GO) vet ./...

.PHONY: test
test: manifests generate vet ## Run unit and envtest suites.
	$(GINKGO) run -r --skip-package=test

.PHONY: lint
lint: ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ E2E

KIND_CLUSTER ?= cloudflare-operator-e2e

# The e2e suite installs CRDs and a Deployment into whatever cluster the ambient
# kubeconfig points at. Write the Kind credentials to a file of their own and
# export KUBECONFIG for the suite so it cannot reach a real cluster.
E2E_KUBECONFIG ?= $(CURDIR)/bin/$(KIND_CLUSTER).kubeconfig

.PHONY: setup-test-e2e
setup-test-e2e: | bin ## Create the Kind cluster used for e2e tests if it does not exist.
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) echo "Kind cluster '$(KIND_CLUSTER)' already exists." ;; \
		*) $(KIND) create cluster --name $(KIND_CLUSTER) --config hack/kind-config.yaml ;; \
	esac
	$(KIND) export kubeconfig --name $(KIND_CLUSTER) --kubeconfig $(E2E_KUBECONFIG)

.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate vet ## Run the e2e tests against a Kind cluster.
	KUBECONFIG=$(E2E_KUBECONFIG) KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) $(GO) test -tags=e2e ./test/e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests.
	@$(KIND) delete cluster --name $(KIND_CLUSTER)
	@rm -f $(E2E_KUBECONFIG)

##@ Build

.PHONY: build
build: ## Build the operator with nix.
	nix build .#

.PHONY: run
run: manifests generate vet ## Run a controller from your host.
	$(GO) run ./cmd/main.go

.PHONY: build-installer
build-installer: manifests generate ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Image

# The image is built by nix/image.nix, which produces a script that streams the
# tarball to stdout rather than a tarball in the store.
hack/stream-image: flake.nix nix/image.nix nix/default.nix
	nix build .#image --out-link hack/stream-image

bin:
	mkdir -p bin

.PHONY: image-tar
image-tar: hack/stream-image | bin ## Stream the image to bin/image.tar.
	./hack/stream-image > bin/image.tar

.PHONY: kind-load
kind-load: hack/stream-image ## Load the image into the kind cluster.
	./hack/stream-image | $(KIND) load image-archive /dev/stdin --name $(KIND_CLUSTER)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
# The CRD bundle is well over a megabyte, so it has to be piped straight into
# kubectl. Holding it in a shell variable first exceeds the exec argument limit.
#
# It also has to go in server-side. A client-side apply records the whole object
# in the last-applied-configuration annotation, and the two larger CRDs are past
# the 256KiB the API server allows for annotations.
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply --server-side --force-conflicts -f -

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply --server-side --force-conflicts -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Nix

.PHONY: update
update: ## Update nix flake inputs.
	nix flake update

.PHONY: check
check: ## Run nix flake checks.
	nix flake check

.PHONY: tidy
tidy: go.sum gomod2nix.toml ## Tidy go modules and regenerate the gomod2nix lock.

go.sum: go.mod ${GO_SRC}
	$(GO) mod tidy

gomod2nix.toml: go.sum ${GO_SRC}
	$(GOMOD2NIX) generate
