# Binaries controller-runtime's envtest expects to find in KUBEBUILDER_ASSETS.
# Sourcing them from nixpkgs replaces `setup-envtest`, which downloads them at
# test time and cannot run inside the nix build sandbox.
{
  etcd,
  kubernetes,
  linkFarm,
}:
linkFarm "envtest-assets" {
  "etcd" = "${etcd}/bin/etcd";
  "kube-apiserver" = "${kubernetes}/bin/kube-apiserver";
  "kubectl" = "${kubernetes}/bin/kubectl";
}
