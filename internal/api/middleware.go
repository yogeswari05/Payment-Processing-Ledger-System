package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/metrics"
	"github.com/payflow/payflow/internal/service"
)

type contextKey string

const (
	contextKeyMerchant  contextKey = "merchant"
	contextKeyRequestID contextKey = "request_id"
)

// ResponseWriter wrapper to capture HTTP status code for logging and metrics.
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// RequestLogger middleware logs every HTTP request as structured JSON.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = uuid.New().String()
			}
			w.Header().Set("X-Request-ID", reqID)

			ctx := context.WithValue(r.Context(), contextKeyRequestID, reqID)
			srw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(srw, r.WithContext(ctx))

			duration := time.Since(start)

			logger.InfoContext(ctx, "http_request",
				"request_id", reqID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", srw.statusCode,
				"duration_ms", duration.Milliseconds(),
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		})
	}
}

// MetricsMiddleware tracks HTTP request count and latency for Prometheus.
func MetricsMiddleware(m *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			srw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(srw, r)

			duration := time.Since(start).Seconds()
			statusStr := strconv.Itoa(srw.statusCode)
			path := r.URL.Path

			m.HTTPRequestsTotal.WithLabelValues(r.Method, path, statusStr).Inc()
			m.HTTPRequestDuration.WithLabelValues(r.Method, path).Observe(duration)
		})
	}
}

// RecoveryMiddleware recovers from panics, logs stack, and returns a 500 error.
func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered in http handler", "panic", rec)
					WriteError(w, http.StatusInternalServerError, "api_error", "internal_server_error", "An internal error occurred.")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBytesMiddleware enforces request body size limits.
func MaxBytesMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware authenticates requests against the MerchantService via Bearer token or X-API-Key.
func AuthMiddleware(merchantSvc *service.MerchantService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var apiKey string
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			} else if headerKey := r.Header.Get("X-API-Key"); headerKey != "" {
				apiKey = headerKey
			}

			if apiKey == "" {
				WriteError(w, http.StatusUnauthorized, "authentication_error", "missing_api_key", "API key is required.")
				return
			}

			merchant, err := merchantSvc.Authenticate(r.Context(), apiKey)
			if err != nil {
				if errors.Is(err, domain.ErrForbidden) {
					WriteError(w, http.StatusForbidden, "authorization_error", "merchant_inactive", "Merchant account is suspended or closed.")
					return
				}
				WriteError(w, http.StatusUnauthorized, "authentication_error", "invalid_api_key", "Invalid API key provided.")
				return
			}

			ctx := context.WithValue(r.Context(), contextKeyMerchant, merchant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MerchantFromContext extracts the authenticated merchant from the request context.
func MerchantFromContext(ctx context.Context) (*domain.Merchant, error) {
	m, ok := ctx.Value(contextKeyMerchant).(*domain.Merchant)
	if !ok || m == nil {
		return nil, domain.ErrUnauthorized
	}
	return m, nil
}
