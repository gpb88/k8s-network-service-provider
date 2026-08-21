package network

import (
	"errors"
	"net/http"

	v1alpha1 "github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/httperror"
	"github.com/dcm-project/k8s-network-service-provider/internal/store"
	"github.com/dcm-project/k8s-network-service-provider/internal/util"
)

func newCreateError400(detail, requestPath string) oapigen.CreateNetworkResponseObject {
	return oapigen.CreateNetwork400ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INVALIDARGUMENT,
		Title:    httperror.InvalidArgumentTitle,
		Status:   util.Ptr(int32(http.StatusBadRequest)),
		Detail:   &detail,
		Instance: &requestPath,
	}
}

func (h *Handler) mapCreateError(err error, requestPath string) oapigen.CreateNetworkResponseObject {
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		detail := err.Error()
		return oapigen.CreateNetwork409ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.ALREADYEXISTS,
			Title:    "Already exists",
			Status:   util.Ptr(int32(http.StatusConflict)),
			Detail:   &detail,
			Instance: &requestPath,
		}
	}

	var invalid *store.InvalidArgumentError
	if errors.As(err, &invalid) {
		return newCreateError400(err.Error(), requestPath)
	}

	h.logger.Error("unexpected error in CreateNetwork", "error", err)
	return oapigen.CreateNetwork500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}

func (h *Handler) mapGetError(err error, requestPath string) oapigen.GetNetworkResponseObject {
	var notFound *store.NotFoundError
	if errors.As(err, &notFound) {
		return oapigen.GetNetwork404ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.NOTFOUND,
			Title:    httperror.NotFoundTitle,
			Status:   util.Ptr(int32(http.StatusNotFound)),
			Detail:   util.Ptr(err.Error()),
			Instance: &requestPath,
		}
	}

	h.logger.Error("unexpected error in GetNetwork", "error", err)
	return oapigen.GetNetwork500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}

func (h *Handler) mapDeleteError(err error, requestPath string) oapigen.DeleteNetworkResponseObject {
	var notFound *store.NotFoundError
	if errors.As(err, &notFound) {
		return oapigen.DeleteNetwork404ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.NOTFOUND,
			Title:    httperror.NotFoundTitle,
			Status:   util.Ptr(int32(http.StatusNotFound)),
			Detail:   util.Ptr(err.Error()),
			Instance: &requestPath,
		}
	}

	h.logger.Error("unexpected error in DeleteNetwork", "error", err)
	return oapigen.DeleteNetwork500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}

func (h *Handler) mapListError(err error, requestPath string) oapigen.ListNetworksResponseObject {
	var invalid *store.InvalidArgumentError
	if errors.As(err, &invalid) {
		return oapigen.ListNetworks400ApplicationProblemPlusJSONResponse{
			Type:     v1alpha1.INVALIDARGUMENT,
			Title:    httperror.InvalidArgumentTitle,
			Status:   util.Ptr(int32(http.StatusBadRequest)),
			Detail:   util.Ptr(err.Error()),
			Instance: &requestPath,
		}
	}

	h.logger.Error("unexpected error in ListNetworks", "error", err)
	return oapigen.ListNetworks500ApplicationProblemPlusJSONResponse{
		Type:     v1alpha1.INTERNAL,
		Title:    httperror.InternalTitle,
		Status:   util.Ptr(int32(http.StatusInternalServerError)),
		Detail:   util.Ptr(httperror.InternalDetail),
		Instance: &requestPath,
	}
}
