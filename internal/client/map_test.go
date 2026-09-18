package client_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	"github.com/unmango/cloudflare-operator/internal/client"
)

var _ = Describe("Map", func() {
	ingressRules := func(config client.CloudflareTunnelConfig) []map[string]any {
		data, err := json.Marshal(config.UpdateParams())
		Expect(err).NotTo(HaveOccurred())

		var params struct {
			Ingress []map[string]any `json:"ingress"`
		}
		Expect(json.Unmarshal(data, &params)).To(Succeed())

		return params.Ingress
	}

	It("should send the hostname and path of a rule that has them", func() {
		rules := ingressRules(client.CloudflareTunnelConfig{
			Ingress: []cfv1alpha1.CloudflareTunnelConfigIngress{{
				Hostname: "app.example.com",
				Path:     "/api",
				Service:  "http://app",
			}},
		})

		Expect(rules).To(HaveLen(1))
		Expect(rules[0]).To(HaveKeyWithValue("hostname", "app.example.com"))
		Expect(rules[0]).To(HaveKeyWithValue("path", "/api"))
		Expect(rules[0]).To(HaveKeyWithValue("service", "http://app"))
	})

	It("should omit the hostname and path of a catch-all rule", func() {
		rules := ingressRules(client.CloudflareTunnelConfig{
			Ingress: []cfv1alpha1.CloudflareTunnelConfigIngress{{
				Hostname: "app.example.com",
				Service:  "http://app",
			}, {
				Service: "http_status:404",
			}},
		})

		Expect(rules).To(HaveLen(2))
		Expect(rules[1]).NotTo(HaveKey("hostname"))
		Expect(rules[1]).NotTo(HaveKey("path"))
		Expect(rules[1]).To(HaveKeyWithValue("service", "http_status:404"))
	})
})
