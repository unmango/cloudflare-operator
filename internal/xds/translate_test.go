package xds_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	"github.com/unmango/cloudflare-operator/internal/gateway"
	"github.com/unmango/cloudflare-operator/internal/xds"
)

func httpListener(name string, port gatewayv1.PortNumber, hostname string) gatewayv1.Listener {
	l := gatewayv1.Listener{Name: gatewayv1.SectionName(name), Port: port, Protocol: gatewayv1.HTTPProtocolType}
	if hostname != "" {
		h := gatewayv1.Hostname(hostname)
		l.Hostname = &h
	}

	return l
}

func listenersOf(ls ...gatewayv1.Listener) []gateway.Listener {
	return gateway.Listeners(&gatewayv1.Gateway{Spec: gatewayv1.GatewaySpec{Listeners: ls}})
}

func resources[T types.Resource](snapshot interface {
	GetResources(string) map[string]types.Resource
}, typeURL string) map[string]T {
	out := map[string]T{}
	for name, r := range snapshot.GetResources(typeURL) {
		out[name] = r.(T)
	}

	return out
}

var _ = Describe("Snapshot", func() {
	It("should group listeners by port into one listener and route configuration each", func() {
		snapshot, err := xds.Snapshot(listenersOf(
			httpListener("a", 80, "a.example.com"),
			httpListener("b", 80, "*.example.com"),
			httpListener("c", 8080, ""),
		))
		Expect(err).NotTo(HaveOccurred())

		listeners := resources[*listenerv3.Listener](snapshot, resourcev3.ListenerType)
		Expect(listeners).To(HaveKey("http-80"))
		Expect(listeners).To(HaveKey("http-8080"))
		Expect(listeners["http-80"].GetAddress().GetSocketAddress().GetPortValue()).To(Equal(uint32(10080)))
		Expect(listeners["http-8080"].GetAddress().GetSocketAddress().GetPortValue()).To(Equal(uint32(8080)))
		for _, l := range listeners {
			Expect(l.ValidateAll()).To(Succeed())
		}

		routes := resources[*routev3.RouteConfiguration](snapshot, resourcev3.RouteType)
		Expect(routes).To(HaveLen(2))
		Expect(routes["http-80"].ValidateAll()).To(Succeed())

		domains := map[string][]string{}
		for _, vh := range routes["http-80"].GetVirtualHosts() {
			domains[vh.GetName()] = vh.GetDomains()
		}
		Expect(domains).To(Equal(map[string][]string{
			"a": {"a.example.com"},
			"b": {"*.example.com"},
		}))
		Expect(routes["http-8080"].GetVirtualHosts()[0].GetDomains()).To(Equal([]string{"*"}))
	})

	It("should leave out listeners that are not valid", func() {
		snapshot, err := xds.Snapshot(listenersOf(
			httpListener("a", 80, ""),
			httpListener("b", 80, ""),
			gatewayv1.Listener{Name: "tls", Port: 443, Protocol: gatewayv1.TLSProtocolType},
		))
		Expect(err).NotTo(HaveOccurred())

		Expect(snapshot.GetResources(resourcev3.ListenerType)).To(BeEmpty())
		Expect(snapshot.GetResources(resourcev3.RouteType)).To(BeEmpty())
	})

	It("should version a snapshot by its content", func() {
		build := func(hostname string) string {
			GinkgoHelper()
			snapshot, err := xds.Snapshot(listenersOf(httpListener("a", 80, hostname)))
			Expect(err).NotTo(HaveOccurred())

			return snapshot.GetVersion(resourcev3.ListenerType)
		}

		Expect(build("a.example.com")).To(Equal(build("a.example.com")))
		Expect(build("a.example.com")).NotTo(Equal(build("b.example.com")))
	})
})
