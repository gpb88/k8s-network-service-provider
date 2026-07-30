package registration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	dcmv1alpha1 "github.com/dcm-project/service-provider-manager/api/v1alpha1/provider"

	"github.com/dcm-project/k8s-network-service-provider/internal/config"
	"github.com/dcm-project/k8s-network-service-provider/internal/registration"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

var _ = Describe("Registration Integration", func() {
	var (
		mockServer *httptest.Server
		cfg        *config.Config
		logBuf     *syncBuffer
		logger     *slog.Logger
	)

	BeforeEach(func() {
		logBuf = &syncBuffer{}
		logger = slog.New(slog.NewJSONHandler(logBuf, nil))
	})

	AfterEach(func() {
		if mockServer != nil {
			mockServer.Close()
		}
	})

	It("sends POST to /providers on startup (TC-I010 / AC-REG-010)", func() {
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestReceived.Store(true)
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestReceived.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"expected POST to /providers but no request was received")
	})

	It("sends payload with network fields including metadata (AC-REG-020)", func() {
		var receivedPayload dcmv1alpha1.Provider
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				defer func() { _ = r.Body.Close() }()
				body, err := io.ReadAll(r.Body)
				if err == nil {
					_ = json.Unmarshal(body, &receivedPayload)
					requestReceived.Store(true)
				}
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
				Region:      "us-east-1",
				Zone:        "us-east-1a",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestReceived.Load).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"expected registration request but none was received")

		Expect(receivedPayload.Name).To(Equal("k8s-network-sp"))
		Expect(receivedPayload.ServiceType).To(Equal("network"))
		Expect(receivedPayload.DisplayName).To(HaveValue(Equal("K8s Network SP")))
		Expect(receivedPayload.Endpoint).To(Equal("https://sp.example.com/api/v1alpha1/networks"))
		Expect(receivedPayload.Operations).To(HaveValue(ConsistOf("CREATE", "READ", "DELETE")))
		Expect(receivedPayload.SchemaVersion).To(Equal("v1alpha1"))
		Expect(receivedPayload.Metadata).NotTo(BeNil())
		Expect(receivedPayload.Metadata.RegionCode).To(HaveValue(Equal("us-east-1")))
		Expect(receivedPayload.Metadata.Zone).To(HaveValue(Equal("us-east-1a")))
	})

	It("Start() returns within 1s; registration completes in background (AC-REG-030)", func() {
		var requestReceived atomic.Bool

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(5 * time.Second)
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestReceived.Store(true)
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		startTime := time.Now()
		registrar.Start(ctx)
		elapsed := time.Since(startTime)
		Expect(elapsed).To(BeNumerically("<", 1*time.Second),
			"Start() must return in under 1 second")

		Eventually(requestReceived.Load).WithTimeout(10*time.Second).WithPolling(200*time.Millisecond).Should(BeTrue(),
			"expected registration to complete in background")
	})

	It("retries with increasing intervals and succeeds on 4th attempt (AC-REG-040)", func() {
		var requestCount atomic.Int32
		var requestTimes []time.Time
		var mu sync.Mutex

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				count := requestCount.Add(1)
				mu.Lock()
				requestTimes = append(requestTimes, time.Now())
				mu.Unlock()

				if count < 4 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusOK)
			}
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(200*time.Millisecond),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(requestCount.Load).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).Should(BeNumerically(">=", int32(4)),
			"expected at least 4 registration attempts")

		mu.Lock()
		defer mu.Unlock()
		Expect(requestTimes).To(HaveLen(4))
		for i := 2; i < len(requestTimes); i++ {
			prev := requestTimes[i-1].Sub(requestTimes[i-2])
			curr := requestTimes[i].Sub(requestTimes[i-1])
			Expect(curr).To(BeNumerically(">=", prev),
				"interval between attempts should increase (attempt %d)", i+1)
		}
	})

	It("logs warnings and keeps registrar retrying on 5xx without exiting (AC-REG-050)", func() {
		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(50*time.Millisecond),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(func() string {
			return logBuf.String()
		}).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(
			And(
				ContainSubstring("registration"),
				ContainSubstring("\"level\":\"WARN\""),
			),
			"expected WARN-level log entries about registration failures")

		Expect(registrar.Done()).NotTo(BeClosed(),
			"registrar should keep retrying on 5xx and not exit")
	})

	It("stops retrying on 4xx client error (AC-REG-045)", func() {
		var requestCount atomic.Int32

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestCount.Add(1)
			}
			w.WriteHeader(http.StatusBadRequest)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger,
			registration.SetInitialBackoff(10*time.Millisecond),
			registration.SetMaxBackoff(50*time.Millisecond),
		)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(registrar.Done()).WithTimeout(3*time.Second).Should(BeClosed(),
			"Done() channel should close after non-retryable 4xx error")

		Expect(requestCount.Load()).To(Equal(int32(1)),
			"expected exactly 1 registration attempt, no retries for 4xx")

		Expect(logBuf.String()).To(ContainSubstring(`"level":"ERROR"`))
		Expect(logBuf.String()).To(ContainSubstring("non-retryable"))
	})

	It("multiple Start() calls launch only one goroutine (AC-REG-070)", func() {
		var requestCount atomic.Int32

		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				requestCount.Add(1)
			}
			w.WriteHeader(http.StatusOK)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)
		registrar.Start(ctx)
		registrar.Start(ctx)

		Eventually(requestCount.Load).WithTimeout(3*time.Second).WithPolling(50*time.Millisecond).Should(BeNumerically(">=", int32(1)),
			"expected at least one registration attempt")

		time.Sleep(200 * time.Millisecond)

		Expect(requestCount.Load()).To(Equal(int32(1)),
			"expected exactly 1 registration attempt from 3 Start() calls")
	})

	It("Done() channel closes after successful registration", func() {
		mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/providers" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))

		cfg = &config.Config{
			Provider: config.ProviderConfig{
				Name:        "k8s-network-sp",
				DisplayName: "K8s Network SP",
				Endpoint:    "https://sp.example.com",
			},
			DCM: config.DCMConfig{
				RegistrationURL: mockServer.URL,
			},
		}

		registrar, err := registration.NewRegistrar(cfg, logger)
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		registrar.Start(ctx)

		Eventually(registrar.Done()).WithTimeout(3*time.Second).Should(BeClosed(),
			"Done() channel should close after successful registration")
	})
})
