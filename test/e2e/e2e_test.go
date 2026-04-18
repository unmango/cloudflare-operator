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

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/a8m/envsubst"
	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/zero_trust"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cfclient "github.com/unmango/cloudflare-operator/internal/client"
	"github.com/unmango/cloudflare-operator/test/utils"
)

const (
	namespace              = "cloudflare-operator-system"
	serviceAccountName     = "cloudflare-operator-controller-manager"
	metricsServiceName     = "cloudflare-operator-controller-manager-metrics-service"
	metricsRoleBindingName = "cloudflare-operator-metrics-binding"
)

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	const testNamespace = "cloudflared-test"

	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")
		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")
		DeferCleanup(func() {
			cmd := exec.Command("make", "uninstall")
			_, _ = utils.Run(cmd)
		})

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
		DeferCleanup(func() {
			cmd := exec.Command("make", "undeploy")
			_, _ = utils.Run(cmd)
		})

		By("creating a namespace for tests")
		cmd = exec.Command("kubectl", "create", "ns", testNamespace)
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})
	})

	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching cloudflared DaemonSet description")
			cmd := exec.Command("kubectl", "describe", "daemonset", "cloudflared-e2e", "-n", testNamespace)
			daemonsetDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("DaemonSet description:\n", daemonsetDescription)
			} else {
				fmt.Println("Failed to describe cloudflared daemonset")
			}

			By("Fetching cloudflared DaemonSet logs")
			cmd = exec.Command("kubectl", "logs", "daemonset/cloudflared-e2e",
				"--namespace", testNamespace,
				"--all-containers=true")
			daemonSetLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "DaemonSet logs:\n %s", daemonSetLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get DaemonSet logs: %s", err)
			}

			By("Fetching controller manager pod logs")
			cmd = exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Controller Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=cloudflare-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "clusterrolebinding", metricsRoleBindingName, "--ignore-not-found")
				_, _ = utils.Run(cmd)
				cmd = exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics\tServing metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccount": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring("controller_runtime_reconcile_total"))
		})
	})

	Context("Cloudflared", func() {
		const cloudflaredName = "cloudflared-e2e"

		It("should create a DaemonSet and set Available condition", func() {
			By("creating a Cloudflared resource with no tunnel config")
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace, "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: cloudflare.unmango.dev/v1alpha1
kind: Cloudflared
metadata:
  name: %s
  namespace: %s
spec:
  kind: DaemonSet
  version: latest
`, cloudflaredName, testNamespace))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				By("deleting the Cloudflared resource")
				cmd := exec.Command("kubectl", "delete", "cloudflared", cloudflaredName,
					"-n", testNamespace, "--ignore-not-found", "--timeout=30s")
				_, _ = utils.Run(cmd)

				By("verifying the DaemonSet is removed via owner reference GC")
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "daemonset", cloudflaredName, "-n", testNamespace)
					_, err := utils.Run(cmd)
					g.Expect(err).To(HaveOccurred())
				}).Should(Succeed())
			})

			By("verifying the DaemonSet is created")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "daemonset", cloudflaredName, "-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}).Should(Succeed())

			By("verifying the Available condition is True")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cloudflared", cloudflaredName,
					"-n", testNamespace,
					"-o", `jsonpath={.status.conditions[?(@.type=="Available")].status}`)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"))
			}).Should(Succeed())
		})
	})

	Context("CloudflareTunnel", func() {
		const tunnelName = "tunnel-e2e"

		It("should initialize conditions on reconciliation", func() {
			By("creating a CloudflareTunnel resource")
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace, "-f", "-")
			cmd.Stdin = strings.NewReader(fmt.Sprintf(`
apiVersion: cloudflare.unmango.dev/v1alpha1
kind: CloudflareTunnel
metadata:
  name: %s
  namespace: %s
spec:
  accountId: fake-account-id
  configSource: local
`, tunnelName, testNamespace))
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "cloudflaretunnel", tunnelName,
					"-n", testNamespace, "--ignore-not-found", "--timeout=30s")
				_, _ = utils.Run(cmd)
			})

			By("verifying conditions are initialized by the controller")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cloudflaretunnel", tunnelName,
					"-n", testNamespace,
					"-o", `jsonpath={.status.conditions}`)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "conditions should be initialized")
			}).Should(Succeed())
		})
	})

	// +kubebuilder:scaffold:e2e-webhooks-checks

	Context("CloudflareTunnel Cloudflare API", Label("cloudflare-api"), func() {
		BeforeEach(func() {
			if os.Getenv("CLOUDFLARE_API_TOKEN") == "" || os.Getenv("CLOUDFLARE_ACCOUNT_ID") == "" {
				Skip("skipping: CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID must be set")
			}
		})

		It("should create and delete a tunnel via the Cloudflare API", func(ctx context.Context) {
			cf := cfclient.New()

			sample, err := envsubst.ReadFile("config/samples/cloudflare_v1alpha1_cloudflaretunnel.yaml")
			Expect(err).NotTo(HaveOccurred())

			By("applying the sample CloudflareTunnel resource")
			cmd := exec.Command("kubectl", "apply", "-n", testNamespace, "-f", "-")
			cmd.Stdin = bytes.NewReader(sample)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func(ctx context.Context) {
				By("deleting the CloudflareTunnel resource")
				cmd := exec.Command("kubectl", "delete", "-n", testNamespace, "--timeout=1m",
					"cloudflaretunnel", "cloudflaretunnel-sample", "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

			var tunnelId string
			By("waiting for the tunnel ID to appear in status")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "cloudflaretunnel",
					"--namespace", testNamespace, "cloudflaretunnel-sample",
					"-o", "jsonpath={.status.id}")
				tunnelId, err = utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(tunnelId).NotTo(BeEmpty())
			}).Should(Succeed())

			By("verifying the tunnel exists in the Cloudflare API")
			Eventually(func(g Gomega) {
				res, err := cf.GetTunnel(ctx, tunnelId, zero_trust.TunnelCloudflaredGetParams{
					AccountID: cloudflare.F(os.Getenv("CLOUDFLARE_ACCOUNT_ID")),
				})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(res.Name).To(Equal("cloudflaretunnel-sample"))
			}).Should(Succeed())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestBody = `{"apiVersion":"authentication.k8s.io/v1","kind":"TokenRequest"}`

	var out string
	verifyTokenCreation := func(g Gomega) {
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", "-")
		cmd.Stdin = strings.NewReader(tokenRequestBody)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, nil
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() string {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	metricsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
	Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
	return metricsOutput
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
