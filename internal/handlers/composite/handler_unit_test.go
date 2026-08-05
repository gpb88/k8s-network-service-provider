package composite_test

import (
	"context"
	"io"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/composite"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/health"
	"github.com/dcm-project/k8s-network-service-provider/internal/store"
)

type mockHealthChecker struct{}

func (m *mockHealthChecker) CheckHealth(_ context.Context) error { return nil }

var _ store.HealthChecker = (*mockHealthChecker)(nil)

var _ oapigen.StrictServerInterface = (*composite.Handler)(nil)

var _ = Describe("Network API Handlers", func() {
	var h *composite.Handler

	BeforeEach(func() {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		hh := health.NewHandler(&mockHealthChecker{}, logger, time.Now(), "0.0.1-test")
		h = composite.NewHandler(hh)
	})

	Describe("ListNetworks", func() {
		It("returns 500 INTERNAL with not-implemented detail", func() {
			resp, err := h.ListNetworks(context.Background(), oapigen.ListNetworksRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			typed, ok := resp.(oapigen.ListNetworks500ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "expected ListNetworks500ApplicationProblemPlusJSONResponse")
			Expect(typed.Type).To(Equal(v1alpha1.INTERNAL))
			Expect(typed.Title).To(Equal("Internal Server Error"))
			Expect(typed.Detail).To(HaveValue(Equal("network API not implemented")))
		})
	})

	Describe("CreateNetwork", func() {
		It("returns 500 INTERNAL with not-implemented detail", func() {
			resp, err := h.CreateNetwork(context.Background(), oapigen.CreateNetworkRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			typed, ok := resp.(oapigen.CreateNetwork500ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "expected CreateNetwork500ApplicationProblemPlusJSONResponse")
			Expect(typed.Type).To(Equal(v1alpha1.INTERNAL))
			Expect(typed.Title).To(Equal("Internal Server Error"))
			Expect(typed.Detail).To(HaveValue(Equal("network API not implemented")))
		})
	})

	Describe("GetNetwork", func() {
		It("returns 500 INTERNAL with not-implemented detail", func() {
			resp, err := h.GetNetwork(context.Background(), oapigen.GetNetworkRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			typed, ok := resp.(oapigen.GetNetwork500ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "expected GetNetwork500ApplicationProblemPlusJSONResponse")
			Expect(typed.Type).To(Equal(v1alpha1.INTERNAL))
			Expect(typed.Title).To(Equal("Internal Server Error"))
			Expect(typed.Detail).To(HaveValue(Equal("network API not implemented")))
		})
	})

	Describe("DeleteNetwork", func() {
		It("returns 500 INTERNAL with not-implemented detail", func() {
			resp, err := h.DeleteNetwork(context.Background(), oapigen.DeleteNetworkRequestObject{})
			Expect(err).NotTo(HaveOccurred())

			typed, ok := resp.(oapigen.DeleteNetwork500ApplicationProblemPlusJSONResponse)
			Expect(ok).To(BeTrue(), "expected DeleteNetwork500ApplicationProblemPlusJSONResponse")
			Expect(typed.Type).To(Equal(v1alpha1.INTERNAL))
			Expect(typed.Title).To(Equal("Internal Server Error"))
			Expect(typed.Detail).To(HaveValue(Equal("network API not implemented")))
		})
	})
})
