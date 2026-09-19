package controller

import (
	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
)

const (
	// Cloudflare reads a TTL of 1 as 'automatic'.
	dnsTtlAutomatic int64 = 1

	dnsProxiedDefault = true
)

// resolvedDns is the DNS configuration for one hostname, with every field settled.
type resolvedDns struct {
	ZoneId  string
	Proxied bool
	Ttl     int64
}

// resolveDns merges the tunnel-level DNS settings with the override on one ingress
// entry, field by field: the entry wins where it is set, the tunnel fills the rest,
// and the field default applies when neither does.
//
// It reports false when no zone resolves, which is what keeps DNS opt-in.
func resolveDns(tunnel, entry *cfv1alpha1.CloudflareTunnelDns) (resolvedDns, bool) {
	resolved := resolvedDns{
		Proxied: dnsProxiedDefault,
		Ttl:     dnsTtlAutomatic,
	}

	for _, level := range []*cfv1alpha1.CloudflareTunnelDns{tunnel, entry} {
		if level == nil {
			continue
		}
		if level.ZoneId != "" {
			resolved.ZoneId = level.ZoneId
		}
		if level.Proxied != nil {
			resolved.Proxied = *level.Proxied
		}
		if level.Ttl != nil {
			resolved.Ttl = *level.Ttl
		}
	}

	return resolved, resolved.ZoneId != ""
}
