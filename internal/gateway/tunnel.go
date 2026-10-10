package gateway

import (
	"fmt"
	"slices"
	"strings"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
)

// notFoundService is what cloudflared answers with when no rule matches.
const notFoundService = "http_status:404"

// TunnelIngress assembles the ingress rules of a tunnel provisioned for a
// Gateway. Hand-written rules come first, in their order, so they win over the
// Gateway. Then each valid HTTP listener with a hostname gets a rule sending
// that hostname to Envoy, exact hostnames ahead of wildcards because cloudflared
// takes the first match. The catch-all goes to the first HTTP listener without
// a hostname, or answers 404 when there is none. A catch-all among the
// hand-written rules would make every rule after it unreachable, so it is
// dropped.
//
// envoyHost is the cluster DNS name of the Service in front of Envoy.
func TunnelIngress(handwritten []cfv1alpha1.CloudflareTunnelConfigIngress, listeners []Listener, envoyHost string) []cfv1alpha1.CloudflareTunnelConfigIngress {
	rules := []cfv1alpha1.CloudflareTunnelConfigIngress{}
	for _, rule := range handwritten {
		if rule.Hostname == "" && rule.Path == "" {
			continue
		}
		rules = append(rules, rule)
	}

	var exact, wildcard []cfv1alpha1.CloudflareTunnelConfigIngress
	seen := map[string]bool{}
	catchAll := notFoundService

	http := slices.DeleteFunc(slices.Clone(listeners), func(l Listener) bool {
		return !l.Valid || l.Protocol != gatewayv1.HTTPProtocolType
	})
	// Listeners are taken in port order so the rule a hostname on two ports gets
	// does not depend on spec order.
	slices.SortStableFunc(http, func(a, b Listener) int {
		return int(a.Port) - int(b.Port)
	})

	for _, l := range http {
		service := fmt.Sprintf("http://%s:%d", envoyHost, l.Port)
		if l.Hostname == nil {
			if catchAll == notFoundService {
				catchAll = service
			}
			continue
		}

		host := string(*l.Hostname)
		if seen[host] {
			continue
		}
		seen[host] = true

		rule := cfv1alpha1.CloudflareTunnelConfigIngress{Hostname: host, Service: service}
		if strings.HasPrefix(host, "*.") {
			wildcard = append(wildcard, rule)
		} else {
			exact = append(exact, rule)
		}
	}

	rules = append(rules, exact...)
	rules = append(rules, wildcard...)

	return append(rules, cfv1alpha1.CloudflareTunnelConfigIngress{Service: catchAll})
}
