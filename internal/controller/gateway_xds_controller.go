package controller

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"

	"github.com/unmango/cloudflare-operator/internal/gateway"
	"github.com/unmango/cloudflare-operator/internal/xds"
)

// GatewayXDSReconciler translates each Gateway of a class this controller owns
// into the snapshot its Envoy streams. It writes nothing to the API server, so
// it runs on every replica, not just the leader: an Envoy may reach any of them
// through the xDS Service and has to find its snapshot there.
type GatewayXDSReconciler struct {
	client.Client
	Cache    cachev3.SnapshotCache
	Features gateway.Features
}

func (r *GatewayXDSReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := gateway.NodeID(req.Namespace, req.Name)

	gw := &gatewayv1.Gateway{}
	if err := r.Get(ctx, req.NamespacedName, gw); err != nil {
		if client.IgnoreNotFound(err) == nil {
			r.Cache.ClearSnapshot(node)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	_, ours, err := gatewayClassFor(ctx, r.Client, gw)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ours || !gw.DeletionTimestamp.IsZero() {
		r.Cache.ClearSnapshot(node)
		return ctrl.Result{}, nil
	}

	model, refs, err := buildGateway(ctx, r.Client, gw, r.Features)
	if err != nil {
		return ctrl.Result{}, err
	}

	snapshot, err := xds.Snapshot(model, refs)
	if err != nil {
		return ctrl.Result{}, err
	}

	logf.FromContext(ctx).V(1).Info("Setting xDS snapshot", "node", node, "version", snapshot.GetVersion(resourcev3.ListenerType))
	return ctrl.Result{}, r.Cache.SetSnapshot(ctx, node, snapshot)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayXDSReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{}).
		Watches(&gatewayv1.GatewayClass{}, gatewaysForClassHandler(mgr.GetClient()))

	return watchGatewayInputs(b, mgr.GetClient(), r.Features, true).
		WithOptions(controller.Options{NeedLeaderElection: new(false)}).
		Named("gateway-xds").
		Complete(r)
}
