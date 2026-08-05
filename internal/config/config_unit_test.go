package config_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/k8s-network-service-provider/internal/config"
)

var _ = Describe("Configuration", func() {
	clearEnv := func() {
		_ = os.Unsetenv("SP_SERVER_ADDRESS")
		_ = os.Unsetenv("SP_SERVER_SHUTDOWN_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_READ_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_WRITE_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_IDLE_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_REQUEST_TIMEOUT")
		_ = os.Unsetenv("SP_NAME")
		_ = os.Unsetenv("SP_DISPLAY_NAME")
		_ = os.Unsetenv("SP_ENDPOINT")
		_ = os.Unsetenv("SP_REGION")
		_ = os.Unsetenv("SP_ZONE")
		_ = os.Unsetenv("DCM_REGISTRATION_URL")
		_ = os.Unsetenv("SP_NATS_URL")
		_ = os.Unsetenv("SP_K8S_NAMESPACE")
		_ = os.Unsetenv("SP_K8S_KUBECONFIG")
	}

	BeforeEach(func() {
		clearEnv()
	})

	AfterEach(func() {
		clearEnv()
	})

	setRequiredEnv := func() {
		_ = os.Setenv("SP_NAME", "test-sp")
		_ = os.Setenv("SP_ENDPOINT", "https://test.example.com")
		_ = os.Setenv("DCM_REGISTRATION_URL", "https://dcm.example.com")
	}

	It("loads configuration from environment variables", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_SERVER_ADDRESS", ":9090")

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Server.Address).To(Equal(":9090"))
	})

	It("applies default values when no config is specified", func() {
		setRequiredEnv()

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Server.Address).To(Equal(":8080"))
		Expect(cfg.Kubernetes.Namespace).To(Equal("default"))
		Expect(cfg.Server.ShutdownTimeout).To(Equal(15 * time.Second))
	})

	It("returns error when required fields are missing", func() {
		cfg, err := config.Load()
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
	})
})
