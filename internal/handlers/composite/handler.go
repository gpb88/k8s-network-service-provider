// Package composite wires health and unimplemented network handlers for incremental delivery.
package composite

import (
	"context"

	v1alpha1 "github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/handlers/health"
	"github.com/dcm-project/k8s-network-service-provider/internal/httperror"
	"github.com/dcm-project/k8s-network-service-provider/internal/util"
)

// Handler implements StrictServerInterface by delegating health to the health handler
// and returning not-implemented responses for network operations.
type Handler struct {
	health *health.Handler
}

// NewHandler creates a composite handler for health and stub network routes.
func NewHandler(healthHandler *health.Handler) *Handler {
	return &Handler{health: healthHandler}
}

var _ oapigen.StrictServerInterface = (*Handler)(nil)

func (h *Handler) GetHealth(ctx context.Context, req oapigen.GetHealthRequestObject) (oapigen.GetHealthResponseObject, error) {
	return h.health.GetHealth(ctx, req)
}

func notImplemented() v1alpha1.Error {
	return v1alpha1.Error{
		Type:   v1alpha1.INTERNAL,
		Title:  httperror.InternalTitle,
		Detail: util.Ptr("network API not implemented"),
	}
}

func (h *Handler) ListNetworks(_ context.Context, _ oapigen.ListNetworksRequestObject) (oapigen.ListNetworksResponseObject, error) {
	err := notImplemented()
	return oapigen.ListNetworks500ApplicationProblemPlusJSONResponse(err), nil
}

func (h *Handler) CreateNetwork(_ context.Context, _ oapigen.CreateNetworkRequestObject) (oapigen.CreateNetworkResponseObject, error) {
	err := notImplemented()
	return oapigen.CreateNetwork500ApplicationProblemPlusJSONResponse(err), nil
}

func (h *Handler) GetNetwork(_ context.Context, _ oapigen.GetNetworkRequestObject) (oapigen.GetNetworkResponseObject, error) {
	err := notImplemented()
	return oapigen.GetNetwork500ApplicationProblemPlusJSONResponse(err), nil
}

func (h *Handler) DeleteNetwork(_ context.Context, _ oapigen.DeleteNetworkRequestObject) (oapigen.DeleteNetworkResponseObject, error) {
	err := notImplemented()
	return oapigen.DeleteNetwork500ApplicationProblemPlusJSONResponse(err), nil
}
