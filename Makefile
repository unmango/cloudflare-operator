GO        ?= nix develop -c go
GOMOD2NIX ?= nix develop -c gomod2nix
GINKGO    ?= nix develop -c ginkgo

GO_SRC ?= $(shell find . -name '*.go')

build:
	nix build .#

test:
	$(GINKGO) run -r

update:
	nix flake update

check:
	nix flake check

format fmt:
	nix fmt

tidy: go.sum

go.sum: go.mod ${GO_SRC}
	$(GO) mod tidy

gomod2nix.toml: go.sum ${GO_SRC}
	$(GOMOD2NIX) generate
