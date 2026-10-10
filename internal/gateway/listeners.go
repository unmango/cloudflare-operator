package gateway

import (
	"crypto/tls"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// supportedKinds lists, per listener protocol, the route kinds a listener of
// that protocol accepts. A protocol absent from the map is not implemented yet.
var supportedKinds = map[gatewayv1.ProtocolType][]gatewayv1.Kind{
	gatewayv1.HTTPProtocolType:  {KindHTTPRoute, KindGRPCRoute},
	gatewayv1.HTTPSProtocolType: {KindHTTPRoute, KindGRPCRoute},
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

	// Certificates are the Secrets an HTTPS listener terminates TLS with, once
	// they resolve.
	Certificates []*corev1.Secret

	// Valid reports whether the listener can be programmed: it is accepted, its
	// references resolve, and it conflicts with no other listener.
	Valid bool
}

// Listeners validates every listener on gw, in spec order. refs supplies the
// certificates of HTTPS listeners and the grants that let a listener use one
// from another namespace; nil resolves none.
func Listeners(gw *gatewayv1.Gateway, refs *References) []Listener {
	if refs == nil {
		refs = &References{}
	}

	listeners := make([]Listener, len(gw.Spec.Listeners))
	for i, l := range gw.Spec.Listeners {
		listeners[i] = validate(gw.Namespace, l, refs)
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

func validate(gwNamespace string, l gatewayv1.Listener, refs *References) Listener {
	out := Listener{Listener: l}

	kinds, supported := supportedKinds[l.Protocol]
	switch {
	case !supported:
		out.Conditions = append(out.Conditions, metav1.Condition{
			Type:    string(gatewayv1.ListenerConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gatewayv1.ListenerReasonUnsupportedProtocol),
			Message: fmt.Sprintf("Protocol %s is not supported", l.Protocol),
		})
	case l.Protocol == gatewayv1.HTTPSProtocolType && (l.TLS == nil || tlsMode(l.TLS) != gatewayv1.TLSModeTerminate):
		out.Conditions = append(out.Conditions, metav1.Condition{
			Type:    string(gatewayv1.ListenerConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gatewayv1.ListenerReasonUnsupportedValue),
			Message: "An HTTPS listener must set tls.mode Terminate",
		})
	default:
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

	if l.Protocol == gatewayv1.HTTPSProtocolType && l.TLS != nil && resolved.Status == metav1.ConditionTrue {
		certs, failed := resolveCertificates(gwNamespace, l.TLS.CertificateRefs, refs)
		if failed != nil {
			resolved = *failed
		} else {
			out.Certificates = certs
		}
	}
	out.Conditions = append(out.Conditions, resolved)

	return out
}

func tlsMode(config *gatewayv1.ListenerTLSConfig) gatewayv1.TLSModeType {
	if config.Mode == nil {
		return gatewayv1.TLSModeTerminate
	}

	return *config.Mode
}

// resolveCertificates looks up the Secrets a listener terminates TLS with. It
// returns the ResolvedRefs condition to report instead when one of them does
// not resolve to a usable certificate.
func resolveCertificates(gwNamespace string, certRefs []gatewayv1.SecretObjectReference, refs *References) ([]*corev1.Secret, *metav1.Condition) {
	fail := func(reason gatewayv1.ListenerConditionReason, format string, args ...any) ([]*corev1.Secret, *metav1.Condition) {
		return nil, &metav1.Condition{
			Type:    string(gatewayv1.ListenerConditionResolvedRefs),
			Status:  metav1.ConditionFalse,
			Reason:  string(reason),
			Message: fmt.Sprintf(format, args...),
		}
	}

	if len(certRefs) == 0 {
		return fail(gatewayv1.ListenerReasonInvalidCertificateRef, "An HTTPS listener needs at least one certificateRef")
	}

	certs := make([]*corev1.Secret, 0, len(certRefs))
	for _, ref := range certRefs {
		g, k := group(ref.Group, corev1.GroupName), kind(ref.Kind, SecretKind.Kind)
		ns := namespace(ref.Namespace, gwNamespace)
		key := types.NamespacedName{Namespace: ns, Name: string(ref.Name)}

		if g != SecretKind.Group || k != SecretKind.Kind {
			return fail(gatewayv1.ListenerReasonInvalidCertificateRef, "certificateRef %s/%s %s is not a Secret", g, k, key)
		}
		if !refs.Permitted(GatewayKind, gwNamespace, SecretKind, ns, key.Name) {
			return fail(gatewayv1.ListenerReasonRefNotPermitted, "No ReferenceGrant allows a Gateway in %s to use Secret %s", gwNamespace, key)
		}
		if !refs.SecretsReadable {
			return fail(gatewayv1.ListenerReasonInvalidCertificateRef,
				"The operator is not allowed to read TLS Secrets; enable rbac.gatewayTLSSecrets in the chart")
		}

		secret, ok := refs.Secrets[key]
		if !ok {
			return fail(gatewayv1.ListenerReasonInvalidCertificateRef, "Secret %s does not exist", key)
		}
		if secret.Type != corev1.SecretTypeTLS {
			return fail(gatewayv1.ListenerReasonInvalidCertificateRef, "Secret %s is not of type %s", key, corev1.SecretTypeTLS)
		}
		if _, err := tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey]); err != nil {
			return fail(gatewayv1.ListenerReasonInvalidCertificateRef, "Secret %s does not hold a valid certificate: %s", key, err)
		}

		certs = append(certs, secret)
	}

	return certs, nil
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
