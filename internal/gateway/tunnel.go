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

// TunnelHostnamePrefix is the annotation prefix that gives a Gateway's TCP
// listeners a public hostname on its tunnel. A TCP listener has no hostname of
// its own, so `tcp.tunnel.cloudflare.unmango.dev/<listener>: db.example.com`
// supplies the one clients name to `cloudflared access tcp`.
const TunnelHostnamePrefix = "tcp.tunnel.cloudflare.unmango.dev/"

// TunnelHostnames reads the public hostnames gw's annotations give its TCP
// listeners, keyed by listener name.
func TunnelHostnames(gw *gatewayv1.Gateway) map[gatewayv1.SectionName]string {
	hostnames := map[gatewayv1.SectionName]string{}
	for key, value := range gw.Annotations {
		if name, ok := strings.CutPrefix(key, TunnelHostnamePrefix); ok && name != "" && value != "" {
			hostnames[gatewayv1.SectionName(name)] = value
		}
	}

	return hostnames
}

// TunnelIngress assembles the ingress rules of a tunnel provisioned for a
// Gateway. Hand-written rules come first, in their order, so they win over the
// Gateway. Then each valid listener with a public hostname gets a rule sending
// that hostname to Envoy, exact hostnames ahead of wildcards because cloudflared
// takes the first match. An HTTP listener is reached over http://. A TLS
// listener, or a TCP listener given a hostname by tcpHostnames, is reached over
// tcp://, which clients connect to with `cloudflared access tcp`; Envoy still
// sees the client's own bytes, so a TLS listener routes by server name as it
// would without the tunnel. A hostname two listeners claim goes to the HTTP
// listener, then to the lower port. The catch-all goes to the first HTTP
// listener without a hostname, or answers 404 when there is none. A catch-all
// among the hand-written rules would make every rule after it unreachable, so
// it is dropped. HTTPS and UDP listeners get no rule: Cloudflare terminates
// TLS for HTTP itself, and carries UDP only to WARP clients.
//
// envoyHost is the cluster DNS name of the Service in front of Envoy.
func TunnelIngress(handwritten []cfv1alpha1.CloudflareTunnelConfigIngress, listeners []Listener, tcpHostnames map[gatewayv1.SectionName]string, envoyHost string) []cfv1alpha1.CloudflareTunnelConfigIngress {
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

	route := func(host, service string) {
		if seen[host] {
			return
		}
		seen[host] = true

		rule := cfv1alpha1.CloudflareTunnelConfigIngress{Hostname: host, Service: service}
		if strings.HasPrefix(host, "*.") {
			wildcard = append(wildcard, rule)
		} else {
			exact = append(exact, rule)
		}
	}

	// Listeners are taken in port order so the rule a hostname on two ports gets
	// does not depend on spec order.
	valid := slices.DeleteFunc(slices.Clone(listeners), func(l Listener) bool { return !l.Valid })
	slices.SortStableFunc(valid, func(a, b Listener) int {
		return int(a.Port) - int(b.Port)
	})

	for _, l := range valid {
		if l.Protocol != gatewayv1.HTTPProtocolType {
			continue
		}

		service := fmt.Sprintf("http://%s:%d", envoyHost, l.Port)
		if l.Hostname == nil {
			if catchAll == notFoundService {
				catchAll = service
			}
			continue
		}

		route(string(*l.Hostname), service)
	}

	for _, l := range valid {
		var host string
		switch l.Protocol {
		case gatewayv1.TLSProtocolType:
			if l.Hostname != nil {
				host = string(*l.Hostname)
			}
		case gatewayv1.TCPProtocolType:
			host = tcpHostnames[l.Name]
		}
		if host == "" {
			continue
		}

		route(host, fmt.Sprintf("tcp://%s:%d", envoyHost, l.Port))
	}

	rules = append(rules, exact...)
	rules = append(rules, wildcard...)

	return append(rules, cfv1alpha1.CloudflareTunnelConfigIngress{Service: catchAll})
}
