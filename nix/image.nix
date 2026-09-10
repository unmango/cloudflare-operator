{
  cacert,
  dockerTools,
  operator,
}:
dockerTools.streamLayeredImage {
  name = "cloudflare-operator";
  tag = "latest";

  contents = [
    cacert
    dockerTools.fakeNss
    operator
  ];

  config = {
    Entrypoint = [ "/bin/manager" ];
    User = "65532:65532";
  };
}
