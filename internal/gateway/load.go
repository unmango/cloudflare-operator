package gateway

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Features records what the cluster offers the Gateway controllers, discovered
// once at startup.
type Features struct {
	// GRPCRoutes, TLSRoutes, TCPRoutes and UDPRoutes report whether the CRD of
	// each route kind is installed.
	GRPCRoutes bool
	TLSRoutes  bool
	TCPRoutes  bool
	UDPRoutes  bool

	// SecretsReadable reports whether the operator may list and watch TLS
	// Secrets, which HTTPS listeners need for their certificates.
	SecretsReadable bool
}

// Load reads what Build needs for gw: the routes that name it, and the
// namespaces, grants, Services, EndpointSlices and Secrets those routes and its
// listeners refer to.
func Load(ctx context.Context, reader client.Reader, gw *gatewayv1.Gateway, features Features) ([]Route, *References, error) {
	routes, err := ListRoutes(ctx, reader, features)
	if err != nil {
		return nil, nil, err
	}
	routes = filterRoutes(routes, gw)

	refs := &References{
		Namespaces:      map[string]labels.Set{},
		Services:        map[types.NamespacedName]*corev1.Service{},
		Endpoints:       map[types.NamespacedName][]discoveryv1.EndpointSlice{},
		Secrets:         map[types.NamespacedName]*corev1.Secret{},
		SecretsReadable: features.SecretsReadable,
	}

	namespaces := &corev1.NamespaceList{}
	if err := reader.List(ctx, namespaces); err != nil {
		return nil, nil, err
	}
	for _, ns := range namespaces.Items {
		refs.Namespaces[ns.Name] = ns.Labels
	}

	grants := &gatewayv1beta1.ReferenceGrantList{}
	if err := reader.List(ctx, grants); err != nil && !meta.IsNoMatchError(err) {
		return nil, nil, err
	}
	refs.Grants = grants.Items

	for _, route := range routes {
		for _, rule := range route.Rules {
			for _, ref := range rule.Backends {
				if group(ref.Group, corev1.GroupName) != ServiceKind.Group || kind(ref.Kind, ServiceKind.Kind) != ServiceKind.Kind {
					continue
				}

				key := types.NamespacedName{Namespace: namespace(ref.Namespace, route.Namespace), Name: string(ref.Name)}
				if err := loadService(ctx, reader, refs, key); err != nil {
					return nil, nil, err
				}
			}
		}
	}

	if features.SecretsReadable {
		for _, l := range gw.Spec.Listeners {
			if l.TLS == nil {
				continue
			}
			for _, ref := range l.TLS.CertificateRefs {
				if group(ref.Group, corev1.GroupName) != SecretKind.Group || kind(ref.Kind, SecretKind.Kind) != SecretKind.Kind {
					continue
				}

				key := types.NamespacedName{Namespace: namespace(ref.Namespace, gw.Namespace), Name: string(ref.Name)}
				secret := &corev1.Secret{}
				if err := reader.Get(ctx, key, secret); apierrors.IsNotFound(err) {
					continue
				} else if err != nil {
					return nil, nil, err
				}
				refs.Secrets[key] = secret
			}
		}
	}

	return routes, refs, nil
}

func loadService(ctx context.Context, reader client.Reader, refs *References, key types.NamespacedName) error {
	if _, ok := refs.Services[key]; ok {
		return nil
	}

	svc := &corev1.Service{}
	if err := reader.Get(ctx, key, svc); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	refs.Services[key] = svc

	slices := &discoveryv1.EndpointSliceList{}
	if err := reader.List(ctx, slices, client.InNamespace(key.Namespace),
		client.MatchingLabels{discoveryv1.LabelServiceName: key.Name}); err != nil {
		return err
	}
	refs.Endpoints[key] = slices.Items

	return nil
}

// ListRoutes reads every HTTPRoute in the cluster, and every route of each
// other kind whose CRD is installed.
func ListRoutes(ctx context.Context, reader client.Reader, features Features) ([]Route, error) {
	var routes []Route

	httpRoutes := &gatewayv1.HTTPRouteList{}
	if err := reader.List(ctx, httpRoutes); err != nil {
		return nil, err
	}
	for i := range httpRoutes.Items {
		routes = append(routes, FromHTTPRoute(&httpRoutes.Items[i]))
	}

	if features.GRPCRoutes {
		list := &gatewayv1.GRPCRouteList{}
		if err := reader.List(ctx, list); err != nil {
			return nil, err
		}
		for i := range list.Items {
			routes = append(routes, FromGRPCRoute(&list.Items[i]))
		}
	}

	if features.TLSRoutes {
		list := &gatewayv1.TLSRouteList{}
		if err := reader.List(ctx, list); err != nil {
			return nil, err
		}
		for i := range list.Items {
			routes = append(routes, FromTLSRoute(&list.Items[i]))
		}
	}

	if features.TCPRoutes {
		list := &gatewayv1.TCPRouteList{}
		if err := reader.List(ctx, list); err != nil {
			return nil, err
		}
		for i := range list.Items {
			routes = append(routes, FromTCPRoute(&list.Items[i]))
		}
	}

	if features.UDPRoutes {
		list := &gatewayv1.UDPRouteList{}
		if err := reader.List(ctx, list); err != nil {
			return nil, err
		}
		for i := range list.Items {
			routes = append(routes, FromUDPRoute(&list.Items[i]))
		}
	}

	return routes, nil
}

func filterRoutes(routes []Route, gw *gatewayv1.Gateway) []Route {
	out := routes[:0]
	for _, r := range routes {
		if r.RefersTo(gw) && r.DeletionTimestamp.IsZero() {
			out = append(out, r)
		}
	}

	return out
}
