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
	"fmt"
	"os"
	"time"

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

		Context("and the cloudflare new tunnel call succeeds", func() {
			var result *zero_trust.TunnelCloudflaredNewResponse

			BeforeEach(func() {
				result = &zero_trust.TunnelCloudflaredNewResponse{
					ID:           "test-id",
					AccountTag:   "test-account-id",
					CreatedAt:    time.Now(),
					Name:         cloudflaretunnel.Name,
					RemoteConfig: true,
					Status:       zero_trust.TunnelCloudflaredNewResponseStatusHealthy,
					TunType:      zero_trust.TunnelCloudflaredNewResponseTunTypeCfdTunnel,
				}

				cfmock.EXPECT().
					CreateTunnel(gomock.Eq(ctx), gomock.Eq(zero_trust.TunnelCloudflaredNewParams{
						AccountID:    cloudflare.F(cloudflaretunnel.Spec.AccountId),
						Name:         cloudflare.F(cloudflaretunnel.Name),
						ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
						TunnelSecret: cloudflare.Null[string](),
					})).
					Return(result, nil)
			})

			It("should successfully reconcile the resource", func() {
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

			It("should update the tunnel status with the result of the request", func() {
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

				Expect(resource.Status.AccountTag).To(Equal(result.AccountTag))
				Expect(resource.Status.Id).To(Equal(result.ID))
				Expect(resource.Status.RemoteConfig).To(Equal(result.RemoteConfig))
				Expect(resource.Status.Status).To(Equal(result.Status))
				Expect(resource.Status.CreatedAt).To(Equal(result.CreatedAt.String()))
			})
		})

		Context("and the cloudflare new tunnel call fails", func() {
			cferr := fmt.Errorf("new tunnel failed")

			BeforeEach(func() {
				cfmock.EXPECT().
					CreateTunnel(gomock.Eq(ctx), gomock.Any()).
					Return(nil, cferr)
			})

			It("should set the error status", func() {
				By("Reconciling the created resource")
				controllerReconciler := &CloudflareTunnelReconciler{
					Client:     k8sClient,
					Scheme:     k8sClient.Scheme(),
					Cloudflare: cfmock,
				}

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).To(MatchError(cferr))

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

		Context("and a tunnel with the same name exists", func() {
			existingTunnel := zero_trust.TunnelCloudflaredListResponse{}

			BeforeEach(func() {
				existingTunnel = zero_trust.TunnelCloudflaredListResponse{
					ID:           "test-tunnel-id",
					AccountTag:   "test-account-tag",
					CreatedAt:    time.Now(),
					Name:         cloudflaretunnel.Spec.Name,
					RemoteConfig: true,
					Status:       zero_trust.TunnelCloudflaredListResponseStatusHealthy,
					TunType:      zero_trust.TunnelCloudflaredListResponseTunTypeCfdTunnel,
				}
				result := []zero_trust.TunnelCloudflaredListResponse{existingTunnel}

				cfmock.EXPECT().
					ListTunnels(gomock.Eq(ctx), gomock.Eq(zero_trust.TunnelCloudflaredListParams{
						AccountID: cloudflare.F(cloudflaretunnel.Spec.AccountId),
						Name:      cloudflare.F(cloudflaretunnel.Spec.Name),
					})).
					Return(result, nil)
			})

			It("should not attempt to create a new tunnel", func() {
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

				// Since no setups for CreateTunnel were configured,
				// this will error if CreateTunnel was called
				ctrl.Finish()
			})

			It("should mark the CloudflareTunnel resource as available", func() {
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

				ctrl.Finish()

				resource := &cfv1alpha1.CloudflareTunnel{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				condition := meta.FindStatusCondition(
					resource.Status.Conditions,
					typeAvailableCloudflareTunnel,
				)
				Expect(condition).NotTo(BeNil(), "Condition not set")
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			})

			It("should update the status from the observed tunnel", func() {
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

				ctrl.Finish()

				resource := &cfv1alpha1.CloudflareTunnel{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource.Status.AccountTag).To(Equal(existingTunnel.AccountTag))
				Expect(resource.Status.CreatedAt).To(Equal(existingTunnel.CreatedAt.String()))
				Expect(resource.Status.Id).To(Equal(existingTunnel.ID))
				Expect(resource.Status.RemoteConfig).To(Equal(existingTunnel.RemoteConfig))
				Expect(resource.Status.Status).To(Equal(existingTunnel.Status))
			})
		})

		Context("and the CloudflareTunnel status contains the tunnel id", func() {
			BeforeEach(func() {
				cloudflaretunnel.Status.Id = "test-tunnel-id"
			})

			Context("and the get tunnel call succeeds", func() {
				result := &zero_trust.TunnelCloudflaredGetResponse{}

				BeforeEach(func() {
					result = &zero_trust.TunnelCloudflaredGetResponse{
						ID:           cloudflaretunnel.Status.Id,
						AccountTag:   "test-account-tag",
						CreatedAt:    time.Now(),
						Name:         cloudflaretunnel.Spec.Name,
						RemoteConfig: true,
						Status:       zero_trust.TunnelCloudflaredGetResponseStatusHealthy,
						TunType:      zero_trust.TunnelCloudflaredGetResponseTunTypeCfdTunnel,
					}

					cfmock.EXPECT().
						GetTunnel(gomock.Eq(ctx), gomock.Eq(result.ID), gomock.Eq(zero_trust.TunnelCloudflaredGetParams{
							AccountID: cloudflare.F(cloudflaretunnel.Spec.AccountId),
						})).
						Return(result, nil)
				})

				It("should not attempt to create a new tunnel", func() {
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

					// Since no setups for CreateTunnel were configured,
					// this will error if CreateTunnel was called
					ctrl.Finish()
				})

				It("should mark the CloudflareTunnel resource as available", func() {
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

					ctrl.Finish()

					resource := &cfv1alpha1.CloudflareTunnel{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					condition := meta.FindStatusCondition(
						resource.Status.Conditions,
						typeAvailableCloudflareTunnel,
					)
					Expect(condition).NotTo(BeNil(), "Condition not set")
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				})

				It("should update the status from the observed tunnel", func() {
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

					ctrl.Finish()

					resource := &cfv1alpha1.CloudflareTunnel{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource.Status.AccountTag).To(Equal(result.AccountTag))
					Expect(resource.Status.CreatedAt).To(Equal(result.CreatedAt.String()))
					Expect(resource.Status.Id).To(Equal(result.ID))
					Expect(resource.Status.RemoteConfig).To(Equal(result.RemoteConfig))
					Expect(resource.Status.Status).To(Equal(result.Status))
				})
			})
		})

		Context("and the cloudflare list tunnels call fails", func() {
			cferr := fmt.Errorf("list tunnels failed")

			BeforeEach(func() {
				cfmock.EXPECT().
					ListTunnels(gomock.Eq(ctx), gomock.Any()).
					Return(nil, cferr)
			})

			It("should set the error status", func() {
				By("Reconciling the created resource")
				controllerReconciler := &CloudflareTunnelReconciler{
					Client:     k8sClient,
					Scheme:     k8sClient.Scheme(),
					Cloudflare: cfmock,
				}

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).To(MatchError(cferr))

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
