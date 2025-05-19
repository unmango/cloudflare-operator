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

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/testing"
)

var _ = Describe("DnsRecord Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName string = "test-resource"
			zoneId       string = "test-zone-id"
		)

		ctx := context.Background()

		var (
			ctrl       *gomock.Controller
			cfmock     *testing.MockClient
			reconciler DnsRecordReconciler
		)

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		dnsrecord := &cfv1alpha1.DnsRecord{}

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			cfmock = testing.NewMockClient(ctrl)

			reconciler = DnsRecordReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Cloudflare: cfmock,
			}
			dnsrecord = &cfv1alpha1.DnsRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: cfv1alpha1.DnsRecordSpec{
					ZoneId: zoneId,
					Record: cfv1alpha1.Record{
						ARecord: &cfv1alpha1.ARecord{
							Name: "test-a-record",
						},
					},
				},
			}
		})

		JustBeforeEach(func() {
			Expect(k8sClient.Create(ctx, dnsrecord)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, dnsrecord)).To(Succeed())
		})

		AfterEach(func() {
			resource := &cfv1alpha1.DnsRecord{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DnsRecord")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			cfmock.EXPECT().
				CreateDnsRecord(gomock.Eq(ctx), gomock.Eq(dns.RecordNewParams{
					ZoneID: cloudflare.F(zoneId),
					Record: dns.ARecordParam{
						Name: cloudflare.F("test-a-record"),
					},
				})).
				Return(&dns.RecordResponse{
					ID: "test-id",
				}, nil)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
