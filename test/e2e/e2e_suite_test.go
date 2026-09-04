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
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unmango/cloudflare-operator/test/utils"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("verifying the kubeconfig points at the Kind cluster")
	cluster := os.Getenv("KIND_CLUSTER")
	Expect(cluster).NotTo(BeEmpty(), "KIND_CLUSTER must be set; run the suite through `make test-e2e`")
	out, err := utils.Run(exec.Command("kubectl", "config", "current-context"))
	Expect(err).NotTo(HaveOccurred(), "Failed to read the current kubectl context")
	Expect(strings.TrimSpace(out)).To(Equal(fmt.Sprintf("kind-%s", cluster)),
		"The suite deploys the operator into the current context; refusing to touch a cluster that is not Kind")

	By("building the manager image with nix and loading it into Kind")
	_, err = utils.Run(exec.Command("make", "kind-load"))
	Expect(err).NotTo(HaveOccurred(), "Failed to load the manager image into Kind")

	By("installing CRDs")
	_, err = utils.Run(exec.Command("make", "install"))
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying the controller-manager")
	_, err = utils.Run(exec.Command("make", "deploy"))
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
})

var _ = AfterSuite(func() {
	By("undeploying the controller-manager")
	_, _ = utils.Run(exec.Command("make", "undeploy"))

	By("uninstalling CRDs")
	_, _ = utils.Run(exec.Command("make", "uninstall"))
})
