package controller

import (
	"context"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

// routeObject is a route kind this controller writes status for.
type routeObject interface {
	client.Object
	*gatewayv1.HTTPRoute | *gatewayv1.GRPCRoute | *gatewayv1.TLSRoute | *gatewayv1.TCPRoute | *gatewayv1.UDPRoute
}

// RouteReconciler reports, on each route that names a Gateway of a class this
// controller owns, whether that Gateway accepted it and whether its backends
// resolve. It builds each such Gateway the way the xDS controller does, so the
// status always describes the configuration Envoy serves.
type RouteReconciler[T routeObject] struct {
	client.Client
	Features gateway.Features

	// New returns an empty route of the kind reconciled.
	New func() T
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status;grpcroutes/status;tlsroutes/status;tcproutes/status;udproutes/status,verbs=get;update;patch

func (r *RouteReconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := r.New()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	route, _ := routeOf(obj)

	var desired []gatewayv1.RouteParentStatus
	for _, key := range parentGateways(&route) {
		parents, err := r.parentStatuses(ctx, &route, key)
		if err != nil {
			return ctrl.Result{}, err
		}
		desired = append(desired, parents...)
	}

	original, _ := obj.DeepCopyObject().(client.Object)
	status := routeStatusOf(obj)
	status.Parents = mergeParents(status.Parents, desired, obj.GetGeneration())
	if equality.Semantic.DeepEqual(routeStatusOf(original).Parents, status.Parents) {
		return ctrl.Result{}, nil
	}

	// Other controllers write their own entries into the same list, and a merge
	// patch replaces a list whole, so a patch built from a stale read must fail
	// rather than drop an entry written since.
	return ctrl.Result{}, r.Status().Patch(ctx, obj,
		client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{}))
}

// parentStatuses builds the Gateway named key and reads off the route's status
// under each parentRef naming it. A Gateway that does not exist, or that
// another controller owns, contributes nothing.
func (r *RouteReconciler[T]) parentStatuses(ctx context.Context, route *gateway.Route, key types.NamespacedName) ([]gatewayv1.RouteParentStatus, error) {
	gw := &gatewayv1.Gateway{}
	if err := r.Get(ctx, key, gw); err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	_, ours, err := gatewayClassFor(ctx, r.Client, gw)
	if err != nil || !ours {
		return nil, err
	}

	model, _, err := buildGateway(ctx, r.Client, gw, r.Features)
	if err != nil {
		return nil, err
	}

	var out []gatewayv1.RouteParentStatus
	for _, attached := range model.Routes {
		if attached.Kind != route.Kind || attached.Namespace != route.Namespace || attached.Name != route.Name {
			continue
		}

		for _, parent := range attached.Parents {
			out = append(out, gatewayv1.RouteParentStatus{
				ParentRef:      parent.Ref,
				ControllerName: gatewayv1.GatewayController(gateway.ControllerName),
				Conditions:     []metav1.Condition{parent.Accepted, attached.ResolvedRefs},
			})
		}
	}

	return out, nil
}

func routeStatusOf(obj client.Object) *gatewayv1.RouteStatus {
	switch r := obj.(type) {
	case *gatewayv1.HTTPRoute:
		return &r.Status.RouteStatus
	case *gatewayv1.GRPCRoute:
		return &r.Status.RouteStatus
	case *gatewayv1.TLSRoute:
		return &r.Status.RouteStatus
	case *gatewayv1.TCPRoute:
		return &r.Status.RouteStatus
	case *gatewayv1.UDPRoute:
		return &r.Status.RouteStatus
	default:
		panic("unsupported route type")
	}
}

// mergeParents replaces this controller's entries in existing with desired,
// leaving other controllers' entries alone. Conditions keep their transition
// times when their status does not change.
func mergeParents(existing, desired []gatewayv1.RouteParentStatus, generation int64) []gatewayv1.RouteParentStatus {
	ours := func(p gatewayv1.RouteParentStatus) bool {
		return p.ControllerName == gatewayv1.GatewayController(gateway.ControllerName)
	}

	// Never nil: the API requires the list, even when it is empty.
	out := []gatewayv1.RouteParentStatus{}
	for _, p := range existing {
		if !ours(p) {
			out = append(out, p)
		}
	}

	for _, d := range desired {
		var conditions []metav1.Condition
		for _, p := range existing {
			if ours(p) && reflect.DeepEqual(p.ParentRef, d.ParentRef) {
				conditions = p.Conditions
				break
			}
		}

		keep := map[string]bool{}
		for _, c := range d.Conditions {
			c.ObservedGeneration = generation
			keep[c.Type] = true
			_ = meta.SetStatusCondition(&conditions, c)
		}
		d.Conditions = removeConditionsExcept(conditions, keep)
		out = append(out, d)
	}

	return out
}

// SetupWithManager sets up the controller with the Manager.
func (r *RouteReconciler[T]) SetupWithManager(mgr ctrl.Manager, name string) error {
	reader := mgr.GetClient()
	all := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, _ client.Object) []reconcile.Request {
		return r.allRoutes(ctx, reader, nil)
	})

	b := ctrl.NewControllerManagedBy(mgr).
		For(r.New()).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			key := client.ObjectKeyFromObject(obj)
			return r.allRoutes(ctx, reader, func(route *gateway.Route) bool {
				return slices.Contains(parentGateways(route), key)
			})
		})).
		Watches(&gatewayv1.GatewayClass{}, all).
		Watches(&gatewayv1beta1.ReferenceGrant{}, all).
		Watches(&corev1.Namespace{}, all).
		// A Service appearing or losing a port changes whether a backend
		// resolves. Its endpoints do not, and a listener's certificate does
		// not change what a route attaches to.
		Watches(&corev1.Service{}, all)

	return b.Named(name).Complete(r)
}

// allRoutes lists the routes of the kind reconciled that keep returns true for,
// or all of them when keep is nil.
func (r *RouteReconciler[T]) allRoutes(ctx context.Context, reader client.Reader, keep func(*gateway.Route) bool) []reconcile.Request {
	routes, err := gateway.ListRoutes(ctx, reader, r.Features)
	if err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list routes")
		return nil
	}

	kind := r.kind()
	var requests []reconcile.Request
	for i := range routes {
		if routes[i].Kind != kind || (keep != nil && !keep(&routes[i])) {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: routes[i].Namespace, Name: routes[i].Name},
		})
	}

	return requests
}

func (r *RouteReconciler[T]) kind() gateway.GroupKind {
	route, _ := routeOf(r.New())
	return route.Kind
}
