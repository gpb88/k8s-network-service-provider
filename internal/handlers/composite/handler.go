// Package composite wires health and network handlers.
package composite

import (
	"context"

	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/health"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/network"
)

// Handler implements StrictServerInterface by delegating to specialized handlers.
type Handler struct {
	health  *health.Handler
	network *network.Handler
}

// NewHandler creates a composite handler.
func NewHandler(healthHandler *health.Handler, networkHandler *network.Handler) *Handler {
	return &Handler{
		health:  healthHandler,
		network: networkHandler,
	}
}

var _ oapigen.StrictServerInterface = (*Handler)(nil)

func (h *Handler) GetHealth(ctx context.Context, req oapigen.GetHealthRequestObject) (oapigen.GetHealthResponseObject, error) {
	return h.health.GetHealth(ctx, req)
}

func (h *Handler) ListNetworks(ctx context.Context, req oapigen.ListNetworksRequestObject) (oapigen.ListNetworksResponseObject, error) {
	return h.network.ListNetworks(ctx, req)
}

func (h *Handler) CreateNetwork(ctx context.Context, req oapigen.CreateNetworkRequestObject) (oapigen.CreateNetworkResponseObject, error) {
	return h.network.CreateNetwork(ctx, req)
}

func (h *Handler) GetNetwork(ctx context.Context, req oapigen.GetNetworkRequestObject) (oapigen.GetNetworkResponseObject, error) {
	return h.network.GetNetwork(ctx, req)
}

func (h *Handler) DeleteNetwork(ctx context.Context, req oapigen.DeleteNetworkRequestObject) (oapigen.DeleteNetworkResponseObject, error) {
	return h.network.DeleteNetwork(ctx, req)
}
