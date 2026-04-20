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
      imports = with inputs; [ treefmt-nix.flakeModule ];

      perSystem =
        { pkgs, system, ... }:
        let
          version = "0.0.4";
          operator = pkgs.callPackage ./nix { inherit version; };
        in
        {
          _module.args.pkgs = import inputs.nixpkgs {
            inherit system;
            overlays = with inputs; [ gomod2nix.overlays.default ];
          };

          packages.default = operator;
          packages.image = pkgs.callPackage ./nix/image.nix { inherit operator; };

          devShells.default = pkgs.mkShellNoCC {
            packages = with pkgs; [
              cloudflared
              direnv
              go
              gomod2nix
              gopls
              ginkgo
              gnumake
              kind
              kubebuilder
              nixfmt
            ];

            GO = "${pkgs.go}/bin/go";
            GOMOD2NIX = "${pkgs.gomod2nix}/bin/gomod2nix";
            GINKGO = "${pkgs.ginkgo}/bin/ginkgo";
          };

          treefmt.programs = {
            nixfmt.enable = true;
            gofmt.enable = true;
          };
        };
    };
}
