{
  buildGoApplication,
  lib,
  version,
}:
buildGoApplication {
  pname = "cloudflare-operator";
  inherit version;

  src = lib.cleanSource ../.;
  modules = ../gomod2nix.toml;

  checkPhase = ''
    go test ./...
  '';
}
