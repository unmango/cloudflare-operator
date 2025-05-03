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
	"context"
	"os"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/unmango/cloudflare-operator/internal/testing"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CloudflareTunnel Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		cloudflaretunnel := &cfv1alpha1.CloudflareTunnel{}

		var (
			ctrl   *gomock.Controller
			cfmock *testing.MockClient
		)

		BeforeEach(func() {
			By("Setting the API token environment variable")
			Expect(os.Setenv("CLOUDFLARE_API_TOKEN", "test-token")).To(Succeed())

			By("Initializing the cloudflare mock")
			ctrl = gomock.NewController(GinkgoT())
			cfmock = testing.NewMockClient(ctrl)

			By("Configuring the base tunnel spec")
			cloudflaretunnel = &cfv1alpha1.CloudflareTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: cfv1alpha1.CloudflareTunnelSpec{
					Name:         resourceName,
					AccountId:    "test-account-id",
					ConfigSource: cfv1alpha1.CloudflareCloudflareTunnelConfigSource,
				},
			}
		})

		JustBeforeEach(func() {
			By("creating the custom resource for the Kind CloudflareTunnel")
			Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
		})

		AfterEach(func() {
			resource := &cfv1alpha1.CloudflareTunnel{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance CloudflareTunnel")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			cfmock.EXPECT().
				CreateTunnel(gomock.Eq(ctx), gomock.Eq(zero_trust.TunnelCloudflaredNewParams{
					AccountID:    cloudflare.F(cloudflaretunnel.Spec.AccountId),
					Name:         cloudflare.F(cloudflaretunnel.Name),
					ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
					TunnelSecret: cloudflare.Null[string](),
				})).
				Return(nil, nil)

			By("Reconciling the created resource")
			controllerReconciler := &CloudflareTunnelReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Cloudflare: cfmock,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			resource := &cfv1alpha1.CloudflareTunnel{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

			ctrl.Finish()

			condition := meta.FindStatusCondition(
				resource.Status.Conditions,
				typeAvailableCloudflareTunnel,
			)
			Expect(condition).NotTo(BeNil(), "Condition not set")
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		})

		Context("and the CLOUDFLARE_API_TOKEN env var is not defined", func() {
			BeforeEach(func() {
				Expect(os.Unsetenv("CLOUDFLARE_API_TOKEN")).To(Succeed())
			})

			It("should set the error status", func() {
				cfmock.EXPECT().
					CreateTunnel(gomock.Any(), gomock.Any()).
					Times(0)

				By("Reconciling the created resource")
				controllerReconciler := &CloudflareTunnelReconciler{
					Client:     k8sClient,
					Scheme:     k8sClient.Scheme(),
					Cloudflare: cfmock,
				}

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())

				resource := &cfv1alpha1.CloudflareTunnel{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				condition := meta.FindStatusCondition(
					resource.Status.Conditions,
					typeErrorCloudflareTunnel,
				)
				Expect(condition).NotTo(BeNil(), "Condition not set")
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))

				ctrl.Finish()
			})
		})
	})
})
