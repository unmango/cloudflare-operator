package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
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

// gatewayConfigHandler maps a CloudflareGatewayConfig to every GatewayClass whose
// parametersRef names it, so creating or deleting one settles the class's
// Accepted condition without waiting for a resync.
func gatewayConfigHandler(reader client.Reader) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(gatewayClassesForConfig(reader))
}

// gatewayClassesForConfig is the mapping gatewayConfigHandler enqueues from.
func gatewayClassesForConfig(reader client.Reader) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		log := logf.FromContext(ctx)

		classes := &gatewayv1.GatewayClassList{}
		if err := reader.List(ctx, classes); err != nil {
			log.Error(err, "Failed to list GatewayClasses referencing CloudflareGatewayConfig",
				"config", client.ObjectKeyFromObject(obj),
			)
			return nil
		}

		// Filtered in memory rather than through an index: a GatewayClass is
		// cluster scoped and there are few of them.
		requests := []reconcile.Request{}
		for _, class := range classes.Items {
			ref := class.Spec.ParametersRef
			if ref == nil {
				continue
			}
			if string(ref.Group) != gateway.ParametersGroup || string(ref.Kind) != gateway.ParametersKind {
				continue
			}
			if ref.Name != obj.GetName() {
				continue
			}
			if ref.Namespace == nil || string(*ref.Namespace) != obj.GetNamespace() {
				continue
			}

			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{Name: class.Name},
			})
		}

		return requests
	}
}
