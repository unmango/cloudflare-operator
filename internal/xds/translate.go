package xds

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	tlsinspectorv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	httpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

// invalidCluster is the cluster a backend that does not resolve is routed to.
// It never exists, and a route sending traffic to it answers with a 500, as
// Gateway API requires for an invalid backend.
const invalidCluster = "invalid-backend"

// Snapshot translates a Gateway into the configuration its Envoy serves. The
// version is a hash of the content, so translating an unchanged Gateway again
// yields a snapshot Envoy already has.
func Snapshot(m *gateway.Model, refs *gateway.References) (*cachev3.Snapshot, error) {
	resources, err := translate(m, refs)
	if err != nil {
		return nil, err
	}

	version, err := hash(resources)
	if err != nil {
		return nil, err
	}

	snapshot, err := cachev3.NewSnapshot(version, resources)
	if err != nil {
		return nil, err
	}
	if err := snapshot.Consistent(); err != nil {
		return nil, fmt.Errorf("inconsistent snapshot: %w", err)
	}

	return snapshot, nil
}

// translate groups the valid listeners by port. An HTTP port becomes one Envoy
// listener with one route configuration shared by the Gateway listeners on it,
// since nothing but the Host header tells their traffic apart. An HTTPS port
// gets a filter chain per Gateway listener, chosen by SNI, each with a route
// configuration of its own.
func translate(m *gateway.Model, refs *gateway.References) (map[resourcev3.Type][]types.Resource, error) {
	byPort := map[gatewayv1.PortNumber][]int{}
	for i, l := range m.Listeners {
		if l.Valid {
			byPort[l.Port] = append(byPort[l.Port], i)
		}
	}

	ports := make([]gatewayv1.PortNumber, 0, len(byPort))
	for port := range byPort {
		ports = append(ports, port)
	}
	slices.Sort(ports)

	resources := map[resourcev3.Type][]types.Resource{
		resourcev3.ListenerType: {},
		resourcev3.RouteType:    {},
		resourcev3.ClusterType:  {},
		resourcev3.EndpointType: {},
	}
	clusters := map[string]*gateway.Cluster{}

	for _, port := range ports {
		indexes := byPort[port]

		var (
			listener *listenerv3.Listener
			err      error
		)
		switch m.Listeners[indexes[0]].Protocol {
		case gatewayv1.HTTPProtocolType:
			name := fmt.Sprintf("http-%d", port)
			resources[resourcev3.RouteType] = append(resources[resourcev3.RouteType],
				routeConfiguration(name, m, indexes, clusters))
			listener, err = httpListener(name, port)
		case gatewayv1.HTTPSProtocolType:
			name := fmt.Sprintf("https-%d", port)
			var chains []*listenerv3.FilterChain
			for _, i := range indexes {
				l := m.Listeners[i]
				routeName := fmt.Sprintf("%s-%s", name, l.Name)
				resources[resourcev3.RouteType] = append(resources[resourcev3.RouteType],
					routeConfiguration(routeName, m, []int{i}, clusters))

				chain, err := httpsFilterChain(routeName, l)
				if err != nil {
					return nil, err
				}
				chains = append(chains, chain)
			}
			listener, err = httpsListener(name, port, chains)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		resources[resourcev3.ListenerType] = append(resources[resourcev3.ListenerType], listener)
	}

	names := make([]string, 0, len(clusters))
	for name := range clusters {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		c, err := cluster(clusters[name])
		if err != nil {
			return nil, err
		}
		resources[resourcev3.ClusterType] = append(resources[resourcev3.ClusterType], c)
		resources[resourcev3.EndpointType] = append(resources[resourcev3.EndpointType], clusterEndpoints(clusters[name], refs))
	}

	return resources, nil
}

func httpConnectionManager(name string) (*anypb.Any, error) {
	router, err := anypb.New(&routerv3.Router{})
	if err != nil {
		return nil, err
	}

	return anypb.New(&hcmv3.HttpConnectionManager{
		StatPrefix: name,
		RouteSpecifier: &hcmv3.HttpConnectionManager_Rds{
			Rds: &hcmv3.Rds{
				RouteConfigName: name,
				ConfigSource:    adsConfigSource(),
			},
		},
		HttpFilters: []*hcmv3.HttpFilter{{
			Name:       "envoy.filters.http.router",
			ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: router},
		}},
	})
}

func adsConfigSource() *corev3.ConfigSource {
	return &corev3.ConfigSource{
		ResourceApiVersion:    corev3.ApiVersion_V3,
		ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
	}
}

