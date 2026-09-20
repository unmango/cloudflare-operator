package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
)

var _ = Describe("resolveDns", func() {
	It("should report no zone when neither level sets one", func() {
		_, ok := resolveDns(nil, nil)

		Expect(ok).To(BeFalse())
	})

	It("should report no zone when only proxied is set", func() {
		_, ok := resolveDns(&cfv1alpha1.CloudflareTunnelDns{
			Proxied: new(false),
		}, nil)

		Expect(ok).To(BeFalse())
	})

	It("should default proxied and ttl when the tunnel sets only a zone", func() {
		resolved, ok := resolveDns(&cfv1alpha1.CloudflareTunnelDns{
			ZoneId: testZoneId,
		}, nil)

		Expect(ok).To(BeTrue())
		Expect(resolved.ZoneId).To(Equal(testZoneId))
		Expect(resolved.Proxied).To(BeTrue())
		Expect(resolved.Ttl).To(Equal(dnsTtlAutomatic))
	})

	It("should take the zone from the entry when the tunnel sets none", func() {
		resolved, ok := resolveDns(nil, &cfv1alpha1.CloudflareTunnelDns{
			ZoneId: testZoneId,
			Ttl:    ptr.To[int64](300),
		})

		Expect(ok).To(BeTrue())
		Expect(resolved.ZoneId).To(Equal(testZoneId))
		Expect(resolved.Ttl).To(BeEquivalentTo(300))
	})

	It("should override one field and inherit the rest", func() {
		resolved, ok := resolveDns(&cfv1alpha1.CloudflareTunnelDns{
			ZoneId:  testZoneId,
			Proxied: new(true),
			Ttl:     ptr.To[int64](300),
		}, &cfv1alpha1.CloudflareTunnelDns{
			Ttl: ptr.To[int64](600),
		})

		Expect(ok).To(BeTrue())
		Expect(resolved.ZoneId).To(Equal(testZoneId))
		Expect(resolved.Proxied).To(BeTrue())
		Expect(resolved.Ttl).To(BeEquivalentTo(600))
	})

	It("should let an entry turn off a proxy the tunnel turned on", func() {
		resolved, ok := resolveDns(&cfv1alpha1.CloudflareTunnelDns{
			ZoneId:  testZoneId,
			Proxied: new(true),
		}, &cfv1alpha1.CloudflareTunnelDns{
			Proxied: new(false),
		})

		Expect(ok).To(BeTrue())
		Expect(resolved.Proxied).To(BeFalse())
	})

	It("should let an entry move a hostname to another zone", func() {
		resolved, ok := resolveDns(&cfv1alpha1.CloudflareTunnelDns{
			ZoneId: testZoneId,
		}, &cfv1alpha1.CloudflareTunnelDns{
			ZoneId: "other-zone-id",
		})

		Expect(ok).To(BeTrue())
		Expect(resolved.ZoneId).To(Equal("other-zone-id"))
	})
})
