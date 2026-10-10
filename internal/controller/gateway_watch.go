package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes;grpcroutes,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch

// watchGatewayInputs adds a watch, to a controller whose requests are Gateways,
// for everything a Gateway's configuration is built from besides the Gateway
// itself and its class. Backends are the Services and EndpointSlices routes
// send traffic to, which only the Envoy configuration depends on; they change
// often, so a controller that does not need them leaves them out.
func watchGatewayInputs(b *builder.Builder, reader client.Reader, features gateway.Features, backends bool) *builder.Builder {
	routes := routeParentsHandler()
	all := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		return ourGateways(ctx, reader)
	})

	b = b.
		Watches(&gatewayv1.HTTPRoute{}, routes).
		Watches(&gatewayv1beta1.ReferenceGrant{}, all).
		Watches(&corev1.Namespace{}, all)
	if backends {
		b = b.
			Watches(&corev1.Service{}, all).
			Watches(&discoveryv1.EndpointSlice{}, all)
	}
	if features.GRPCRoutes {
		b = b.Watches(&gatewayv1.GRPCRoute{}, routes)
	}
	if features.SecretsReadable {
		b = b.Watches(&corev1.Secret{}, all)
	}

	return b
}

// routeParentsHandler maps a route to the Gateways it names, before and after
// an update, so a Gateway a route leaves is rebuilt as well as the one it joins.
func routeParentsHandler() handler.EventHandler {
	enqueue := func(q workqueue.TypedRateLimitingInterface[reconcile.Request], objs ...client.Object) {
		for _, obj := range objs {
			route, ok := routeOf(obj)
			if !ok {
				continue
			}
			for _, key := range parentGateways(&route) {
				q.Add(reconcile.Request{NamespacedName: key})
			}
		}
	}

	return handler.Funcs{
		CreateFunc: func(_ context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(q, e.Object)
		},
		UpdateFunc: func(_ context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(q, e.ObjectOld, e.ObjectNew)
		},
		DeleteFunc: func(_ context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(q, e.Object)
		},
		GenericFunc: func(_ context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(q, e.Object)
		},
	}
}

func routeOf(obj client.Object) (gateway.Route, bool) {
	switch r := obj.(type) {
	case *gatewayv1.HTTPRoute:
		return gateway.FromHTTPRoute(r), true
	case *gatewayv1.GRPCRoute:
		return gateway.FromGRPCRoute(r), true
	default:
		return gateway.Route{}, false
	}
}

// parentGateways lists the Gateways a route's parentRefs name.
func parentGateways(route *gateway.Route) []types.NamespacedName {
	var keys []types.NamespacedName
	seen := map[types.NamespacedName]bool{}
	for _, ref := range route.ParentRefs {
		if ref.Group != nil && *ref.Group != gatewayv1.GroupName {
			continue
		}
		if ref.Kind != nil && *ref.Kind != "Gateway" {
			continue
		}

		key := types.NamespacedName{Namespace: route.Namespace, Name: string(ref.Name)}
		if ref.Namespace != nil && *ref.Namespace != "" {
			key.Namespace = string(*ref.Namespace)
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}

	return keys
}

// ourGateways lists every Gateway of a class this controller owns.
func ourGateways(ctx context.Context, reader client.Reader) []reconcile.Request {
	classes := &gatewayv1.GatewayClassList{}
	if err := reader.List(ctx, classes); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list GatewayClasses")
		return nil
	}

	var names []string
	for _, class := range classes.Items {
		if string(class.Spec.ControllerName) == gateway.ControllerName {
			names = append(names, class.Name)
		}
	}

	return gatewaysForClasses(ctx, reader, names...)
}

// buildGateway reads everything gw's configuration depends on and builds it.
func buildGateway(ctx context.Context, reader client.Reader, gw *gatewayv1.Gateway, features gateway.Features) (*gateway.Model, *gateway.References, error) {
	routes, refs, err := gateway.Load(ctx, reader, gw, features)
	if err != nil {
		return nil, nil, err
	}

	return gateway.Build(gw, routes, refs), refs, nil
}
