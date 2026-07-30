package health_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/health"
	"github.com/dcm-project/k8s-network-service-provider/internal/store"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mockHealthChecker struct {
	CheckHealthFunc func(ctx context.Context) error
}

func (m *mockHealthChecker) CheckHealth(ctx context.Context) error {
	if m.CheckHealthFunc == nil {
		return nil
	}
	return m.CheckHealthFunc(ctx)
}

var _ store.HealthChecker = (*mockHealthChecker)(nil)

var _ = Describe("Health Handler", func() {
	Describe("GetHealth", func() {
		It("returns healthy when the backing cluster is reachable (TC-U080)", func() {
			checker := &mockHealthChecker{CheckHealthFunc: func(_ context.Context) error { return nil }}
			h := health.NewHandler(checker, slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Now(), "2.3.4")

			resp, err := h.GetHealth(context.Background(), oapigen.GetHealthRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			okResp, ok := resp.(oapigen.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(okResp.Status).To(Equal("healthy"))
			Expect(okResp.Type).NotTo(BeNil())
			Expect(*okResp.Type).To(Equal("k8s-network-service-provider.dcm.io/health"))
			Expect(okResp.Path).NotTo(BeNil())
			Expect(*okResp.Path).To(Equal("health"))
			Expect(okResp.Version).NotTo(BeNil())
			Expect(*okResp.Version).To(Equal("2.3.4"))
			Expect(okResp.Uptime).NotTo(BeNil())
			Expect(*okResp.Uptime).To(BeNumerically(">=", 0))
		})

		It("returns unhealthy when the health check fails (TC-U081)", func() {
			checker := &mockHealthChecker{CheckHealthFunc: func(_ context.Context) error { return errors.New("connection refused") }}
			h := health.NewHandler(checker, slog.New(slog.NewJSONHandler(io.Discard, nil)), time.Now(), "1.0.0")

			resp, err := h.GetHealth(context.Background(), oapigen.GetHealthRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			okResp, ok := resp.(oapigen.GetHealth200JSONResponse)
			Expect(ok).To(BeTrue())
			Expect(okResp.Status).To(Equal("unhealthy"))
		})
	})
})
