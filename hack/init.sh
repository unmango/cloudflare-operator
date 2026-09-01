#!/usr/bin/env bash

kubebuilder init \
  --domain cloudflare.unmango.dev \
  --plugins go/v4,helm/v2-alpha \
  --owner unmango \
  --repo github.com/unmango/cloudflare-operator \
  --license apache2
