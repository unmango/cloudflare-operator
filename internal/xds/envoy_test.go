package xds_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
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
		snapshot := snapshotOf(testGateway(httpListener("http", listener, "")), nil)
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
		snapshot := snapshotOf(testGateway(
			httpListener("http", listener, ""),
			httpListener("second", second, "app.example.com"),
		), nil)
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

	It("should route to a backend and answer 500 for one that does not resolve", func() {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Seen", r.Header.Get("X-Set"))
			w.Header().Set("X-Path", r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}))
		DeferCleanup(backend.Close)
		addr := backend.Listener.Addr().(*net.TCPAddr)

		snapshot := snapshotOf(testGateway(httpListener("http", listener, "")), testRefs("127.0.0.1", int32(addr.Port)),
			httpRoute("r", time.Now(), withRule(gatewayv1.HTTPRouteRule{
				Matches: []gatewayv1.HTTPRouteMatch{prefix("/app")},
				Filters: []gatewayv1.HTTPRouteFilter{{
					Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier,
					RequestHeaderModifier: &gatewayv1.HTTPHeaderFilter{
						Set: []gatewayv1.HTTPHeader{{Name: "X-Set", Value: "yes"}},
					},
				}, {
					Type: gatewayv1.HTTPRouteFilterURLRewrite,
					URLRewrite: &gatewayv1.HTTPURLRewriteFilter{Path: &gatewayv1.HTTPPathModifier{
						Type:               gatewayv1.PrefixMatchHTTPPathModifier,
						ReplacePrefixMatch: new("/"),
					}},
				}},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			}), withRule(gatewayv1.HTTPRouteRule{
				Matches:     []gatewayv1.HTTPRouteMatch{prefix("/missing")},
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("missing", 1)},
			})),
		)
		Expect(cache.SetSnapshot(context.Background(), node, snapshot)).To(Succeed())

		Eventually(func(g Gomega) {
			res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/app/x", listener))
			g.Expect(err).NotTo(HaveOccurred())
			_ = res.Body.Close()
			g.Expect(res.StatusCode).To(Equal(http.StatusTeapot))
			g.Expect(res.Header.Get("X-Seen")).To(Equal("yes"))
			g.Expect(res.Header.Get("X-Path")).To(Equal("/x"))
		}, 30*time.Second, 250*time.Millisecond).Should(Succeed())

		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/missing", listener))
		Expect(err).NotTo(HaveOccurred())
		_ = res.Body.Close()
		Expect(res.StatusCode).To(Equal(http.StatusInternalServerError))

		res, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/appendix", listener))
		Expect(err).NotTo(HaveOccurred())
		_ = res.Body.Close()
		Expect(res.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("should terminate TLS on an HTTPS listener", func() {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
		DeferCleanup(backend.Close)
		addr := backend.Listener.Addr().(*net.TCPAddr)

		certPEM, keyPEM := selfSigned("secure.example.com")
		refs := testRefs("127.0.0.1", int32(addr.Port))
		withCert(refs, certPEM, keyPEM)

		port := gatewayv1.PortNumber(freePort())
		https := gatewayv1.Listener{
			Name: "https", Port: port, Protocol: gatewayv1.HTTPSProtocolType,
			TLS: &gatewayv1.ListenerTLSConfig{CertificateRefs: []gatewayv1.SecretObjectReference{{Name: certName}}},
		}
		model := gateway.Build(testGateway(httpListener("http", listener, ""), https), []gateway.Route{
			httpRoute("r", time.Now(), withRule(gatewayv1.HTTPRouteRule{
				BackendRefs: []gatewayv1.HTTPBackendRef{backendRef("backend", 1)},
			})),
		}, refs)
		Expect(model.Listeners[1].Valid).To(BeTrue())
		snapshot, err := xds.Snapshot(model, refs)
		Expect(err).NotTo(HaveOccurred())
		Expect(cache.SetSnapshot(context.Background(), node, snapshot)).To(Succeed())

		pool := x509.NewCertPool()
		Expect(pool.AppendCertsFromPEM(certPEM)).To(BeTrue())
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			ServerName: "secure.example.com",
		}}}

		Eventually(func() (int, error) {
			res, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
			if err != nil {
				return 0, err
			}
			_ = res.Body.Close()

			return res.StatusCode, nil
		}, 30*time.Second, 250*time.Millisecond).Should(Equal(http.StatusTeapot))
	})

	It("should forward TCP connections to a TCPRoute's backend", func() {
		echo := tcpEcho("tcp:")
		port := gatewayv1.PortNumber(freePort())
		snapshot := snapshotOf(testGateway(httpListener("http", listener, ""), l4Listener("tcp", port, gatewayv1.TCPProtocolType)),
			testRefs("127.0.0.1", int32(echo)),
			tcpRoute("r", time.Now(), l4Backend("backend", 1)),
		)
		Expect(cache.SetSnapshot(context.Background(), node, snapshot)).To(Succeed())

		Eventually(func() (string, error) {
			return roundTrip(func() (net.Conn, error) { return net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)) })
		}, 30*time.Second, 250*time.Millisecond).Should(Equal("tcp:ping"))
	})

	It("should pass TLS through by server name, and terminate it where the listener says", func() {
		certPEM, keyPEM := selfSigned("pass.example.com")
		pair, err := tls.X509KeyPair(certPEM, keyPEM)
		Expect(err).NotTo(HaveOccurred())
		backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
		backend.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
		backend.StartTLS()
		DeferCleanup(backend.Close)
		passPort := backend.Listener.Addr().(*net.TCPAddr).Port

		termPEM, termKey := selfSigned("term.example.com")
		echo := tcpEcho("term:")
		refs := withSecondBackend(testRefs("127.0.0.1", int32(passPort)), "127.0.0.1", int32(echo))
		withCert(refs, termPEM, termKey)

		port := gatewayv1.PortNumber(freePort())
		snapshot := snapshotOf(testGateway(
			httpListener("http", listener, ""),
			tlsListener("pass", port, "pass.example.com", gatewayv1.TLSModePassthrough),
			tlsListener("term", port, "term.example.com", gatewayv1.TLSModeTerminate),
		), refs,
			tlsRoute("pass", time.Now(), []string{"pass.example.com"}, l4Backend("backend", 1)),
			tlsRoute("term", time.Now(), []string{"term.example.com"}, l4Backend("backend2", 1)),
		)
		Expect(cache.SetSnapshot(context.Background(), node, snapshot)).To(Succeed())

		pool := x509.NewCertPool()
		Expect(pool.AppendCertsFromPEM(certPEM)).To(BeTrue())
		Expect(pool.AppendCertsFromPEM(termPEM)).To(BeTrue())
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			ServerName: "pass.example.com",
		}}}

		// The backend's own certificate reaches the client, so the connection
		// was passed through.
		Eventually(func() (int, error) {
			res, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
			if err != nil {
				return 0, err
			}
			_ = res.Body.Close()

			return res.StatusCode, nil
		}, 30*time.Second, 250*time.Millisecond).Should(Equal(http.StatusTeapot))

		Expect(roundTrip(func() (net.Conn, error) {
			return tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{RootCAs: pool, ServerName: "term.example.com"})
		})).To(Equal("term:ping"))

		_, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{RootCAs: pool, ServerName: "other.example.com"})
		Expect(err).To(HaveOccurred())
	})

	It("should split UDP sessions across a UDPRoute's backends by weight", func() {
		one, two := udpEcho("one:"), udpEcho("two:")
		port := gatewayv1.PortNumber(freePort())
		snapshot := snapshotOf(testGateway(httpListener("http", listener, ""), l4Listener("udp", port, gatewayv1.UDPProtocolType)),
			withSecondBackend(testRefs("127.0.0.1", int32(one)), "127.0.0.1", int32(two)),
			udpRoute("r", time.Now(), l4Backend("backend", 1), l4Backend("backend2", 1)),
		)
		Expect(cache.SetSnapshot(context.Background(), node, snapshot)).To(Succeed())

		// Each socket is a session of its own, so enough of them reach both.
		seen := map[string]bool{}
		Eventually(func() map[string]bool {
			reply, err := roundTrip(func() (net.Conn, error) { return net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port)) })
			if err == nil {
				seen[reply] = true
			}

			return seen
		}, 30*time.Second, 50*time.Millisecond).Should(And(HaveKey("one:ping"), HaveKey("two:ping")))
	})
})

// roundTrip writes "ping" on a new connection and returns what comes back.
func roundTrip(dial func() (net.Conn, error)) (string, error) {
	conn, err := dial()
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		return "", err
	}

	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}

	return string(buf[:n]), nil
}

// tcpEcho serves one reply per connection: prefix, then what it read.
func tcpEcho(prefix string) int {
	GinkgoHelper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(lis.Close)

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				buf := make([]byte, 64)
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				_, _ = conn.Write(append([]byte(prefix), buf[:n]...))
			}()
		}
	}()

	return lis.Addr().(*net.TCPAddr).Port
}

// udpEcho answers each datagram with prefix, then the datagram.
func udpEcho(prefix string) int {
	GinkgoHelper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(conn.Close)

	go func() {
		buf := make([]byte, 64)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteTo(append([]byte(prefix), buf[:n]...), addr)
		}
	}()

	return conn.LocalAddr().(*net.UDPAddr).Port
}

func selfSigned(host string) ([]byte, []byte) {
	GinkgoHelper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())

	keyDER, err := x509.MarshalECPrivateKey(key)
	Expect(err).NotTo(HaveOccurred())

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
