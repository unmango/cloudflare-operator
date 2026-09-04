package client_test

import (
	"fmt"
	"net/http"

	"github.com/cloudflare/cloudflare-go/v7"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unmango/cloudflare-operator/internal/client"
)

var _ = Describe("Errors", func() {
	apiErr := func(code int) error {
		return &cloudflare.Error{StatusCode: code}
	}

	DescribeTable("classifying API errors",
		func(err error, isNotFound, isConflict bool) {
			Expect(client.IsNotFound(err)).To(Equal(isNotFound))
			Expect(client.IsConflict(err)).To(Equal(isConflict))
		},
		Entry("not found", apiErr(http.StatusNotFound), true, false),
		Entry("conflict", apiErr(http.StatusConflict), false, true),
		Entry("server error", apiErr(http.StatusInternalServerError), false, false),
		Entry("not an API error", fmt.Errorf("boom"), false, false),
	)

	DescribeTable("ignoring API errors",
		func(ignore func(error) error, err error, want bool) {
			if want {
				Expect(ignore(err)).To(Succeed())
			} else {
				Expect(ignore(err)).To(MatchError(err))
			}
		},
		Entry("IgnoreNotFound swallows 404", client.IgnoreNotFound, apiErr(http.StatusNotFound), true),
		Entry("IgnoreNotFound passes 409 through", client.IgnoreNotFound, apiErr(http.StatusConflict), false),
		Entry("IgnoreConflict swallows 409", client.IgnoreConflict, apiErr(http.StatusConflict), true),
		Entry("IgnoreConflict passes 404 through", client.IgnoreConflict, apiErr(http.StatusNotFound), false),
	)
})
