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
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/shared"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/unmango/cloudflare-operator/internal/testing"
	"go.uber.org/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CloudflareTunnel Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName = "test-resource"
			accountId    = "test-account-id"
			accountTag   = "test-account-tag"
			tunnelId     = "test-tunnel-id"
			secretName   = "test-tunnel-secret"
		)

		// Cloudflare requires a base64 string decoding to at least 32 bytes.
		tunnelSecret := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))

		// audTag and teamName are required by the CRD, so even an unused access
		// block has to carry them.
		originRequest := cfv1alpha1.CloudflareTunnelOriginRequest{
			Access: cfv1alpha1.CloudflareTunnelOriginRequestAccess{
				AudTag:   []string{},
				TeamName: "test-team",
			},
		}

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
				Sources:    k8sClient,
			}).Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			return result
		}

		// reconcileFails is the counterpart to reconcileOnce for the paths where
		// a failed API call has to surface as a failed reconcile.
		reconcileFails := func() {
			GinkgoHelper()
			_, err := (&CloudflareTunnelReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Cloudflare: cfmock,
				Sources:    k8sClient,
			}).Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
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
			deleteIfExists(ctx, types.NamespacedName{
				Name:      secretName,
				Namespace: testNamespace,
			}, &corev1.Secret{})
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

			Context("and a tunnel secret is provided inline", func() {
				BeforeEach(func() {
					cloudflaretunnel.Spec.TunnelSecret = &cfv1alpha1.CloudflareTunnelSecret{
						Value: new(tunnelSecret),
					}

					cfmock.EXPECT().
						CreateTunnel(gomock.Any(), gomock.Eq(zero_trust.TunnelCloudflaredNewParams{
							AccountID:    cloudflare.F(accountId),
							Name:         cloudflare.F(resourceName),
							ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
							TunnelSecret: cloudflare.F(tunnelSecret),
						})).
						Return(created, nil)

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
				})

				It("should send the secret to the API", func() {
					reconcileOnce()
				})
			})

			Context("and a tunnel secret is read from a Secret", func() {
				BeforeEach(func() {
					Expect(k8sClient.Create(ctx, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      secretName,
							Namespace: testNamespace,
						},
						Data: map[string][]byte{testSecretKey: []byte(tunnelSecret)},
					})).To(Succeed())

					cloudflaretunnel.Spec.TunnelSecret = &cfv1alpha1.CloudflareTunnelSecret{
						ValueFrom: &cfv1alpha1.CloudflareTunnelSecretReference{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
								Key:                  testSecretKey,
							},
						},
					}

					cfmock.EXPECT().
						CreateTunnel(gomock.Any(), gomock.Eq(zero_trust.TunnelCloudflaredNewParams{
							AccountID:    cloudflare.F(accountId),
							Name:         cloudflare.F(resourceName),
							ConfigSrc:    cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
							TunnelSecret: cloudflare.F(tunnelSecret),
						})).
						Return(created, nil)

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
				})

				It("should send the secret to the API", func() {
					reconcileOnce()
				})
			})

			Context("and the referenced Secret does not exist", func() {
				BeforeEach(func() {
					// CreateTunnel is deliberately not expected: an unresolvable
					// secret must not reach the API.
					cloudflaretunnel.Spec.TunnelSecret = &cfv1alpha1.CloudflareTunnelSecret{
						ValueFrom: &cfv1alpha1.CloudflareTunnelSecretReference{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
								Key:                  testSecretKey,
							},
						},
					}

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
					// A create that never reached Cloudflare requeues rather than
					// erroring, so nothing else has to notice the Secret appearing.
					reconcileOnce()
				})

				It("should mark the resource as degraded", func() {
					reconcileOnce()

					Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
						HaveField("Type", typeDegradedCloudflareTunnel),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", reasonInvalidSpec),
					)))
				})

				It("should try again without waiting for a spec change", func() {
					// Nothing watches the referenced Secret, so a requeue is the
					// only thing that notices it appearing later.
					Expect(reconcileOnce().RequeueAfter).To(Equal(retryAfterFailedCreate))
				})
			})

			Context("and valueFrom names no source", func() {
				It("should be rejected by the api server", func() {
					cloudflaretunnel.Spec.TunnelSecret = &cfv1alpha1.CloudflareTunnelSecret{
						ValueFrom: &cfv1alpha1.CloudflareTunnelSecretReference{},
					}

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).NotTo(Succeed())
				})
			})

			Context("and valueFrom names both a Secret and a ConfigMap", func() {
				It("should be rejected by the api server", func() {
					cloudflaretunnel.Spec.TunnelSecret = &cfv1alpha1.CloudflareTunnelSecret{
						ValueFrom: &cfv1alpha1.CloudflareTunnelSecretReference{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
								Key:                  testSecretKey,
							},
							ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
								Key:                  testSecretKey,
							},
						},
					}

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).NotTo(Succeed())
				})
			})

			Context("and the tunnel secret is too short", func() {
				BeforeEach(func() {
					// CreateTunnel is deliberately not expected: Cloudflare
					// requires at least 32 bytes and would reject this.
					cloudflaretunnel.Spec.TunnelSecret = &cfv1alpha1.CloudflareTunnelSecret{
						Value: new(base64.StdEncoding.EncodeToString([]byte("too-short"))),
					}

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
					reconcileOnce()
				})

				It("should mark the resource as degraded", func() {
					Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
						HaveField("Type", typeDegradedCloudflareTunnel),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", reasonInvalidSpec),
					)))
				})
			})

			Context("and the tunnel name is already taken", func() {
				listParams := zero_trust.TunnelCloudflaredListParams{
					AccountID: cloudflare.F(accountId),
					Name:      cloudflare.F(resourceName),
					IsDeleted: cloudflare.F(false),
				}

				BeforeEach(func() {
					// Error() dereferences Request and Response, and the
					// controller logs the error.
					cfmock.EXPECT().
						CreateTunnel(gomock.Any(), gomock.Any()).
						Return(nil, &cloudflare.Error{
							StatusCode: http.StatusConflict,
							Request:    httptest.NewRequest(http.MethodPost, "/", nil),
							Response:   &http.Response{StatusCode: http.StatusConflict},
						})

					Expect(k8sClient.Create(ctx, cloudflaretunnel)).To(Succeed())
				})

				Context("and exactly one tunnel has that name", func() {
					BeforeEach(func() {
						cfmock.EXPECT().
							ListTunnels(gomock.Any(), gomock.Eq(listParams)).
							Return([]shared.CloudflareTunnel{*created}, nil)

						reconcileOnce()
					})

					It("should record the existing tunnel", func() {
						status := observed().Status

						Expect(status.Id).To(Equal(new(created.ID)))
						Expect(status.Name).To(Equal(created.Name))
						Expect(status.AccountTag).To(Equal(created.AccountTag))
						Expect(status.RemoteConfig).To(BeTrue())
						Expect(status.Status).To(Equal(cfv1alpha1.HealthyCloudflareTunnelHealth))
						Expect(status.Type).To(Equal(cfv1alpha1.CfdTunnelCloudflareTunnelType))
					})
				})

				Context("and no tunnel has that name", func() {
					var result reconcile.Result

					BeforeEach(func() {
						cfmock.EXPECT().
							ListTunnels(gomock.Any(), gomock.Eq(listParams)).
							Return(nil, nil)

						result = reconcileOnce()
					})

					It("should not record a tunnel id", func() {
						Expect(observed().Status.Id).To(BeNil())
					})

					It("should try again later", func() {
						Expect(result.RequeueAfter).To(Equal(retryAfterFailedCreate))
					})
				})

				Context("and more than one tunnel has that name", func() {
					BeforeEach(func() {
						other := *created
						other.ID = "other-tunnel-id"

						cfmock.EXPECT().
							ListTunnels(gomock.Any(), gomock.Eq(listParams)).
							Return([]shared.CloudflareTunnel{*created, other}, nil)

						reconcileOnce()
					})

					It("should not record a tunnel id", func() {
						Expect(observed().Status.Id).To(BeNil())
					})

					It("should mark the resource as degraded", func() {
						Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
							HaveField("Type", typeDegradedCloudflareTunnel),
							HaveField("Status", metav1.ConditionTrue),
							HaveField("Reason", reasonInvalidSpec),
							HaveField("Message", ContainSubstring(resourceName)),
						)))
					})
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

				// The finalizer goes on before the create is attempted, so a
				// tunnel that never reached Cloudflare still has to be able to
				// leave the cluster.
				It("should release the finalizer when deleted", func() {
					Expect(observed().Finalizers).To(ConsistOf(cloudflareTunnelFinalizer))
					Expect(k8sClient.Delete(ctx, cloudflaretunnel)).To(Succeed())

					reconcileOnce()

					err := k8sClient.Get(ctx, typeNamespacedName, &cfv1alpha1.CloudflareTunnel{})
					Expect(apierrors.IsNotFound(err)).To(BeTrueBecause("Resource was deleted"))
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

			Context("and config is provided", func() {
				BeforeEach(func() {
					Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflaretunnel)).To(Succeed())
					cloudflaretunnel.Spec.Config = &cfv1alpha1.CloudflareTunnelConfig{
						Ingress: []cfv1alpha1.CloudflareTunnelConfigIngress{{
							Hostname:      "test.example.com",
							Service:       "http://test.default.svc.cluster.local:80",
							OriginRequest: originRequest,
						}, {
							Service:       testCatchAllService,
							OriginRequest: originRequest,
						}},
						OriginRequest: originRequest,
					}
					Expect(k8sClient.Update(ctx, cloudflaretunnel)).To(Succeed())

					cfmock.EXPECT().
						GetTunnel(gomock.Any(), gomock.Eq(tunnelId), gomock.Any()).
						Return(found, nil)
				})

				Context("and the tunnel is remotely managed", func() {
					BeforeEach(func() {
						cfmock.EXPECT().
							UpdateConfiguration(gomock.Any(), gomock.Eq(tunnelId), gomock.Any()).
							Return(nil, nil)

						reconcileOnce()
					})

					It("should not mark the resource as degraded", func() {
						Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
							HaveField("Type", typeDegradedCloudflareTunnel),
							HaveField("Status", metav1.ConditionFalse),
						)))
					})
				})

				Context("and the tunnel is locally managed", func() {
					BeforeEach(func() {
						// UpdateConfiguration is deliberately not expected:
						// Cloudflare stores no configuration for a locally
						// managed tunnel.
						found.ConfigSrc = shared.CloudflareTunnelConfigSrcLocal

						reconcileOnce()
					})

					It("should mark the resource as degraded", func() {
						Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
							HaveField("Type", typeDegradedCloudflareTunnel),
							HaveField("Status", metav1.ConditionTrue),
							HaveField("Reason", reasonInvalidSpec),
						)))
					})
				})

				Context("and the ingress rules are out of order", func() {
					BeforeEach(func() {
						// Admission rejects an ill-ordered list on a spec that
						// declares configSource: cloudflare, so the only way to
						// store one is the divergence the controller handles:
						// Cloudflare owns the configuration and the spec says
						// otherwise.
						Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflaretunnel)).To(Succeed())
						cloudflaretunnel.Spec.ConfigSource = cfv1alpha1.LocalCloudflareTunnelConfigSource
						cloudflaretunnel.Spec.Config.Ingress = []cfv1alpha1.CloudflareTunnelConfigIngress{{
							Service:       testCatchAllService,
							OriginRequest: originRequest,
						}, {
							Hostname:      "test.example.com",
							Service:       "http://test.default.svc.cluster.local:80",
							OriginRequest: originRequest,
						}, {
							Service:       testCatchAllService,
							OriginRequest: originRequest,
						}}
						Expect(k8sClient.Update(ctx, cloudflaretunnel)).To(Succeed())

						// UpdateConfiguration is deliberately not expected:
						// Cloudflare would answer the push with a 400.
						reconcileOnce()
					})

					It("should mark the resource as degraded", func() {
						Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
							HaveField("Type", typeDegradedCloudflareTunnel),
							HaveField("Status", metav1.ConditionTrue),
							HaveField("Reason", reasonInvalidSpec),
							HaveField("Message", ContainSubstring("only the last rule in spec.config.ingress")),
						)))
					})
				})
			})

			Context("and a cloudflared template is provided", func() {
				const labelKey = "app"

				setCloudflared := func(cloudflared *cfv1alpha1.CloudflareTunnelCloudflared) {
					GinkgoHelper()
					Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflaretunnel)).To(Succeed())
					cloudflaretunnel.Spec.Cloudflared = cloudflared
					Expect(k8sClient.Update(ctx, cloudflaretunnel)).To(Succeed())

					cfmock.EXPECT().
						GetTunnel(gomock.Any(), gomock.Eq(tunnelId), gomock.Any()).
						Return(found, nil)
				}

				assertDegraded := func() {
					GinkgoHelper()
					Expect(observed().Status.Conditions).To(ContainElements(SatisfyAll(
						HaveField("Type", typeDegradedCloudflareTunnel),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", reasonInvalidSpec),
					)))
					Expect(apierrors.IsNotFound(
						k8sClient.Get(ctx, typeNamespacedName, &cfv1alpha1.Cloudflared{}),
					)).To(BeTrueBecause("No Cloudflared should have been created"))
				}

				Context("and the selector does not match the template labels", func() {
					BeforeEach(func() {
						setCloudflared(&cfv1alpha1.CloudflareTunnelCloudflared{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{labelKey: "selected"},
							},
							Template: &cfv1alpha1.CloudflaredTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{labelKey: "something-else"},
								},
							},
						})

						reconcileOnce()
					})

					It("should mark the resource as degraded", assertDegraded)
				})

				Context("and the selector is malformed", func() {
					BeforeEach(func() {
						setCloudflared(&cfv1alpha1.CloudflareTunnelCloudflared{
							Selector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{{
									Key:      labelKey,
									Operator: "NotAnOperator",
								}},
							},
							Template: &cfv1alpha1.CloudflaredTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{labelKey: "selected"},
								},
							},
						})

						reconcileOnce()
					})

					It("should mark the resource as degraded", assertDegraded)
				})
			})

			Context("and Name is not provided", func() {
				BeforeEach(func() {
					Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflaretunnel)).To(Succeed())
					cloudflaretunnel.Spec.Name = ""
					Expect(k8sClient.Update(ctx, cloudflaretunnel)).To(Succeed())

					// EditTunnel is deliberately not expected: the tunnel was
					// created under the resource name, so the remote name
					// already matches and there is nothing to rename.
					cfmock.EXPECT().
						GetTunnel(gomock.Any(), gomock.Eq(tunnelId), gomock.Any()).
						Return(found, nil)

					reconcileOnce()
				})

				It("should not rename the tunnel", func() {
					Expect(observed().Status.Name).To(Equal(resourceName))
				})
			})

			Context("and Name differs from the observed tunnel", func() {
				const renamed = "renamed-tunnel"

				BeforeEach(func() {
					Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflaretunnel)).To(Succeed())
					cloudflaretunnel.Spec.Name = renamed
					Expect(k8sClient.Update(ctx, cloudflaretunnel)).To(Succeed())

					cfmock.EXPECT().
						GetTunnel(gomock.Any(), gomock.Eq(tunnelId), gomock.Any()).
						Return(found, nil)

					edited := *found
					edited.Name = renamed
					cfmock.EXPECT().
						EditTunnel(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredEditParams{
							AccountID: cloudflare.F(accountId),
							Name:      cloudflare.F(renamed),
						})).
						Return(&edited, nil)

					reconcileOnce()
				})

				It("should record the name the API returned", func() {
					Expect(observed().Status.Name).To(Equal(renamed))
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
						reconcileFails()

						Expect(observed().Finalizers).NotTo(BeEmpty())
					})
				})
			})
		})
	})
})

