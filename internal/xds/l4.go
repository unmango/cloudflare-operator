package xds

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	xdscorev3 "github.com/cncf/xds/go/xds/core/v3"
	xdsmatcherv3 "github.com/cncf/xds/go/xds/type/matcher/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tlsinspectorv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	tcpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	udpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

// A TLSRoute, TCPRoute or UDPRoute has nothing to match a connection on beyond
// the listener and, for TLS, the server name. When several routes claim the
// same connections, the oldest wins, then the first by namespace and name.
func oldest(routes []*gateway.AttachedRoute) *gateway.AttachedRoute {
	return slices.MinFunc(routes, func(a, b *gateway.AttachedRoute) int {
		return cmp.Or(
			a.CreationTimestamp.Compare(b.CreationTimestamp.Time),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})
}

// attachedTo lists the routes attached to listener i, with the hostnames each
// serves there.
func attachedTo(m *gateway.Model, i int) map[*gateway.AttachedRoute][]string {
	out := map[*gateway.AttachedRoute][]string{}
	for r := range m.Routes {
		route := &m.Routes[r]
		for _, parent := range route.Parents {
			for _, host := range parent.Hostnames[i] {
				if !slices.Contains(out[route], host) {
					out[route] = append(out[route], host)
				}
			}
		}
	}

	return out
}

// backendsOf returns the backends of a route's one rule.
func backendsOf(route *gateway.AttachedRoute) []gateway.Backend {
	if len(route.Backends) == 0 {
		return nil
	}

	return route.Backends[0]
}

// tlsListener serves the TLS listeners on one port. Each server name a route
// serves gets a filter chain chosen by SNI, which forwards the connection to
// the route's backends either as it arrives or, when its listener terminates
// TLS, decrypted. A server name is served by the routes of the most specific
// listener covering it, and a connection whose server name has no chain is
// closed. It returns nil when no route is attached.
func tlsListener(name string, port gatewayv1.PortNumber, m *gateway.Model, listeners []int, clusters map[string]*gateway.Cluster) (*listenerv3.Listener, error) {
	type candidate struct {
		route    *gateway.AttachedRoute
		listener int
	}

	byHost := map[string][]candidate{}
	for _, i := range listeners {
		for route, hosts := range attachedTo(m, i) {
			for _, host := range hosts {
				byHost[host] = append(byHost[host], candidate{route: route, listener: i})
			}
		}
	}

	hosts := make([]string, 0, len(byHost))
	for host := range byHost {
		hosts = append(hosts, host)
	}
	slices.Sort(hosts)

	var chains []*listenerv3.FilterChain
	for _, host := range hosts {
		owner := owningListener(m, listeners, host)

		var routes []*gateway.AttachedRoute
		for _, c := range byHost[host] {
			if c.listener == owner {
				routes = append(routes, c.route)
			}
		}
		if len(routes) == 0 {
			continue
		}

		route := oldest(routes)
		proxy, err := tcpProxyFilter(fmt.Sprintf("%s-%s", name, host), backendsOf(route), clusters)
		if err != nil {
			return nil, err
		}

		chain := &listenerv3.FilterChain{Name: host, Filters: []*listenerv3.Filter{proxy}}
		if host != "*" {
			chain.FilterChainMatch = &listenerv3.FilterChainMatch{ServerNames: []string{host}}
		}
		if l := m.Listeners[owner]; gateway.Terminates(l.Listener) {
			socket, err := downstreamTLS(l.Certificates, nil)
			if err != nil {
				return nil, err
			}
			chain.TransportSocket = socket
		}
		chains = append(chains, chain)
	}

	if len(chains) == 0 {
		return nil, nil
	}

	inspector, err := anypb.New(&tlsinspectorv3.TlsInspector{})
	if err != nil {
		return nil, err
	}

	return &listenerv3.Listener{
		Name:    name,
		Address: listenerAddress(port),
		ListenerFilters: []*listenerv3.ListenerFilter{{
			Name:       "envoy.filters.listener.tls_inspector",
			ConfigType: &listenerv3.ListenerFilter_TypedConfig{TypedConfig: inspector},
		}},
		FilterChains: chains,
	}, nil
}

// tcpListener forwards every connection on a TCP listener to the backends of
// the oldest route attached to it. It returns nil when no route is attached.
func tcpListener(name string, port gatewayv1.PortNumber, m *gateway.Model, i int, clusters map[string]*gateway.Cluster) (*listenerv3.Listener, error) {
	routes := slices.Collect(maps.Keys(attachedTo(m, i)))
	if len(routes) == 0 {
		return nil, nil
	}

	proxy, err := tcpProxyFilter(name, backendsOf(oldest(routes)), clusters)
	if err != nil {
		return nil, err
	}

	return &listenerv3.Listener{
		Name:         name,
		Address:      listenerAddress(port),
		FilterChains: []*listenerv3.FilterChain{{Filters: []*listenerv3.Filter{proxy}}},
	}, nil
}

// tcpProxyFilter splits connections across backends by weight. A backend that
// does not resolve keeps its share, and those connections are closed.
func tcpProxyFilter(statPrefix string, backends []gateway.Backend, clusters map[string]*gateway.Cluster) (*listenerv3.Filter, error) {
	var weighted []*tcpproxyv3.TcpProxy_WeightedCluster_ClusterWeight
	for _, b := range backends {
		if b.Weight <= 0 {
			continue
		}

		name := invalidCluster
		if b.Cluster != nil {
			name = b.Cluster.Name
			clusters[name] = b.Cluster
		}
		weighted = append(weighted, &tcpproxyv3.TcpProxy_WeightedCluster_ClusterWeight{
			Name:   name,
			Weight: uint32(b.Weight),
		})
	}

	proxy := &tcpproxyv3.TcpProxy{StatPrefix: statPrefix}
	switch len(weighted) {
	case 0:
		proxy.ClusterSpecifier = &tcpproxyv3.TcpProxy_Cluster{Cluster: invalidCluster}
	case 1:
		proxy.ClusterSpecifier = &tcpproxyv3.TcpProxy_Cluster{Cluster: weighted[0].Name}
	default:
		proxy.ClusterSpecifier = &tcpproxyv3.TcpProxy_WeightedClusters{
			WeightedClusters: &tcpproxyv3.TcpProxy_WeightedCluster{Clusters: weighted},
		}
	}

	config, err := anypb.New(proxy)
	if err != nil {
		return nil, err
	}

	return &listenerv3.Filter{
		Name:       "envoy.filters.network.tcp_proxy",
		ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: config},
	}, nil
}

