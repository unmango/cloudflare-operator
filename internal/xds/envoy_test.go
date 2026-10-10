package xds_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/unmango/cloudflare-operator/internal/gateway"
	"github.com/unmango/cloudflare-operator/internal/xds"
)

// envoyEnvVar names an Envoy binary to run these specs against. They are skipped
// without one, which is the case in CI: they exist to check the bootstrap and
// the translation against a real proxy when either changes.
const envoyEnvVar = "ENVOY"

func freePort() int {
	GinkgoHelper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = lis.Close() }()

	return lis.Addr().(*net.TCPAddr).Port
}

var _ = Describe("Envoy", Ordered, func() {
	const node = "default/smoke"

	var (
		cache    = xds.NewCache(logr.Discard())
		listener gatewayv1.PortNumber
	)

	BeforeAll(func() {
		bin, ok := os.LookupEnv(envoyEnvVar)
		if !ok {
			Skip(envoyEnvVar + " is not set")
		}

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		xdsPort := freePort()
		go func() {
			defer GinkgoRecover()
			server := &xds.Server{Address: fmt.Sprintf("127.0.0.1:%d", xdsPort), Cache: cache, Log: logr.Discard()}
			Expect(server.Start(ctx)).To(Succeed())
		}()

		listener = gatewayv1.PortNumber(freePort())
		snapshot, err := xds.Snapshot(listenersOf(httpListener("http", listener, "")))
		Expect(err).NotTo(HaveOccurred())
		Expect(cache.SetSnapshot(ctx, node, snapshot)).To(Succeed())

		bootstrap, err := xds.Bootstrap(node, "127.0.0.1", uint32(xdsPort))
		Expect(err).NotTo(HaveOccurred())

		cmd := exec.CommandContext(ctx, bin,
			"--config-yaml", bootstrap,
			"--disable-hot-restart",
			"--base-id", strconv.Itoa(xdsPort),
			"--log-level", "warn",
		)
		cmd.Stdout = GinkgoWriter
		cmd.Stderr = GinkgoWriter
		Expect(cmd.Start()).To(Succeed())
		DeferCleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	})

	It("should report ready once it has its configuration", func() {
		Eventually(func() (int, error) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ready", gateway.EnvoyReadinessPort))
			if err != nil {
				return 0, err
			}
			_ = res.Body.Close()

			return res.StatusCode, nil
		}, 30*time.Second, 250*time.Millisecond).Should(Equal(http.StatusOK))
	})

	It("should answer 404 on a listener with no routes", func() {
		Eventually(func() (int, error) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", listener))
			if err != nil {
				return 0, err
			}
			_ = res.Body.Close()

			return res.StatusCode, nil
		}, 30*time.Second, 250*time.Millisecond).Should(Equal(http.StatusNotFound))
	})

	It("should pick up a new listener from the next snapshot", func() {
		second := gatewayv1.PortNumber(freePort())
		snapshot, err := xds.Snapshot(listenersOf(
			httpListener("http", listener, ""),
			httpListener("second", second, "app.example.com"),
		))
		Expect(err).NotTo(HaveOccurred())
		Expect(cache.SetSnapshot(context.Background(), node, snapshot)).To(Succeed())

		Eventually(func() (int, error) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", second))
			if err != nil {
				return 0, err
			}
			_ = res.Body.Close()

			return res.StatusCode, nil
		}, 30*time.Second, 250*time.Millisecond).Should(Equal(http.StatusNotFound))
	})
})
