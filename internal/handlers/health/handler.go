// Package health implements the health API handler.
package health

import (
	"context"
	"log/slog"
	"time"

	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/store"
	"github.com/dcm-project/k8s-network-service-provider/internal/util"
)

// Handler serves GET /api/v1alpha1/networks/health.
type Handler struct {
	checker   store.HealthChecker
	logger    *slog.Logger
	startTime time.Time
	version   string
}

// NewHandler creates a Handler backed by the given health checker.
func NewHandler(checker store.HealthChecker, logger *slog.Logger, startTime time.Time, version string) *Handler {
	return &Handler{
		checker:   checker,
		logger:    logger,
		startTime: startTime,
		version:   version,
	}
}

func (h *Handler) GetHealth(ctx context.Context, _ oapigen.GetHealthRequestObject) (oapigen.GetHealthResponseObject, error) {
	status := "healthy"
	if h.checker != nil {
		if err := h.checker.CheckHealth(ctx); err != nil {
			status = "unhealthy"
		}
	}

	uptime := max(0, int(time.Since(h.startTime).Seconds()))
	return oapigen.GetHealth200JSONResponse{
		Status:  status,
		Type:    util.Ptr("k8s-network-service-provider.dcm.io/health"),
		Path:    util.Ptr("health"),
		Uptime:  &uptime,
		Version: util.Ptr(h.version),
	}, nil
}
