{
  description = "A kubernetes operator for Cloudflare";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    systems.url = "github:nix-systems/default";

    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };

    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.inputs.systems.follows = "systems";
    };
  };

  outputs =
    inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = import inputs.systems;
      imports = [ inputs.treefmt-nix.flakeModule ];

      perSystem =
        { pkgs, system, ... }:
        let
          version = "0.0.4";
          envtest-assets = pkgs.callPackage ./nix/envtest.nix { };
          operator = pkgs.callPackage ./nix { inherit envtest-assets version; };
        in
        {
          _module.args.pkgs = import inputs.nixpkgs {
            inherit system;
            overlays = [ inputs.gomod2nix.overlays.default ];
          };

          packages = {
            default = operator;
            image = pkgs.callPackage ./nix/image.nix { inherit operator; };
            inherit envtest-assets;
          };

          # `nix flake check` evaluates `packages` without building them, so the
          # operator is exposed as a check to make CI compile it and run the
          # suites in its checkPhase.
          checks.operator = operator;

          devShells.default = pkgs.mkShellNoCC {
            packages = with pkgs; [
              cloudflared
              ginkgo
              gnumake
              go
              golangci-lint
              gomod2nix
              gopls
              kind
              kubebuilder
              kubectl
              kubernetes-controller-tools
              kubernetes-helm
              kustomize
              mockgen
              nixfmt
            ];

            KUBEBUILDER_ASSETS = "${envtest-assets}";
          };

          treefmt.programs = {
            gofmt.enable = true;
            nixfmt.enable = true;
            shfmt.enable = true;
          };
        };
    };
}
