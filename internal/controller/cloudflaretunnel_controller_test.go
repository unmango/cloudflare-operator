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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
			By("Creating the custom resource for the Kind CloudflareTunnel")
			Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
		})

		AfterEach(func() {
			resource := &cfv1alpha1.CloudflareTunnel{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			if controllerutil.RemoveFinalizer(resource, cloudflareTunnelFinalizer) {
				By("Removing the resource finalizer")
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
			}

			if err == nil {
				By("Cleanup the specific resource instance CloudflareTunnel")
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		Context("and a matching tunnel does not exist", func() {
			Context("and the cloudflare new tunnel call succeeds", func() {
				var result *zero_trust.TunnelCloudflaredNewResponse

				BeforeEach(func() {
					result = &zero_trust.TunnelCloudflaredNewResponse{
						ID:              "test-id",
						AccountTag:      "test-account-id",
						CreatedAt:       time.Now(),
						ConnsActiveAt:   time.Now(),
						ConnsInactiveAt: time.Now(),
						Name:            cloudflaretunnel.Name,
						RemoteConfig:    true,
						Status:          zero_trust.TunnelCloudflaredNewResponseStatusHealthy,
						TunType:         zero_trust.TunnelCloudflaredNewResponseTunTypeCfdTunnel,
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

					status := resource.Status
					Expect(status.Name).To(Equal(result.Name))
					Expect(status.AccountTag).To(Equal(result.AccountTag))
					Expect(status.Id).To(Equal(ptr.To(result.ID)))
					Expect(status.RemoteConfig).To(Equal(result.RemoteConfig))
					Expect(status.Status).To(Equal(cfv1alpha1.HealthyCloudflareTunnelHealth))
					Expect(status.CreatedAt.Time).To(BeTemporally("~", result.CreatedAt, time.Second))
					Expect(status.ConnectionsActiveAt.Time).To(BeTemporally("~", result.ConnsActiveAt, time.Second))
					Expect(status.ConnectionsInactiveAt.Time).To(BeTemporally("~", result.ConnsInactiveAt, time.Second))
					Expect(status.Type).To(Equal(cfv1alpha1.CfdTunnelCloudflareTunnelType))
				})

				It("should add a finalizer to the CloudflareTunnel", func() {
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

					Expect(resource.Finalizers).To(ConsistOf(cloudflareTunnelFinalizer))
				})

				Context("and Name is not provided", func() {
					BeforeEach(func() {
						cloudflaretunnel.Spec.Name = ""
					})

					It("should use the resource name as the tunnel name", func() {
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
						Expect(resource.Status.Name).To(Equal(cloudflaretunnel.Name))
					})
				})
			})

			Context("and the cloudflare new tunnel call fails", func() {
				BeforeEach(func() {
					cfmock.EXPECT().
						CreateTunnel(gomock.Eq(ctx), gomock.Any()).
						Return(nil, fmt.Errorf("new tunnel failed"))
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
					Expect(err).NotTo(HaveOccurred())

					resource := &cfv1alpha1.CloudflareTunnel{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					condition := meta.FindStatusCondition(
						resource.Status.Conditions,
						typeErrorCloudflareTunnel,
					)
					Expect(condition).NotTo(BeNil(), "Condition not set")
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				})
			})
		})

		Context("and the CloudflareTunnel status contains the tunnel id", func() {
			const tunnelId = "test-tunnel-id"

			JustBeforeEach(func() {
				By("Updating the CloudflareTunnel status")
				cloudflaretunnel.Status.Id = ptr.To(tunnelId)
				Expect(k8sClient.Status().Update(ctx, cloudflaretunnel)).To(Succeed())
			})

			Context("and the cloudflare get tunnel call succeeds", func() {
				result := &zero_trust.TunnelCloudflaredGetResponse{}

				BeforeEach(func() {
					result = &zero_trust.TunnelCloudflaredGetResponse{
						ID:              tunnelId,
						AccountTag:      "test-account-tag",
						CreatedAt:       time.Now(),
						ConnsActiveAt:   time.Now(),
						ConnsInactiveAt: time.Now(),
						Name:            cloudflaretunnel.Spec.Name,
						RemoteConfig:    true,
						Status:          zero_trust.TunnelCloudflaredGetResponseStatusHealthy,
						TunType:         zero_trust.TunnelCloudflaredGetResponseTunTypeCfdTunnel,
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

					status := resource.Status
					Expect(status.Name).To(Equal(result.Name))
					Expect(status.AccountTag).To(Equal(result.AccountTag))
					Expect(status.CreatedAt.Time).To(BeTemporally("~", result.CreatedAt, time.Second))
					Expect(status.ConnectionsActiveAt.Time).To(BeTemporally("~", result.ConnsActiveAt, time.Second))
					Expect(status.ConnectionsInactiveAt.Time).To(BeTemporally("~", result.ConnsInactiveAt, time.Second))
					Expect(status.Id).To(Equal(ptr.To(result.ID)))
					Expect(status.RemoteConfig).To(Equal(result.RemoteConfig))
					Expect(status.Status).To(Equal(cfv1alpha1.HealthyCloudflareTunnelHealth))
					Expect(status.Type).To(Equal(cfv1alpha1.CfdTunnelCloudflareTunnelType))
				})

				It("should add a finalizer to the CloudflareTunnel", func() {
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
					Expect(resource.Finalizers).To(ConsistOf(cloudflareTunnelFinalizer))
				})

				Context("and the spec name does not match the tunnel", func() {
					BeforeEach(func() {
						cloudflaretunnel.Spec.Name = "a-new-name"
					})

					It("should update the tunnel name", func() {
						result := &zero_trust.TunnelCloudflaredEditResponse{
							Name: cloudflaretunnel.Spec.Name,
						}

						cfmock.EXPECT().
							EditTunnel(gomock.Eq(ctx), tunnelId, zero_trust.TunnelCloudflaredEditParams{
								AccountID: cloudflare.F(cloudflaretunnel.Spec.AccountId),
								Name:      cloudflare.F(cloudflaretunnel.Spec.Name),
							}).
							Return(result, nil)

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
						Expect(resource.Status.Name).To(Equal(cloudflaretunnel.Spec.Name))
					})
				})
			})

			Context("and the cloudflare get tunnel call fails", func() {
				BeforeEach(func() {
					cfmock.EXPECT().
						GetTunnel(gomock.Eq(ctx), gomock.Any(), gomock.Any()).
						Return(nil, fmt.Errorf("get tunnel failed"))
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
					Expect(err).NotTo(HaveOccurred())

					resource := &cfv1alpha1.CloudflareTunnel{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					condition := meta.FindStatusCondition(
						resource.Status.Conditions,
						typeErrorCloudflareTunnel,
					)
					Expect(condition).NotTo(BeNil(), "Condition not set")
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				})
			})

			Context("and the CloudflareTunnel is marked for deletion", func() {
				BeforeEach(func() {
					cloudflaretunnel.Finalizers = []string{cloudflareTunnelFinalizer}
				})

				JustBeforeEach(func() {
					By("Deleting the resource")
					Expect(k8sClient.Delete(ctx, cloudflaretunnel)).To(Succeed())
				})

				Context("and the cloudflare delete tunnel call succeeds", func() {
					BeforeEach(func() {
						result := &zero_trust.TunnelCloudflaredDeleteResponse{}

						cfmock.EXPECT().
							DeleteTunnel(gomock.Eq(ctx), tunnelId, zero_trust.TunnelCloudflaredDeleteParams{
								AccountID: cloudflare.F(cloudflaretunnel.Spec.AccountId),
							}).
							Return(result, nil)
					})

					It("should remove the finalizer", func() {
						By("Reconciling the deleted resource")
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
						err = k8sClient.Get(ctx, typeNamespacedName, resource)
						Expect(errors.IsNotFound(err)).To(BeTrueBecause("Resource was deleted"))
					})
				})

				Context("and the cloudflare delete tunnel call fails", func() {
					cferr := fmt.Errorf("delete tunnel failed")

					BeforeEach(func() {
						cfmock.EXPECT().
							DeleteTunnel(gomock.Eq(ctx), gomock.Any(), gomock.Any()).
							Return(nil, cferr)
					})

					It("should set the error status", func() {
						By("Reconciling the deleted resource")
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
					})
				})
			})
		})

		Context("and the CloudflareTunnel is marked for deletion", func() {
			BeforeEach(func() {
				cloudflaretunnel.Finalizers = []string{cloudflareTunnelFinalizer}
				// result := &zero_trust.TunnelCloudflaredNewResponse{
				// 	ID:              "test-id",
				// 	AccountTag:      "test-account-id",
				// 	CreatedAt:       time.Now(),
				// 	ConnsActiveAt:   time.Now(),
				// 	ConnsInactiveAt: time.Now(),
				// 	Name:            cloudflaretunnel.Name,
				// 	RemoteConfig:    true,
				// 	Status:          zero_trust.TunnelCloudflaredNewResponseStatusHealthy,
				// 	TunType:         zero_trust.TunnelCloudflaredNewResponseTunTypeCfdTunnel,
				// }
				// cfmock.EXPECT().
				// 	CreateTunnel(gomock.Any(), gomock.Any()).
				// 	Return(result, nil).
				// 	AnyTimes()
			})

			JustBeforeEach(func() {
				By("Deleting the resource")
				Expect(k8sClient.Delete(ctx, cloudflaretunnel)).To(Succeed())
			})

			It("should remove the finalizer", func() {
				By("Reconciling the deleted resource")
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
				err = k8sClient.Get(ctx, typeNamespacedName, resource)
				Expect(errors.IsNotFound(err)).To(BeTrueBecause("Resource was deleted"))
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
			})
		})
	})
})