var _ = Describe("validateIngress", func() {
	rule := func(hostname, path string) cfv1alpha1.CloudflareTunnelConfigIngress {
		return cfv1alpha1.CloudflareTunnelConfigIngress{
			Hostname: hostname,
			Path:     path,
			Service:  "http://localhost",
		}
	}
	catchAll := rule("", "")

	DescribeTable("accepting a well-ordered list",
		func(rules []cfv1alpha1.CloudflareTunnelConfigIngress) {
			Expect(validateIngress(rules)).To(BeEmpty())
		},
		Entry("when it is empty", []cfv1alpha1.CloudflareTunnelConfigIngress{}),
		Entry("when it is nil", nil),
		Entry("when it holds only the catch-all", []cfv1alpha1.CloudflareTunnelConfigIngress{catchAll}),
		Entry("when the catch-all is last", []cfv1alpha1.CloudflareTunnelConfigIngress{
			rule(testHostname, ""), rule("", "/api"), catchAll,
		}),
	)

	DescribeTable("rejecting an ill-ordered list",
		func(rules []cfv1alpha1.CloudflareTunnelConfigIngress, message string) {
			Expect(validateIngress(rules)).To(ContainSubstring(message))
		},
		Entry("when the last rule carries a hostname",
			[]cfv1alpha1.CloudflareTunnelConfigIngress{rule(testHostname, "")},
			"the last rule in spec.config.ingress must omit both hostname and path",
		),
		Entry("when the last rule carries a path",
			[]cfv1alpha1.CloudflareTunnelConfigIngress{catchAll, rule("", "/api")},
			"the last rule in spec.config.ingress must omit both hostname and path",
		),
		Entry("when an earlier rule is also a catch-all",
			[]cfv1alpha1.CloudflareTunnelConfigIngress{catchAll, rule(testHostname, ""), catchAll},
			"only the last rule in spec.config.ingress may omit both hostname and path",
		),
	)
})

