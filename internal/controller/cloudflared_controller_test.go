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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/unmango/cloudflare-operator/internal/testing"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Cloudflared Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName = "test-resource"
			tunnelId     = "test-tunnel-id"
			accountId    = "test-account-id"
			token        = "test-token"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}
		cloudflared := &cfv1alpha1.Cloudflared{}

		expectedLabels := map[string]string{
			"app.kubernetes.io/name":       "cloudflare-operator",
			"app.kubernetes.io/managed-by": "CloudflaredController",
			"app.kubernetes.io/version":    "latest",
		}

		var cfmock *testing.MockClient

		reconcileOnce := func() reconcile.Result {
			GinkgoHelper()
			result, err := (&CloudflaredReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Cloudflare: cfmock,
			}).Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			return result
		}

		// Fetches the DaemonSet the controller is expected to have created.
		daemonSet := func() *appsv1.DaemonSet {
			GinkgoHelper()
			resource := &appsv1.DaemonSet{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			return resource
		}

		// Fetches the named container out of a pod template.
		containerNamed := func(template corev1.PodTemplateSpec, name string) *corev1.Container {
			GinkgoHelper()
			container := &corev1.Container{}
			Expect(template.Spec.Containers).To(ContainElement(HaveField("Name", name), container))
			return container
		}

		observed := func() *cfv1alpha1.Cloudflared {
			GinkgoHelper()
			resource := &cfv1alpha1.Cloudflared{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			return resource
		}

		BeforeEach(func() {
			cfmock = testing.NewMockClient(gomock.NewController(GinkgoT()))

			cloudflared = &cfv1alpha1.Cloudflared{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
			}
		})

		AfterEach(func() {
			for _, obj := range []client.Object{
				&cfv1alpha1.Cloudflared{},
				&cfv1alpha1.CloudflareTunnel{},
				&appsv1.DaemonSet{},
				&appsv1.Deployment{},
			} {
				deleteIfExists(ctx, typeNamespacedName, obj)
			}
		})

		Context("and the resource is created", func() {
			BeforeEach(func() {
				By("Creating the custom resource for the Kind Cloudflared")
				Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				reconcileOnce()
			})

			It("should default to a DaemonSet running the latest tag", func() {
				resource := observed()
				Expect(resource.Spec.Kind).To(Equal(cfv1alpha1.DaemonSetCloudflaredKind))
				Expect(resource.Spec.Version).To(Equal("latest"))
			})

			It("should update the Cloudflared status", func() {
				Expect(observed().Status.Kind).To(Equal(ptr.To(cfv1alpha1.DaemonSetCloudflaredKind)))
			})

			It("should create a selector that matches pod labels", func() {
				resource := daemonSet()
				Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
				Expect(resource.Spec.Template.Labels).To(Equal(expectedLabels))
			})

			It("should add an owner reference", func() {
				owner := &metav1.OwnerReference{}
				Expect(daemonSet().OwnerReferences).To(ContainElement(
					HaveField("Name", resourceName), owner,
				))
				Expect(owner.APIVersion).To(Equal("cloudflare.unmango.dev/v1alpha1"))
				Expect(owner.Kind).To(Equal("Cloudflared"))
				Expect(owner.Controller).To(Equal(new(true)))
				Expect(owner.BlockOwnerDeletion).To(Equal(new(true)))
			})

			It("should run a hello world tunnel by default", func() {
				container := containerNamed(daemonSet().Spec.Template, cloudflaredContainerName)

				Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
				Expect(container.Command).To(HaveExactElements(
					"cloudflared", "tunnel", "--no-autoupdate", "--metrics", "0.0.0.0:2000",
				))
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

			Context("and the Kind is changed to Deployment", func() {
				var result reconcile.Result

				BeforeEach(func() {
					By("Switching the Kind to Deployment")
					Expect(k8sClient.Get(ctx, typeNamespacedName, cloudflared)).To(Succeed())
					cloudflared.Spec.Kind = cfv1alpha1.DeploymentCloudflaredKind
					Expect(k8sClient.Update(ctx, cloudflared)).To(Succeed())

					// The first pass deletes the DaemonSet and requeues; the
					// second observes it gone and clears the status.
					result = reconcileOnce()
					reconcileOnce()
				})

				It("should delete the DaemonSet", func() {
					err := k8sClient.Get(ctx, typeNamespacedName, &appsv1.DaemonSet{})
					Expect(err).To(MatchError(`daemonsets.apps "test-resource" not found`))
				})

				It("should clear the Kind status", func() {
					Expect(observed().Status.Kind).To(BeNil())
				})

				It("should requeue reconciliation", func() {
					Expect(result.RequeueAfter).To(Equal(5 * time.Second))
				})
			})
		})

		Context("and a pod template is configured", func() {
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
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					reconcileOnce()
				})

				It("should keep the supplied container alongside cloudflared", func() {
					template := daemonSet().Spec.Template

					Expect(template.Labels).To(Equal(expectedLabels))
					Expect(containerNamed(template, cloudflaredContainerName)).NotTo(BeNil())
					Expect(containerNamed(template, expectedContainer).Image).To(Equal(expectedImage))
				})
			})

			Context("and the cloudflared image is overridden", func() {
				BeforeEach(func() {
					cloudflared.Spec.Template.Spec.Containers = []corev1.Container{{
						Name:  cloudflaredContainerName,
						Image: expectedImage,
					}}
					Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
					reconcileOnce()
				})

				It("should use the supplied image and keep the command", func() {
					container := containerNamed(daemonSet().Spec.Template, cloudflaredContainerName)
					Expect(container.Image).To(Equal(expectedImage))
					Expect(container.Command).NotTo(BeEmpty())
				})

				It("should take the version label from the image tag", func() {
					Expect(daemonSet().Spec.Selector.MatchLabels).To(SatisfyAll(
						HaveKeyWithValue("app.kubernetes.io/name", "cloudflare-operator"),
						HaveKeyWithValue("app.kubernetes.io/managed-by", "CloudflaredController"),
						HaveKeyWithValue("app.kubernetes.io/version", "0.0.69"),
					))
				})
			})
		})

		Context("and inline config is provided", func() {
			BeforeEach(func() {
				cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
					CloudflaredConfigInline: cfv1alpha1.CloudflaredConfigInline{
						TunnelId:  ptr.To(tunnelId),
						AccountId: ptr.To(accountId),
					},
				}
				Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())

				cfmock.EXPECT().
					GetTunnelToken(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredTokenGetParams{
						AccountID: cloudflare.F(accountId),
					})).
					Return(ptr.To(token), nil)

				reconcileOnce()
			})

			It("should run the given tunnel", func() {
				container := containerNamed(daemonSet().Spec.Template, cloudflaredContainerName)
				Expect(container.Env).To(ConsistOf(
					corev1.EnvVar{Name: "TUNNEL_TOKEN", Value: token},
				))
				Expect(container.Args).To(HaveExactElements("run", tunnelId))
			})

			It("should update the resource status", func() {
				Expect(observed().Status.TunnelId).To(Equal(ptr.To(tunnelId)))
			})
		})

		Context("and a tunnel reference is provided", func() {
			BeforeEach(func() {
				By("Creating a CloudflareTunnel with an observed id")
				tunnel := &cfv1alpha1.CloudflareTunnel{
					ObjectMeta: cloudflared.ObjectMeta,
					Spec: cfv1alpha1.CloudflareTunnelSpec{
						ConfigSource: cfv1alpha1.CloudflareCloudflareTunnelConfigSource,
					},
				}
				Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

				tunnel.Status = cfv1alpha1.CloudflareTunnelStatus{
					AccountTag: accountId,
					Id:         ptr.To(tunnelId),
				}
				Expect(k8sClient.Status().Update(ctx, tunnel)).To(Succeed())

				cloudflared.Spec.Config = &cfv1alpha1.CloudflaredConfig{
					TunnelRef: &cfv1alpha1.CloudflaredTunnelReference{
						Name: cloudflared.Name,
					},
				}
				Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())

				cfmock.EXPECT().
					GetTunnelToken(gomock.Eq(ctx), gomock.Eq(tunnelId), gomock.Eq(zero_trust.TunnelCloudflaredTokenGetParams{
						AccountID: cloudflare.F(accountId),
					})).
					Return(ptr.To(token), nil)

				reconcileOnce()
			})

			It("should run the referenced tunnel", func() {
				container := containerNamed(daemonSet().Spec.Template, cloudflaredContainerName)
				Expect(container.Env).To(ConsistOf(
					corev1.EnvVar{Name: "TUNNEL_TOKEN", Value: token},
				))
				Expect(container.Args).To(HaveExactElements("run", tunnelId))
			})

			It("should update the resource status", func() {
				Expect(observed().Status.TunnelId).To(Equal(ptr.To(tunnelId)))
			})
		})

		Context("and kind is Deployment", func() {
			BeforeEach(func() {
				cloudflared.Spec.Kind = cfv1alpha1.DeploymentCloudflaredKind
				By("Creating the custom resource for the Kind Cloudflared")
				Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())
				reconcileOnce()
			})

			deployment := func() *appsv1.Deployment {
				GinkgoHelper()
				resource := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
				return resource
			}

			It("should create a Deployment and record it in the status", func() {
				container := containerNamed(deployment().Spec.Template, cloudflaredContainerName)
				Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
				Expect(observed().Status.Kind).To(Equal(ptr.To(cfv1alpha1.DeploymentCloudflaredKind)))
			})

			// https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/deployment-guides/kubernetes/#routing-with-cloudflare-tunnel
			It("should configure the pod and container security contexts", func() {
				template := deployment().Spec.Template

				pod := template.Spec.SecurityContext
				Expect(pod.RunAsNonRoot).To(Equal(new(true)))
				Expect(pod.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
				Expect(pod.Sysctls).To(ConsistOf(corev1.Sysctl{
					Name:  "net.ipv4.ping_group_range",
					Value: "65532 65532",
				}))

				container := containerNamed(template, cloudflaredContainerName).SecurityContext
				Expect(container.RunAsNonRoot).To(Equal(new(true)))
				Expect(container.RunAsUser).To(Equal(ptr.To[int64](1001)))
				Expect(container.AllowPrivilegeEscalation).To(Equal(new(false)))
				Expect(container.Capabilities.Drop).To(ConsistOf(corev1.Capability("ALL")))
			})
		})
	})
})
