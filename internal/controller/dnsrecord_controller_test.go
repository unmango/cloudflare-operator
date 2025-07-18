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

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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

		JustBeforeEach(func() {
			Expect(k8sClient.Create(ctx, dnsrecord)).To(Succeed())
		})

		AfterEach(func() {
			resource := &cfv1alpha1.DnsRecord{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				By("Cleanup the specific resource instance DnsRecord")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
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
				Return(&dns.RecordResponse{
					ID:                "test-id",
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
				}, nil)

			By("Reconciling the resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, dnsrecord)).To(Succeed())
			Expect(dnsrecord.Status.Id).To(Equal(ptr.To("test-id")))
			Expect(dnsrecord.Status.Comment).To(Equal(ptr.To("test-comment")))
			Expect(dnsrecord.Status.Content).To(Equal(ptr.To("test-content")))
			Expect(dnsrecord.Status.Name).To(Equal(ptr.To("test-a-record")))
			Expect(dnsrecord.Status.Type).To(Equal(ptr.To("A")))

			By("Reconciling the created resource")
			cfmock.EXPECT().
				GetDnsRecord(gomock.Eq(ctx), "test-id", gomock.Eq(dns.RecordGetParams{
					ZoneID: cloudflare.F(zoneId),
				})).
				Return(&dns.RecordResponse{
					ID:                "test-id",
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
				}, nil).
				Times(2) // Refresh, then update

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating the resource to a TXT record")
			dnsrecord.Spec.Record = cfv1alpha1.Record{
				TXTRecord: &cfv1alpha1.TXTRecord{
					Comment: "test-comment-2",
					Content: "test-content-2",
					Name:    "test-txt-record",
					Proxied: true,
					Settings: cfv1alpha1.RecordSettings{
						Ipv4Only: true,
						Ipv6Only: true,
					},
					Tags: []cfv1alpha1.RecordTags{"test-tag-2"},
					Ttl:  420,
				},
			}
			Expect(k8sClient.Update(ctx, dnsrecord)).To(Succeed())

			cfmock.EXPECT().
				UpdateDnsRecord(ctx, "test-id", dns.RecordUpdateParams{
					ZoneID: cloudflare.F(zoneId),
					Body: dns.TXTRecordParam{
						Comment: cloudflare.F("test-comment-2"),
						Content: cloudflare.F("test-content-2"),
						Name:    cloudflare.F("test-txt-record"),
						Proxied: cloudflare.F(true),
						Settings: cloudflare.F(dns.TXTRecordSettingsParam{
							IPV4Only: cloudflare.F(true),
							IPV6Only: cloudflare.F(true),
						}),
						Tags: cloudflare.F([]dns.RecordTagsParam{"test-tag-2"}),
						TTL:  cloudflare.F(dns.TTL(420)),
						Type: cloudflare.F(dns.TXTRecordTypeTXT),
					},
				}).
				Return(&dns.RecordResponse{
					ID:                "new-id",
					Comment:           "new-comment",
					CommentModifiedOn: time.Now(),
					Content:           "new-content",
					CreatedOn:         time.Now(),
					ModifiedOn:        time.Now(),
					Name:              "test-txt-record",
					Priority:          69,
					Proxiable:         true,
					Proxied:           true,
					TagsModifiedOn:    time.Now(),
					Type:              dns.RecordResponseTypeTXT,
				}, nil)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, dnsrecord)).To(Succeed())
			Expect(dnsrecord.Status.Id).To(Equal(ptr.To("new-id")))
			Expect(dnsrecord.Status.Comment).To(Equal(ptr.To("new-comment")))
			Expect(dnsrecord.Status.Content).To(Equal(ptr.To("new-content")))
			Expect(dnsrecord.Status.Name).To(Equal(ptr.To("test-txt-record")))
			Expect(dnsrecord.Status.Type).To(Equal(ptr.To("TXT")))

			By("Deleting the resource")
			cfmock.EXPECT().
				DeleteDnsRecord(ctx, "new-id", gomock.Eq(dns.RecordDeleteParams{
					ZoneID: cloudflare.F(zoneId),
				})).
				Return(&dns.RecordDeleteResponse{
					ID: "new-id",
				}, nil)

			Expect(k8sClient.Delete(ctx, dnsrecord)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
