package xds_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	"github.com/unmango/cloudflare-operator/internal/gateway"
	"github.com/unmango/cloudflare-operator/internal/xds"
)

const testNamespace = "default"

func httpListener(name string, port gatewayv1.PortNumber, hostname string) gatewayv1.Listener {
	l := gatewayv1.Listener{Name: gatewayv1.SectionName(name), Port: port, Protocol: gatewayv1.HTTPProtocolType}
	if hostname != "" {
		h := gatewayv1.Hostname(hostname)
		l.Hostname = &h
	}

	return l
}

func testGateway(ls ...gatewayv1.Listener) *gatewayv1.Gateway {
	return &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: testNamespace},
		Spec:       gatewayv1.GatewaySpec{Listeners: ls},
	}
}

// testRefs holds one Service, backend, on port 8080, with one ready endpoint.
func testRefs(ip string, port int32) *gateway.References {
	key := ktypes.NamespacedName{Namespace: testNamespace, Name: "backend"}
	return &gateway.References{
		Services: map[ktypes.NamespacedName]*corev1.Service{
			key: {
				ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
				Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8080}}},
			},
		},
		Endpoints: map[ktypes.NamespacedName][]discoveryv1.EndpointSlice{
			key: {{
				AddressType: discoveryv1.AddressTypeIPv4,
				Ports:       []discoveryv1.EndpointPort{{Name: new("http"), Port: new(port)}},
				Endpoints: []discoveryv1.Endpoint{
					{Addresses: []string{ip}},
					{Addresses: []string{"10.0.0.99"}, Conditions: discoveryv1.EndpointConditions{Ready: new(false)}},
				},
			}},
		},
	}
}

type routeOption func(*gatewayv1.HTTPRoute)

func withHostnames(hosts ...string) routeOption {
	return func(r *gatewayv1.HTTPRoute) {
		for _, h := range hosts {
			r.Spec.Hostnames = append(r.Spec.Hostnames, gatewayv1.Hostname(h))
		}
	}
}

func withRule(rule gatewayv1.HTTPRouteRule) routeOption {
	return func(r *gatewayv1.HTTPRoute) { r.Spec.Rules = append(r.Spec.Rules, rule) }
}

// backendRef refers to port 8080 of the named Service.
func backendRef(name string, weight int32) gatewayv1.HTTPBackendRef {
	p := gatewayv1.PortNumber(8080)
	return gatewayv1.HTTPBackendRef{BackendRef: gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{Name: gatewayv1.ObjectName(name), Port: &p},
		Weight:                 &weight,
	}}
}

func prefix(path string) gatewayv1.HTTPRouteMatch {
	return gatewayv1.HTTPRouteMatch{Path: &gatewayv1.HTTPPathMatch{Type: new(gatewayv1.PathMatchPathPrefix), Value: new(path)}}
}

func exact(path string) gatewayv1.HTTPRouteMatch {
	return gatewayv1.HTTPRouteMatch{Path: &gatewayv1.HTTPPathMatch{Type: new(gatewayv1.PathMatchExact), Value: new(path)}}
}

func httpRoute(name string, created time.Time, opts ...routeOption) gateway.Route {
	r := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, CreationTimestamp: metav1.NewTime(created)},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "gw"}}},
		},
	}
	for _, opt := range opts {
		opt(r)
	}

	return gateway.FromHTTPRoute(r)
}

