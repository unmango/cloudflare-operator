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

  # The e2e suite sits behind the `e2e` build tag and needs a live cluster,
  # so this covers only the unit and envtest suites.
  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';
}
