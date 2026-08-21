// Package network implements HTTP handlers for network CRUD operations.
package network

import (
	"context"
	"log/slog"

	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/store"
)

type Handler struct {
	store  store.NetworkRepository
	logger *slog.Logger
}

func NewHandler(repo store.NetworkRepository, logger *slog.Logger) *Handler {
	return &Handler{
		store:  repo,
		logger: logger,
	}
}

const networksBasePath = "/api/v1alpha1/networks"

func (h *Handler) CreateNetwork(ctx context.Context, req oapigen.CreateNetworkRequestObject) (oapigen.CreateNetworkResponseObject, error) {
	requestPath := networksBasePath

	if req.Body == nil {
		return newCreateError400("request body is required", requestPath), nil
	}

	spec := req.Body.Spec

	var id string
	if req.Params.Id != nil {
		id = *req.Params.Id
	} else {
		generated, err := generateNetworkID()
		if err != nil {
			h.logger.Error("failed to generate network id", "error", err)
			return h.mapCreateError(err, requestPath), nil
		}
		id = generated
	}

	if err := validateNetworkID(id); err != nil {
		return newCreateError400(err.Error(), requestPath), nil
	}
	if err := validateUserLabels(spec.Metadata.Labels); err != nil {
		return newCreateError400(err.Error(), requestPath), nil
	}

	result, err := h.store.Create(ctx, req.Body.Spec, id)
	if err != nil {
		return h.mapCreateError(err, requestPath), nil
	}

	return oapigen.CreateNetwork201JSONResponse(*result), nil
}

func (h *Handler) GetNetwork(ctx context.Context, req oapigen.GetNetworkRequestObject) (oapigen.GetNetworkResponseObject, error) {
	requestPath := networksBasePath + "/" + req.NetworkId
	result, err := h.store.Get(ctx, req.NetworkId)
	if err != nil {
		return h.mapGetError(err, requestPath), nil
	}

	return oapigen.GetNetwork200JSONResponse(*result), nil
}

func (h *Handler) DeleteNetwork(ctx context.Context, req oapigen.DeleteNetworkRequestObject) (oapigen.DeleteNetworkResponseObject, error) {
	requestPath := networksBasePath + "/" + req.NetworkId
	if err := h.store.Delete(ctx, req.NetworkId); err != nil {
		return h.mapDeleteError(err, requestPath), nil
	}

	return oapigen.DeleteNetwork204Response{}, nil
}

func (h *Handler) ListNetworks(ctx context.Context, req oapigen.ListNetworksRequestObject) (oapigen.ListNetworksResponseObject, error) {
	var maxPageSize int32
	if req.Params.MaxPageSize != nil {
		maxPageSize = *req.Params.MaxPageSize
	}

	var pageToken string
	if req.Params.PageToken != nil {
		pageToken = *req.Params.PageToken
	}

	result, err := h.store.List(ctx, maxPageSize, pageToken)
	if err != nil {
		return h.mapListError(err, networksBasePath), nil
	}

	return oapigen.ListNetworks200JSONResponse(*result), nil
}