func listenerAddress(port gatewayv1.PortNumber) *corev3.Address {
	return socketAddress("0.0.0.0", uint32(gateway.ContainerPort(port)))
}

func httpListener(name string, port gatewayv1.PortNumber) (*listenerv3.Listener, error) {
	manager, err := httpConnectionManager(name)
	if err != nil {
		return nil, err
	}

	return &listenerv3.Listener{
		Name:    name,
		Address: listenerAddress(port),
		FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{{
				Name:       httpConnectionManagerFilter,
				ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: manager},
			}},
		}},
	}, nil
}

func httpsListener(name string, port gatewayv1.PortNumber, chains []*listenerv3.FilterChain) (*listenerv3.Listener, error) {
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

// httpsFilterChain terminates TLS for one listener. A listener with a hostname
// is chosen by SNI; the one without serves every other name.
func httpsFilterChain(routeName string, l gateway.Listener) (*listenerv3.FilterChain, error) {
	manager, err := httpConnectionManager(routeName)
	if err != nil {
		return nil, err
	}

	certificates := make([]*tlsv3.TlsCertificate, 0, len(l.Certificates))
	for _, secret := range l.Certificates {
		certificates = append(certificates, &tlsv3.TlsCertificate{
			CertificateChain: &corev3.DataSource{Specifier: &corev3.DataSource_InlineBytes{InlineBytes: secret.Data[corev1.TLSCertKey]}},
			PrivateKey:       &corev3.DataSource{Specifier: &corev3.DataSource_InlineBytes{InlineBytes: secret.Data[corev1.TLSPrivateKeyKey]}},
		})
	}

	tlsContext, err := anypb.New(&tlsv3.DownstreamTlsContext{
		CommonTlsContext: &tlsv3.CommonTlsContext{
			TlsCertificates: certificates,
			AlpnProtocols:   []string{"h2", "http/1.1"},
		},
	})
	if err != nil {
		return nil, err
	}

	chain := &listenerv3.FilterChain{
		Name: string(l.Name),
		Filters: []*listenerv3.Filter{{
			Name:       httpConnectionManagerFilter,
			ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: manager},
		}},
		TransportSocket: &corev3.TransportSocket{
			Name:       "envoy.transport_sockets.tls",
			ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: tlsContext},
		},
	}
	if l.Hostname != nil && *l.Hostname != "" {
		chain.FilterChainMatch = &listenerv3.FilterChainMatch{ServerNames: []string{string(*l.Hostname)}}
	}

	return chain, nil
}

// entry is one match of one rule of an attached route, served on one host of
// one listener.
type entry struct {
	route    *gateway.AttachedRoute
	listener int
	host     string
	rule     int
	match    int
}

// routeConfiguration holds a virtual host for each hostname the routes attached
// to the given listeners serve. Envoy picks the one virtual host with the most
// specific domain, but Gateway API lets a request fall through to a route with
// a less specific hostname when no rule of a more specific one matches it, so
// each virtual host also carries the entries of every hostname that covers its
// own, after them. Those come only from the most specific listener whose
// hostname covers the virtual host's, since a request for a host is routed by
// the routes of that listener alone, whatever another listener on the same
// port allows. Every request that matches nothing gets a 404.
func routeConfiguration(name string, m *gateway.Model, listeners []int, clusters map[string]*gateway.Cluster) *routev3.RouteConfiguration {
	byHost := map[string][]entry{}
	seen := map[string]bool{}
	for r := range m.Routes {
		route := &m.Routes[r]
		for _, parent := range route.Parents {
			for _, i := range listeners {
				for _, host := range parent.Hostnames[i] {
					for rule, ru := range route.Rules {
						for match := range ru.Matches {
							key := fmt.Sprintf("%d|%s|%s|%d|%d", i, host, route.Key(), rule, match)
							if seen[key] {
								continue
							}
							seen[key] = true
							byHost[host] = append(byHost[host], entry{route: route, listener: i, host: host, rule: rule, match: match})
						}
					}
				}
			}
		}
	}

	hosts := make([]string, 0, len(byHost))
	for host := range byHost {
		hosts = append(hosts, host)
	}
	slices.Sort(hosts)

	// Listener hostnames never carry a port, and a client may send one. The
	// port is ignored for matching only, so the backend still sees it.
	config := &routev3.RouteConfiguration{Name: name, IgnorePortInHostMatching: true}
	for _, host := range hosts {
		owner := owningListener(m, listeners, host)

		var entries []entry
		for _, other := range hosts {
			if !gateway.HostCovers(other, host) {
				continue
			}
			for _, e := range byHost[other] {
				if e.listener == owner {
					entries = append(entries, e)
				}
			}
		}
		slices.SortStableFunc(entries, compareEntries)

		vh := &routev3.VirtualHost{Name: host, Domains: []string{host}}
		added := map[string]bool{}
		for _, e := range entries {
			// A route naming both this hostname and one that covers it would
			// otherwise appear twice, the second time unreachable.
			key := fmt.Sprintf("%s|%d|%d", e.route.Key(), e.rule, e.match)
			if added[key] {
				continue
			}
			added[key] = true
			vh.Routes = append(vh.Routes, envoyRoute(e, clusters))
		}
		config.VirtualHosts = append(config.VirtualHosts, vh)
	}

	return config
}

