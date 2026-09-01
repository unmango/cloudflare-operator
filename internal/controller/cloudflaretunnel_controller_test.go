/*
Copyright 2026 unmango.

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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/unmango/cloudflare-operator/internal/testing"
	"go.uber.org/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CloudflareTunnel Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName = "test-resource"
			accountId    = "test-account-id"
			accountTag   = "test-account-tag"
			tunnelId     = "test-tunnel-id"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}

		var (
			cfmock           *testing.MockClient
			cloudflaretunnel *cfv1alpha1.CloudflareTunnel
		)

		reconcileOnce := func() reconcile.Result {
			GinkgoHelper()
			result, err := (&CloudflareTunnelReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Cloudflare: cfmock,
			}).Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			return result
		}

		observed := func() *cfv1alpha1.CloudflareTunnel {
			GinkgoHelper()
			resource := &cfv1alpha1.CloudflareTunnel{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			return resource
		}

		BeforeEach(func() {
			GinkgoT().Setenv("CLOUDFLARE_API_TOKEN", "test-token")

			cfmock = testing.NewMockClient(gomock.NewController(GinkgoT()))

			cloudflaretunnel = &cfv1alpha1.CloudflareTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: cfv1alpha1.CloudflareTunnelSpec{
					Name:         resourceName,
					AccountId:    accountId,
					ConfigSource: cfv1alpha1.CloudflareCloudflareTunnelConfigSource,
				},
			}
		})

		AfterEach(func() {
			deleteIfExists(ctx, typeNamespacedName, &cfv1alpha1.CloudflareTunnel{})
			deleteIfExists(ctx, typeNamespacedName, &cfv1alpha1.Cloudflared{})
		})

		Context("and a matching tunnel does not exist", func() {
			var created *shared.CloudflareTunnel

			BeforeEach(func() {
				created = &shared.CloudflareTunnel{
					ID:              tunnelId,
					AccountTag:      accountId,
					CreatedAt:       time.Now(),
					ConnsActiveAt:   time.Now(),
					ConnsInactiveAt: time.Now(),
					Name:            resourceName,
					ConfigSrc:       shared.CloudflareTunnelConfigSrcCloudflare,
					Status:          shared.CloudflareTunnelStatusHealthy,
					TunType:         shared.CloudflareTunnelTunTypeCfdTunnel,
				}
			})

			Context("and the cloudflare new tunnel call succeeds", func() {
				BeforeEach(func() {
					cfmock.EXPECT().
						CreateTunnel(gomock.Eq(ctx), gomock.Eq(zero_trust.TunnelCloudflaredNewParams{
							AccountID:    cloudflare.F(accountId),
							Name:         cloudflare.F(resourceName),
							ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
							TunnelSecret: cloudflare.Null[string](),
						})).
						Return(created, nil)

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
					reconcileOnce()
				})

				It("should mark the resource as progressing", func() {
					Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
						HaveField("Type", typeProgressingCloudflareTunnel),
						HaveField("Status", metav1.ConditionTrue),
					)))
				})

				It("should update the status from the created tunnel", func() {
					status := observed().Status

					Expect(status.Name).To(Equal(created.Name))
					Expect(status.AccountTag).To(Equal(created.AccountTag))
					Expect(status.Id).To(Equal(new(created.ID)))
					Expect(status.RemoteConfig).To(BeTrue())
					Expect(status.Status).To(Equal(cfv1alpha1.HealthyCloudflareTunnelHealth))
					Expect(status.Type).To(Equal(cfv1alpha1.CfdTunnelCloudflareTunnelType))
					Expect(status.CreatedAt.Time).To(BeTemporally("~", created.CreatedAt, time.Second))
					Expect(status.ConnectionsActiveAt.Time).To(BeTemporally("~", created.ConnsActiveAt, time.Second))
					Expect(status.ConnectionsInactiveAt.Time).To(BeTemporally("~", created.ConnsInactiveAt, time.Second))
				})

				It("should add a finalizer", func() {
					Expect(observed().Finalizers).To(ConsistOf(cloudflareTunnelFinalizer))
				})
			})

			Context("and Name is not provided", func() {
				BeforeEach(func() {
					cloudflaretunnel.Spec.Name = ""

					cfmock.EXPECT().
						CreateTunnel(gomock.Eq(ctx), gomock.Eq(zero_trust.TunnelCloudflaredNewParams{
							AccountID:    cloudflare.F(accountId),
							Name:         cloudflare.F(resourceName),
							ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
							TunnelSecret: cloudflare.Null[string](),
						})).
						Return(created, nil)

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
				})

				It("should use the resource name as the tunnel name", func() {
					reconcileOnce()
				})
			})

			Context("and the cloudflare new tunnel call fails", func() {
				BeforeEach(func() {
					cfmock.EXPECT().
						CreateTunnel(gomock.Any(), gomock.Any()).
						Return(nil, fmt.Errorf("new tunnel failed"))

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
					reconcileOnce()
				})

				It("should not record a tunnel id", func() {
					Expect(observed().Status.Id).To(BeNil())
				})
			})
		})

		Context("and the status contains the tunnel id", func() {
			var found *shared.CloudflareTunnel

			BeforeEach(func() {
				found = &shared.CloudflareTunnel{
					ID:              tunnelId,
					AccountTag:      accountTag,
					CreatedAt:       time.Now(),
					ConnsActiveAt:   time.Now(),
					ConnsInactiveAt: time.Now(),
					Name:            resourceName,
					ConfigSrc:       shared.CloudflareTunnelConfigSrcCloudflare,
					Status:          shared.CloudflareTunnelStatusHealthy,
					TunType:         shared.CloudflareTunnelTunTypeCfdTunnel,
				}

				Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
				cloudflaretunnel.Status.Id = ptr.To(tunnelId)
				Expect(k8sClient.Status().Update(ctx, cloudflaretunnel)).To(Succeed())
			})

			Context("and the cloudflare get tunnel call succeeds", func() {
				BeforeEach(func() {
					// CreateTunnel is deliberately not expected: observing an
					// existing id must not provoke a second create.
					cfmock.EXPECT().
						GetTunnel(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredGetParams{
							AccountID: cloudflare.F(accountId),
						})).
						Return(found, nil)

					reconcileOnce()
				})

				It("should mark the resource as progressing", func() {
					Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
						HaveField("Type", typeProgressingCloudflareTunnel),
						HaveField("Status", metav1.ConditionTrue),
					)))
				})

				It("should update the status from the observed tunnel", func() {
					status := observed().Status
					Expect(status.AccountTag).To(Equal(accountTag))
					Expect(status.Id).To(Equal(ptr.To(tunnelId)))
				})

				It("should add a finalizer", func() {
					Expect(observed().Finalizers).To(ConsistOf(cloudflareTunnelFinalizer))
				})
			})

			Context("and the resource is marked for deletion", func() {
				BeforeEach(func() {
					Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflaretunnel)).To(Succeed())
					cloudflaretunnel.Finalizers = []string{cloudflareTunnelFinalizer}
					Expect(k8sClient.Update(ctx, cloudflaretunnel)).To(Succeed())
					Expect(k8sClient.Delete(ctx, cloudflaretunnel)).To(Succeed())
				})

				Context("and the cloudflare delete tunnel call succeeds", func() {
					BeforeEach(func() {
						cfmock.EXPECT().
							DeleteTunnel(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Any()).
							Return(found, nil)
					})

					It("should remove the finalizer and let the resource go", func() {
						reconcileOnce()

						err := k8sClient.Get(ctx, typeNamespacedName, &cfv1alpha1.CloudflareTunnel{})
						Expect(apierrors.IsNotFound(err)).To(BeTrueBecause("Resource was deleted"))
					})
				})

				Context("and the cloudflare delete tunnel call fails", func() {
					BeforeEach(func() {
						cfmock.EXPECT().
							DeleteTunnel(gomock.Any(), gomock.Any(), gomock.Any()).
							Return(nil, fmt.Errorf("delete tunnel failed"))
					})

					It("should keep the finalizer", func() {
						reconcileOnce()

						Expect(observed().Finalizers).NotTo(BeEmpty())
					})
				})
			})
		})
	})
})
