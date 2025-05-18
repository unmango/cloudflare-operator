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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/ingress/annotation"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Ingress Controller", func() {
	var (
		reconciler         IngressReconciler
		typeNamespacedName types.NamespacedName
		ingress            *networkingv1.Ingress
	)

	BeforeEach(func() {
		reconciler = IngressReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		typeNamespacedName = types.NamespacedName{
			Namespace: "default",
			Name:      "resource-name",
		}
		ingress = &networkingv1.Ingress{
			ObjectMeta: v1.ObjectMeta{
				Name:      typeNamespacedName.Name,
				Namespace: typeNamespacedName.Namespace,
				Annotations: map[string]string{
					annotation.Definitions.ConfigSource.String(): "cloudflare",
					annotation.Definitions.AccountId.String():    "test-account",
				},
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: ptr.To("cloudflare"),
				Rules: []networkingv1.IngressRule{{
					Host: "example.com",
				}},
			},
		}
	})

	JustBeforeEach(func() {
		Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())

		tunnel := &cfv1alpha1.CloudflareTunnel{}
		if err := k8sClient.Get(ctx, typeNamespacedName, tunnel); err == nil {
			Expect(k8sClient.Delete(ctx, tunnel)).To(Succeed())
		}
	})

	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func() {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			tunnel := &cfv1alpha1.CloudflareTunnel{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tunnel)).To(Succeed())
		})

		Context("and ingressClassName does not match", func() {
			BeforeEach(func() {
				ingress.Spec.IngressClassName = ptr.To("blah")
			})

			It("should not create a tunnel", func() {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				tunnel := &cfv1alpha1.CloudflareTunnel{}
				err = k8sClient.Get(ctx, typeNamespacedName, tunnel)
				Expect(err).To(MatchError(ContainSubstring("not found")))
			})
		})
	})
})
