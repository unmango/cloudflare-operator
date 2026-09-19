package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ = Describe("Gateway API CRDs", func() {
	It("should accept a GatewayClass and an HTTPRoute", func() {
		class := &gatewayv1.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-api-crds"},
			Spec: gatewayv1.GatewayClassSpec{
				ControllerName: "cloudflare.unmango.dev/gateway-controller",
			},
		}
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(class), &gatewayv1.GatewayClass{})

		route := &gatewayv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{Name: "gateway-api-crds", Namespace: testNamespace},
			Spec: gatewayv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{
					ParentRefs: []gatewayv1.ParentReference{{
						Name: gatewayv1.ObjectName("gateway-api-crds"),
					}},
				},
			},
		}
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(route), &gatewayv1.HTTPRoute{})

		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		Expect(k8sClient.Create(ctx, route)).To(Succeed())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: class.Name}, &gatewayv1.GatewayClass{})).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(route), &gatewayv1.HTTPRoute{})).To(Succeed())
	})
})
