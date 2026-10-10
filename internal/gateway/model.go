package gateway

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// h2cAppProtocol marks a Service port that speaks HTTP/2 without TLS.
const h2cAppProtocol = "kubernetes.io/h2c"

// Model is a Gateway with its listeners validated and its routes attached and
// resolved: everything its status and its Envoy configuration derive from.
type Model struct {
	Gateway   *gatewayv1.Gateway
	Listeners []Listener
	Routes    []AttachedRoute

	// AttachedRoutes counts, per listener, the routes attached to it.
	AttachedRoutes []int32
}

// AttachedRoute is a route that names the Gateway in at least one parentRef.
type AttachedRoute struct {
	Route

	// Parents holds one result for each parentRef naming the Gateway.
	Parents []ParentResult

	// Backends holds, per rule, its backends in order.
	Backends [][]Backend

	// ResolvedRefs reports whether every backend of the route resolved, which
	// is the same for every parent.
	ResolvedRefs metav1.Condition
}

// ParentResult is how one parentRef of a route resolved against the Gateway.
type ParentResult struct {
	Ref      gatewayv1.ParentReference
	Accepted metav1.Condition

	// Hostnames holds, for each listener index the route attached to, the
	// hostnames it serves there: the intersection of the listener's hostname
	// with the route's. "*" stands for any host.
	Hostnames map[int][]string
}

// Backend is one backendRef of a rule.
type Backend struct {
	Weight int32

	// Cluster is nil when the reference does not resolve, and traffic sent to
	// it is answered with a 500.
	Cluster *Cluster
}

// Cluster is a Service port Envoy sends traffic to.
type Cluster struct {
	Name    string
	Service types.NamespacedName
	Port    corev1.ServicePort

	// H2 reports whether the backend speaks HTTP/2, which gRPC always does.
	H2 bool
}

// Build validates gw's listeners, attaches the routes that name it, and
// resolves their backends.
func Build(gw *gatewayv1.Gateway, routes []Route, refs *References) *Model {
	if refs == nil {
		refs = &References{}
	}

	m := &Model{
		Gateway:   gw,
		Listeners: Listeners(gw, refs),
	}
	m.AttachedRoutes = make([]int32, len(m.Listeners))

	for _, route := range routes {
		if !route.RefersTo(gw) {
			continue
		}

		attached := AttachedRoute{Route: route}
		listeners := map[int]bool{}
		for _, ref := range route.ParentRefs {
			if !targets(ref, route.Namespace, gw) {
				continue
			}

			result := m.attach(&route, ref, refs)
			for i := range result.Hostnames {
				listeners[i] = true
			}
			attached.Parents = append(attached.Parents, result)
		}
		for i := range listeners {
			m.AttachedRoutes[i]++
		}

		attached.Backends, attached.ResolvedRefs = resolveBackends(&route, refs)
		m.Routes = append(m.Routes, attached)
	}

	return m
}

// attach decides which of the Gateway's listeners a parentRef attaches the
// route to, and the Accepted condition that results.
func (m *Model) attach(route *Route, ref gatewayv1.ParentReference, refs *References) ParentResult {
	result := ParentResult{Ref: ref, Hostnames: map[int][]string{}}
	reject := func(reason gatewayv1.RouteConditionReason, message string) ParentResult {
		result.Accepted = metav1.Condition{
			Type:    string(gatewayv1.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(reason),
			Message: message,
		}
		result.Hostnames = nil
		return result
	}

	candidates := 0
	allowed := 0
	for i, l := range m.Listeners {
		if ref.SectionName != nil && *ref.SectionName != l.Name {
			continue
		}
		if ref.Port != nil && *ref.Port != l.Port {
			continue
		}
		candidates++

		if !allowsNamespace(l.Listener, m.Gateway.Namespace, route.Namespace, refs) || !allowsKind(l, route.Kind) {
			continue
		}
		allowed++

		if hosts := intersect(l.Hostname, route.Hostnames); len(hosts) > 0 {
			result.Hostnames[i] = hosts
		}
	}

	switch {
	case candidates == 0:
		return reject(gatewayv1.RouteReasonNoMatchingParent, "No listener matches the parentRef")
	case allowed == 0:
		return reject(gatewayv1.RouteReasonNotAllowedByListeners,
			fmt.Sprintf("No matching listener allows a %s from namespace %s", route.Kind.Kind, route.Namespace))
	case len(result.Hostnames) == 0:
		return reject(gatewayv1.RouteReasonNoMatchingListenerHostname,
			"None of the route's hostnames matches a listener's hostname")
	}

	result.Accepted = metav1.Condition{
		Type:    string(gatewayv1.RouteConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.RouteReasonAccepted),
		Message: "Route accepted",
	}
	return result
}

func allowsNamespace(l gatewayv1.Listener, gwNamespace, routeNamespace string, refs *References) bool {
	from := gatewayv1.NamespacesFromSame
	var selector *metav1.LabelSelector
	if l.AllowedRoutes != nil && l.AllowedRoutes.Namespaces != nil {
		if l.AllowedRoutes.Namespaces.From != nil {
			from = *l.AllowedRoutes.Namespaces.From
		}
		selector = l.AllowedRoutes.Namespaces.Selector
	}

	switch from {
	case gatewayv1.NamespacesFromAll:
		return true
	case gatewayv1.NamespacesFromSelector:
		if selector == nil {
			return false
		}
		s, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return false
		}
		return s.Matches(refs.namespaceLabels(routeNamespace))
	default:
		return gwNamespace == routeNamespace
	}
}