// owningListener is the most specific of listeners whose hostname covers host.
func owningListener(m *gateway.Model, listeners []int, host string) int {
	owner, best := -1, ""
	for _, i := range listeners {
		h := "*"
		if l := m.Listeners[i].Hostname; l != nil && *l != "" {
			h = string(*l)
		}
		if !gateway.HostCovers(h, host) {
			continue
		}
		if owner < 0 || moreSpecific(h, best) {
			owner, best = i, h
		}
	}

	return owner
}

// moreSpecific reports whether hostname a is more specific than b: more
// characters outside a wildcard, then more characters.
func moreSpecific(a, b string) bool {
	return cmp.Or(
		cmp.Compare(exactChars(a), exactChars(b)),
		cmp.Compare(len(a), len(b)),
	) > 0
}

func exactChars(host string) int {
	return len(strings.TrimPrefix(host, "*"))
}

// compareEntries orders matches by Gateway API precedence: the most specific
// hostname, by its characters outside a wildcard and then by all of them; an
// exact path, then a regular expression, then the longest prefix; a method
// match; the most header matches; the most query matches; then the oldest
// route, the route first by namespace and name, and the order within the route.
func compareEntries(a, b entry) int {
	ma := &a.route.Rules[a.rule].Matches[a.match]
	mb := &b.route.Rules[b.rule].Matches[b.match]

	pathRank := func(m *gatewayv1.HTTPRouteMatch) (int, int) {
		t, v := pathOf(m)
		switch t {
		case gatewayv1.PathMatchExact:
			return 0, -len(v)
		case gatewayv1.PathMatchRegularExpression:
			return 1, -len(v)
		default:
			return 2, -len(strings.TrimSuffix(v, "/"))
		}
	}
	ta, la := pathRank(ma)
	tb, lb := pathRank(mb)

	method := func(m *gatewayv1.HTTPRouteMatch) int {
		if m.Method != nil {
			return 0
		}
		return 1
	}

	return cmp.Or(
		cmp.Compare(exactChars(b.host), exactChars(a.host)),
		cmp.Compare(len(b.host), len(a.host)),
		cmp.Compare(ta, tb),
		cmp.Compare(la, lb),
		cmp.Compare(method(ma), method(mb)),
		cmp.Compare(len(mb.Headers), len(ma.Headers)),
		cmp.Compare(len(mb.QueryParams), len(ma.QueryParams)),
		a.route.CreationTimestamp.Compare(b.route.CreationTimestamp.Time),
		cmp.Compare(a.route.Namespace+"/"+a.route.Name, b.route.Namespace+"/"+b.route.Name),
		cmp.Compare(a.rule, b.rule),
		cmp.Compare(a.match, b.match),
	)
}

func pathOf(m *gatewayv1.HTTPRouteMatch) (gatewayv1.PathMatchType, string) {
	if m.Path == nil {
		return gatewayv1.PathMatchPathPrefix, "/"
	}

	t := gatewayv1.PathMatchPathPrefix
	if m.Path.Type != nil {
		t = *m.Path.Type
	}
	v := "/"
	if m.Path.Value != nil {
		v = *m.Path.Value
	}

	return t, v
}

