package xds_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"google.golang.org/protobuf/encoding/protojson"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"

	"github.com/unmango/cloudflare-operator/internal/xds"
)

var _ = Describe("Bootstrap", func() {
	It("should point Envoy at the xDS server under its node id", func() {
		out, err := xds.Bootstrap("default/web", "xds.example.svc", 18000)
		Expect(err).NotTo(HaveOccurred())

		bootstrap := &bootstrapv3.Bootstrap{}
		Expect(protojson.Unmarshal([]byte(out), bootstrap)).To(Succeed())
		Expect(bootstrap.GetNode().GetId()).To(Equal("default/web"))

		var found bool
		for _, c := range bootstrap.GetStaticResources().GetClusters() {
			if c.GetName() != "xds" {
				continue
			}
			found = true
			addr := c.GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress()
			Expect(addr.GetAddress()).To(Equal("xds.example.svc"))
			Expect(addr.GetPortValue()).To(Equal(uint32(18000)))
		}
		Expect(found).To(BeTrue())
	})
})

var _ = Describe("ParseAddress", func() {
	It("should split host and port", func() {
		addr, err := xds.ParseAddress("xds.example.svc:18000")
		Expect(err).NotTo(HaveOccurred())
		Expect(addr).To(Equal(xds.Address{Host: "xds.example.svc", Port: 18000}))
		Expect(addr.String()).To(Equal("xds.example.svc:18000"))
	})

	It("should reject a missing port", func() {
		_, err := xds.ParseAddress("xds.example.svc")
		Expect(err).To(HaveOccurred())
	})
})
