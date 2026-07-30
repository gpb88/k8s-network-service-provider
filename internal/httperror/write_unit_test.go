package httperror_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
	"github.com/dcm-project/k8s-network-service-provider/internal/httperror"
	"github.com/dcm-project/k8s-network-service-provider/internal/util"
)

func TestHTTPError(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTPError Suite")
}

var _ = Describe("WriteResponse", func() {
	It("writes a complete RFC 7807 response with all fields", func() {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		w := httptest.NewRecorder()
		instance := "/some/path"

		httperror.WriteResponse(w, logger, http.StatusBadRequest, v1alpha1.INVALIDARGUMENT, "Bad Request", "missing required field", &instance)

		Expect(w.Code).To(Equal(http.StatusBadRequest))
		Expect(w.Header().Get("Content-Type")).To(Equal("application/problem+json"))

		var body map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(HaveKeyWithValue("type", "INVALID_ARGUMENT"))
		Expect(body).To(HaveKeyWithValue("title", "Bad Request"))
		Expect(body["status"]).To(BeNumerically("==", 400))
		Expect(body).To(HaveKeyWithValue("detail", "missing required field"))
		Expect(body).To(HaveKeyWithValue("instance", "/some/path"))
	})

	It("writes a 500 INTERNAL response", func() {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		w := httptest.NewRecorder()

		httperror.WriteResponse(w, logger, http.StatusInternalServerError, v1alpha1.INTERNAL, httperror.InternalTitle, httperror.InternalDetail, util.Ptr("/api/v1alpha1/networks"))

		Expect(w.Code).To(Equal(http.StatusInternalServerError))
		Expect(w.Header().Get("Content-Type")).To(Equal("application/problem+json"))

		var body map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(HaveKeyWithValue("type", "INTERNAL"))
		Expect(body).To(HaveKeyWithValue("title", "Internal Server Error"))
		Expect(body["status"]).To(BeNumerically("==", 500))
		Expect(body).To(HaveKeyWithValue("detail", "an unexpected error occurred"))
	})

	It("handles nil instance", func() {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		w := httptest.NewRecorder()

		httperror.WriteResponse(w, logger, http.StatusBadRequest, v1alpha1.INVALIDARGUMENT, "Bad Request", "some detail", nil)

		var body map[string]any
		Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
		Expect(body).NotTo(HaveKey("instance"))
	})
})
