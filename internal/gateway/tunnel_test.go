package gateway_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/gateway"
)

const dbHostname = "db.example.com"

var _ = Describe("TunnelIngress", func() {
	const envoy = "gw-envoy.default.svc"

	passthrough := func(name string, port gatewayv1.PortNumber, hostname string) gatewayv1.Listener {
		l := listener(name, port, gatewayv1.TLSProtocolType, hostname)
		l.TLS = &gatewayv1.ListenerTLSConfig{Mode: new(gatewayv1.TLSModePassthrough)}
		return l
	}

	rule := func(hostname, service string) cfv1alpha1.CloudflareTunnelConfigIngress {
		return cfv1alpha1.CloudflareTunnelConfigIngress{Hostname: hostname, Service: service}
	}

	It("should answer 404 when there are no listeners", func() {
		Expect(gateway.TunnelIngress(nil, nil, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("", "http_status:404"),
		}))
	})

	It("should send the catch-all to a listener without a hostname", func() {
		listeners := gateway.Listeners(gatewayWith(listener("http", 80, gatewayv1.HTTPProtocolType, "")), nil)

		Expect(gateway.TunnelIngress(nil, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("", "http://gw-envoy.default.svc:80"),
		}))
	})

	It("should order hand-written rules, exact hostnames, wildcards, then the catch-all", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("wild", 80, gatewayv1.HTTPProtocolType, "*.example.com"),
			listener("exact", 80, gatewayv1.HTTPProtocolType, "app.example.com"),
			listener("alt", 8080, gatewayv1.HTTPProtocolType, "alt.example.com"),
		), nil)
		handwritten := []cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("ssh.example.com", "ssh://bastion:22"),
			rule("", "http_status:503"),
		}

		Expect(gateway.TunnelIngress(handwritten, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("ssh.example.com", "ssh://bastion:22"),
			rule("app.example.com", "http://gw-envoy.default.svc:80"),
			rule("alt.example.com", "http://gw-envoy.default.svc:8080"),
			rule("*.example.com", "http://gw-envoy.default.svc:80"),
			rule("", "http_status:404"),
		}))
	})

	It("should skip listeners that are not valid", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("tls", 443, gatewayv1.TLSProtocolType, "secure.example.com"),
		), nil)

		Expect(gateway.TunnelIngress(nil, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("", "http_status:404"),
		}))
	})

	It("should give a hostname on two ports the lower port", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("b", 8080, gatewayv1.HTTPProtocolType, "app.example.com"),
			listener("a", 80, gatewayv1.HTTPProtocolType, "app.example.com"),
		), nil)

		Expect(gateway.TunnelIngress(nil, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("app.example.com", "http://gw-envoy.default.svc:80"),
			rule("", "http_status:404"),
		}))
	})

	It("should reach a TLS listener over tcp://", func() {
		listeners := gateway.Listeners(gatewayWith(
			passthrough("tls", 443, "secure.example.com"),
			passthrough("wild", 8443, "*.example.com"),
		), nil)

		Expect(gateway.TunnelIngress(nil, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("secure.example.com", "tcp://gw-envoy.default.svc:443"),
			rule("*.example.com", "tcp://gw-envoy.default.svc:8443"),
			rule("", "http_status:404"),
		}))
	})

	It("should skip a TLS listener without a hostname", func() {
		listeners := gateway.Listeners(gatewayWith(passthrough("tls", 443, "")), nil)

		Expect(gateway.TunnelIngress(nil, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("", "http_status:404"),
		}))
	})

	It("should reach a TCP listener over tcp:// only when given a hostname", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("db", 5432, gatewayv1.TCPProtocolType, ""),
			listener("cache", 6379, gatewayv1.TCPProtocolType, ""),
		), nil)
		hostnames := map[gatewayv1.SectionName]string{"db": dbHostname}

		Expect(gateway.TunnelIngress(nil, listeners, hostnames, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule(dbHostname, "tcp://gw-envoy.default.svc:5432"),
			rule("", "http_status:404"),
		}))
	})

	It("should give a hostname an HTTP and a TLS listener share to HTTP", func() {
		listeners := gateway.Listeners(gatewayWith(
			passthrough("tls", 443, "app.example.com"),
			listener("http", 8080, gatewayv1.HTTPProtocolType, "app.example.com"),
		), nil)

		Expect(gateway.TunnelIngress(nil, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("app.example.com", "http://gw-envoy.default.svc:8080"),
			rule("", "http_status:404"),
		}))
	})

	It("should put a narrower wildcard ahead of one that covers it", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("web", 80, gatewayv1.HTTPProtocolType, "*.example.com"),
			passthrough("db", 5432, "*.db.example.com"),
		), nil)

		Expect(gateway.TunnelIngress(nil, listeners, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("*.db.example.com", "tcp://"+envoy+":5432"),
			rule("*.example.com", "http://"+envoy+":80"),
			rule("", "http_status:404"),
		}))
	})

	It("should give a UDP listener no rule", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("dns", 53, gatewayv1.UDPProtocolType, ""),
		), nil)
		hostnames := map[gatewayv1.SectionName]string{"dns": "dns.example.com"}

		Expect(gateway.TunnelIngress(nil, listeners, hostnames, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("", "http_status:404"),
		}))
	})
})

var _ = Describe("TunnelHostnames", func() {
	It("should read listener hostnames from the Gateway's annotations", func() {
		gw := gatewayWith()
		gw.Annotations = map[string]string{
			"tcp.tunnel.cloudflare.unmango.dev/db": dbHostname,
			"tcp.tunnel.cloudflare.unmango.dev/":   "ignored.example.com",
			"tcp.tunnel.cloudflare.unmango.dev/a":  "*",
			"tcp.tunnel.cloudflare.unmango.dev/b":  "not a hostname",
			"tcp.tunnel.cloudflare.unmango.dev/c":  "*.example.com",
			"example.com/db":                       "other.example.com",
		}

		Expect(gateway.TunnelHostnames(gw)).To(Equal(map[gatewayv1.SectionName]string{"db": dbHostname, "c": "*.example.com"}))
	})
})