func envoyRoute(e entry, clusters map[string]*gateway.Cluster) *routev3.Route {
	rule := e.route.Rules[e.rule]
	match := rule.Matches[e.match]

	route := &routev3.Route{
		Name:  fmt.Sprintf("%s/%d/%d", e.route.Key(), e.rule, e.match),
		Match: routeMatch(&match),
	}

	var (
		redirect *gatewayv1.HTTPRequestRedirectFilter
		rewrite  *gatewayv1.HTTPURLRewriteFilter
	)
	for _, f := range rule.Filters {
		switch f.Type {
		case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
			add, remove := headerModifier(f.RequestHeaderModifier)
			route.RequestHeadersToAdd = append(route.RequestHeadersToAdd, add...)
			route.RequestHeadersToRemove = append(route.RequestHeadersToRemove, remove...)
		case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
			add, remove := headerModifier(f.ResponseHeaderModifier)
			route.ResponseHeadersToAdd = append(route.ResponseHeadersToAdd, add...)
			route.ResponseHeadersToRemove = append(route.ResponseHeadersToRemove, remove...)
		case gatewayv1.HTTPRouteFilterRequestRedirect:
			redirect = f.RequestRedirect
		case gatewayv1.HTTPRouteFilterURLRewrite:
			rewrite = f.URLRewrite
		default:
			// A filter that is not implemented must not be skipped silently,
			// since the rule would then do something it was not written to.
			route.Action = directResponse(500)
			return route
		}
	}

	if redirect != nil {
		route.Action = &routev3.Route_Redirect{Redirect: redirectAction(redirect, &match)}
		return route
	}

	action, ok := routeAction(e.route.Backends[e.rule], clusters)
	if !ok {
		route.Action = directResponse(500)
		return route
	}
	if rewrite != nil {
		applyRewrite(action, rewrite, &match)
	}
	route.Action = &routev3.Route_Route{Route: action}

	return route
}

func routeMatch(m *gatewayv1.HTTPRouteMatch) *routev3.RouteMatch {
	match := &routev3.RouteMatch{}

	switch t, v := pathOf(m); t {
	case gatewayv1.PathMatchExact:
		match.PathSpecifier = &routev3.RouteMatch_Path{Path: v}
	case gatewayv1.PathMatchRegularExpression:
		match.PathSpecifier = &routev3.RouteMatch_SafeRegex{SafeRegex: &matcherv3.RegexMatcher{Regex: v}}
	default:
		// A prefix matches whole path segments, and a trailing slash in it
		// changes nothing.
		if prefix := strings.TrimSuffix(v, "/"); prefix == "" {
			match.PathSpecifier = &routev3.RouteMatch_Prefix{Prefix: "/"}
		} else {
			match.PathSpecifier = &routev3.RouteMatch_PathSeparatedPrefix{PathSeparatedPrefix: prefix}
		}
	}

	for _, h := range m.Headers {
		match.Headers = append(match.Headers, &routev3.HeaderMatcher{
			Name: string(h.Name),
			HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
				StringMatch: stringMatch(h.Type != nil && *h.Type == gatewayv1.HeaderMatchRegularExpression, h.Value),
			},
		})
	}

	if m.Method != nil {
		match.Headers = append(match.Headers, &routev3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
				StringMatch: stringMatch(false, string(*m.Method)),
			},
		})
	}

	for _, q := range m.QueryParams {
		match.QueryParameters = append(match.QueryParameters, &routev3.QueryParameterMatcher{
			Name: string(q.Name),
			QueryParameterMatchSpecifier: &routev3.QueryParameterMatcher_StringMatch{
				StringMatch: stringMatch(q.Type != nil && *q.Type == gatewayv1.QueryParamMatchRegularExpression, q.Value),
			},
		})
	}

	return match
}

func stringMatch(regex bool, value string) *matcherv3.StringMatcher {
	if regex {
		return &matcherv3.StringMatcher{MatchPattern: &matcherv3.StringMatcher_SafeRegex{SafeRegex: &matcherv3.RegexMatcher{Regex: value}}}
	}

	return &matcherv3.StringMatcher{MatchPattern: &matcherv3.StringMatcher_Exact{Exact: value}}
}

