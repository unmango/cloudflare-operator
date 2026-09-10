#!/usr/bin/env bash

kubebuilder init \
  --domain unmango.dev \
  --plugins go/v4,helm/v2-alpha \
  --owner unmango \
  --repo github.com/unmango/cloudflare-operator \
  --license apache2

for kind in Cloudflared CloudflareTunnel DnsRecord; do
  kubebuilder create api \
    --group cloudflare \
    --version v1alpha1 \
    --kind "$kind" \
    --resource \
    --controller
done

# Watches core Ingress objects; the type comes from k8s.io/api, so no resource
# is scaffolded for it.
kubebuilder create api \
  --group networking \
  --version v1 \
  --kind Ingress \
  --resource=false \
  --controller
