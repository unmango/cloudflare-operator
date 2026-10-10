package gateway

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// References are the objects other than routes that a Gateway's configuration
// depends on, read from the cluster before translation so the translation itself
// stays a pure function.
type References struct {
	// Namespaces holds the labels of every namespace, for listeners that select
	// the namespaces routes may attach from.
	Namespaces map[string]labels.Set

	// Grants are the ReferenceGrants of every namespace.
	Grants []gatewayv1beta1.ReferenceGrant

	// Services and Endpoints are the Services routes send traffic to, and the
	// EndpointSlices of each, by the Service's name.
	Services  map[types.NamespacedName]*corev1.Service
	Endpoints map[types.NamespacedName][]discoveryv1.EndpointSlice

	// Secrets are the listener certificates. SecretsReadable is false when the
	// operator has not been granted access to Secrets, in which case Secrets is
	// empty and no certificate resolves.
	Secrets         map[types.NamespacedName]*corev1.Secret
	SecretsReadable bool
}

// GroupKind names a kind of object in a reference.
type GroupKind struct {
	Group string
	Kind  string
}

// Route kinds this implementation supports.
const (
	KindHTTPRoute gatewayv1.Kind = "HTTPRoute"
	KindGRPCRoute gatewayv1.Kind = "GRPCRoute"
	KindTLSRoute  gatewayv1.Kind = "TLSRoute"
	KindTCPRoute  gatewayv1.Kind = "TCPRoute"
	KindUDPRoute  gatewayv1.Kind = "UDPRoute"
)

// Kinds that appear in references.
var (
	GatewayKind   = GroupKind{Group: gatewayv1.GroupName, Kind: "Gateway"}
	HTTPRouteKind = GroupKind{Group: gatewayv1.GroupName, Kind: string(KindHTTPRoute)}
	GRPCRouteKind = GroupKind{Group: gatewayv1.GroupName, Kind: string(KindGRPCRoute)}
	TLSRouteKind  = GroupKind{Group: gatewayv1.GroupName, Kind: string(KindTLSRoute)}
	TCPRouteKind  = GroupKind{Group: gatewayv1.GroupName, Kind: string(KindTCPRoute)}
	UDPRouteKind  = GroupKind{Group: gatewayv1.GroupName, Kind: string(KindUDPRoute)}
	SecretKind    = GroupKind{Group: corev1.GroupName, Kind: "Secret"}
	ServiceKind   = GroupKind{Group: corev1.GroupName, Kind: "Service"}
)

// Permitted reports whether an object of kind from in namespace fromNamespace
// may refer to the object of kind to named name in toNamespace. A reference
// within one namespace is always permitted; one across namespaces needs a
// ReferenceGrant in the target namespace.
func (r *References) Permitted(from GroupKind, fromNamespace string, to GroupKind, toNamespace, name string) bool {
	if fromNamespace == toNamespace {
		return true
	}

	for _, grant := range r.Grants {
		if grant.Namespace != toNamespace {
			continue
		}

		fromMatches := false
		for _, f := range grant.Spec.From {
			if string(f.Group) == from.Group && string(f.Kind) == from.Kind && string(f.Namespace) == fromNamespace {
				fromMatches = true
				break
			}
		}
		if !fromMatches {
			continue
		}

		for _, t := range grant.Spec.To {
			if string(t.Group) != to.Group || string(t.Kind) != to.Kind {
				continue
			}
			if t.Name == nil || string(*t.Name) == name {
				return true
			}
		}
	}

	return false
}

func (r *References) namespaceLabels(name string) labels.Set {
	if r == nil || r.Namespaces == nil {
		return nil
	}

	return r.Namespaces[name]
}

// group and kind read the optional group and kind of a reference, applying
// the defaults the API gives them.
func group(g *gatewayv1.Group, def string) string {
	if g == nil {
		return def
	}

	return string(*g)
}

func kind(k *gatewayv1.Kind, def string) string {
	if k == nil {
		return def
	}

	return string(*k)
}

func namespace(ns *gatewayv1.Namespace, def string) string {
	if ns == nil || *ns == "" {
		return def
	}

	return string(*ns)
}