func headerModifier(f *gatewayv1.HTTPHeaderFilter) ([]*corev3.HeaderValueOption, []string) {
	if f == nil {
		return nil, nil
	}

	var add []*corev3.HeaderValueOption
	for _, h := range f.Set {
		add = append(add, &corev3.HeaderValueOption{
			Header:       &corev3.HeaderValue{Key: string(h.Name), Value: h.Value},
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	for _, h := range f.Add {
		add = append(add, &corev3.HeaderValueOption{
			Header:       &corev3.HeaderValue{Key: string(h.Name), Value: h.Value},
			AppendAction: corev3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
		})
	}

	return add, f.Remove
}

// routeAction splits traffic across a rule's backends by weight. A backend that
// does not resolve keeps its share, which is answered with a 500. It reports
// false when there is no backend to send anything to.
func routeAction(backends []gateway.Backend, clusters map[string]*gateway.Cluster) (*routev3.RouteAction, bool) {
	var weighted []*routev3.WeightedCluster_ClusterWeight
	for _, b := range backends {
		if b.Weight <= 0 {
			continue
		}

		name := invalidCluster
		if b.Cluster != nil {
			name = b.Cluster.Name
			clusters[name] = b.Cluster
		}

		weighted = append(weighted, &routev3.WeightedCluster_ClusterWeight{
			Name:   name,
			Weight: wrapperspb.UInt32(uint32(b.Weight)),
		})
	}

	switch {
	case len(weighted) == 0:
		return nil, false
	case len(weighted) == 1 && weighted[0].Name == invalidCluster:
		return nil, false
	}

	action := &routev3.RouteAction{
		ClusterNotFoundResponseCode: routev3.RouteAction_INTERNAL_SERVER_ERROR,
	}
	if len(weighted) == 1 {
		action.ClusterSpecifier = &routev3.RouteAction_Cluster{Cluster: weighted[0].Name}
	} else {
		action.ClusterSpecifier = &routev3.RouteAction_WeightedClusters{
			WeightedClusters: &routev3.WeightedCluster{Clusters: weighted},
		}
	}

	return action, true
}

func applyRewrite(action *routev3.RouteAction, f *gatewayv1.HTTPURLRewriteFilter, match *gatewayv1.HTTPRouteMatch) {
	if f.Hostname != nil {
		action.HostRewriteSpecifier = &routev3.RouteAction_HostRewriteLiteral{HostRewriteLiteral: string(*f.Hostname)}
	}

	if f.Path == nil {
		return
	}

	switch f.Path.Type {
	case gatewayv1.FullPathHTTPPathModifier:
		if f.Path.ReplaceFullPath != nil {
			action.RegexRewrite = &matcherv3.RegexMatchAndSubstitute{
				Pattern:      &matcherv3.RegexMatcher{Regex: "^.*$"},
				Substitution: *f.Path.ReplaceFullPath,
			}
		}
	case gatewayv1.PrefixMatchHTTPPathModifier:
		if f.Path.ReplacePrefixMatch != nil {
			action.RegexRewrite = prefixRewrite(match, *f.Path.ReplacePrefixMatch)
		}
	}
}

// prefixRewrite replaces the part of the path a prefix match matched. It is a
// regular expression rather than Envoy's prefix_rewrite, which would leave a
// doubled slash when the replacement is "/".
func prefixRewrite(match *gatewayv1.HTTPRouteMatch, replacement string) *matcherv3.RegexMatchAndSubstitute {
	_, v := pathOf(match)
	prefix := strings.TrimSuffix(v, "/")
	replacement = strings.TrimSuffix(replacement, "/")

	if replacement == "" {
		// Removing the prefix leaves the rest of the path, or "/" when there
		// is none.
		return &matcherv3.RegexMatchAndSubstitute{
			Pattern:      &matcherv3.RegexMatcher{Regex: "^" + regexp.QuoteMeta(prefix) + `/*(.*)$`},
			Substitution: `/\1`,
		}
	}

	return &matcherv3.RegexMatchAndSubstitute{
		Pattern:      &matcherv3.RegexMatcher{Regex: "^" + regexp.QuoteMeta(prefix) + `(/.*)?$`},
		Substitution: replacement + `\1`,
	}
}

func redirectAction(f *gatewayv1.HTTPRequestRedirectFilter, match *gatewayv1.HTTPRouteMatch) *routev3.RedirectAction {
	redirect := &routev3.RedirectAction{ResponseCode: routev3.RedirectAction_FOUND}

	if f.Scheme != nil {
		redirect.SchemeRewriteSpecifier = &routev3.RedirectAction_SchemeRedirect{SchemeRedirect: *f.Scheme}
	}
	if f.Hostname != nil {
		redirect.HostRedirect = string(*f.Hostname)
	}
	if f.Port != nil {
		redirect.PortRedirect = uint32(*f.Port)
	}
	if f.StatusCode != nil {
		switch *f.StatusCode {
		case 301:
			redirect.ResponseCode = routev3.RedirectAction_MOVED_PERMANENTLY
		case 303:
			redirect.ResponseCode = routev3.RedirectAction_SEE_OTHER
		case 307:
			redirect.ResponseCode = routev3.RedirectAction_TEMPORARY_REDIRECT
		case 308:
			redirect.ResponseCode = routev3.RedirectAction_PERMANENT_REDIRECT
		}
	}

	if f.Path != nil {
		switch f.Path.Type {
		case gatewayv1.FullPathHTTPPathModifier:
			if f.Path.ReplaceFullPath != nil {
				redirect.PathRewriteSpecifier = &routev3.RedirectAction_PathRedirect{PathRedirect: *f.Path.ReplaceFullPath}
			}
		case gatewayv1.PrefixMatchHTTPPathModifier:
			if f.Path.ReplacePrefixMatch != nil {
				redirect.PathRewriteSpecifier = &routev3.RedirectAction_RegexRewrite{
					RegexRewrite: prefixRewrite(match, *f.Path.ReplacePrefixMatch),
				}
			}
		}
	}

	return redirect
}

func directResponse(status uint32) *routev3.Route_DirectResponse {
	return &routev3.Route_DirectResponse{DirectResponse: &routev3.DirectResponseAction{Status: status}}
}

// cluster is a Service port, whose endpoints come over EDS.
func cluster(c *gateway.Cluster) (*clusterv3.Cluster, error) {
	out := &clusterv3.Cluster{
		Name:                 c.Name,
		ConnectTimeout:       durationpb.New(5 * time.Second),
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS},
		EdsClusterConfig:     &clusterv3.Cluster_EdsClusterConfig{EdsConfig: adsConfigSource()},
	}

	if c.H2 {
		http2, err := anypb.New(&httpv3.HttpProtocolOptions{
			UpstreamProtocolOptions: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &httpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
						Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		out.TypedExtensionProtocolOptions = map[string]*anypb.Any{
			"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": http2,
		}
	}

	return out, nil
}

// clusterEndpoints lists the ready endpoints of a cluster's Service port, from
// the Service's EndpointSlices. A slice names its ports after the Service's.
func clusterEndpoints(c *gateway.Cluster, refs *gateway.References) *endpointv3.ClusterLoadAssignment {
	type address struct {
		ip   string
		port int32
	}

	var addresses []address
	seen := map[address]bool{}
	if refs != nil {
		for _, slice := range refs.Endpoints[c.Service] {
			if slice.AddressType != discoveryv1.AddressTypeIPv4 && slice.AddressType != discoveryv1.AddressTypeIPv6 {
				continue
			}

			var port *int32
			for _, p := range slice.Ports {
				if p.Port != nil && ptrString(p.Name) == c.Port.Name {
					port = p.Port
					break
				}
			}
			if port == nil {
				continue
			}

			for _, ep := range slice.Endpoints {
				if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
					continue
				}
				for _, ip := range ep.Addresses {
					a := address{ip: ip, port: *port}
					if !seen[a] {
						seen[a] = true
						addresses = append(addresses, a)
					}
				}
			}
		}
	}
	slices.SortFunc(addresses, func(a, b address) int {
		return cmp.Or(cmp.Compare(a.ip, b.ip), cmp.Compare(a.port, b.port))
	})

	endpoints := make([]*endpointv3.LbEndpoint, 0, len(addresses))
	for _, a := range addresses {
		endpoints = append(endpoints, &endpointv3.LbEndpoint{
			HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
				Endpoint: &endpointv3.Endpoint{Address: socketAddress(a.ip, uint32(a.port))},
			},
		})
	}

	return &endpointv3.ClusterLoadAssignment{
		ClusterName: c.Name,
		Endpoints:   []*endpointv3.LocalityLbEndpoints{{LbEndpoints: endpoints}},
	}
}

func ptrString(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// hash fingerprints a set of resources in a stable order.
func hash(resources map[resourcev3.Type][]types.Resource) (string, error) {
	typeURLs := make([]string, 0, len(resources))
	for typeURL := range resources {
		typeURLs = append(typeURLs, typeURL)
	}
	slices.Sort(typeURLs)

	h := sha256.New()
	opts := proto.MarshalOptions{Deterministic: true}
	for _, typeURL := range typeURLs {
		h.Write([]byte(typeURL))
		for _, r := range resources[typeURL] {
			b, err := opts.Marshal(r)
			if err != nil {
				return "", err
			}
			h.Write(b)
		}
	}

	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
