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
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
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