func snapshotOf(gw *gatewayv1.Gateway, refs *gateway.References, routes ...gateway.Route) *cachev3.Snapshot {
	GinkgoHelper()
	snapshot, err := xds.Snapshot(gateway.Build(gw, routes, refs), refs)
	Expect(err).NotTo(HaveOccurred())

	return snapshot
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

func routeNames(vh *routev3.VirtualHost) []string {
	names := make([]string, 0, len(vh.GetRoutes()))
	for _, r := range vh.GetRoutes() {
		names = append(names, r.GetName())
	}

	return names
}

var _ = Describe("Snapshot", func() {
	now := time.Now()

	It("should give each HTTP port one listener and route configuration", func() {
		snapshot := snapshotOf(testGateway(
			httpListener("a", 80, "a.example.com"),
			httpListener("b", 80, "*.example.com"),
			httpListener("c", 8080, ""),
		), nil)

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
		// Without routes every request is a 404.
		Expect(routes["http-80"].GetVirtualHosts()).To(BeEmpty())
		// A Host header with a port still matches, and reaches the backend intact.
		Expect(routes["http-80"].GetIgnorePortInHostMatching()).To(BeTrue())
		hcm := &hcmv3.HttpConnectionManager{}
		Expect(listeners["http-80"].GetFilterChains()[0].GetFilters()[0].GetTypedConfig().UnmarshalTo(hcm)).To(Succeed())
		Expect(hcm.GetStripAnyHostPort()).To(BeFalse())
	})

	It("should leave out listeners that are not valid", func() {
		snapshot := snapshotOf(testGateway(
			httpListener("a", 80, ""),
			httpListener("b", 80, ""),
			gatewayv1.Listener{Name: "tls", Port: 443, Protocol: gatewayv1.TLSProtocolType},
		), nil)

		Expect(snapshot.GetResources(resourcev3.ListenerType)).To(BeEmpty())
		Expect(snapshot.GetResources(resourcev3.RouteType)).To(BeEmpty())
	})

	It("should give each hostname a route serves a virtual host", func() {
		snapshot := snapshotOf(testGateway(
			httpListener("wild", 80, "*.example.com"),
			httpListener("any", 8080, ""),
		), testRefs("10.0.0.1", 9000),
			httpRoute("a", now, withHostnames("a.example.com", "other.org"), withRule(gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
			httpRoute("all", now, withRule(gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
		)

		routes := resources[*routev3.RouteConfiguration](snapshot, resourcev3.RouteType)
		domains := func(name string) []string {
			var out []string
			for _, vh := range routes[name].GetVirtualHosts() {
				out = append(out, vh.GetDomains()...)
			}
			return out
		}

		// other.org does not match *.example.com, and a route without
		// hostnames takes the listener's.
		Expect(domains("http-80")).To(ConsistOf("*.example.com", "a.example.com"))
		Expect(domains("http-8080")).To(ConsistOf("*", "a.example.com", "other.org"))
		for _, r := range routes {
			Expect(r.ValidateAll()).To(Succeed())
		}
	})

	It("should order matches by precedence across routes", func() {
		older := now.Add(-time.Hour)
		snapshot := snapshotOf(testGateway(httpListener("http", 80, "")), testRefs("10.0.0.1", 9000),
			httpRoute("b-new", now, withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{prefix("/"), prefix("/foo/"), exact("/foo")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
			httpRoute("a-old", older, withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{prefix("/foo")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
		)

		routes := resources[*routev3.RouteConfiguration](snapshot, resourcev3.RouteType)
		vh := routes["http-80"].GetVirtualHosts()[0]
		Expect(routeNames(vh)).To(Equal([]string{
			"HTTPRoute/default/b-new/0/2", // exact
			"HTTPRoute/default/a-old/0/0", // /foo, the older route
			"HTTPRoute/default/b-new/0/1", // /foo/, the same prefix
			"HTTPRoute/default/b-new/0/0", // /
		}))
		Expect(vh.GetRoutes()[1].GetMatch().GetPathSeparatedPrefix()).To(Equal("/foo"))
		Expect(vh.GetRoutes()[3].GetMatch().GetPrefix()).To(Equal("/"))
	})

	It("should fall through to a route with a less specific hostname", func() {
		snapshot := snapshotOf(testGateway(httpListener("http", 80, "")), testRefs("10.0.0.1", 9000),
			httpRoute("wild", now, withHostnames("*.example.com"), withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{prefix("/api")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
			httpRoute("web", now, withHostnames("web.example.com"), withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{prefix("/web")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
			httpRoute("any", now, withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{exact("/health")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
		)

		const anyHealth = "HTTPRoute/default/any/0/0"

		routes := resources[*routev3.RouteConfiguration](snapshot, resourcev3.RouteType)
		byDomain := map[string]*routev3.VirtualHost{}
		for _, vh := range routes["http-80"].GetVirtualHosts() {
			byDomain[vh.GetDomains()[0]] = vh
		}

		// A request for web.example.com/api matches no rule of the route for
		// web.example.com, so the wildcard route serves it, and an exact path
		// on the catch-all route still comes after both hostnames.
		Expect(routeNames(byDomain["web.example.com"])).To(Equal([]string{
			"HTTPRoute/default/web/0/0",
			"HTTPRoute/default/wild/0/0",
			anyHealth,
		}))
		Expect(routeNames(byDomain["*.example.com"])).To(Equal([]string{
			"HTTPRoute/default/wild/0/0",
			anyHealth,
		}))
		Expect(routeNames(byDomain["*"])).To(Equal([]string{anyHealth}))
		Expect(routes["http-80"].ValidateAll()).To(Succeed())
	})

	It("should not fall through to a route of another listener on the port", func() {
		onListener := func(section string) routeOption {
			return func(r *gatewayv1.HTTPRoute) {
				r.Spec.ParentRefs = []gatewayv1.ParentReference{{Name: "gw", SectionName: new(gatewayv1.SectionName(section))}}
			}
		}
		snapshot := snapshotOf(testGateway(
			httpListener("specific", 80, "foo.example.com"),
			httpListener("wild", 80, "*.example.com"),
		), testRefs("10.0.0.1", 9000),
			httpRoute("s", now, onListener("specific"), withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{prefix("/s")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
			httpRoute("w", now, onListener("wild"), withRule(gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
		)

		routes := resources[*routev3.RouteConfiguration](snapshot, resourcev3.RouteType)
		byDomain := map[string]*routev3.VirtualHost{}
		for _, vh := range routes["http-80"].GetVirtualHosts() {
			byDomain[vh.GetDomains()[0]] = vh
		}

		// foo.example.com/other is a 404, not a request for the wildcard
		// listener's route.
		Expect(routeNames(byDomain["foo.example.com"])).To(Equal([]string{"HTTPRoute/default/s/0/0"}))
		Expect(routeNames(byDomain["*.example.com"])).To(Equal([]string{"HTTPRoute/default/w/0/0"}))
	})

	It("should send traffic to the ready endpoints of a Service port", func() {
		snapshot := snapshotOf(testGateway(httpListener("http", 80, "")), testRefs("10.0.0.1", 9000),
			httpRoute("r", now, withRule(gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
		)

		clusters := resources[*clusterv3.Cluster](snapshot, resourcev3.ClusterType)
		Expect(clusters).To(HaveKey("default/backend/8080"))
		Expect(clusters["default/backend/8080"].ValidateAll()).To(Succeed())

		endpoints := resources[*endpointv3.ClusterLoadAssignment](snapshot, resourcev3.EndpointType)
		lb := endpoints["default/backend/8080"].GetEndpoints()[0].GetLbEndpoints()
		Expect(lb).To(HaveLen(1))
		addr := lb[0].GetEndpoint().GetAddress().GetSocketAddress()
		Expect(addr.GetAddress()).To(Equal("10.0.0.1"))
		Expect(addr.GetPortValue()).To(Equal(uint32(9000)))
	})

	It("should answer a 500 for the share of a backend that does not resolve", func() {
		snapshot := snapshotOf(testGateway(httpListener("http", 80, "")), testRefs("10.0.0.1", 9000),
			httpRoute("split", now, withRule(gatewayv1.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{prefix("/split")},
				BackendRefs: []gatewayv1.HTTPBackendRef{
					backendRef("backend", 3),
					backendRef("missing", 1),
				},
			}), withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{prefix("/missing")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("missing", 1)},
			}), withRule(gatewayv1.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{prefix("/none")},
			})),
		)

		vh := resources[*routev3.RouteConfiguration](snapshot, resourcev3.RouteType)["http-80"].GetVirtualHosts()[0]
		byName := map[string]*routev3.Route{}
		for _, r := range vh.GetRoutes() {
			byName[r.GetName()] = r
		}

		split := byName["HTTPRoute/default/split/0/0"].GetRoute()
		Expect(split.GetClusterNotFoundResponseCode()).To(Equal(routev3.RouteAction_INTERNAL_SERVER_ERROR))
		weighted := split.GetWeightedClusters().GetClusters()
		Expect(weighted).To(HaveLen(2))
		Expect(weighted[0].GetName()).To(Equal("default/backend/8080"))
		Expect(weighted[0].GetWeight().GetValue()).To(Equal(uint32(3)))
		Expect(weighted[1].GetWeight().GetValue()).To(Equal(uint32(1)))
		Expect(byName["HTTPRoute/default/split/1/0"].GetDirectResponse().GetStatus()).To(Equal(uint32(500)))
		Expect(byName["HTTPRoute/default/split/2/0"].GetDirectResponse().GetStatus()).To(Equal(uint32(500)))
		Expect(snapshot.Consistent()).To(Succeed())
	})

	It("should modify request headers", func() {
		snapshot := snapshotOf(testGateway(httpListener("http", 80, "")), testRefs("10.0.0.1", 9000),
			httpRoute("r", now, withRule(gatewayv1.HTTPRouteRule{
				Filters: []gatewayv1.HTTPRouteFilter{{
					Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
					RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
						Set:    []gatewayv1.HTTPHeader{{Name: "X-Set", Value: "1"}},
						Add:    []gatewayv1.HTTPHeader{{Name: "X-Add", Value: "2"}},
						Remove: []string{"X-Remove"},
					},
				}},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
		)

		r := resources[*routev3.RouteConfiguration](snapshot, resourcev3.RouteType)["http-80"].GetVirtualHosts()[0].GetRoutes()[0]
		Expect(r.GetRequestHeadersToAdd()).To(HaveLen(2))
		Expect(r.GetRequestHeadersToRemove()).To(Equal([]string{"X-Remove"}))
		Expect(r.ValidateAll()).To(Succeed())
	})

	It("should version a snapshot by its content", func() {
		build := func(port gatewayv1.PortNumber) string {
			GinkgoHelper()
			return snapshotOf(testGateway(httpListener("a", port, "")), nil).GetVersion(resourcev3.ListenerType)
		}

		Expect(build(80)).To(Equal(build(80)))
		Expect(build(80)).NotTo(Equal(build(8080)))
	})
})

// certName names the Secret withCert adds.
const certName = "cert"

// withCert lets refs read a TLS Secret named certName holding the given
// certificate and key.
func withCert(refs *gateway.References, certPEM, keyPEM []byte) {
	refs.SecretsReadable = true
	refs.Secrets = map[ktypes.NamespacedName]*corev1.Secret{
		{Namespace: testNamespace, Name: certName}: {
			Type: corev1.SecretTypeTLS,
			Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM},
		},
	}
}

// l4Backend refers to port 8080 of the named Service with the given weight.
func l4Backend(name string, weight int32) gatewayv1.BackendRef {
	return backendRef(name, weight).BackendRef
}

func l4Meta(name string, created time.Time) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: testNamespace, CreationTimestamp: metav1.NewTime(created)}
}

var parentGW = gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "gw"}}}

func tlsRoute(name string, created time.Time, hosts []string, backends ...gatewayv1.BackendRef) gateway.Route {
	r := &gatewayv1.TLSRoute{
		ObjectMeta: l4Meta(name, created),
		Spec: gatewayv1.TLSRouteSpec{
			CommonRouteSpec: parentGW,
			Rules:           []gatewayv1.TLSRouteRule{{BackendRefs: backends}},
		},
	}
	for _, h := range hosts {
		r.Spec.Hostnames = append(r.Spec.Hostnames, gatewayv1.Hostname(h))
	}

	return gateway.FromTLSRoute(r)
}

func tcpRoute(name string, created time.Time, backends ...gatewayv1.BackendRef) gateway.Route {
	return gateway.FromTCPRoute(&gatewayv1.TCPRoute{
		ObjectMeta: l4Meta(name, created),
		Spec: gatewayv1.TCPRouteSpec{
			CommonRouteSpec: parentGW,
			Rules:           []gatewayv1.TCPRouteRule{{BackendRefs: backends}},
		},
	})
}

func udpRoute(name string, created time.Time, backends ...gatewayv1.BackendRef) gateway.Route {
	return gateway.FromUDPRoute(&gatewayv1.UDPRoute{
		ObjectMeta: l4Meta(name, created),
		Spec: gatewayv1.UDPRouteSpec{
			CommonRouteSpec: parentGW,
			Rules:           []gatewayv1.UDPRouteRule{{BackendRefs: backends}},
		},
	})
}

func tlsListener(name string, port gatewayv1.PortNumber, hostname string, mode gatewayv1.TLSModeType) gatewayv1.Listener {
	l := httpListener(name, port, hostname)
	l.Protocol = gatewayv1.TLSProtocolType
	l.TLS = &gatewayv1.ListenerTLSConfig{Mode: &mode}
	if mode == gatewayv1.TLSModeTerminate {
		l.TLS.CertificateRefs = []gatewayv1.SecretObjectReference{{Name: certName}}
	}

	return l
}

func l4Listener(name string, port gatewayv1.PortNumber, protocol gatewayv1.ProtocolType) gatewayv1.Listener {
	return gatewayv1.Listener{Name: gatewayv1.SectionName(name), Port: port, Protocol: protocol}
}

// withSecondBackend adds a Service backend2 to refs, on port 8080 with one
// ready endpoint at ip:port.
func withSecondBackend(refs *gateway.References, ip string, port int32) *gateway.References {
	key := ktypes.NamespacedName{Namespace: testNamespace, Name: "backend2"}
	refs.Services[key] = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8080}}},
	}
	refs.Endpoints[key] = []discoveryv1.EndpointSlice{{
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: new("http"), Port: new(port)}},
		Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{ip}}},
	}}

	return refs
}

func tcpProxyOf(f *listenerv3.Filter) *tcpproxyv3.TcpProxy {
	GinkgoHelper()
	proxy := &tcpproxyv3.TcpProxy{}
	Expect(f.GetTypedConfig().UnmarshalTo(proxy)).To(Succeed())

	return proxy
}

var _ = Describe("Layer 4 snapshot", func() {
	now := time.Now()

	It("should give each server name on a TLS port a filter chain", func() {
		snapshot := snapshotOf(testGateway(tlsListener("tls", 443, "", gatewayv1.TLSModePassthrough)), testRefs("10.0.0.1", 8443),
			tlsRoute("a", now, []string{"a.example.com"}, l4Backend("backend", 1)),
			tlsRoute("wild", now, []string{"*.example.com"}, l4Backend("backend", 1)),
			tlsRoute("newer", now.Add(time.Minute), []string{"a.example.com"}, l4Backend("missing", 1)),
		)

		l := resources[*listenerv3.Listener](snapshot, resourcev3.ListenerType)["tls-443"]
		Expect(l.ValidateAll()).To(Succeed())
		Expect(l.GetAddress().GetSocketAddress().GetPortValue()).To(Equal(uint32(10443)))
		Expect(l.GetFilterChains()).To(HaveLen(2))

		byName := map[string]*listenerv3.FilterChain{}
		for _, c := range l.GetFilterChains() {
			byName[c.GetFilterChainMatch().GetServerNames()[0]] = c
			Expect(c.GetTransportSocket()).To(BeNil())
		}
		// The older route wins the name both claim.
		Expect(tcpProxyOf(byName["a.example.com"].GetFilters()[0]).GetCluster()).To(Equal("default/backend/8080"))
		Expect(byName).To(HaveKey("*.example.com"))
	})

	It("should terminate TLS on a chain whose listener says so", func() {
		certPEM, keyPEM := selfSigned("t.example.com")
		refs := testRefs("10.0.0.1", 3000)
		withCert(refs, certPEM, keyPEM)

		snapshot := snapshotOf(testGateway(
			tlsListener("terminate", 8443, "t.example.com", gatewayv1.TLSModeTerminate),
			tlsListener("passthrough", 8443, "p.example.com", gatewayv1.TLSModePassthrough),
		), refs,
			tlsRoute("t", now, nil, l4Backend("backend", 1)),
		)

		l := resources[*listenerv3.Listener](snapshot, resourcev3.ListenerType)["tls-8443"]
		Expect(l.ValidateAll()).To(Succeed())
		// The route attaches to both listeners, and serves each one's name.
		Expect(l.GetFilterChains()).To(HaveLen(2))
		for _, c := range l.GetFilterChains() {
			if c.GetFilterChainMatch().GetServerNames()[0] == "t.example.com" {
				Expect(c.GetTransportSocket()).NotTo(BeNil())
			} else {
				Expect(c.GetTransportSocket()).To(BeNil())
			}
		}
	})

	It("should send a TCP port to the oldest route's backends by weight", func() {
		snapshot := snapshotOf(testGateway(l4Listener("tcp", 9000, gatewayv1.TCPProtocolType)),
			withSecondBackend(testRefs("10.0.0.1", 3000), "10.0.0.2", 3000),
			tcpRoute("old", now, l4Backend("backend", 3), l4Backend("backend2", 1)),
			tcpRoute("new", now.Add(time.Minute), l4Backend("backend2", 1)),
		)

		l := resources[*listenerv3.Listener](snapshot, resourcev3.ListenerType)["tcp-9000"]
		Expect(l.ValidateAll()).To(Succeed())
		weights := map[string]uint32{}
		for _, c := range tcpProxyOf(l.GetFilterChains()[0].GetFilters()[0]).GetWeightedClusters().GetClusters() {
			weights[c.GetName()] = c.GetWeight()
		}
		Expect(weights).To(Equal(map[string]uint32{"default/backend/8080": 3, "default/backend2/8080": 1}))
	})

	It("should give a UDPRoute with several backends a cluster weighted by locality", func() {
		snapshot := snapshotOf(testGateway(
			l4Listener("udp", 53, gatewayv1.UDPProtocolType),
			l4Listener("tcp", 53, gatewayv1.TCPProtocolType),
		), withSecondBackend(testRefs("10.0.0.1", 5353), "10.0.0.2", 5353),
			udpRoute("dns", now, l4Backend("backend", 1), l4Backend("backend2", 4)),
		)

		listeners := resources[*listenerv3.Listener](snapshot, resourcev3.ListenerType)
		// No TCPRoute is attached, so the TCP port is not bound.
		Expect(listeners).To(HaveLen(1))
		l := listeners["udp-53"]
		Expect(l.ValidateAll()).To(Succeed())
		Expect(l.GetAddress().GetSocketAddress().GetProtocol()).To(Equal(corev3.SocketAddress_UDP))

		clusters := resources[*clusterv3.Cluster](snapshot, resourcev3.ClusterType)
		Expect(clusters).To(HaveKey("udp/default/dns"))
		Expect(clusters["udp/default/dns"].GetCommonLbConfig().GetLocalityWeightedLbConfig()).NotTo(BeNil())

		assignment := resources[*endpointv3.ClusterLoadAssignment](snapshot, resourcev3.EndpointType)["udp/default/dns"]
		Expect(assignment.GetEndpoints()).To(HaveLen(2))
		Expect(assignment.GetEndpoints()[0].GetLoadBalancingWeight().GetValue()).To(Equal(uint32(1)))
		Expect(assignment.GetEndpoints()[1].GetLoadBalancingWeight().GetValue()).To(Equal(uint32(4)))
	})
})
