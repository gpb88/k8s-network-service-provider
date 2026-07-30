// Package apiserver provides the HTTP server and middleware for the network API.
package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	v1alpha1 "github.com/dcm-project/k8s-network-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/k8s-network-service-provider/internal/api/server"
	"github.com/dcm-project/k8s-network-service-provider/internal/config"
	"github.com/dcm-project/k8s-network-service-provider/internal/httperror"
	"github.com/dcm-project/k8s-network-service-provider/internal/util"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
	"github.com/go-chi/chi/v5"
)

// Server is the HTTP server for the network service provider API.
type Server struct {
	cfg     *config.Config
	logger  *slog.Logger
	srv     *http.Server
	onReady func(context.Context)
}

func newBadRequestHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		detail := scrubValidationError(err)
		httperror.WriteResponse(w, logger, http.StatusBadRequest, v1alpha1.INVALIDARGUMENT, "Bad Request", detail, requestInstance(r))
	}
}

// NewRequestErrorHandler returns an error handler for the strict adapter's
// RequestErrorHandlerFunc that writes an RFC 7807 INVALID_ARGUMENT response.
func NewRequestErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return newBadRequestHandler(logger)
}

// NewResponseErrorHandler returns an error handler for the strict adapter's
// ResponseErrorHandlerFunc that writes an RFC 7807 INTERNAL response without
// exposing implementation details.
func NewResponseErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("strict handler response error", "error", err)
		httperror.WriteResponse(w, logger, http.StatusInternalServerError, v1alpha1.INTERNAL, httperror.InternalTitle, httperror.InternalDetail, requestInstance(r))
	}
}

func requestInstance(r *http.Request) *string {
	if r == nil {
		return nil
	}
	return util.Ptr(r.URL.RequestURI())
}

const readinessProbeTimeout = 5 * time.Second

const readinessProbeInterval = 50 * time.Millisecond

// WithOnReady registers a callback invoked once the server is confirmed to be
// serving HTTP requests.
func (s *Server) WithOnReady(fn func(context.Context)) *Server {
	s.onReady = fn
	return s
}

func scrubValidationError(err error) string {
	const genericMsg = "invalid request"

	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) {
		var prefix string
		if p := reqErr.Parameter; p != nil {
			prefix = fmt.Sprintf("parameter %q in %s", p.Name, p.In)
		} else if reqErr.RequestBody != nil {
			prefix = "request body"
		}

		var schemaErr *openapi3.SchemaError
		if errors.As(reqErr.Err, &schemaErr) && schemaErr.Reason != "" {
			if prefix != "" {
				return prefix + ": " + schemaErr.Reason
			}
			return schemaErr.Reason
		}

		if reqErr.Reason != "" {
			if prefix != "" {
				return prefix + ": " + reqErr.Reason
			}
			return reqErr.Reason
		}

		return genericMsg
	}

	var paramErr *oapigen.InvalidParamFormatError
	if errors.As(err, &paramErr) {
		return fmt.Sprintf("invalid format for parameter %q", paramErr.ParamName)
	}

	var reqParamErr *oapigen.RequiredParamError
	if errors.As(err, &reqParamErr) {
		return fmt.Sprintf("missing required parameter %q", reqParamErr.ParamName)
	}

	var reqHeaderErr *oapigen.RequiredHeaderError
	if errors.As(err, &reqHeaderErr) {
		return fmt.Sprintf("missing required header %q", reqHeaderErr.ParamName)
	}

	var cookieErr *oapigen.UnescapedCookieParamError
	if errors.As(err, &cookieErr) {
		return fmt.Sprintf("invalid cookie parameter %q", cookieErr.ParamName)
	}

	var unmarshalErr *oapigen.UnmarshalingParamError
	if errors.As(err, &unmarshalErr) {
		return fmt.Sprintf("invalid value for parameter %q", unmarshalErr.ParamName)
	}

	var tooManyErr *oapigen.TooManyValuesForParamError
	if errors.As(err, &tooManyErr) {
		return fmt.Sprintf("too many values for parameter %q", tooManyErr.ParamName)
	}

	return genericMsg
}

type statusRecordingResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	statusCode  int
}

