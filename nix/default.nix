{
  buildGoApplication,
  envtest-assets,
  lib,
  version,
}:
buildGoApplication {
  pname = "cloudflare-operator";
  inherit version;

  src = lib.cleanSource ../.;
  modules = ../gomod2nix.toml;

  env.KUBEBUILDER_ASSETS = "${envtest-assets}";

  # envtest starts a real kube-apiserver, which picks its advertise address by
  # looking for a default route. The build sandbox has only loopback and no
  # route, so the apiserver cannot start. CI runs the suites through the dev
  # shell instead, where KUBEBUILDER_ASSETS points at these same binaries.
  doCheck = false;
}
