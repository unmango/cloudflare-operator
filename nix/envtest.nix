# Binaries controller-runtime's envtest expects to find in KUBEBUILDER_ASSETS.
# Sourcing them from nix replaces `setup-envtest`, which downloads them at test
# time and cannot run inside the nix build sandbox.
#
# They come from the kubepkgs flake input, pinned to one Kubernetes minor in
# flake.nix, so etcd is the release that minor pins rather than an unrelated
# package version.
{
  etcd,
  kube-apiserver,
  kubectl,
  linkFarm,
}:
linkFarm "envtest-assets" {
  "etcd" = "${etcd}/bin/etcd";
  "kube-apiserver" = "${kube-apiserver}/bin/kube-apiserver";
  "kubectl" = "${kubectl}/bin/kubectl";
}