// originRequestKey is the originRequest field name in an unstructured spec.
const originRequestKey = "originRequest"

// tunnelRule builds an unstructured ingress rule. An empty hostname is omitted
// so the rule serializes as the catch-all Cloudflare requires last.
func tunnelRule(hostname, service string, originRequest map[string]any) map[string]any {
	rule := map[string]any{"service": service}
	if hostname != "" {
		rule["hostname"] = hostname
	}
	if originRequest != nil {
		rule[originRequestKey] = originRequest
	}

	return rule
}

// tunnelObject builds an unstructured CloudflareTunnel around the given config.
func tunnelObject(name string, config map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": cfv1alpha1.GroupVersion.String(),
		"kind":       "CloudflareTunnel",
		"metadata": map[string]any{
			"name":      name,
			"namespace": testNamespace,
		},
		"spec": map[string]any{
			"accountId": "test-account-id",
			"config":    config,
		},
	}}
}

// remoteTunnelObject builds a tunnelObject whose configuration Cloudflare owns,
// the only case the ingress ordering rules apply to.
func remoteTunnelObject(name string, config map[string]any) *unstructured.Unstructured {
	GinkgoHelper()
	obj := tunnelObject(name, config)
	Expect(unstructured.SetNestedField(obj.Object, "cloudflare", "spec", "configSource")).To(Succeed())

	return obj
}

