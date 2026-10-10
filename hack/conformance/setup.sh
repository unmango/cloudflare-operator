#!/usr/bin/env bash
# Installs what the Gateway API conformance suite runs against into the kind
# cluster KUBECONFIG names: the Gateway API CRDs at the version go.mod pins, the
# operator from the chart with the image `make kind-load` loaded, and the
# GatewayClass in gatewayclass.yaml. cloud-provider-kind must be running for
# Gateways to get addresses.
set -euo pipefail

: "${KUBECONFIG:?KUBECONFIG must name the kind cluster}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
namespace=cloudflare-operator-system

crds="$(go list -m -f '{{.Dir}}' sigs.k8s.io/gateway-api)/config/crd/standard"
kubectl apply --server-side -f "$crds"

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
# The Gateway controllers never call the Cloudflare API without a tunnel, but
# the chart wires the token from a Secret either way.
kubectl create secret generic cloudflare-credentials \
  --namespace "$namespace" \
  --from-literal=CLOUDFLARE_API_TOKEN=dummy \
  --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install cloudflare-operator "$root/dist/chart" \
  --namespace "$namespace" \
  --set manager.image.repository=cloudflare-operator \
  --set manager.image.tag=latest \
  --set cloudflare.auth.apiTokenRef.name=cloudflare-credentials \
  --set rbac.gatewayTLSSecrets.enabled=true \
  --wait --timeout 5m

kubectl apply -f "$root/hack/conformance/gatewayclass.yaml"
kubectl wait --for=condition=Accepted --timeout=2m gatewayclass/cloudflare-conformance
