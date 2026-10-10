package gateway

import (
	"fmt"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// Route is an HTTPRoute or a GRPCRoute, reduced to what attachment and
// translation need. A GRPCRoute's matches and filters are rewritten as their
// HTTP equivalents: gRPC is HTTP/2, a method is a path, and Envoy routes both
// alike.
type Route struct {
	Kind GroupKind
	metav1.ObjectMeta
	ParentRefs []gatewayv1.ParentReference
	Hostnames  []gatewayv1.Hostname
	Rules      []Rule
}

// Rule is one rule of a route.
type Rule struct {
	Matches  []gatewayv1.HTTPRouteMatch
	Filters  []gatewayv1.HTTPRouteFilter
	Backends []gatewayv1.BackendRef

	// GRPC marks a rule of a GRPCRoute, whose backends always speak HTTP/2.
	GRPC bool
}

// FromHTTPRoute reduces an HTTPRoute.
func FromHTTPRoute(r *gatewayv1.HTTPRoute) Route {
	route := Route{
		Kind:       HTTPRouteKind,
		ObjectMeta: r.ObjectMeta,
		ParentRefs: r.Spec.ParentRefs,
		Hostnames:  r.Spec.Hostnames,
	}

	for _, rule := range r.Spec.Rules {
		matches := rule.Matches
		if len(matches) == 0 {
			// The API server defaults this, but a route read from anywhere
			// else may not carry the default.
			matches = []gatewayv1.HTTPRouteMatch{{}}
		}

		backends := make([]gatewayv1.BackendRef, 0, len(rule.BackendRefs))
		for _, b := range rule.BackendRefs {
			backends = append(backends, b.BackendRef)
		}

		route.Rules = append(route.Rules, Rule{
			Matches:  matches,
			Filters:  rule.Filters,
			Backends: backends,
		})
	}

	return route
}

// FromGRPCRoute reduces a GRPCRoute, translating each method match to the path
// gRPC sends it on.
func FromGRPCRoute(r *gatewayv1.GRPCRoute) Route {
	route := Route{
		Kind:       GRPCRouteKind,
		ObjectMeta: r.ObjectMeta,
		ParentRefs: r.Spec.ParentRefs,
		Hostnames:  r.Spec.Hostnames,
	}

	for _, rule := range r.Spec.Rules {
		matches := make([]gatewayv1.HTTPRouteMatch, 0, len(rule.Matches))
		for _, m := range rule.Matches {
			match := gatewayv1.HTTPRouteMatch{}
			if m.Method != nil {
				match.Path = grpcPath(m.Method)
			}
			for _, h := range m.Headers {
				t := gatewayv1.HeaderMatchExact
				if h.Type != nil && *h.Type == gatewayv1.GRPCHeaderMatchRegularExpression {
					t = gatewayv1.HeaderMatchRegularExpression
				}
				match.Headers = append(match.Headers, gatewayv1.HTTPHeaderMatch{
					Type:  &t,
					Name:  gatewayv1.HTTPHeaderName(h.Name),
					Value: h.Value,
				})
			}
			matches = append(matches, match)
		}
		if len(matches) == 0 {
			matches = []gatewayv1.HTTPRouteMatch{{}}
		}

		filters := make([]gatewayv1.HTTPRouteFilter, 0, len(rule.Filters))
		for _, f := range rule.Filters {
			filters = append(filters, gatewayv1.HTTPRouteFilter{
				Type:                   gatewayv1.HTTPRouteFilterType(f.Type),
				RequestHeaderModifier:  f.RequestHeaderModifier,
				ResponseHeaderModifier: f.ResponseHeaderModifier,
				RequestMirror:          f.RequestMirror,
				ExtensionRef:           f.ExtensionRef,
			})
		}

		backends := make([]gatewayv1.BackendRef, 0, len(rule.BackendRefs))
		for _, b := range rule.BackendRefs {
			backends = append(backends, b.BackendRef)
		}

		route.Rules = append(route.Rules, Rule{
			Matches:  matches,
			Filters:  filters,
			Backends: backends,
			GRPC:     true,
		})
	}

	return route
}

// grpcPath matches the path a gRPC call travels on, /<service>/<method>.
func grpcPath(m *gatewayv1.GRPCMethodMatch) *gatewayv1.HTTPPathMatch {
	const segment = "[^/]+"

	service, method := "", ""
	if m.Service != nil {
		service = *m.Service
	}
	if m.Method != nil {
		method = *m.Method
	}

	if m.Type != nil && *m.Type == gatewayv1.GRPCMethodMatchRegularExpression {
		if service == "" {
			service = segment
		}
		if method == "" {
			method = segment
		}

		return &gatewayv1.HTTPPathMatch{
			Type:  new(gatewayv1.PathMatchRegularExpression),
			Value: new("/" + service + "/" + method),
		}
	}

	switch {
	case service != "" && method != "":
		return &gatewayv1.HTTPPathMatch{
			Type:  new(gatewayv1.PathMatchExact),
			Value: new("/" + service + "/" + method),
		}
	case service != "":
		return &gatewayv1.HTTPPathMatch{
			Type:  new(gatewayv1.PathMatchPathPrefix),
			Value: new("/" + service),
		}
	case method != "":
		return &gatewayv1.HTTPPathMatch{
			Type:  new(gatewayv1.PathMatchRegularExpression),
			Value: new("/" + segment + "/" + regexp.QuoteMeta(method)),
		}
	default:
		return nil
	}
}

// Key names a route for logs and for ordering.
func (r *Route) Key() string {
	return fmt.Sprintf("%s/%s/%s", r.Kind.Kind, r.Namespace, r.Name)
}

// targets reports whether ref names gw.
func targets(ref gatewayv1.ParentReference, routeNamespace string, gw *gatewayv1.Gateway) bool {
	return group(ref.Group, gatewayv1.GroupName) == GatewayKind.Group &&
		kind(ref.Kind, GatewayKind.Kind) == GatewayKind.Kind &&
		namespace(ref.Namespace, routeNamespace) == gw.Namespace &&
		string(ref.Name) == gw.Name
}

// RefersTo reports whether any of the route's parentRefs names gw.
func (r *Route) RefersTo(gw *gatewayv1.Gateway) bool {
	for _, ref := range r.ParentRefs {
		if targets(ref, r.Namespace, gw) {
			return true
		}
	}

	return false
}
