package apiserver_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/apiserver"
	"github.com/dcm-project/k8s-network-service-provider/internal/config"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/composite"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/health"
	"github.com/dcm-project/k8s-network-service-provider/internal/store"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

type mockHealthChecker struct {
	err error
}

func (m *mockHealthChecker) CheckHealth(_ context.Context) error { return m.err }

var _ store.HealthChecker = (*mockHealthChecker)(nil)

var _ = Describe("HTTP Server", func() {
	startServer := func(cfg *config.Config, logBuf *syncBuffer, signals []os.Signal, wrappers ...func(http.Handler) http.Handler) (
		addr string,
		cancel context.CancelFunc,
		errCh chan error,
	) {
		var logger *slog.Logger
		if logBuf != nil {
			logger = slog.New(slog.NewJSONHandler(logBuf, nil))
		} else {
			logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
		}

		checker := &mockHealthChecker{}
		hh := health.NewHandler(checker, logger, time.Now(), "0.0.1-test")
		apiHandler := composite.NewHandler(hh)
		strictAdapter := oapigen.NewStrictHandlerWithOptions(apiHandler, nil, oapigen.StrictHTTPServerOptions{})
		srv := apiserver.New(cfg, logger, strictAdapter)
		Expect(srv).NotTo(BeNil())

		for _, w := range wrappers {
			srv.WrapHandler(w)
		}

		ln, err := net.Listen("tcp", ":0")
		Expect(err).NotTo(HaveOccurred())
		addr = ln.Addr().String()

		var ctx context.Context
		if len(signals) > 0 {
			signal.Reset(signals...)
			ctx, cancel = signal.NotifyContext(context.Background(), signals...)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}

		errCh = make(chan error, 1)
		go func() {
			errCh <- srv.Run(ctx, ln)
		}()

		Eventually(func() error {
			resp, reqErr := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/networks/health", addr))
			if reqErr != nil {
				return reqErr
			}
			_ = resp.Body.Close()
			return nil
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		return addr, cancel, errCh
	}

	defaultConfig := func() *config.Config {
		return &config.Config{
			Server: config.ServerConfig{
				Address:         ":0",
				ShutdownTimeout: 5 * time.Second,
			},
		}
	}

	It("starts and accepts HTTP connections (TC-I001)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		resp, err := http.Get(fmt.Sprintf("http://%s/api/v1alpha1/networks/health", addr))
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	It("registers all OpenAPI-defined routes (TC-I002)", func() {
		addr, cancel, errCh := startServer(defaultConfig(), nil, nil)
		defer func() {
			cancel()
			Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())
		}()

		baseURL := fmt.Sprintf("http://%s", addr)

		type routeCheck struct {
			method string
			path   string
		}

		routes := []routeCheck{
			{"GET", "/api/v1alpha1/networks/health"},
			{"GET", "/api/v1alpha1/networks"},
			{"POST", "/api/v1alpha1/networks"},
			{"GET", "/api/v1alpha1/networks/test-id"},
			{"DELETE", "/api/v1alpha1/networks/test-id"},
		}

		for _, rc := range routes {
			req, err := http.NewRequest(rc.method, baseURL+rc.path, http.NoBody)
			Expect(err).NotTo(HaveOccurred(), "route: %s %s", rc.method, rc.path)

			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred(), "route: %s %s", rc.method, rc.path)
			_ = resp.Body.Close()

			Expect(resp.StatusCode).NotTo(Equal(http.StatusNotFound),
				"route %s %s should not return 404", rc.method, rc.path)
			Expect(resp.StatusCode).NotTo(Equal(http.StatusMethodNotAllowed),
				"route %s %s should not return 405", rc.method, rc.path)
		}
	})

	It("drains in-flight requests on SIGTERM (TC-I003)", func() {
		reqStarted := make(chan struct{})
		reqRelease := make(chan struct{})

		slowWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/slow" {
					close(reqStarted)
					<-reqRelease
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(defaultConfig(), nil, []os.Signal{syscall.SIGTERM}, slowWrapper)
		defer cancel()

		type result struct {
			resp *http.Response
			err  error
		}
		respCh := make(chan result, 1)
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/test/slow", addr))
			respCh <- result{resp, err}
		}()

		<-reqStarted

		proc, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		Expect(proc.Signal(syscall.SIGTERM)).To(Succeed())

		close(reqRelease)

		var res result
		Eventually(respCh).WithTimeout(5 * time.Second).Should(Receive(&res))
		Expect(res.err).NotTo(HaveOccurred())
		defer func() { _ = res.resp.Body.Close() }()
		Expect(res.resp.StatusCode).To(Equal(http.StatusOK))

		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive(BeNil()))

		_, err = http.Get(fmt.Sprintf("http://%s/api/v1alpha1/networks/health", addr))
		Expect(err).To(HaveOccurred())
	})

	It("logs startup with listen address and shutdown event (TC-I004)", func() {
		var logBuf syncBuffer
		addr, cancel, errCh := startServer(defaultConfig(), &logBuf, nil)

		Expect(addr).NotTo(BeEmpty())
		Expect(logBuf.String()).To(ContainSubstring(addr))

		cancel()
		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive())

		logOutput := logBuf.String()
		Expect(logOutput).To(SatisfyAny(
			ContainSubstring("shutdown"),
			ContainSubstring("shutting down"),
			ContainSubstring("stopping"),
		))
	})

	It("drains in-flight requests on SIGINT (TC-I005)", func() {
		reqStarted := make(chan struct{})
		reqRelease := make(chan struct{})

		slowWrapper := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test/slow" {
					close(reqStarted)
					<-reqRelease
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		}

		addr, cancel, errCh := startServer(defaultConfig(), nil, []os.Signal{syscall.SIGINT}, slowWrapper)
		defer cancel()

		type result struct {
			resp *http.Response
			err  error
		}
		respCh := make(chan result, 1)
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://%s/test/slow", addr))
			respCh <- result{resp, err}
		}()

		<-reqStarted

		proc, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		Expect(proc.Signal(syscall.SIGINT)).To(Succeed())

		close(reqRelease)

		var res result
		Eventually(respCh).WithTimeout(5 * time.Second).Should(Receive(&res))
		Expect(res.err).NotTo(HaveOccurred())
		defer func() { _ = res.resp.Body.Close() }()
		Expect(res.resp.StatusCode).To(Equal(http.StatusOK))

		Eventually(errCh).WithTimeout(10 * time.Second).Should(Receive(BeNil()))

		_, err = http.Get(fmt.Sprintf("http://%s/api/v1alpha1/networks/health", addr))
		Expect(err).To(HaveOccurred())
	})
})
