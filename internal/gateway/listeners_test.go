package gateway_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

func listener(name string, port gatewayv1.PortNumber, protocol gatewayv1.ProtocolType, hostname string) gatewayv1.Listener {
	l := gatewayv1.Listener{
		Name:     gatewayv1.SectionName(name),
		Port:     port,
		Protocol: protocol,
	}
	if hostname != "" {
		h := gatewayv1.Hostname(hostname)
		l.Hostname = &h
	}

	return l
}

func gatewayWith(listeners ...gatewayv1.Listener) *gatewayv1.Gateway {
	return &gatewayv1.Gateway{Spec: gatewayv1.GatewaySpec{Listeners: listeners}}
}

func condition(l gateway.Listener, t gatewayv1.ListenerConditionType) *metav1.Condition {
	return meta.FindStatusCondition(l.Conditions, string(t))
}

var _ = Describe("Listeners", func() {
	It("should accept an HTTP listener and support HTTPRoute on it", func() {
		listeners := gateway.Listeners(gatewayWith(listener("http", 80, gatewayv1.HTTPProtocolType, "")), nil)

		Expect(listeners).To(HaveLen(1))
		l := listeners[0]
		Expect(l.Valid).To(BeTrue())
		Expect(condition(l, gatewayv1.ListenerConditionAccepted).Status).To(Equal(metav1.ConditionTrue))
		Expect(condition(l, gatewayv1.ListenerConditionResolvedRefs).Status).To(Equal(metav1.ConditionTrue))
		Expect(condition(l, gatewayv1.ListenerConditionConflicted).Status).To(Equal(metav1.ConditionFalse))
		Expect(l.SupportedKinds).To(HaveLen(2))
		Expect(string(l.SupportedKinds[0].Kind)).To(Equal("HTTPRoute"))
		Expect(string(l.SupportedKinds[1].Kind)).To(Equal("GRPCRoute"))
		Expect(string(*l.SupportedKinds[0].Group)).To(Equal(gatewayv1.GroupName))
	})

	It("should not accept a protocol it does not know", func() {
		l := gateway.Listeners(gatewayWith(listener("l", 443, "INVALID", "")), nil)[0]

		Expect(l.Valid).To(BeFalse())
		accepted := condition(l, gatewayv1.ListenerConditionAccepted)
		Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
		Expect(accepted.Reason).To(Equal(string(gatewayv1.ListenerReasonUnsupportedProtocol)))
		Expect(l.SupportedKinds).To(BeEmpty())
	})

	DescribeTable("should support one route kind on a layer 4 listener",
		func(protocol gatewayv1.ProtocolType, kind string) {
			l := listener("l", 443, protocol, "")
			if protocol == gatewayv1.TLSProtocolType {
				l.TLS = &gatewayv1.ListenerTLSConfig{Mode: new(gatewayv1.TLSModePassthrough)}
			}

			out := gateway.Listeners(gatewayWith(l), nil)[0]

			Expect(out.Valid).To(BeTrue())
			Expect(out.SupportedKinds).To(HaveLen(1))
			Expect(string(out.SupportedKinds[0].Kind)).To(Equal(kind))
		},
		Entry("TLS", gatewayv1.TLSProtocolType, "TLSRoute"),
		Entry("TCP", gatewayv1.TCPProtocolType, "TCPRoute"),
		Entry("UDP", gatewayv1.UDPProtocolType, "UDPRoute"),
	)

	It("should not accept a TLS listener without tls", func() {
		l := gateway.Listeners(gatewayWith(listener("tls", 443, gatewayv1.TLSProtocolType, "")), nil)[0]

		Expect(l.Valid).To(BeFalse())
		Expect(condition(l, gatewayv1.ListenerConditionAccepted).Reason).To(Equal(string(gatewayv1.ListenerReasonUnsupportedValue)))
	})

	It("should need certificates for a TLS listener that terminates", func() {
		l := listener("tls", 443, gatewayv1.TLSProtocolType, "")
		l.TLS = &gatewayv1.ListenerTLSConfig{Mode: new(gatewayv1.TLSModeTerminate)}

		out := gateway.Listeners(gatewayWith(l), nil)[0]

		Expect(out.Valid).To(BeFalse())
		Expect(condition(out, gatewayv1.ListenerConditionResolvedRefs).Reason).To(Equal(string(gatewayv1.ListenerReasonInvalidCertificateRef)))
	})

	It("should mark two TCP listeners on one port as conflicted whatever their hostnames", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("a", 9000, gatewayv1.TCPProtocolType, "a.example.com"),
			listener("b", 9000, gatewayv1.TCPProtocolType, "b.example.com"),
		), nil)

		Expect(listeners[0].Valid).To(BeFalse())
		Expect(listeners[1].Valid).To(BeFalse())
	})

	It("should keep the supported kinds of allowedRoutes and reject the rest", func() {
		l := listener("http", 80, gatewayv1.HTTPProtocolType, "")
		core := gatewayv1.Group("")
		l.AllowedRoutes = &gatewayv1.AllowedRoutes{Kinds: []gatewayv1.RouteGroupKind{
			{Kind: "HTTPRoute"},
			{Kind: "TCPRoute"},
			{Group: &core, Kind: "Service"},
		}}

		out := gateway.Listeners(gatewayWith(l), nil)[0]

		Expect(out.Valid).To(BeFalse())
		resolved := condition(out, gatewayv1.ListenerConditionResolvedRefs)
		Expect(resolved.Status).To(Equal(metav1.ConditionFalse))
		Expect(resolved.Reason).To(Equal(string(gatewayv1.ListenerReasonInvalidRouteKinds)))
		Expect(resolved.Message).To(ContainSubstring("TCPRoute"))
		Expect(resolved.Message).To(ContainSubstring("core/Service"))
		Expect(out.SupportedKinds).To(HaveLen(1))
		Expect(string(out.SupportedKinds[0].Kind)).To(Equal("HTTPRoute"))
	})

	It("should mark listeners on one port with one hostname as conflicted", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("a", 80, gatewayv1.HTTPProtocolType, "example.com"),
			listener("b", 80, gatewayv1.HTTPProtocolType, "example.com"),
			listener("c", 80, gatewayv1.HTTPProtocolType, "other.example.com"),
		), nil)

		for _, l := range listeners[:2] {
			conflicted := condition(l, gatewayv1.ListenerConditionConflicted)
			Expect(conflicted.Status).To(Equal(metav1.ConditionTrue))
			Expect(conflicted.Reason).To(Equal(string(gatewayv1.ListenerReasonHostnameConflict)))
			Expect(l.Valid).To(BeFalse())
		}
		Expect(listeners[2].Valid).To(BeTrue())
	})

	It("should treat two listeners without a hostname on one port as conflicted", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("a", 80, gatewayv1.HTTPProtocolType, ""),
			listener("b", 80, gatewayv1.HTTPProtocolType, ""),
		), nil)

		Expect(listeners[0].Valid).To(BeFalse())
		Expect(listeners[1].Valid).To(BeFalse())
	})

	It("should mark listeners with different protocols on one port as conflicted", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("http", 8080, gatewayv1.HTTPProtocolType, ""),
			listener("tcp", 8080, gatewayv1.TCPProtocolType, ""),
		), nil)

		conflicted := condition(listeners[0], gatewayv1.ListenerConditionConflicted)
		Expect(conflicted.Status).To(Equal(metav1.ConditionTrue))
		Expect(conflicted.Reason).To(Equal(string(gatewayv1.ListenerReasonProtocolConflict)))
		Expect(listeners[0].Valid).To(BeFalse())
	})

	It("should not treat TCP and UDP on one port as conflicted", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("tcp", 53, gatewayv1.TCPProtocolType, ""),
			listener("udp", 53, gatewayv1.UDPProtocolType, ""),
		), nil)

		Expect(condition(listeners[0], gatewayv1.ListenerConditionConflicted).Status).To(Equal(metav1.ConditionFalse))
		Expect(condition(listeners[1], gatewayv1.ListenerConditionConflicted).Status).To(Equal(metav1.ConditionFalse))
	})

	It("should reject a port Envoy reserves", func() {
		l := gateway.Listeners(gatewayWith(listener("admin", 19001, gatewayv1.HTTPProtocolType, "")), nil)[0]

		Expect(l.Valid).To(BeFalse())
		Expect(condition(l, gatewayv1.ListenerConditionAccepted).Reason).To(Equal(string(gatewayv1.ListenerReasonPortUnavailable)))
	})

	It("should reject a privileged port whose moved port another listener uses", func() {
		listeners := gateway.Listeners(gatewayWith(
			listener("http", 80, gatewayv1.HTTPProtocolType, ""),
			listener("high", 10080, gatewayv1.HTTPProtocolType, ""),
		), nil)

		Expect(listeners[0].Valid).To(BeFalse())
		Expect(condition(listeners[0], gatewayv1.ListenerConditionAccepted).Reason).To(Equal(string(gatewayv1.ListenerReasonPortUnavailable)))
		Expect(listeners[1].Valid).To(BeTrue())
	})
})
