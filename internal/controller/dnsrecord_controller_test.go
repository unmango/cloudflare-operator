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
	"github.com/cloudflare/cloudflare-go/v7/dns"
	"github.com/unmango/cloudflare-operator/internal/testing"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("DnsRecord Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName = "test-resource"
			zoneId       = "test-zone-id"
			recordId     = "test-id"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNamespace,
		}

		var (
			cfmock     *testing.MockClient
			reconciler DnsRecordReconciler
			dnsrecord  *cfv1alpha1.DnsRecord
		)

		// The response the Cloudflare API returns for the A record below.
		recordResponse := func() *dns.RecordResponse {
			return &dns.RecordResponse{
				ID:                recordId,
				Comment:           "test-comment",
				CommentModifiedOn: time.Now(),
				Content:           "test-content",
				CreatedOn:         time.Now(),
				ModifiedOn:        time.Now(),
				Name:              "test-a-record",
				Priority:          69,
				Proxiable:         true,
				Proxied:           true,
				TagsModifiedOn:    time.Now(),
				Type:              dns.RecordResponseTypeA,
			}
		}

		reconcileOnce := func() {
			GinkgoHelper()
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
		}

		observed := func() *cfv1alpha1.DnsRecord {
			GinkgoHelper()
			resource := &cfv1alpha1.DnsRecord{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			return resource
		}

		BeforeEach(func() {
			cfmock = testing.NewMockClient(gomock.NewController(GinkgoT()))
			reconciler = DnsRecordReconciler{
				Client:     k8sClient,
				Scheme:     k8sClient.Scheme(),
				Cloudflare: cfmock,
			}

			dnsrecord = &cfv1alpha1.DnsRecord{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: testNamespace,
				},
				Spec: cfv1alpha1.DnsRecordSpec{
					ZoneId: zoneId,
					Record: cfv1alpha1.Record{
						ARecord: &cfv1alpha1.ARecord{
							Comment: "test-comment",
							Content: "test-content",
							Name:    "test-a-record",
							Proxied: true,
							Settings: cfv1alpha1.RecordSettings{
								Ipv4Only: true,
								Ipv6Only: true,
							},
							Tags: []cfv1alpha1.RecordTags{"test-tag"},
							Ttl:  69,
						},
					},
				},
			}
		})

		AfterEach(func() {
			deleteIfExists(ctx, typeNamespacedName, &cfv1alpha1.DnsRecord{})
		})

		Context("and the record does not exist yet", func() {
			BeforeEach(func() {
				// Asserting on the full parameter set is the point: this is
				// where the CRD spec is translated into the Cloudflare API.
				cfmock.EXPECT().
					CreateDnsRecord(gomock.Eq(ctx), gomock.Eq(dns.RecordNewParams{
						ZoneID: cloudflare.F(zoneId),
						Body: dns.ARecordParam{
							Comment: cloudflare.F("test-comment"),
							Content: cloudflare.F("test-content"),
							Name:    cloudflare.F("test-a-record"),
							Proxied: cloudflare.F(true),
							Settings: cloudflare.F(dns.ARecordSettingsParam{
								IPV4Only: cloudflare.F(true),
								IPV6Only: cloudflare.F(true),
							}),
							Tags: cloudflare.F([]dns.RecordTagsParam{"test-tag"}),
							TTL:  cloudflare.F(dns.TTL(69)),
							Type: cloudflare.F(dns.ARecordTypeA),
						},
					})).
					Return(recordResponse(), nil)

				Expect(k8sClient.Create(ctx, dnsrecord)).To(Succeed())
				reconcileOnce()
			})

			It("should record the created record in the status", func() {
				status := observed().Status
				Expect(status.Id).To(Equal(ptr.To(recordId)))
				Expect(status.Comment).To(Equal(new("test-comment")))
				Expect(status.Content).To(Equal(new("test-content")))
				Expect(status.Name).To(Equal(new("test-a-record")))
				Expect(status.Type).To(Equal(new("A")))
			})

			It("should add a finalizer", func() {
				Expect(observed().Finalizers).To(ConsistOf(dnsRecordFinalizer))
			})
		})

		Context("and the status already holds a record id", func() {
			BeforeEach(func() {
				Expect(k8sClient.Create(ctx, dnsrecord)).To(Succeed())
				dnsrecord.Status.Id = ptr.To(recordId)
				Expect(k8sClient.Status().Update(ctx, dnsrecord)).To(Succeed())
			})

			It("should refresh the record rather than create another", func() {
				// CreateDnsRecord is deliberately not expected.
				cfmock.EXPECT().
					GetDnsRecord(gomock.Eq(ctx), gomock.Eq(recordId), gomock.Eq(dns.RecordGetParams{
						ZoneID: cloudflare.F(zoneId),
					})).
					Return(recordResponse(), nil).
					AnyTimes()

				reconcileOnce()

				Expect(observed().Status.Id).To(Equal(ptr.To(recordId)))
			})
		})
	})
})