// weightedCluster is a cluster made of the endpoints of several backends, one
// locality per backend weighted by the backend's weight. The UDP proxy sends
// each session to one cluster and has no weighted clusters of its own, so a
// UDPRoute with several backends is served by one of these.
type weightedCluster struct {
	name     string
	backends []gateway.Backend
}

// udpListener forwards every datagram session on a UDP listener to the
// backends of the oldest route attached to it. It returns nil when no route is
// attached.
func udpListener(name string, port gatewayv1.PortNumber, m *gateway.Model, i int, clusters map[string]*gateway.Cluster, weighted map[string]*weightedCluster) (*listenerv3.Listener, error) {
	routes := slices.Collect(maps.Keys(attachedTo(m, i)))
	if len(routes) == 0 {
		return nil, nil
	}
	route := oldest(routes)

	var backends []gateway.Backend
	for _, b := range backendsOf(route) {
		if b.Weight > 0 {
			backends = append(backends, b)
		}
	}

	target := invalidCluster
	switch {
	case len(backends) == 1 && backends[0].Cluster != nil:
		target = backends[0].Cluster.Name
		clusters[target] = backends[0].Cluster
	case len(backends) > 1:
		target = "udp/" + route.Namespace + "/" + route.Name
		weighted[target] = &weightedCluster{name: target, backends: backends}
	}

	action, err := anypb.New(&udpproxyv3.Route{Cluster: target})
	if err != nil {
		return nil, err
	}
	proxy, err := anypb.New(&udpproxyv3.UdpProxyConfig{
		StatPrefix: name,
		RouteSpecifier: &udpproxyv3.UdpProxyConfig_Matcher{Matcher: &xdsmatcherv3.Matcher{
			OnNoMatch: &xdsmatcherv3.Matcher_OnMatch{
				OnMatch: &xdsmatcherv3.Matcher_OnMatch_Action{Action: &xdscorev3.TypedExtensionConfig{
					Name:        "route",
					TypedConfig: action,
				}},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	address := listenerAddress(port)
	address.GetSocketAddress().Protocol = corev3.SocketAddress_UDP

	return &listenerv3.Listener{
		Name:    name,
		Address: address,
		ListenerFilters: []*listenerv3.ListenerFilter{{
			Name:       "envoy.filters.udp_listener.udp_proxy",
			ConfigType: &listenerv3.ListenerFilter_TypedConfig{TypedConfig: proxy},
		}},
	}, nil
}

// weightedClusterResources builds a weighted cluster and its endpoints. A
// backend that does not resolve has no endpoints, and its locality gets none of
// the traffic.
func weightedClusterResources(w *weightedCluster, refs *gateway.References) (*clusterv3.Cluster, *endpointv3.ClusterLoadAssignment) {
	c := &clusterv3.Cluster{
		Name:                 w.name,
		ConnectTimeout:       connectTimeout,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS},
		EdsClusterConfig:     &clusterv3.Cluster_EdsClusterConfig{EdsConfig: adsConfigSource()},
		CommonLbConfig: &clusterv3.Cluster_CommonLbConfig{
			LocalityConfigSpecifier: &clusterv3.Cluster_CommonLbConfig_LocalityWeightedLbConfig_{
				LocalityWeightedLbConfig: &clusterv3.Cluster_CommonLbConfig_LocalityWeightedLbConfig{},
			},
		},
	}

	assignment := &endpointv3.ClusterLoadAssignment{ClusterName: w.name}
	for i, b := range w.backends {
		var endpoints []*endpointv3.LbEndpoint
		if b.Cluster != nil {
			for _, l := range clusterEndpoints(b.Cluster, refs).GetEndpoints() {
				endpoints = append(endpoints, l.GetLbEndpoints()...)
			}
		}
		assignment.Endpoints = append(assignment.Endpoints, &endpointv3.LocalityLbEndpoints{
			Locality:            &corev3.Locality{SubZone: fmt.Sprintf("backend-%d", i)},
			LoadBalancingWeight: wrapperspb.UInt32(uint32(b.Weight)),
			LbEndpoints:         endpoints,
		})
	}

	return c, assignment
}

// downstreamTLS terminates TLS with the given certificates, offering alpn when
// it is not empty.
func downstreamTLS(secrets []*corev1.Secret, alpn []string) (*corev3.TransportSocket, error) {
	certificates := make([]*tlsv3.TlsCertificate, 0, len(secrets))
	for _, secret := range secrets {
		certificates = append(certificates, &tlsv3.TlsCertificate{
			CertificateChain: &corev3.DataSource{Specifier: &corev3.DataSource_InlineBytes{InlineBytes: secret.Data[corev1.TLSCertKey]}},
			PrivateKey:       &corev3.DataSource{Specifier: &corev3.DataSource_InlineBytes{InlineBytes: secret.Data[corev1.TLSPrivateKeyKey]}},
		})
	}

	config, err := anypb.New(&tlsv3.DownstreamTlsContext{
		CommonTlsContext: &tlsv3.CommonTlsContext{
			TlsCertificates: certificates,
			AlpnProtocols:   alpn,
		},
	})
	if err != nil {
		return nil, err
	}

	return &corev3.TransportSocket{
		Name:       "envoy.transport_sockets.tls",
		ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: config},
	}, nil
}
