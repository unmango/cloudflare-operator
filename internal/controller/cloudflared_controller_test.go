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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	"github.com/unmango/cloudflare-operator/internal/testing"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This suite is overkill and kind of a mess, lots we could do to clean it up

var _ = Describe("Cloudflared Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		cloudflared := &cfv1alpha1.Cloudflared{}

		expectedLabels := map[string]string{
			"app.kubernetes.io/name":       "cloudflare-operator",
			"app.kubernetes.io/managed-by": "CloudflaredController",
			"app.kubernetes.io/version":    "latest",
		}

		var (
			ctrl   *gomock.Controller
			cfmock *testing.MockClient
		)

		BeforeEach(func() {
			Expect(os.Setenv("CLOUDFLARE_API_TOKEN", "test-api-token")).To(Succeed())

			ctrl = gomock.NewController(GinkgoT())
			cfmock = testing.NewMockClient(ctrl)

			By("Initializing the Cloudfared resource")
			cloudflared = &cfv1alpha1.Cloudflared{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
			}
		})

		JustBeforeEach(func() {
			By("Reconciling the resource")
			controllerReconciler := &CloudflaredReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Recorder:   &record.FakeRecorder{},
				Cloudflare: cfmock,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("Removing the custom resource for the kind Cloudflared")
			resource := &cfv1alpha1.Cloudflared{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

			Eventually(func() error {
				return k8sClient.Delete(ctx, resource)
			}).Should(Succeed())

			// TODO: Is there a better way to ensure the resource is deleted?
			By("Reconciling to remove the finalizer")
			controllerReconciler := &CloudflaredReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Recorder:   &record.FakeRecorder{},
				Cloudflare: cfmock,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			tunnel := &cfv1alpha1.CloudflareTunnel{}
			if err := k8sClient.Get(ctx, typeNamespacedName, tunnel); err == nil {
				By("Cleaning up the CloudflareTunnel")
				Expect(k8sClient.Delete(ctx, tunnel)).To(Succeed())
			}

			daemonSet := &appsv1.DaemonSet{}
			if err := k8sClient.Get(ctx, typeNamespacedName, daemonSet); err == nil {
				By("Cleaning up the DaemonSet")
				Expect(k8sClient.Delete(ctx, daemonSet)).To(Succeed())
			}

			deployment := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, typeNamespacedName, deployment); err == nil {
				By("Cleaning up the Deployment")
				Expect(k8sClient.Delete(ctx, deployment)).To(Succeed())
			}
		})

		Context("and the resource is created", func() {
			BeforeEach(func() {
				By("Creating the custom resource for the Kind Cloudflared")
				Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
			})

			It("should default to a DaemonSet", func() {
				By("Fetching the resource")
				resource := &cfv1alpha1.Cloudflared{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource.Spec.Kind).To(Equal(cfv1alpha1.DaemonSetCloudflaredKind))
			})

			It("should create a DaemonSet", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource).NotTo(BeNil())
				container := &corev1.Container{}
				Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
					HaveField("Name", "cloudflared"), container,
				))
				Expect(container.Name).To(Equal("cloudflared"))
				Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
				Expect(container.Command).To(HaveExactElements(
					"cloudflared", "tunnel", "--no-autoupdate", "--metrics", "0.0.0.0:2000",
				))

				// Unless otherwise specified, run a hello world tunnel
				Expect(container.Args).To(HaveExactElements("--hello-world"))

				probe := container.LivenessProbe
				Expect(probe.HTTPGet).To(Equal(&corev1.HTTPGetAction{
					Path:   "/ready",
					Port:   intstr.FromInt(2000),
					Scheme: "HTTP",
				}))
				Expect(probe.FailureThreshold).To(Equal(int32(1)))
				Expect(probe.InitialDelaySeconds).To(Equal(int32(10)))
				Expect(probe.PeriodSeconds).To(Equal(int32(10)))
			})

			It("should create a selector that matches pod labels", func() {
				resource := &appsv1.DaemonSet{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
			})

			It("should add an owner reference", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource).NotTo(BeNil())
				owner := &metav1.OwnerReference{}
				Expect(resource.OwnerReferences).To(ContainElement(
					HaveField("Name", typeNamespacedName.Name), owner,
				))
				Expect(owner.APIVersion).To(Equal("cloudflare.unmango.dev/v1alpha1"))
				Expect(owner.Kind).To(Equal("Cloudflared"))
				Expect(owner.Controller).To(Equal(ptr.To(true)))
				Expect(owner.BlockOwnerDeletion).To(Equal(ptr.To(true)))
			})

			// https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/deployment-guides/kubernetes/#routing-with-cloudflare-tunnel
			It("should configure the pod security context", func() {
				resource := &appsv1.DaemonSet{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				sec := resource.Spec.Template.Spec.SecurityContext
				Expect(sec.RunAsNonRoot).To(Equal(ptr.To(true)))
				Expect(sec.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
				Expect(sec.Sysctls).To(ConsistOf(corev1.Sysctl{
					Name:  "net.ipv4.ping_group_range",
					Value: "65532 65532",
				}))
			})

			It("should configure the container security context", func() {
				resource := &appsv1.DaemonSet{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				container := &corev1.Container{}
				Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
					HaveField("Name", "cloudflared"), container,
				))

				sec := container.SecurityContext
				Expect(sec.RunAsNonRoot).To(Equal(ptr.To(true)))
				Expect(sec.RunAsUser).To(Equal(ptr.To[int64](1001)))
				Expect(sec.AllowPrivilegeEscalation).To(Equal(ptr.To(false)))
				Expect(sec.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
			})

			It("should update the Cloudflared status", func() {
				By("Fetching the resource")
				resource := &cfv1alpha1.Cloudflared{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource.Status.Kind).To(Equal(cfv1alpha1.DaemonSetCloudflaredKind))
			})

			Context("and a matching DaemonSet exists", func() {
				daemonSet := &appsv1.DaemonSet{}

				BeforeEach(func() {
					By("Reconciling to create the DaemonSet")
					controllerReconciler := &CloudflaredReconciler{
						Client:     k8sClient,
						Scheme:     k8sClient.Scheme(),
						Recorder:   &record.FakeRecorder{},
						Cloudflare: cfmock,
					}
					_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
						NamespacedName: typeNamespacedName,
					})
					Expect(err).NotTo(HaveOccurred())

					By("Ensuring the DaemonSet exists")
					Expect(k8sClient.Get(ctx, typeNamespacedName, daemonSet)).Should(Succeed())
				})

				Context("and the pod template spec is modified", func() {
					BeforeEach(func() {
						By("Re-fetching the resource")
						Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())

						By("Configuring a new container")
						cloudflared.Spec.Template = &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "some-new-container",
									Image: "busybox",
								}},
							},
						}

						By("Updating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())
					})

					It("should update the DaemonSet", func() {
						controllerReconciler := &CloudflaredReconciler{
							Client:     k8sClient,
							Scheme:     k8sClient.Scheme(),
							Recorder:   &record.FakeRecorder{},
							Cloudflare: cfmock,
						}
						_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
							NamespacedName: typeNamespacedName,
						})
						Expect(err).NotTo(HaveOccurred())

						daemonSet := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, daemonSet)).To(Succeed())

						container := &corev1.Container{}
						Expect(daemonSet.Spec.Template.Spec.Containers).To(ContainElement(
							HaveField("Name", "some-new-container"), container,
						))
					})
				})

				Context("and the Kind is changed to Deployment", func() {
					BeforeEach(func() {
						By("Re-fetching the resource")
						Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())

						cloudflared.Spec.Kind = cfv1alpha1.DeploymentCloudflaredKind

						By("Updating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())
					})

					It("should delete the DaemonSet", func() {
						err := k8sClient.Get(ctx, typeNamespacedName, &appsv1.DaemonSet{})
						Expect(err).To(MatchError(`daemonsets.apps "test-resource" not found`))
					})

					It("should create a Deployment", func() {
						By("Reconciling to create the Deployment")
						controllerReconciler := &CloudflaredReconciler{
							Client:     k8sClient,
							Scheme:     k8sClient.Scheme(),
							Recorder:   &record.FakeRecorder{},
							Cloudflare: cfmock,
						}
						_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
							NamespacedName: typeNamespacedName,
						})
						Expect(err).NotTo(HaveOccurred())

						Expect(k8sClient.Get(ctx, typeNamespacedName, &appsv1.Deployment{})).To(Succeed())
					})
				})
			})
		})

		Context("and pod spec template is configured", func() {
			const (
				expectedImage     = "something/not/cloudflared:v0.0.69"
				expectedContainer = "container-name"
			)

			BeforeEach(func() {
				cloudflared.Spec.Template = &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: expectedLabels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:  expectedContainer,
							Image: expectedImage,
						}},
					},
				}
			})

			Context("and the resource is created", func() {
				BeforeEach(func() {
					By("creating the custom resource for the Kind Cloudflared")
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				})

				It("should create a DaemonSet", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					template := resource.Spec.Template
					Expect(template.Labels).To(Equal(expectedLabels))
					container := &corev1.Container{}
					Expect(template.Spec.Containers).To(ContainElement(
						HaveField("Name", "cloudflared"), container,
					))
					Expect(template.Spec.Containers).To(ContainElement(
						HaveField("Name", expectedContainer), container,
					))
					Expect(container.Image).To(Equal(expectedImage))
				})

				It("should create a selector that matches pod labels", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
				})

				It("should add an owner reference", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					owner := &metav1.OwnerReference{}
					Expect(resource.OwnerReferences).To(ContainElement(
						HaveField("Name", typeNamespacedName.Name), owner,
					))
					Expect(owner.APIVersion).To(Equal("cloudflare.unmango.dev/v1alpha1"))
					Expect(owner.Kind).To(Equal("Cloudflared"))
					Expect(owner.Controller).To(Equal(ptr.To(true)))
					Expect(owner.BlockOwnerDeletion).To(Equal(ptr.To(true)))
				})
			})

			Context("with a custom cloudflared container image", func() {
				BeforeEach(func() {
					cloudflared.Spec.Template.Spec.Containers = []corev1.Container{{
						Name:  "cloudflared",
						Image: expectedImage,
					}}
				})

				Context("and the resource is created", func() {
					BeforeEach(func() {
						By("creating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					})

					It("should use the supplied image", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						container := &corev1.Container{}
						Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(container.Image).To(Equal(expectedImage))
					})

					It("should keep the existing command", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						container := &corev1.Container{}
						Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(container.Command).NotTo(BeEmpty())
					})

					It("should use the version label from the supplied image", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource).NotTo(BeNil())
						Expect(resource.Spec.Selector.MatchLabels).To(SatisfyAll(
							HaveKeyWithValue("app.kubernetes.io/name", "cloudflare-operator"),
							HaveKeyWithValue("app.kubernetes.io/managed-by", "CloudflaredController"),
							HaveKeyWithValue("app.kubernetes.io/version", "0.0.69"),
						))
					})
				})
			})
		})

		Context("and inline config is provided", func() {
			const (
				tunnelId  string = "test-tunnel-id"
				accountId string = "test-account-id"
				token     string = "test-token"
			)

			BeforeEach(func() {
				cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
					CloudflaredConfigInline: cfv1alpha1.CloudflaredConfigInline{
						TunnelId:  ptr.To(tunnelId),
						AccountId: ptr.To(accountId),
					},
				}
			})

			Context("and the resource is created", func() {
				BeforeEach(func() {
					By("Creating the custom resource for the Kind Cloudflared")
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				})

				Context("and the token call succeeds", func() {
					BeforeEach(func() {
						cfmock.EXPECT().
							GetTunnelToken(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredTokenGetParams{
								AccountID: cloudflare.F(accountId),
							})).
							Return(ptr.To(token), nil)
					})

					It("should run the given tunnel", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						container := &corev1.Container{}
						Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(container.Env).To(ConsistOf(
							corev1.EnvVar{Name: "TUNNEL_TOKEN", Value: token},
						))
						Expect(container.Args).To(HaveExactElements("run", tunnelId))
					})

					It("should update the resource status", func() {
						resource := &cfv1alpha1.Cloudflared{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
						Expect(resource.Status.TunnelId).To(Equal(ptr.To(tunnelId)))
					})
				})

				Context("and the api token environment variable is not set", func() {
					BeforeEach(func() {
						Expect(os.Unsetenv("CLOUDFLARE_API_TOKEN")).To(Succeed())

						cfmock.EXPECT().
							GetTunnelToken(gomock.Any(), gomock.Any(), gomock.Any()).
							Times(0)
					})

					It("should not attempt to lookup the tunnel token", func() {
						// Assertion is handled via the cfmock
					})
				})
			})
		})

		Context("and a tunnel reference is provided", func() {
			const (
				tunnelId  string = "test-tunnel-id"
				accountId string = "test-account-id"
				token     string = "test-token"
			)

			BeforeEach(func() {
				By("Creating a CloudflareTunnel")
				tunnel := &cfv1alpha1.CloudflareTunnel{
					ObjectMeta: cloudflared.ObjectMeta,
					Spec: cfv1alpha1.CloudflareTunnelSpec{
						ConfigSource: cfv1alpha1.CloudflareCloudflareTunnelConfigSource,
					},
				}
				Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

				By("Updating the CloudflareTunnel status")
				tunnel.Status = cfv1alpha1.CloudflareTunnelStatus{
					AccountTag: accountId,
					Id:         tunnelId,
				}
				Expect(k8sClient.Status().Update(ctx, tunnel)).To(Succeed())

				cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
					TunnelRef: &cfv1alpha1.CloudflaredTunnelReference{
						Name: cloudflared.Name,
					},
				}
			})

			Context("and the resource is created", func() {
				BeforeEach(func() {
					By("Creating the custom resource for the Kind Cloudflared")
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				})

				Context("and the token call succeeds", func() {
					BeforeEach(func() {
						cfmock.EXPECT().
							GetTunnelToken(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredTokenGetParams{
								AccountID: cloudflare.F(accountId),
							})).
							Return(ptr.To(token), nil)
					})

					It("should run the given tunnel", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						container := &corev1.Container{}
						Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(container.Env).To(ConsistOf(
							corev1.EnvVar{Name: "TUNNEL_TOKEN", Value: token},
						))
						Expect(container.Args).To(HaveExactElements("run", tunnelId))
					})

					It("should update the resource status", func() {
						resource := &cfv1alpha1.Cloudflared{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
						Expect(resource.Status.TunnelId).To(Equal(ptr.To(tunnelId)))
					})
				})

				Context("and the api token environment variable is not set", func() {
					BeforeEach(func() {
						Expect(os.Unsetenv("CLOUDFLARE_API_TOKEN")).To(Succeed())

						cfmock.EXPECT().
							GetTunnelToken(gomock.Any(), gomock.Any(), gomock.Any()).
							Times(0)
					})

					It("should not attempt to lookup the tunnel token", func() {
						// Assertion is handled via the cfmock
					})
				})
			})
		})

		Context("and kind is DaemonSet", func() {
			BeforeEach(func() {
				By("Setting the kind to DaemonSet")
				cloudflared.Spec.Kind = cfv1alpha1.DaemonSetCloudflaredKind
			})

			Context("and the resource is created", func() {
				BeforeEach(func() {
					By("creating the custom resource for the Kind Cloudflared")
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				})

				It("should create a DaemonSet", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					container := &corev1.Container{}
					Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
						HaveField("Name", "cloudflared"), container,
					))
					Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
					Expect(container.Command).To(HaveExactElements(
						"cloudflared", "tunnel", "--no-autoupdate", "--metrics", "0.0.0.0:2000",
					))

					// Unless otherwise specified, run a hello world tunnel
					Expect(container.Args).To(HaveExactElements("--hello-world"))

					probe := container.LivenessProbe
					Expect(probe.HTTPGet).To(Equal(&corev1.HTTPGetAction{
						Path:   "/ready",
						Port:   intstr.FromInt(2000),
						Scheme: "HTTP",
					}))
					Expect(probe.FailureThreshold).To(Equal(int32(1)))
					Expect(probe.InitialDelaySeconds).To(Equal(int32(10)))
					Expect(probe.PeriodSeconds).To(Equal(int32(10)))
				})

				It("should create a selector that matches pod labels", func() {
					By("Fetching the DaemonSet")
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
				})

				It("should add an owner reference", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					owner := &metav1.OwnerReference{}
					Expect(resource.OwnerReferences).To(ContainElement(
						HaveField("Name", typeNamespacedName.Name), owner,
					))
					Expect(owner.APIVersion).To(Equal("cloudflare.unmango.dev/v1alpha1"))
					Expect(owner.Kind).To(Equal("Cloudflared"))
					Expect(owner.Controller).To(Equal(ptr.To(true)))
					Expect(owner.BlockOwnerDeletion).To(Equal(ptr.To(true)))
				})

				// https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/deployment-guides/kubernetes/#routing-with-cloudflare-tunnel
				It("should configure the pod security context", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					sec := resource.Spec.Template.Spec.SecurityContext
					Expect(sec.RunAsNonRoot).To(Equal(ptr.To(true)))
					Expect(sec.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
					Expect(sec.Sysctls).To(ConsistOf(corev1.Sysctl{
						Name:  "net.ipv4.ping_group_range",
						Value: "65532 65532",
					}))
				})

				It("should configure the container security context", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					container := &corev1.Container{}
					Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
						HaveField("Name", "cloudflared"), container,
					))

					sec := container.SecurityContext
					Expect(sec.RunAsNonRoot).To(Equal(ptr.To(true)))
					Expect(sec.RunAsUser).To(Equal(ptr.To[int64](1001)))
					Expect(sec.AllowPrivilegeEscalation).To(Equal(ptr.To(false)))
					Expect(sec.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
				})

				It("should update the Cloudflared status", func() {
					By("Fetching the resource")
					resource := &cfv1alpha1.Cloudflared{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource.Status.Kind).To(Equal(cfv1alpha1.DaemonSetCloudflaredKind))
				})

				Context("and a matching DaemonSet exists", func() {
					daemonSet := &appsv1.DaemonSet{}

					BeforeEach(func() {
						By("Reconciling to create the DaemonSet")
						controllerReconciler := &CloudflaredReconciler{
							Client:     k8sClient,
							Scheme:     k8sClient.Scheme(),
							Recorder:   &record.FakeRecorder{},
							Cloudflare: cfmock,
						}
						_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
							NamespacedName: typeNamespacedName,
						})
						Expect(err).NotTo(HaveOccurred())

						By("Ensuring the DaemonSet exists")
						Expect(k8sClient.Get(ctx, typeNamespacedName, daemonSet)).Should(Succeed())
					})

					Context("and the pod template spec is modified", func() {
						BeforeEach(func() {
							By("Re-fetching the resource")
							Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())

							By("Configuring a new container")
							cloudflared.Spec.Template = &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "some-new-container",
										Image: "busybox",
									}},
								},
							}

							By("Updating the custom resource for the Kind Cloudflared")
							Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())
						})

						It("should update the DaemonSet", func() {
							controllerReconciler := &CloudflaredReconciler{
								Client:     k8sClient,
								Scheme:     k8sClient.Scheme(),
								Recorder:   &record.FakeRecorder{},
								Cloudflare: cfmock,
							}
							_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
								NamespacedName: typeNamespacedName,
							})
							Expect(err).NotTo(HaveOccurred())

							daemonSet := &appsv1.DaemonSet{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, daemonSet)).To(Succeed())

							container := &corev1.Container{}
							Expect(daemonSet.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "some-new-container"), container,
							))
						})
					})

					Context("and the Kind is changed to Deployment", func() {
						BeforeEach(func() {
							By("Re-fetching the resource")
							Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())

							cloudflared.Spec.Kind = cfv1alpha1.DeploymentCloudflaredKind

							By("Updating the custom resource for the Kind Cloudflared")
							Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())
						})

						It("should delete the DaemonSet", func() {
							err := k8sClient.Get(ctx, typeNamespacedName, &appsv1.DaemonSet{})
							Expect(err).To(MatchError(`daemonsets.apps "test-resource" not found`))
						})

						It("should create a Deployment", func() {
							By("Reconciling to create the Deployment")
							controllerReconciler := &CloudflaredReconciler{
								Client:     k8sClient,
								Scheme:     k8sClient.Scheme(),
								Recorder:   &record.FakeRecorder{},
								Cloudflare: cfmock,
							}
							_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
								NamespacedName: typeNamespacedName,
							})
							Expect(err).NotTo(HaveOccurred())

							Expect(k8sClient.Get(ctx, typeNamespacedName, &appsv1.Deployment{})).To(Succeed())
						})
					})
				})
			})

			Context("and pod spec template is configured", func() {
				const (
					expectedImage     = "something/not/cloudflared:v0.0.69"
					expectedContainer = "container-name"
				)

				BeforeEach(func() {
					cloudflared.Spec.Template = &corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: expectedLabels},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  expectedContainer,
								Image: expectedImage,
							}},
						},
					}
				})

				Context("and the resource is created", func() {
					BeforeEach(func() {
						By("creating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					})

					It("should create a DaemonSet", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource).NotTo(BeNil())
						template := resource.Spec.Template
						Expect(template.Labels).To(Equal(expectedLabels))
						container := &corev1.Container{}
						Expect(template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(template.Spec.Containers).To(ContainElement(
							HaveField("Name", expectedContainer), container,
						))
						Expect(container.Image).To(Equal(expectedImage))
					})

					It("should create a selector that matches pod labels", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource).NotTo(BeNil())
						Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
					})

					It("should add an owner reference", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource).NotTo(BeNil())
						owner := &metav1.OwnerReference{}
						Expect(resource.OwnerReferences).To(ContainElement(
							HaveField("Name", typeNamespacedName.Name), owner,
						))
						Expect(owner.APIVersion).To(Equal("cloudflare.unmango.dev/v1alpha1"))
						Expect(owner.Kind).To(Equal("Cloudflared"))
						Expect(owner.Controller).To(Equal(ptr.To(true)))
						Expect(owner.BlockOwnerDeletion).To(Equal(ptr.To(true)))
					})

					It("should configure the RollingUpdate strategy", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						strategy := resource.Spec.UpdateStrategy
						Expect(strategy.Type).To(Equal(appsv1.RollingUpdateDaemonSetStrategyType))
					})
				})

				Context("with a custom cloudflared container image", func() {
					BeforeEach(func() {
						cloudflared.Spec.Template.Spec.Containers = []corev1.Container{{
							Name:  "cloudflared",
							Image: expectedImage,
						}}
					})

					Context("and the resource is created", func() {
						BeforeEach(func() {
							By("creating the custom resource for the Kind Cloudflared")
							Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
						})

						It("should use the supplied image", func() {
							resource := &appsv1.DaemonSet{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							container := &corev1.Container{}
							Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "cloudflared"), container,
							))
							Expect(container.Image).To(Equal(expectedImage))
						})

						It("should keep the existing command", func() {
							resource := &appsv1.DaemonSet{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							container := &corev1.Container{}
							Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "cloudflared"), container,
							))
							Expect(container.Command).NotTo(BeEmpty())
						})

						It("should use the version label from the supplied image", func() {
							resource := &appsv1.DaemonSet{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							Expect(resource).NotTo(BeNil())
							Expect(resource.Spec.Selector.MatchLabels).To(SatisfyAll(
								HaveKeyWithValue("app.kubernetes.io/name", "cloudflare-operator"),
								HaveKeyWithValue("app.kubernetes.io/managed-by", "CloudflaredController"),
								HaveKeyWithValue("app.kubernetes.io/version", "0.0.69"),
							))
						})
					})
				})
			})
		})

		Context("and kind is Deployment", func() {
			BeforeEach(func() {
				cloudflared.Spec.Kind = cfv1alpha1.DeploymentCloudflaredKind
			})

			Context("and the resource is created", func() {
				BeforeEach(func() {
					By("creating the custom resource for the Kind Cloudflared")
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				})

				It("should create a Deployment", func() {
					resource := &appsv1.Deployment{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					container := &corev1.Container{}
					Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
						HaveField("Name", "cloudflared"), container,
					))
					Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
					Expect(container.Command).To(HaveExactElements(
						"cloudflared", "tunnel", "--no-autoupdate", "--metrics", "0.0.0.0:2000",
					))

					// Unless otherwise specified, run a hello world tunnel
					Expect(container.Args).To(HaveExactElements("--hello-world"))

					probe := container.LivenessProbe
					Expect(probe.HTTPGet).To(Equal(&corev1.HTTPGetAction{
						Path:   "/ready",
						Port:   intstr.FromInt(2000),
						Scheme: "HTTP",
					}))
					Expect(probe.FailureThreshold).To(Equal(int32(1)))
					Expect(probe.InitialDelaySeconds).To(Equal(int32(10)))
					Expect(probe.PeriodSeconds).To(Equal(int32(10)))
				})

				It("should add an owner reference", func() {
					resource := &appsv1.Deployment{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource).NotTo(BeNil())
					owner := &metav1.OwnerReference{}
					Expect(resource.OwnerReferences).To(ContainElement(
						HaveField("Name", typeNamespacedName.Name), owner,
					))
					Expect(owner.APIVersion).To(Equal("cloudflare.unmango.dev/v1alpha1"))
					Expect(owner.Kind).To(Equal("Cloudflared"))
					Expect(owner.Controller).To(Equal(ptr.To(true)))
					Expect(owner.BlockOwnerDeletion).To(Equal(ptr.To(true)))
				})

				// https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/deployment-guides/kubernetes/#routing-with-cloudflare-tunnel
				It("should configure the pod security context", func() {
					resource := &appsv1.Deployment{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					sec := resource.Spec.Template.Spec.SecurityContext
					Expect(sec.RunAsNonRoot).To(Equal(ptr.To(true)))
					Expect(sec.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
					Expect(sec.Sysctls).To(ConsistOf(corev1.Sysctl{
						Name:  "net.ipv4.ping_group_range",
						Value: "65532 65532",
					}))
				})

				It("should configure the container security context", func() {
					resource := &appsv1.Deployment{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					container := &corev1.Container{}
					Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
						HaveField("Name", "cloudflared"), container,
					))

					sec := container.SecurityContext
					Expect(sec.RunAsNonRoot).To(Equal(ptr.To(true)))
					Expect(sec.RunAsUser).To(Equal(ptr.To[int64](1001)))
					Expect(sec.AllowPrivilegeEscalation).To(Equal(ptr.To(false)))
					Expect(sec.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
				})

				It("should update the Cloudflared status", func() {
					By("Fetching the resource")
					resource := &cfv1alpha1.Cloudflared{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource.Status.Kind).To(Equal(cfv1alpha1.DeploymentCloudflaredKind))
				})

				Context("and a matching Deployment exists", func() {
					deployment := &appsv1.Deployment{}

					BeforeEach(func() {
						By("Reconciling to create the Deployment")
						controllerReconciler := &CloudflaredReconciler{
							Client:     k8sClient,
							Scheme:     k8sClient.Scheme(),
							Recorder:   &record.FakeRecorder{},
							Cloudflare: cfmock,
						}
						_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
							NamespacedName: typeNamespacedName,
						})
						Expect(err).NotTo(HaveOccurred())

						By("Ensuring the Deployment exists")
						Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).Should(Succeed())
					})

					Context("and the replicas field has been modified", func() {
						BeforeEach(func() {
							By("Re-fetching the resource")
							Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())

							By("Configuring a new container")
							cloudflared.Spec.Replicas = ptr.To[int32](3)

							By("Updating the custom resource for the Kind Cloudflared")
							Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())
						})

						It("should update the Deployment", func() {
							controllerReconciler := &CloudflaredReconciler{
								Client:     k8sClient,
								Scheme:     k8sClient.Scheme(),
								Recorder:   &record.FakeRecorder{},
								Cloudflare: cfmock,
							}
							_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
								NamespacedName: typeNamespacedName,
							})
							Expect(err).NotTo(HaveOccurred())

							deployment := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
							Expect(deployment.Spec.Replicas).To(Equal(ptr.To[int32](3)))
						})
					})

					Context("and the Kind is changed to DaemonSet", func() {
						BeforeEach(func() {
							By("Re-fetching the resource")
							Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())

							cloudflared.Spec.Kind = cfv1alpha1.DaemonSetCloudflaredKind

							By("Updating the custom resource for the Kind Cloudflared")
							Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())
						})

						It("should delete the Deployment", func() {
							err := k8sClient.Get(ctx, typeNamespacedName, &appsv1.Deployment{})
							Expect(err).To(MatchError(`deployments.apps "test-resource" not found`))
						})

						It("should create a DaemonSet", func() {
							By("Reconciling to create the DaemonSet")
							controllerReconciler := &CloudflaredReconciler{
								Client:     k8sClient,
								Scheme:     k8sClient.Scheme(),
								Recorder:   &record.FakeRecorder{},
								Cloudflare: cfmock,
							}
							_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
								NamespacedName: typeNamespacedName,
							})
							Expect(err).NotTo(HaveOccurred())

							Expect(k8sClient.Get(ctx, typeNamespacedName, &appsv1.DaemonSet{})).To(Succeed())
						})
					})

					Context("and the pod template spec is modified", func() {
						BeforeEach(func() {
							By("Re-fetching the resource")
							Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())

							By("Configuring a new container")
							cloudflared.Spec.Template = &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:  "some-new-container",
										Image: "busybox",
									}},
								},
							}

							By("Updating the custom resource for the Kind Cloudflared")
							Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())
						})

						It("should update the Deployment", func() {
							controllerReconciler := &CloudflaredReconciler{
								Client:     k8sClient,
								Scheme:     k8sClient.Scheme(),
								Recorder:   &record.FakeRecorder{},
								Cloudflare: cfmock,
							}
							_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
								NamespacedName: typeNamespacedName,
							})
							Expect(err).NotTo(HaveOccurred())

							deployment := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())

							container := &corev1.Container{}
							Expect(deployment.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "some-new-container"), container,
							))
						})
					})
				})
			})

			Context("and replicas is specified", func() {
				BeforeEach(func() {
					cloudflared.Spec.Replicas = ptr.To[int32](3)

					By("creating the custom resource for the Kind Cloudflared")
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				})

				It("should configure the replicas on the deployment", func() {
					resource := &appsv1.Deployment{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

					Expect(resource.Spec.Replicas).To(Equal(ptr.To[int32](3)))
				})
			})

			Context("and pod spec template is configured", func() {
				const (
					expectedImage     = "something/not/cloudflared:v0.0.69"
					expectedContainer = "container-name"
				)

				BeforeEach(func() {
					cloudflared.Spec.Template = &corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: expectedLabels},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  expectedContainer,
								Image: expectedImage,
							}},
						},
					}
				})

				Context("and the resource is created", func() {
					BeforeEach(func() {
						By("creating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					})

					It("should create a Deployment", func() {
						resource := &appsv1.Deployment{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource).NotTo(BeNil())
						template := resource.Spec.Template
						Expect(template.Labels).To(Equal(expectedLabels))
						container := &corev1.Container{}
						Expect(template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(template.Spec.Containers).To(ContainElement(
							HaveField("Name", expectedContainer), container,
						))
						Expect(container.Image).To(Equal(expectedImage))
					})

					It("should create a selector that matches pod labels", func() {
						resource := &appsv1.Deployment{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource).NotTo(BeNil())
						Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
					})

					It("should add an owner reference", func() {
						resource := &appsv1.Deployment{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource).NotTo(BeNil())
						owner := &metav1.OwnerReference{}
						Expect(resource.OwnerReferences).To(ContainElement(
							HaveField("Name", typeNamespacedName.Name), owner,
						))
						Expect(owner.APIVersion).To(Equal("cloudflare.unmango.dev/v1alpha1"))
						Expect(owner.Kind).To(Equal("Cloudflared"))
						Expect(owner.Controller).To(Equal(ptr.To(true)))
						Expect(owner.BlockOwnerDeletion).To(Equal(ptr.To(true)))
					})

					It("should configure the RollingUpdate strategy", func() {
						resource := &appsv1.Deployment{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						strategy := resource.Spec.Strategy
						Expect(strategy.Type).To(Equal(appsv1.RollingUpdateDeploymentStrategyType))
					})
				})

				Context("with a custom cloudflared container image", func() {
					BeforeEach(func() {
						cloudflared.Spec.Template.Spec.Containers = []corev1.Container{{
							Name:  "cloudflared",
							Image: expectedImage,
						}}
					})

					Context("and the resource is created", func() {
						BeforeEach(func() {
							By("creating the custom resource for the Kind Cloudflared")
							Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
						})

						It("should use the supplied image", func() {
							resource := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							container := &corev1.Container{}
							Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "cloudflared"), container,
							))
							Expect(container.Image).To(Equal(expectedImage))
						})

						It("should keep the existing command", func() {
							resource := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							container := &corev1.Container{}
							Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "cloudflared"), container,
							))
							Expect(container.Command).NotTo(BeEmpty())
						})

						It("should use the version label from the supplied image", func() {
							resource := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							Expect(resource).NotTo(BeNil())
							Expect(resource.Spec.Selector.MatchLabels).To(SatisfyAll(
								HaveKeyWithValue("app.kubernetes.io/name", "cloudflare-operator"),
								HaveKeyWithValue("app.kubernetes.io/managed-by", "CloudflaredController"),
								HaveKeyWithValue("app.kubernetes.io/version", "0.0.69"),
							))
						})
					})
				})
			})

			Context("and inline config is provided", func() {
				const (
					tunnelId  string = "test-tunnel-id"
					accountId string = "test-account-id"
					token     string = "test-token"
				)

				BeforeEach(func() {
					cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
						CloudflaredConfigInline: cfv1alpha1.CloudflaredConfigInline{
							TunnelId:  ptr.To(tunnelId),
							AccountId: ptr.To(accountId),
						},
					}
				})

				Context("and the resource is created", func() {
					BeforeEach(func() {
						By("Creating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					})

					Context("and the token call succeeds", func() {
						BeforeEach(func() {
							cfmock.EXPECT().
								GetTunnelToken(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredTokenGetParams{
									AccountID: cloudflare.F(accountId),
								})).
								Return(ptr.To(token), nil)
						})

						It("should run the given tunnel", func() {
							resource := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							container := &corev1.Container{}
							Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "cloudflared"), container,
							))
							Expect(container.Env).To(ConsistOf(
								corev1.EnvVar{Name: "TUNNEL_TOKEN", Value: token},
							))
							Expect(container.Args).To(HaveExactElements("run", tunnelId))
						})

						It("should update the resource status", func() {
							resource := &cfv1alpha1.Cloudflared{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
							Expect(resource.Status.TunnelId).To(Equal(ptr.To(tunnelId)))
						})
					})

					Context("and the api token environment variable is not set", func() {
						BeforeEach(func() {
							Expect(os.Unsetenv("CLOUDFLARE_API_TOKEN")).To(Succeed())

							cfmock.EXPECT().
								GetTunnelToken(gomock.Any(), gomock.Any(), gomock.Any()).
								Times(0)
						})

						It("should not attempt to lookup the tunnel token", func() {
							// Assertion is handled via the cfmock
						})
					})
				})
			})

			Context("and a tunnel reference is provided", func() {
				const (
					tunnelId  string = "test-tunnel-id"
					accountId string = "test-account-id"
					token     string = "test-token"
				)

				BeforeEach(func() {
					By("Creating a CloudflareTunnel")
					tunnel := &cfv1alpha1.CloudflareTunnel{
						ObjectMeta: cloudflared.ObjectMeta,
						Spec: cfv1alpha1.CloudflareTunnelSpec{
							ConfigSource: cfv1alpha1.CloudflareCloudflareTunnelConfigSource,
						},
					}
					Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

					By("Updating the CloudflareTunnel status")
					tunnel.Status = cfv1alpha1.CloudflareTunnelStatus{
						AccountTag: accountId,
						Id:         tunnelId,
					}
					Expect(k8sClient.Status().Update(ctx, tunnel)).To(Succeed())

					cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
						TunnelRef: &cfv1alpha1.CloudflaredTunnelReference{
							Name: cloudflared.Name,
						},
					}
				})

				Context("and the resource is created", func() {
					BeforeEach(func() {
						By("Creating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					})

					Context("and the token call succeeds", func() {
						BeforeEach(func() {
							cfmock.EXPECT().
								GetTunnelToken(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredTokenGetParams{
									AccountID: cloudflare.F(accountId),
								})).
								Return(ptr.To(token), nil)
						})

						It("should run the given tunnel", func() {
							resource := &appsv1.Deployment{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

							container := &corev1.Container{}
							Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
								HaveField("Name", "cloudflared"), container,
							))
							Expect(container.Env).To(ConsistOf(
								corev1.EnvVar{Name: "TUNNEL_TOKEN", Value: token},
							))
							Expect(container.Args).To(HaveExactElements("run", tunnelId))
						})

						It("should update the resource status", func() {
							resource := &cfv1alpha1.Cloudflared{}
							Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
							Expect(resource.Status.TunnelId).To(Equal(ptr.To(tunnelId)))
						})
					})

					Context("and the api token environment variable is not set", func() {
						BeforeEach(func() {
							Expect(os.Unsetenv("CLOUDFLARE_API_TOKEN")).To(Succeed())

							cfmock.EXPECT().
								GetTunnelToken(gomock.Any(), gomock.Any(), gomock.Any()).
								Times(0)
						})

						It("should not attempt to lookup the tunnel token", func() {
							// Assertion is handled via the cfmock
						})
					})
				})
			})
		})

		Context("and a ConfigMap reference is configured", func() {
			BeforeEach(func() {
				cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
					ValueFrom: &cfv1alpha1.CloudflaredConfigReference{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "cloudflared-config",
							},
							Key: "my-config.yml",
						},
					},
				}

				By("creating the custom resource for the Kind Cloudflared")
				Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
			})

			It("should mount the config in the cloudflared container", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource).NotTo(BeNil())
				templateSpec := resource.Spec.Template.Spec
				Expect(templateSpec.Volumes).To(ConsistOf(corev1.Volume{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "cloudflared-config",
							},
							Items: []corev1.KeyToPath{{
								Key:  "my-config.yml",
								Path: "config.yml",
							}},
							DefaultMode: ptr.To[int32](420),
						},
					},
				}))
				container := &corev1.Container{}
				Expect(templateSpec.Containers).To(ContainElement(
					HaveField("Name", "cloudflared"), container,
				))
				Expect(container.VolumeMounts).To(ConsistOf(corev1.VolumeMount{
					Name:      "config",
					MountPath: "/etc/cloudflared",
				}))
			})
		})

		Context("and a Secret reference is configured", func() {
			BeforeEach(func() {
				cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
					ValueFrom: &cfv1alpha1.CloudflaredConfigReference{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "cloudflared-config",
							},
							Key: "my-config.yml",
						},
					},
				}

				By("creating the custom resource for the Kind Cloudflared")
				Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
			})

			It("should mount the secret in the cloudflared container", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				Expect(resource).NotTo(BeNil())
				templateSpec := resource.Spec.Template.Spec
				Expect(templateSpec.Volumes).To(ConsistOf(corev1.Volume{
					Name: "config",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "cloudflared-config",
							Items: []corev1.KeyToPath{{
								Key:  "my-config.yml",
								Path: "config.yml",
							}},
							DefaultMode: ptr.To[int32](420),
						},
					},
				}))
				container := &corev1.Container{}
				Expect(templateSpec.Containers).To(ContainElement(
					HaveField("Name", "cloudflared"), container,
				))
				Expect(container.VolumeMounts).To(ConsistOf(corev1.VolumeMount{
					Name:      "config",
					MountPath: "/etc/cloudflared",
				}))
			})
		})

		DescribeTableSubtree("and version is specified",
			func(version string) {
				BeforeEach(func() {
					cloudflared.Spec.Version = version
				})

				Context("and the resource is created", func() {
					BeforeEach(func() {
						By("creating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					})

					It("should configure the version tag", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						container := &corev1.Container{}
						Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:2025.4.2"))
					})

					It("should configure the version label", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						Expect(resource.Spec.Selector.MatchLabels).To(
							HaveKeyWithValue("app.kubernetes.io/version", "2025.4.2"),
						)
						Expect(resource.Spec.Template.Labels).To(
							HaveKeyWithValue("app.kubernetes.io/version", "2025.4.2"),
						)
					})
				})

				Context("and the cloudflared container image is customized", func() {
					const expectedImage = "blah-blah-blah:latest"

					BeforeEach(func() {
						cloudflared.Spec.Template = &corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: expectedLabels},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "cloudflared",
									Image: expectedImage,
								}},
							},
						}

						By("creating the custom resource for the Kind Cloudflared")
						Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					})

					It("should give precedence to the customized image", func() {
						resource := &appsv1.DaemonSet{}
						Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

						container := &corev1.Container{}
						Expect(resource.Spec.Template.Spec.Containers).To(ContainElement(
							HaveField("Name", "cloudflared"), container,
						))
						Expect(container.Image).To(Equal(expectedImage))
					})
				})
			},
			Entry(nil, "2025.4.2"),
			Entry(nil, "v2025.4.2"),
		)
	})
})
