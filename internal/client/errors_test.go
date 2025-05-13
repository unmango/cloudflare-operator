package client_test

import (
	"net/http"

	"github.com/cloudflare/cloudflare-go/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unmango/cloudflare-operator/internal/client"
)

var _ = Describe("Errors", func() {
	It("should be a not found error", func() {
		err := &cloudflare.Error{
			StatusCode: http.StatusNotFound,
		}

		Expect(client.IsNotFound(err)).To(BeTrue())
	})

	It("should be a conflict error", func() {
		err := &cloudflare.Error{
			StatusCode: http.StatusConflict,
		}

		Expect(client.IsConflict(err)).To(BeTrue())
	})

	It("should ignore not found errors", func() {
		err := &cloudflare.Error{
			StatusCode: http.StatusNotFound,
		}

		Expect(client.IgnoreNotFound(err)).To(Succeed())
	})

	It("should ignore conflict errors", func() {
		err := &cloudflare.Error{
			StatusCode: http.StatusConflict,
		}

		Expect(client.IgnoreConflict(err)).To(Succeed())
	})
})
