package gateway_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/gateway"
)

var _ = Describe("TunnelIngress", func() {
	const envoy = "gw-envoy.default.svc"

	rule := func(hostname, service string) cfv1alpha1.CloudflareTunnelConfigIngress {
		return cfv1alpha1.CloudflareTunnelConfigIngress{Hostname: hostname, Service: service}
	}

	It("should answer 404 when there are no listeners", func() {
		Expect(gateway.TunnelIngress(nil, nil, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("", "http_status:404"),
		}))
	})

	It("should send the catch-all to a listener without a hostname", func() {
		listeners := gateway.Listeners(gatewayWith(listener("http", 80, gatewayv1.HTTPProtocolType, "")), nil)

		Expect(gateway.TunnelIngress(nil, listeners, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
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

		Expect(gateway.TunnelIngress(handwritten, listeners, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
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

		Expect(gateway.TunnelIngress(nil, listeners, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("", "http_status:404"),
		}))
	})

	It("should give a hostname on two ports the lower port", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("b", 8080, gatewayv1.HTTPProtocolType, "app.example.com"),
			listener("a", 80, gatewayv1.HTTPProtocolType, "app.example.com"),
		), nil)

		Expect(gateway.TunnelIngress(nil, listeners, envoy)).To(Equal([]cfv1alpha1.CloudflareTunnelConfigIngress{
			rule("app.example.com", "http://gw-envoy.default.svc:80"),
			rule("", "http_status:404"),
		}))
	})
})
