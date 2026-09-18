package gatewayapi_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unmango/cloudflare-operator/internal/gatewayapi"
)

var _ = Describe("CRDDirectory", func() {
	It("should locate the standard channel CRDs in the module", func() {
		dir, err := gatewayapi.CRDDirectory()

		Expect(err).NotTo(HaveOccurred())
		Expect(dir).To(BeADirectory())
		Expect(filepath.Join(dir, "gateway.networking.k8s.io_httproutes.yaml")).To(BeAnExistingFile())
	})

	It("should prefer the override", func() {
		Expect(os.Setenv(gatewayapi.DirectoryEnvVar, "/some/where")).To(Succeed())
		DeferCleanup(os.Unsetenv, gatewayapi.DirectoryEnvVar)

		Expect(gatewayapi.CRDDirectory()).To(Equal("/some/where"))
	})
})
