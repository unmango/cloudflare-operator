package gateway_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
)

var _ = Describe("Envoy naming", func() {
	named := func(name string) *gatewayv1.Gateway {
		return &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: name, UID: "1234"}}
	}

	It("should name the proxy after the Gateway", func() {
		Expect(gateway.EnvoyObjectName(named("web"))).To(Equal("web-envoy"))
	})

	DescribeTable("should hash a name that is not a DNS-1035 label",
		func(name string) {
			out := gateway.EnvoyObjectName(named(name))

			Expect(out).To(HavePrefix("envoy-"))
			Expect(validation.IsDNS1035Label(out)).To(BeEmpty())
			Expect(gateway.EnvoyObjectName(named(name))).To(Equal(out))
		},
		Entry("with a dot", "web.example"),
		Entry("starting with a digit", "1web"),
		Entry("too long", strings.Repeat("a", 60)),
	)

	It("should only set the gateway-name label when the name fits", func() {
		Expect(gateway.Labels(named("web"))).To(HaveKeyWithValue(gateway.LabelGatewayName, "web"))
		Expect(gateway.Labels(named(strings.Repeat("a", 64)))).NotTo(HaveKey(gateway.LabelGatewayName))
	})

	It("should move privileged ports out of the way", func() {
		Expect(gateway.ContainerPort(80)).To(Equal(int32(10080)))
		Expect(gateway.ContainerPort(8080)).To(Equal(int32(8080)))
	})
})
