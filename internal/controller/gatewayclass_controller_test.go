/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/gateway"
)

var _ = Describe("GatewayClass Controller", func() {
	const className = "gatewayclass-test"

	var (
		reconciler GatewayClassReconciler
		key        types.NamespacedName
		class      *gatewayv1.GatewayClass
		config     *cfv1alpha1.CloudflareGatewayConfig
	)

	parametersRef := func(group, kind, name string, namespace *string) *gatewayv1.ParametersReference {
		ref := &gatewayv1.ParametersReference{
			Group: gatewayv1.Group(group),
			Kind:  gatewayv1.Kind(kind),
			Name:  name,
		}
		if namespace != nil {
			ns := gatewayv1.Namespace(*namespace)
			ref.Namespace = &ns
		}

		return ref
	}

	reconcileOnce := func() {
		GinkgoHelper()
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
	}

	observed := func() *gatewayv1.GatewayClass {
		GinkgoHelper()
		obj := &gatewayv1.GatewayClass{}
		Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())

		return obj
	}

	condition := func(t gatewayv1.GatewayClassConditionType) *metav1.Condition {
		GinkgoHelper()
		return meta.FindStatusCondition(observed().Status.Conditions, string(t))
	}

	BeforeEach(func() {
		reconciler = GatewayClassReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		key = types.NamespacedName{Name: className}

		config = &cfv1alpha1.CloudflareGatewayConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "params", Namespace: testNamespace},
			Spec: cfv1alpha1.CloudflareGatewayConfigSpec{
				TunnelRef: &cfv1alpha1.CloudflareGatewayTunnelReference{
					Name:      "some-tunnel",
					Namespace: testNamespace,
				},
			},
		}
		class = &gatewayv1.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: gatewayv1.GatewayClassSpec{
				ControllerName: gateway.ControllerName,
				ParametersRef:  parametersRef(gateway.ParametersGroup, gateway.ParametersKind, config.Name, &config.Namespace),
			},
		}
	})

	AfterEach(func() {
		deleteIfExists(ctx, key, &gatewayv1.GatewayClass{})
		deleteIfExists(ctx, client.ObjectKeyFromObject(config), &cfv1alpha1.CloudflareGatewayConfig{})
	})

	Context("When the class belongs to another controller", func() {
		BeforeEach(func() {
			class.Spec.ControllerName = "example.com/other-controller"
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
		})

		// The CRD defaults Accepted to Unknown/Pending, so leaving the class alone
		// means that default survives.
		It("should leave the status default and add no finalizer", func() {
			reconcileOnce()

			accepted := condition(gatewayv1.GatewayClassConditionStatusAccepted)
			Expect(accepted).NotTo(BeNil())
			Expect(accepted.Status).To(Equal(metav1.ConditionUnknown))
			Expect(accepted.Reason).To(Equal(string(gatewayv1.GatewayClassReasonPending)))
			Expect(observed().Finalizers).To(BeEmpty())
		})
	})

	Context("When the parameters resolve", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, config)).To(Succeed())
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
		})

		It("should accept the class", func() {
			reconcileOnce()

			accepted := condition(gatewayv1.GatewayClassConditionStatusAccepted)
			Expect(accepted).NotTo(BeNil())
			Expect(accepted.Status).To(Equal(metav1.ConditionTrue))
			Expect(accepted.Reason).To(Equal(string(gatewayv1.GatewayClassReasonAccepted)))
		})

		It("should report a supported version", func() {
			reconcileOnce()

			supported := condition(gatewayv1.GatewayClassConditionStatusSupportedVersion)
			Expect(supported).NotTo(BeNil())
			Expect(supported.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should add the finalizer", func() {
			reconcileOnce()

			Expect(observed().Finalizers).To(ContainElement(gatewayClassFinalizer))
		})
	})

	DescribeTable("rejecting a parametersRef the user has to fix",
		func(mutate func()) {
			mutate()
			Expect(k8sClient.Create(ctx, class)).To(Succeed())

			reconcileOnce()

			accepted := condition(gatewayv1.GatewayClassConditionStatusAccepted)
			Expect(accepted).NotTo(BeNil())
			Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
			Expect(accepted.Reason).To(Equal(reasonInvalidParameters))
			Expect(accepted.Message).NotTo(BeEmpty())
		},
		Entry("when it is absent", func() {
			class.Spec.ParametersRef = nil
		}),
		Entry("when it names another group", func() {
			class.Spec.ParametersRef.Group = "example.com"
		}),
		Entry("when it names another kind", func() {
			class.Spec.ParametersRef.Kind = "ConfigMap"
		}),
		Entry("when it omits the namespace", func() {
			class.Spec.ParametersRef.Namespace = nil
		}),
		Entry("when it names an object that does not exist", func() {
			// The config fixture is deliberately not created.
		}),
	)

	Context("When a Gateway references the class", func() {
		var gw *gatewayv1.Gateway

		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, config)).To(Succeed())
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			reconcileOnce()

			gw = &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "holder", Namespace: testNamespace},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: className,
					Listeners: []gatewayv1.Listener{{
						Name:     "http",
						Port:     80,
						Protocol: gatewayv1.HTTPProtocolType,
					}},
				},
			}
			Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		})

		AfterEach(func() {
			deleteIfExists(ctx, client.ObjectKeyFromObject(gw), &gatewayv1.Gateway{})
		})

		It("should hold the finalizer while the Gateway exists", func() {
			Expect(k8sClient.Delete(ctx, observed())).To(Succeed())

			reconcileOnce()

			Expect(observed().Finalizers).To(ContainElement(gatewayClassFinalizer))
		})

		It("should release the finalizer once the Gateway is gone", func() {
			Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
			Expect(k8sClient.Delete(ctx, observed())).To(Succeed())

			reconcileOnce()

			err := k8sClient.Get(ctx, key, &gatewayv1.GatewayClass{})
			Expect(err).To(HaveOccurred())
		})
	})

	Context("When mapping a CloudflareGatewayConfig to the classes that name it", func() {
		mapped := func() []reconcile.Request {
			GinkgoHelper()
			return gatewayClassesForConfig(k8sClient)(ctx, config)
		}

		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
		})

		It("should enqueue a class whose parametersRef names it", func() {
			Expect(mapped()).To(ConsistOf(reconcile.Request{NamespacedName: key}))
		})

		It("should ignore a class naming another object", func() {
			class.Spec.ParametersRef.Name = "other"
			Expect(k8sClient.Update(ctx, class)).To(Succeed())

			Expect(mapped()).To(BeEmpty())
		})

		It("should ignore a class naming another namespace", func() {
			ns := gatewayv1.Namespace("other")
			class.Spec.ParametersRef.Namespace = &ns
			Expect(k8sClient.Update(ctx, class)).To(Succeed())

			Expect(mapped()).To(BeEmpty())
		})

		It("should ignore a class naming another kind", func() {
			class.Spec.ParametersRef.Kind = "ConfigMap"
			Expect(k8sClient.Update(ctx, class)).To(Succeed())

			Expect(mapped()).To(BeEmpty())
		})

		It("should ignore a class without a parametersRef", func() {
			class.Spec.ParametersRef = nil
			Expect(k8sClient.Update(ctx, class)).To(Succeed())

			Expect(mapped()).To(BeEmpty())
		})
	})

	It("should reject a config setting both tunnelRef and template", func() {
		config.Spec.Template = &cfv1alpha1.CloudflareGatewayTunnelTemplate{
			Spec: cfv1alpha1.CloudflareTunnelSpec{AccountId: "test-account"},
		}

		err := k8sClient.Create(ctx, config)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one of spec.tunnelRef and spec.template"))
	})

	It("should reject a config setting neither tunnelRef nor template", func() {
		config.Spec.TunnelRef = nil

		err := k8sClient.Create(ctx, config)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one of spec.tunnelRef and spec.template"))
	})
})
