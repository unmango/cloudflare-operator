package gateway

import (
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// supportedKinds lists, per listener protocol, the route kinds a listener of
// that protocol accepts. A protocol absent from the map is not implemented yet.
var supportedKinds = map[gatewayv1.ProtocolType][]gatewayv1.Kind{
	gatewayv1.HTTPProtocolType: {"HTTPRoute"},
}

// Listener is one Gateway listener after validation.
type Listener struct {
	gatewayv1.Listener

	// SupportedKinds are the route kinds the listener accepts: those it allows
	// that this implementation also supports.
	SupportedKinds []gatewayv1.RouteGroupKind

	// Conditions are Accepted, ResolvedRefs and Conflicted. Programmed depends on
	// the data plane, so the controller adds it.
	Conditions []metav1.Condition

	// Valid reports whether the listener can be programmed: it is accepted, its
	// references resolve, and it conflicts with no other listener.
	Valid bool
}

// Listeners validates every listener on gw, in spec order.
func Listeners(gw *gatewayv1.Gateway) []Listener {
	listeners := make([]Listener, len(gw.Spec.Listeners))
	for i, l := range gw.Spec.Listeners {
		listeners[i] = validate(l)
	}

	markConflicts(listeners)
	markUnavailablePorts(listeners)

	for i := range listeners {
		l := &listeners[i]
		l.Valid = isTrue(l.Conditions, gatewayv1.ListenerConditionAccepted) &&
			isTrue(l.Conditions, gatewayv1.ListenerConditionResolvedRefs) &&
			!isTrue(l.Conditions, gatewayv1.ListenerConditionConflicted)
	}

	return listeners
}

func validate(l gatewayv1.Listener) Listener {
	out := Listener{Listener: l}

	kinds, supported := supportedKinds[l.Protocol]
	if !supported {
		out.Conditions = append(out.Conditions, metav1.Condition{
			Type:    string(gatewayv1.ListenerConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gatewayv1.ListenerReasonUnsupportedProtocol),
			Message: fmt.Sprintf("Protocol %s is not supported", l.Protocol),
		})
	} else {
		out.Conditions = append(out.Conditions, metav1.Condition{
			Type:    string(gatewayv1.ListenerConditionAccepted),
			Status:  metav1.ConditionTrue,
			Reason:  string(gatewayv1.ListenerReasonAccepted),
			Message: "Listener accepted",
		})
	}

	resolved := metav1.Condition{
		Type:    string(gatewayv1.ListenerConditionResolvedRefs),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.ListenerReasonResolvedRefs),
		Message: "References resolved",
	}

	// Without allowedRoutes.kinds a listener accepts every kind its protocol
	// supports. With them, it accepts the supported subset, and any kind outside
	// that subset is a reference that does not resolve.
	if l.AllowedRoutes == nil || len(l.AllowedRoutes.Kinds) == 0 {
		for _, kind := range kinds {
			out.SupportedKinds = append(out.SupportedKinds, routeGroupKind(kind))
		}
	} else {
		var invalid []string
		for _, rgk := range l.AllowedRoutes.Kinds {
			if (rgk.Group == nil || *rgk.Group == gatewayv1.GroupName) && slices.Contains(kinds, rgk.Kind) {
				out.SupportedKinds = append(out.SupportedKinds, routeGroupKind(rgk.Kind))
			} else {
				invalid = append(invalid, groupKindString(rgk))
			}
		}
		if len(invalid) > 0 {
			resolved = metav1.Condition{
				Type:    string(gatewayv1.ListenerConditionResolvedRefs),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayv1.ListenerReasonInvalidRouteKinds),
				Message: fmt.Sprintf("Route kinds not supported on a %s listener: %v", l.Protocol, invalid),
			}
		}
	}
	out.Conditions = append(out.Conditions, resolved)

	return out
}

