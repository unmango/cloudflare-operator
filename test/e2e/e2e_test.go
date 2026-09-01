//go:build e2e

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

package e2e

import (
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unmango/cloudflare-operator/test/utils"
)

// namespace is where config/default puts the controller-manager.
const namespace = "cloudflare-operator-system"

// The unit and envtest suites cover reconciliation behaviour. This suite only
// answers the question envtest cannot: does the image we ship actually start in
// a real cluster with the manifests we generate, and does the API server accept
// the CRDs we publish.
var _ = Describe("Manager", Ordered, func() {
	It("should run successfully", func() {
		verifyRunning := func(g Gomega) {
			output, err := utils.Run(exec.Command("kubectl", "get", "pods",
				"-l", "control-plane=controller-manager",
				"-o", "jsonpath={.items[*].status.phase}",
				"-n", namespace,
			))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.Fields(output)).To(ConsistOf("Running"))
		}

		Eventually(verifyRunning, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("should not report errors in its logs", func() {
		output, err := utils.Run(exec.Command("kubectl", "logs",
			"-l", "control-plane=controller-manager",
			"-c", "manager",
			"-n", namespace,
		))
		Expect(err).NotTo(HaveOccurred())
		Expect(output).NotTo(ContainSubstring("Failed to start manager"))
	})

	It("should accept the sample resources", func() {
		_, err := utils.Run(exec.Command("kubectl", "apply", "-k", "config/samples/"))
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "-k", "config/samples/", "--ignore-not-found"))
		})

		for _, kind := range []string{"cloudflareds", "cloudflaretunnels", "dnsrecords"} {
			output, err := utils.Run(exec.Command("kubectl", "get", kind, "-o", "name"))
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(BeEmpty(), "expected a %s sample to exist", kind)
		}
	})
})
