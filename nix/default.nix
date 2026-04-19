{
  buildGoApplication,
  cleanSource,
  ginkgo,
  version,
}:
buildGoApplication {
  pname = "";
  inherit version;

  src = cleanSource ../.;
  modules = ../gomod2nix.toml;

  nativeCheckInputs = [ ginkgo ];

  checkPhase = ''
    ginkgo run ./...
  '';
}