// markUnavailablePorts rejects a listener whose port Envoy cannot bind: one of
// its own, or one a privileged listener port was moved to. The listener keeps
// its other conditions, but Accepted turns false.
func markUnavailablePorts(listeners []Listener) {
	ports := map[gatewayv1.PortNumber]bool{}
	for _, l := range listeners {
		ports[l.Port] = true
	}

	for i := range listeners {
		l := &listeners[i]

		var message string
		switch bound := ContainerPort(l.Port); {
		case bound == EnvoyAdminPort || bound == EnvoyReadinessPort:
			message = fmt.Sprintf("Port %d is reserved by Envoy", bound)
		case bound != l.Port && ports[bound]:
			message = fmt.Sprintf("Port %d is bound for port %d, which another listener uses", bound, l.Port)
		default:
			continue
		}

		for j, c := range l.Conditions {
			if c.Type == string(gatewayv1.ListenerConditionAccepted) {
				l.Conditions[j] = metav1.Condition{
					Type:    string(gatewayv1.ListenerConditionAccepted),
					Status:  metav1.ConditionFalse,
					Reason:  string(gatewayv1.ListenerReasonPortUnavailable),
					Message: message,
				}
			}
		}
	}
}

// transport is the layer 4 protocol a listener protocol runs over. Listeners on
// one port conflict when they share a transport but not a protocol.
func transport(p gatewayv1.ProtocolType) string {
	if p == gatewayv1.UDPProtocolType {
		return "UDP"
	}

	return "TCP"
}

// markConflicts adds a Conflicted condition to every listener. Two listeners on
// one port and transport conflict when their protocols differ, or when they
// share a protocol and a hostname, since nothing could tell their traffic apart.
func markConflicts(listeners []Listener) {
	for i := range listeners {
		a := &listeners[i]
		conflict := metav1.Condition{
			Type:    string(gatewayv1.ListenerConditionConflicted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gatewayv1.ListenerReasonNoConflicts),
			Message: "No conflicts",
		}

		for j := range listeners {
			b := &listeners[j]
			if i == j || a.Port != b.Port || transport(a.Protocol) != transport(b.Protocol) {
				continue
			}

			if a.Protocol != b.Protocol {
				conflict = metav1.Condition{
					Type:    string(gatewayv1.ListenerConditionConflicted),
					Status:  metav1.ConditionTrue,
					Reason:  string(gatewayv1.ListenerReasonProtocolConflict),
					Message: fmt.Sprintf("Listener %s uses protocol %s on port %d", b.Name, b.Protocol, b.Port),
				}
				break
			}
			if hostname(a.Hostname) == hostname(b.Hostname) {
				conflict = metav1.Condition{
					Type:    string(gatewayv1.ListenerConditionConflicted),
					Status:  metav1.ConditionTrue,
					Reason:  string(gatewayv1.ListenerReasonHostnameConflict),
					Message: fmt.Sprintf("Listener %s uses the same hostname on port %d", b.Name, b.Port),
				}
			}
		}

		a.Conditions = append(a.Conditions, conflict)
	}
}

func hostname(h *gatewayv1.Hostname) string {
	if h == nil {
		return ""
	}

	return string(*h)
}

func routeGroupKind(kind gatewayv1.Kind) gatewayv1.RouteGroupKind {
	group := gatewayv1.Group(gatewayv1.GroupName)
	return gatewayv1.RouteGroupKind{Group: &group, Kind: kind}
}

func groupKindString(rgk gatewayv1.RouteGroupKind) string {
	if rgk.Group == nil {
		return gatewayv1.GroupName + "/" + string(rgk.Kind)
	}
	if *rgk.Group == "" {
		return "core/" + string(rgk.Kind)
	}

	return string(*rgk.Group) + "/" + string(rgk.Kind)
}

func isTrue(conditions []metav1.Condition, t gatewayv1.ListenerConditionType) bool {
	for _, c := range conditions {
		if c.Type == string(t) {
			return c.Status == metav1.ConditionTrue
		}
	}

	return false
}
