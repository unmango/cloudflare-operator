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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
)

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

		BeforeEach(func() {
			By("Initializing the Cloudfared resource")
			cloudflared = &cfv1alpha1.Cloudflared{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
			}
		})

		JustBeforeEach(func() {
			By("creating the custom resource for the Kind Cloudflared")
			Expect(k8sClient.Create(ctx, cloudflared)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &CloudflaredReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &record.FakeRecorder{},
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
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &record.FakeRecorder{},
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

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

		It("should default to a DaemonSet", func() {
			By("Fetching the resource")
			resource := &cfv1alpha1.Cloudflared{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

			Expect(resource.Spec.Kind).To(Equal(cfv1alpha1.DaemonSet))
		})

		It("should create a DaemonSet", func() {
			By("Fetching the daemon set")
			resource := &appsv1.DaemonSet{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

			Expect(resource).NotTo(BeNil())
			Expect(resource.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := resource.Spec.Template.Spec.Containers[0]
			Expect(container.Name).To(Equal("cloudflared"))
			Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
		})

		It("should create a selector that matches pod labels", func() {
			By("Fetching the DaemonSet")
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

		Context("and pod spec template is configured", func() {
			const (
				expectedImage     = "something/not/cloudflared"
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

		Context("and kind is DaemonSet", func() {
			BeforeEach(func() {
				By("Setting the kind to DaemonSet")
				cloudflared.Spec.Kind = cfv1alpha1.DaemonSet
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

			Context("and pod spec template is configured", func() {
				const (
					expectedImage     = "something/not/cloudflared"
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
		})

		Context("and kind is Deployment", func() {
			BeforeEach(func() {
				cloudflared.Spec.Kind = cfv1alpha1.Deployment
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

			Context("and pod spec template is configured", func() {
				const (
					expectedImage     = "something/not/cloudflared"
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
			})
		})
	})
})
