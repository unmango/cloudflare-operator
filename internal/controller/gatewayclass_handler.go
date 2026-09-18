package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// gatewayClassHandler maps a Gateway to the class it names, so deleting the last
// Gateway releases the class finalizer without waiting for a resync.
func gatewayClassHandler() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
		gw, ok := obj.(*gatewayv1.Gateway)
		if !ok {
			return nil
		}

		return []reconcile.Request{{
			NamespacedName: client.ObjectKey{Name: string(gw.Spec.GatewayClassName)},
		}}
	})
}