func (w *statusRecordingResponseWriter) WriteHeader(code int) {
	w.wroteHeader = true
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecordingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.statusCode = http.StatusOK
	}
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

func (w *statusRecordingResponseWriter) Flush() {
	w.wroteHeader = true
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (w *statusRecordingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func recoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusRecordingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(http.ErrAbortHandler)
					}

					logger.Error("panic recovered", "panic", rec, "stack", string(debug.Stack()))

					if sw.wroteHeader {
						logger.Warn("headers already sent, cannot write RFC 7807 response")
						return
					}

					httperror.WriteResponse(w, logger, http.StatusInternalServerError, v1alpha1.INTERNAL, httperror.InternalTitle, httperror.InternalDetail, requestInstance(r))
				}
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

func requestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if timeout <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requestLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusRecordingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.statusCode,
				"duration", time.Since(start).String(),
			)
		})
	}
}

func (s *Server) waitForReady(ctx context.Context, addr string) error {
	url := fmt.Sprintf("http://%s/api/v1alpha1/networks/health", addr)
	client := &http.Client{Timeout: 1 * time.Second}

	deadline := time.NewTimer(readinessProbeTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(readinessProbeInterval)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return fmt.Errorf("creating readiness probe request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("server readiness probe timed out after %s", readinessProbeTimeout)
		case <-ticker.C:
		}
	}
}

// New creates a new Server with the given config and logger.
func New(cfg *config.Config, logger *slog.Logger, handler oapigen.ServerInterface) *Server {
	badReq := newBadRequestHandler(logger)

	r := chi.NewRouter()
	r.Use(recoveryMiddleware(logger))
	r.Use(requestLoggingMiddleware(logger))
	r.Use(requestTimeoutMiddleware(cfg.Server.RequestTimeout))

	spec, err := v1alpha1.GetSwagger()
	if err != nil {
		logger.Warn("failed to load OpenAPI spec, request validation disabled", "error", err)
	} else {
		spec.Servers = nil
		specRouter, routerErr := legacyrouter.NewRouter(spec)
		if routerErr != nil {
			logger.Warn("failed to create OpenAPI router, request validation disabled", "error", routerErr)
		} else {
			r.Use(openAPIValidationMiddleware(specRouter, badReq))
		}
	}

	emptyIDHandler := func(w http.ResponseWriter, r *http.Request) {
		httperror.WriteResponse(w, logger, http.StatusBadRequest, v1alpha1.INVALIDARGUMENT, "Bad Request", "network_id is required and cannot be empty", requestInstance(r))
	}
	postPath, pathErr := v1alpha1.PostPath()
	if pathErr != nil {
		logger.Warn("failed to resolve POST path from OpenAPI spec, trailing-slash guards disabled", "error", pathErr)
	} else {
		r.Get(postPath+"/", emptyIDHandler)
		r.Delete(postPath+"/", emptyIDHandler)
	}

	httpHandler := oapigen.HandlerWithOptions(handler, oapigen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: badReq,
	})

	s := &Server{
		cfg:    cfg,
		logger: logger,
		srv: &http.Server{
			Handler:      httpHandler,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		},
	}
	return s
}

func openAPIValidationMiddleware(specRouter routers.Router, badReq func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, pathParams, err := specRouter.FindRoute(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			input := &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
			}

			if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
				badReq(w, r, err)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Run starts the HTTP server on the provided listener and blocks until
// the context is cancelled.
func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	s.logger.Info("server starting", "address", ln.Addr().String())

	serveCh := make(chan error, 1)
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveCh <- err
		}
		close(serveCh)
	}()

	if s.onReady != nil {
		if err := s.waitForReady(ctx, ln.Addr().String()); err != nil {
			s.logger.Error("readiness probe failed, skipping onReady callback", "error", err)
		} else {
			func() {
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("onReady callback panicked", "panic", r)
					}
				}()
				s.onReady(ctx)
			}()
		}
	}

	select {
	case <-ctx.Done():
	case err := <-serveCh:
		if err != nil {
			return fmt.Errorf("serving on %s: %w", ln.Addr(), err)
		}
	}

	s.logger.Info("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}
	return nil
}
