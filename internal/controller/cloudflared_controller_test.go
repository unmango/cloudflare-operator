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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
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
		expectedLabels := map[string]string{"app": "cloudflared"}

		JustBeforeEach(func() {
			By("creating the custom resource for the Kind Cloudflared")
			err := k8sClient.Get(ctx, typeNamespacedName, cloudflared)
			if err != nil && errors.IsNotFound(err) {
				resource := &cfv1alpha1.Cloudflared{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}

			By("Reconciling the created resource")
			controllerReconciler := &CloudflaredReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			resource := &cfv1alpha1.Cloudflared{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Cloudflared")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			daemonSet := &appsv1.DaemonSet{}
			if err = k8sClient.Get(ctx, typeNamespacedName, daemonSet); err == nil {
				By("Cleaning up the DaemonSet")
				_ = k8sClient.Delete(ctx, daemonSet)
			}

			deployment := &appsv1.Deployment{}
			if err = k8sClient.Get(ctx, typeNamespacedName, deployment); err == nil {
				By("Cleaning up the Deployment")
				_ = k8sClient.Delete(ctx, deployment)
			}

			By("Resetting the spec")
			cloudflared.Spec = cfv1alpha1.CloudflaredSpec{}
		})

		It("should default to a DaemonSet", func() {
			By("Fetching the resource")
			resource := &cfv1alpha1.Cloudflared{}
			Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

			Expect(resource.Spec.Kind).To(Equal(cfv1alpha1.DaemonSet))
		})

		It("should create a DaemonSet", func() {
			By("Fetching the daemon set")
			resource := &appsv1.DaemonSet{}
			Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

			Expect(resource).NotTo(BeNil())
			Expect(resource.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := resource.Spec.Template.Spec.Containers[0]
			Expect(container.Name).To(Equal("cloudflared"))
			Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
		})

		It("should create a selector that matches pod labels", func() {
			By("Fetching the DaemonSet")
			resource := &appsv1.DaemonSet{}
			Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

			Expect(resource).NotTo(BeNil())
			Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
		})

		It("should add an owner reference", func() {
			By("Fetching the DaemonSet")
			resource := &appsv1.DaemonSet{}
			Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

			Expect(resource).NotTo(BeNil())
			Expect(resource.OwnerReferences).To(ContainElement(metav1.OwnerReference{
				APIVersion: "v1alpha1",
				Kind:       "Cloudflared",
				Name:       typeNamespacedName.Name,
				Controller: ptr.To(true),
				UID:        cloudflared.UID,
			}))
		})

		Context("and pod spec template is configured", func() {
			const (
				expectedImage     = "something/not/cloudflared"
				expectedContainer = "container-name"
			)

			BeforeEach(func() {
				By("Setting labels and containers")
				cloudflared.Spec = cfv1alpha1.CloudflaredSpec{
					Template: &v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: expectedLabels},
						Spec: v1.PodSpec{
							Containers: []v1.Container{{
								Name:  expectedContainer,
								Image: expectedImage,
							}},
						},
					},
				}
			})

			It("should create a DaemonSet", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.Spec.Template.Labels).To(Equal(expectedLabels))
				Expect(resource.Spec.Template.Spec.Containers).To(HaveLen(1))
				container := resource.Spec.Template.Spec.Containers[0]
				Expect(container.Name).To(Equal(expectedContainer))
				Expect(container.Image).To(Equal(expectedImage))
			})

			It("should create a selector that matches pod labels", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
			})

			It("should add an owner reference", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.OwnerReferences).To(ContainElement(metav1.OwnerReference{
					APIVersion: "v1alpha1",
					Kind:       "CloudflaredDeployment",
					Name:       typeNamespacedName.Name,
					Controller: ptr.To(true),
					UID:        cloudflared.UID,
				}))
			})
		})

		Context("and kind is DaemonSet", func() {
			BeforeEach(func() {
				By("Setting the kind to DaemonSet")
				cloudflared.Spec = cfv1alpha1.CloudflaredSpec{
					Kind: cfv1alpha1.DaemonSet,
				}
			})

			It("should create a DaemonSet", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.Spec.Template.Spec.Containers).To(HaveLen(1))
				container := resource.Spec.Template.Spec.Containers[0]
				Expect(container.Name).To(Equal("cloudflared"))
				Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
			})

			It("should create a selector that matches pod labels", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
			})

			It("should add an owner reference", func() {
				By("Fetching the DaemonSet")
				resource := &appsv1.DaemonSet{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.OwnerReferences).To(ContainElement(metav1.OwnerReference{
					// TODO: Can any of this be pulled from somewhere else?
					APIVersion: "v1alpha1",
					Kind:       "CloudflaredDeployment",
					Name:       typeNamespacedName.Name,
					Controller: ptr.To(true),
					UID:        cloudflared.UID,
				}))
			})

			Context("and pod spec template is configured", func() {
				const (
					expectedImage     = "something/not/cloudflared"
					expectedContainer = "container-name"
				)

				BeforeEach(func() {
					By("Setting labels and containers")
					cloudflared.Spec = cfv1alpha1.CloudflaredSpec{
						Template: &v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: expectedLabels},
							Spec: v1.PodSpec{
								Containers: []v1.Container{{
									Name:  expectedContainer,
									Image: expectedImage,
								}},
							},
						},
					}
				})

				It("should create a DaemonSet", func() {
					By("Fetching the DaemonSet")
					resource := &appsv1.DaemonSet{}
					Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.Spec.Template.Labels).To(Equal(expectedLabels))
					Expect(resource.Spec.Template.Spec.Containers).To(HaveLen(1))
					container := resource.Spec.Template.Spec.Containers[0]
					Expect(container.Name).To(Equal(expectedContainer))
					Expect(container.Image).To(Equal(expectedImage))
				})

				It("should create a selector that matches pod labels", func() {
					By("Fetching the DaemonSet")
					resource := &appsv1.DaemonSet{}
					Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
				})

				It("should add an owner reference", func() {
					By("Fetching the DaemonSet")
					resource := &appsv1.DaemonSet{}
					Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.OwnerReferences).To(ContainElement(metav1.OwnerReference{
						APIVersion: "v1alpha1",
						Kind:       "CloudflaredDeployment",
						Name:       typeNamespacedName.Name,
						Controller: ptr.To(true),
						UID:        cloudflared.UID,
					}))
				})
			})
		})

		Context("and kind is Deployment", func() {
			BeforeEach(func() {
				By("Setting the kind to Deployment")
				cloudflared.Spec = cfv1alpha1.CloudflaredSpec{
					Kind: cfv1alpha1.Deployment,
				}
			})

			It("should create a Deployment", func() {
				By("Fetching the deployment")
				resource := &appsv1.Deployment{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.Spec.Template.Spec.Containers).To(HaveLen(1))
				container := resource.Spec.Template.Spec.Containers[0]
				Expect(container.Name).To(Equal("cloudflared"))
				Expect(container.Image).To(Equal("docker.io/cloudflare/cloudflared:latest"))
			})

			It("should add an owner reference", func() {
				By("Fetching the Deployment")
				resource := &appsv1.Deployment{}
				Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

				Expect(resource).NotTo(BeNil())
				Expect(resource.OwnerReferences).To(ContainElement(metav1.OwnerReference{
					APIVersion: "v1alpha1",
					Kind:       "CloudflaredDeployment",
					Name:       typeNamespacedName.Name,
					Controller: ptr.To(true),
					UID:        cloudflared.UID,
				}))
			})

			Context("and pod spec template is configured", func() {
				const (
					expectedImage     = "something/not/cloudflared"
					expectedContainer = "container-name"
				)

				expectedLabels := map[string]string{"app": "cloudflared"}

				BeforeEach(func() {
					By("Setting labels and containers")
					cloudflared.Spec = cfv1alpha1.CloudflaredSpec{
						Template: &v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: expectedLabels},
							Spec: v1.PodSpec{
								Containers: []v1.Container{{
									Name:  expectedContainer,
									Image: expectedImage,
								}},
							},
						},
					}
				})

				It("should create a Deployment", func() {
					By("Fetching the Deployment")
					resource := &appsv1.Deployment{}
					Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.Spec.Template.Labels).To(Equal(expectedLabels))
					Expect(resource.Spec.Template.Spec.Containers).To(HaveLen(1))
					container := resource.Spec.Template.Spec.Containers[0]
					Expect(container.Name).To(Equal(expectedContainer))
					Expect(container.Image).To(Equal(expectedImage))
				})

				It("should create a selector that matches pod labels", func() {
					By("Fetching the Deployment")
					resource := &appsv1.Deployment{}
					Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.Spec.Selector.MatchLabels).To(Equal(expectedLabels))
				})

				It("should add an owner reference", func() {
					By("Fetching the Deployment")
					resource := &appsv1.Deployment{}
					Eventually(k8sClient.Get(ctx, typeNamespacedName, resource)).Should(Succeed())

					Expect(resource).NotTo(BeNil())
					Expect(resource.OwnerReferences).To(ContainElement(metav1.OwnerReference{
						APIVersion: "v1alpha1",
						Kind:       "CloudflaredDeployment",
						Name:       typeNamespacedName.Name,
						Controller: ptr.To(true),
						UID:        cloudflared.UID,
					}))
				})
			})
		})

		Context("and deployment is marked for deletion", func() {
			var expectedTimestamp metav1.Time

			BeforeEach(func() {
				By("Setting the deletion timestamp")
				expectedTimestamp = metav1.NewTime(time.Now())
				cloudflared.DeletionTimestamp = &expectedTimestamp
			})

			AfterEach(func() {
				By("Clearing the deletion timestamp")
				cloudflared.DeletionTimestamp = nil
			})

			It("should not leave any DaemonSets", func() {
				By("Attempting to fetch a matching DaemonSet")
				daemonSet := &appsv1.DaemonSet{}
				err := k8sClient.Get(ctx, typeNamespacedName, daemonSet)

				Expect(errors.IsNotFound(err)).To(BeTrue())
			})

			It("should not leave any Deployments", func() {
				By("Attempting to fetch a matching Deployment")
				resource := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, typeNamespacedName, resource)

				Expect(errors.IsNotFound(err)).To(BeTrue())
			})

			Context("and a similarly named DaemonSet exists", func() {
				existingDaemonSet := appsv1.DaemonSet{}

				BeforeEach(func() {
					By("Setting existing DaemonSet metadata")
					existingDaemonSet.Name = typeNamespacedName.Name
					existingDaemonSet.Namespace = typeNamespacedName.Namespace

					By("Configuring the existing DaemonSet spec")
					existingDaemonSet.Spec = appsv1.DaemonSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"testKey": "testValue"},
						},
						Template: v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"testKey": "testValue"},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{{
									Name:  "testing",
									Image: defaultCloudflaredImage,
								}},
							},
						},
					}
				})

				JustBeforeEach(func() {
					By("Creating the existing DaemonSet")
					Expect(k8sClient.Create(ctx, &existingDaemonSet)).To(Succeed())
				})

				AfterEach(func() {
					resource := &appsv1.DaemonSet{}
					if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
						By("Cleaning up the DaemonSet")
						_ = k8sClient.Delete(ctx, resource)
					}

					By("Clearing the existing DaemonSet")
					existingDaemonSet = appsv1.DaemonSet{}
				})

				// We don't own this DaemonSet, so we shouldn't touch it
				It("should ignore the existing DaemonSet", func() {
					resource := &appsv1.DaemonSet{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
				})
			})

			Context("and an owned DaemonSet exists", func() {
				existingDaemonSet := appsv1.DaemonSet{}

				BeforeEach(func() {
					By("Setting existing DaemonSet metadata")
					existingDaemonSet.Name = typeNamespacedName.Name
					existingDaemonSet.Namespace = typeNamespacedName.Namespace

					By("Configuring the existing DaemonSet spec")
					existingDaemonSet.Spec = appsv1.DaemonSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"testKey": "testValue"},
						},
						Template: v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"testKey": "testValue"},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{{
									Name:  "testing",
									Image: defaultCloudflaredImage,
								}},
							},
						},
					}

					By("Setting the owner reference")
					existingDaemonSet.OwnerReferences = append(existingDaemonSet.OwnerReferences, metav1.OwnerReference{
						APIVersion: "v1alpha1",
						Kind:       "CloudflaredDeployment",
						Name:       typeNamespacedName.Name,
						Controller: ptr.To(true),
						UID:        cloudflared.UID,
					})
				})

				JustBeforeEach(func() {
					By("Creating the existing DaemonSet")
					Expect(k8sClient.Create(ctx, &existingDaemonSet)).To(Succeed())
				})

				AfterEach(func() {
					resource := &appsv1.DaemonSet{}
					if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
						By("Cleaning up the DaemonSet")
						_ = k8sClient.Delete(ctx, resource)
					}

					By("Clearing the existing DaemonSet")
					existingDaemonSet = appsv1.DaemonSet{}
				})

				It("should delete the existing DaemonSet", func() {
					resource := &appsv1.DaemonSet{}
					err := k8sClient.Get(ctx, typeNamespacedName, resource)

					Expect(errors.IsNotFound(err)).To(BeTrue())
				})
			})

			Context("and a similarly named Deployment exists", func() {
				existingDeployment := appsv1.Deployment{}

				BeforeEach(func() {
					By("Setting existing Deployment metadata")
					existingDeployment.Name = typeNamespacedName.Name
					existingDeployment.Namespace = typeNamespacedName.Namespace

					By("Configuring the existing Deployment spec")
					existingDeployment.Spec = appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"testKey": "testValue"},
						},
						Template: v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"testKey": "testValue"},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{{
									Name:  "testing",
									Image: defaultCloudflaredImage,
								}},
							},
						},
					}
				})

				JustBeforeEach(func() {
					By("Creating the existing Deployment")
					Expect(k8sClient.Create(ctx, &existingDeployment)).To(Succeed())
				})

				AfterEach(func() {
					resource := &appsv1.Deployment{}
					if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
						By("Cleaning up the Deployment")
						_ = k8sClient.Delete(ctx, resource)
					}

					By("Clearing the existing Deployment")
					existingDeployment = appsv1.Deployment{}
				})

				// We don't own this Deployment, so we shouldn't touch it
				It("should ignore the existing Deployment", func() {
					resource := &appsv1.Deployment{}
					Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
				})
			})

			Context("and an owned Deployment exists", func() {
				existingDeployment := appsv1.Deployment{}

				BeforeEach(func() {
					By("Setting existing Deployment metadata")
					existingDeployment.Name = typeNamespacedName.Name
					existingDeployment.Namespace = typeNamespacedName.Namespace

					By("Configuring the existing Deployment spec")
					existingDeployment.Spec = appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"testKey": "testValue"},
						},
						Template: v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"testKey": "testValue"},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{{
									Name:  "testing",
									Image: defaultCloudflaredImage,
								}},
							},
						},
					}

					By("Setting the owner reference")
					existingDeployment.OwnerReferences = append(existingDeployment.OwnerReferences, metav1.OwnerReference{
						APIVersion: "v1alpha1",
						Kind:       "CloudflaredDeployment",
						Name:       typeNamespacedName.Name,
						Controller: ptr.To(true),
						UID:        cloudflared.UID,
					})
				})

				JustBeforeEach(func() {
					By("Creating the existing Deployment")
					Expect(k8sClient.Create(ctx, &existingDeployment)).To(Succeed())
				})

				AfterEach(func() {
					resource := &appsv1.Deployment{}
					if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
						By("Cleaning up the Deployment")
						_ = k8sClient.Delete(ctx, resource)
					}

					By("Clearing the existing Deployment")
					existingDeployment = appsv1.Deployment{}
				})

				It("should delete the existing Deployment", func() {
					resource := &appsv1.Deployment{}
					err := k8sClient.Get(ctx, typeNamespacedName, resource)

					Expect(errors.IsNotFound(err)).To(BeTrue())
				})
			})
		})
	})
})