// tunnelConfig builds an unstructured spec.config from ingress rules.
func tunnelConfig(rules ...map[string]any) map[string]any {
	ingress := make([]any, len(rules))
	for i, rule := range rules {
		ingress[i] = rule
	}

	return map[string]any{"ingress": ingress}
}

var _ = Describe("CloudflareTunnel CRD", func() {
	ctx := context.Background()

	// Unstructured, because the typed client serializes every field without
	// omitempty and so would always satisfy a required marker.
	It("should accept an originRequest without caPool", func() {
		obj := tunnelObject("no-ca-pool", tunnelConfig(
			tunnelRule(testHostname, "https://localhost", map[string]any{
				"noTlsVerify": true,
			}),
		))
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(obj), &cfv1alpha1.CloudflareTunnel{})

		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("should accept a catch-all ingress rule without a hostname", func() {
		obj := remoteTunnelObject("catch-all-ingress", tunnelConfig(
			tunnelRule(testHostname, "https://localhost", nil),
			tunnelRule("", testCatchAllService, nil),
		))
		key := client.ObjectKeyFromObject(obj)
		DeferCleanup(deleteIfExists, ctx, key, &cfv1alpha1.CloudflareTunnel{})

		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		tunnel := &cfv1alpha1.CloudflareTunnel{}
		Expect(k8sClient.Get(ctx, key, tunnel)).To(Succeed())
		Expect(tunnel.Spec.Config.Ingress).To(HaveLen(2))
		Expect(tunnel.Spec.Config.Ingress[1].Hostname).To(BeEmpty())
		Expect(tunnel.Spec.Config.Ingress[1].Service).To(Equal(testCatchAllService))
	})

	It("should reject a catch-all ingress rule before the end of the list", func() {
		obj := remoteTunnelObject("catch-all-not-last", tunnelConfig(
			tunnelRule("", testCatchAllService, nil),
			tunnelRule(testHostname, "https://localhost", nil),
			tunnelRule("", testCatchAllService, nil),
		))
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(obj), &cfv1alpha1.CloudflareTunnel{})

		err := k8sClient.Create(ctx, obj)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("only the last rule in spec.config.ingress may omit both hostname and path"))
	})

	It("should reject an ingress list without a catch-all rule", func() {
		obj := remoteTunnelObject("no-catch-all", tunnelConfig(
			tunnelRule(testHostname, "https://localhost", nil),
		))
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(obj), &cfv1alpha1.CloudflareTunnel{})

		err := k8sClient.Create(ctx, obj)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("the last rule in spec.config.ingress must omit both hostname and path"))
	})

	It("should read an empty hostname and path on the last rule as the catch-all", func() {
		obj := remoteTunnelObject("empty-string-catch-all", tunnelConfig(
			tunnelRule(testHostname, "https://localhost", nil),
			map[string]any{"service": testCatchAllService, "hostname": "", "path": ""},
		))
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(obj), &cfv1alpha1.CloudflareTunnel{})

		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("should not order the ingress rules of a locally managed tunnel", func() {
		// Cloudflare stores no configuration for a local tunnel, so it never
		// sees the list and the ordering carries no meaning.
		obj := tunnelObject("local-unordered-ingress", tunnelConfig(
			tunnelRule("", testCatchAllService, nil),
			tunnelRule(testHostname, "https://localhost", nil),
		))
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(obj), &cfv1alpha1.CloudflareTunnel{})

		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("should preserve disableChunkedEncoding", func() {
		config := tunnelConfig(
			tunnelRule(testHostname, "https://localhost", map[string]any{
				"disableChunkedEncoding": true,
			}),
		)
		config[originRequestKey] = map[string]any{"disableChunkedEncoding": true}

		obj := tunnelObject("disable-chunked-encoding", config)
		key := client.ObjectKeyFromObject(obj)
		DeferCleanup(deleteIfExists, ctx, key, &cfv1alpha1.CloudflareTunnel{})

		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		tunnel := &cfv1alpha1.CloudflareTunnel{}
		Expect(k8sClient.Get(ctx, key, tunnel)).To(Succeed())
		Expect(tunnel.Spec.Config.OriginRequest.DisableChunkedEncoding).To(BeTrue())
		Expect(tunnel.Spec.Config.Ingress[0].OriginRequest.DisableChunkedEncoding).To(BeTrue())
	})

	It("should accept dns settings at the tunnel and on a rule", func() {
		rule := tunnelRule(testHostname, "https://localhost", nil)
		rule["dns"] = map[string]any{"proxied": false, "ttl": int64(300)}

		obj := tunnelObject("dns-both-levels", tunnelConfig(rule))
		Expect(unstructured.SetNestedField(obj.Object,
			testZoneId, "spec", "dns", "zoneId",
		)).To(Succeed())
		key := client.ObjectKeyFromObject(obj)
		DeferCleanup(deleteIfExists, ctx, key, &cfv1alpha1.CloudflareTunnel{})

		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		tunnel := &cfv1alpha1.CloudflareTunnel{}
		Expect(k8sClient.Get(ctx, key, tunnel)).To(Succeed())
		Expect(tunnel.Spec.Dns.ZoneId).To(Equal(testZoneId))
		// A pointer, so that an entry can override a tunnel-level true.
		Expect(tunnel.Spec.Config.Ingress[0].Dns.Proxied).To(HaveValue(BeFalse()))
		Expect(tunnel.Spec.Config.Ingress[0].Dns.Ttl).To(HaveValue(BeEquivalentTo(300)))
	})

	It("should reject a ttl outside the supported range", func() {
		obj := tunnelObject("dns-ttl-out-of-range", tunnelConfig(
			tunnelRule(testHostname, "https://localhost", nil),
		))
		Expect(unstructured.SetNestedField(obj.Object,
			int64(86401), "spec", "dns", "ttl",
		)).To(Succeed())
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(obj), &cfv1alpha1.CloudflareTunnel{})

		err := k8sClient.Create(ctx, obj)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.dns.ttl"))
	})

	It("should reject dns on a rule without a hostname", func() {
		catchAll := tunnelRule("", testCatchAllService, nil)
		catchAll["dns"] = map[string]any{"zoneId": testZoneId}

		obj := remoteTunnelObject("dns-on-catch-all", tunnelConfig(
			tunnelRule(testHostname, "https://localhost", nil),
			catchAll,
		))
		DeferCleanup(deleteIfExists, ctx, client.ObjectKeyFromObject(obj), &cfv1alpha1.CloudflareTunnel{})

		err := k8sClient.Create(ctx, obj)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("a rule in spec.config.ingress without a hostname cannot set dns"))
	})
})