func allowsKind(l Listener, k GroupKind) bool {
	for _, rgk := range l.SupportedKinds {
		if group(rgk.Group, gatewayv1.GroupName) == k.Group && string(rgk.Kind) == k.Kind {
			return true
		}
	}

	return false
}

// intersect returns the hostnames a route serves on a listener: for each of the
// route's hostnames that the listener's matches, the more specific of the two.
// A route without hostnames serves the listener's, and a listener without one
// serves any.
func intersect(listener *gatewayv1.Hostname, route []gatewayv1.Hostname) []string {
	lh := "*"
	if listener != nil && *listener != "" {
		lh = string(*listener)
	}

	if len(route) == 0 {
		return []string{lh}
	}

	var out []string
	seen := map[string]bool{}
	for _, h := range route {
		rh := string(h)

		var host string
		switch {
		case lh == "*":
			host = rh
		case matchesHost(lh, rh):
			host = rh
		case matchesHost(rh, lh):
			host = lh
		default:
			continue
		}

		if !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}

	return out
}

// matchesHost reports whether pattern covers host. An exact pattern covers only
// itself. A wildcard covers every host with at least one more label in front
// of its suffix, and every narrower wildcard.
func matchesHost(pattern, host string) bool {
	if pattern == host {
		return true
	}
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}

	prefix, ok := strings.CutSuffix(host, pattern[1:])
	return ok && prefix != ""
}

// resolveBackends resolves every backendRef of the route, and reports the
// first that does not resolve in the route's ResolvedRefs condition.
func resolveBackends(route *Route, refs *References) ([][]Backend, metav1.Condition) {
	resolved := metav1.Condition{
		Type:    string(gatewayv1.RouteConditionResolvedRefs),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.RouteReasonResolvedRefs),
		Message: "References resolved",
	}

	out := make([][]Backend, len(route.Rules))
	for i, rule := range route.Rules {
		for _, ref := range rule.Backends {
			weight := int32(1)
			if ref.Weight != nil {
				weight = *ref.Weight
			}

			cluster, failed := resolveBackend(route, rule, ref.BackendObjectReference, refs)
			if failed != nil && resolved.Status == metav1.ConditionTrue {
				resolved = *failed
			}
			out[i] = append(out[i], Backend{Weight: weight, Cluster: cluster})
		}
	}

	return out, resolved
}

func resolveBackend(route *Route, rule Rule, ref gatewayv1.BackendObjectReference, refs *References) (*Cluster, *metav1.Condition) {
	fail := func(reason gatewayv1.RouteConditionReason, format string, args ...any) (*Cluster, *metav1.Condition) {
		return nil, &metav1.Condition{
			Type:    string(gatewayv1.RouteConditionResolvedRefs),
			Status:  metav1.ConditionFalse,
			Reason:  string(reason),
			Message: fmt.Sprintf(format, args...),
		}
	}

	g, k := group(ref.Group, corev1.GroupName), kind(ref.Kind, ServiceKind.Kind)
	key := types.NamespacedName{Namespace: namespace(ref.Namespace, route.Namespace), Name: string(ref.Name)}
	if g != ServiceKind.Group || k != ServiceKind.Kind {
		return fail(gatewayv1.RouteReasonInvalidKind, "backendRef %s/%s %s is not a Service", g, k, key)
	}
	if !refs.Permitted(route.Kind, route.Namespace, ServiceKind, key.Namespace, key.Name) {
		return fail(gatewayv1.RouteReasonRefNotPermitted, "No ReferenceGrant allows a %s in %s to use Service %s",
			route.Kind.Kind, route.Namespace, key)
	}

	svc, ok := refs.Services[key]
	if !ok {
		return fail(gatewayv1.RouteReasonBackendNotFound, "Service %s does not exist", key)
	}
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		return fail(gatewayv1.RouteReasonUnsupportedProtocol, "Service %s is an ExternalName Service, which is not supported", key)
	}
	if ref.Port == nil {
		return fail(gatewayv1.RouteReasonBackendNotFound, "backendRef to Service %s has no port", key)
	}

	for _, port := range svc.Spec.Ports {
		if port.Port != *ref.Port {
			continue
		}

		h2 := rule.GRPC || (port.AppProtocol != nil && *port.AppProtocol == h2cAppProtocol)
		name := fmt.Sprintf("%s/%s/%d", key.Namespace, key.Name, port.Port)
		if h2 {
			name += "/h2"
		}

		return &Cluster{Name: name, Service: key, Port: port, H2: h2}, nil
	}

	return fail(gatewayv1.RouteReasonBackendNotFound, "Service %s has no port %d", key, *ref.Port)
}
