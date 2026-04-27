// Package common provides HTTP middleware for the NSSAAF SBI API layer.
// Spec: TS 29.500 §5, RFC 7230
package common

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/operator/nssAAF/internal/metrics"
)

// RequestIDMiddleware injects a unique X-Request-ID into every request.
// If the client already provided one, it is preserved.
// Spec: TS 29.500 §6.1 (SBI correlation)
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(HeaderXRequestID)
		if reqID == "" {
			reqID = uuid.NewString()
		}
		ctx := WithRequestID(r.Context(), reqID)
		w.Header().Set(HeaderXRequestID, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware produces structured log entries for every request.
// Spec: TS 29.500 §5 (observability)
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := GetRequestID(r.Context())

		// Wrap ResponseWriter to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		// Log in slog structured format
		slog.Log(r.Context(), slog.LevelInfo, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", reqID,
			"client_ip", r.RemoteAddr,
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture the written status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// RecoveryMiddleware recovers from panics, logs the stack trace,
// and returns a 500 error response.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		defer func() {
			if err := recover(); err != nil {
				reqID := GetRequestID(ctx)
				slog.Error("panic recovered",
					"error", err,
					"request_id", reqID,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				problem := InternalServerProblem("An unexpected error occurred")
				w.Header().Set(HeaderContentType, MediaTypeProblemJSON)
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(problem)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware adds CORS headers for browser-based clients.
// This is not part of the 3GPP SBI spec but is useful for OAM interfaces.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only apply CORS to OAM endpoints (paths starting with /oam/)
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/oam" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+HeaderXRequestID)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// MetricsMiddleware records request count and latency using Prometheus metrics.
// Spec: REQ-14 / TS 29.500 §5 (observability)
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Seconds()
		status := wrapped.statusCode
		endpoint := stripAPIversion(r.URL.Path)
		service := inferService(r.URL.Path)
		method := r.Method

		metrics.RequestsTotal.WithLabelValues(service, endpoint, method, statusLabel(status)).Inc()
		metrics.RequestDuration.WithLabelValues(service, endpoint, method).Observe(duration)
	})
}

// statusLabel normalises a numeric HTTP status code to a string bucket (e.g. "2xx", "4xx").
func statusLabel(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "unknown"
	}
}

// stripAPIversion removes the /v1 (or /vN) prefix from a path so that
// label cardinality stays bounded regardless of which API version is used.
func stripAPIversion(path string) string {
	path = strings.TrimSuffix(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		candidate := path[:i]
		if strings.HasSuffix(candidate, "/v1") || strings.HasSuffix(candidate, "/v2") {
			return candidate
		}
	}
	return path
}

// inferService returns a stable service label from the request path.
// Returns "nssaa" for /nnssaaf-nssaa/*, "aiw" for /nnssaaf-aiw/*, "internal" for internal routes.
func inferService(path string) string {
	switch {
	case strings.HasPrefix(path, "/nnssaaf-nssaa"):
		return "nssaa"
	case strings.HasPrefix(path, "/nnssaaf-aiw"):
		return "aiw"
	case strings.HasPrefix(path, "/aaa"):
		return "internal"
	default:
		return "oam"
	}
}

// WriteProblem writes a ProblemDetails response with the correct status code
// and Content-Type header.
// Spec: RFC 7807 §3.1
func WriteProblem(w http.ResponseWriter, problem *ProblemDetails) {
	w.Header().Set(HeaderContentType, MediaTypeProblemJSON)
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(problem)
}

// WriteJSON writes a JSON response with the correct Content-Type header.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set(HeaderContentType, MediaTypeJSONVersion)
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}
