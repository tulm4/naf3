// Package common provides HTTP middleware for the NSSAAF SBI API layer.
// Spec: TS 29.500 §5, RFC 7230
package common

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	runtimedebug "runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	dbg "github.com/operator/nssAAF/internal/debug"
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
					"stack", string(runtimedebug.Stack()),
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

// DebugMiddleware emits one http.request event before the handler runs and
// one http.request.exit event after it returns. Both share the same trace_id
// (since they run in the same request scope). Status and duration are captured
// via a wrapped ResponseWriter. Safe to call with d == nil (no-op).
// Spec: docs/superpowers/specs/2026-07-13-nssAAF-per-ue-debug-tracing-verification-spec.md §3
func DebugMiddleware(d *dbg.Debug) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if d == nil || !d.Enabled() {
				next.ServeHTTP(w, r)
				return
			}
			// Best-effort GPSI/SUPI extraction from request body so events
			// land in the per-UE stream rather than _no_sub. Failure to
			// parse is non-fatal — we still emit the event without a key.
			var gpsi, supi string
			if r.Body != nil && r.ContentLength != 0 {
				body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if err == nil {
					gpsi, supi = extractSubscriber(body)
					r.Body = io.NopCloser(strings.NewReader(string(body)))
				}
			}
			start := time.Now()
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Entry event: emit BEFORE the handler runs. Same trace_id as
			// the exit event because both run within the request span.
			d.Emit(r.Context(), dbg.Event{
				Op:   "http.request",
				Kind: dbg.KindHTTP,
				GPSI: gpsi,
				SUPI: supi,
				Detail: map[string]any{
					"method":    r.Method,
					"path":      stripAPIversion(r.URL.Path),
					"client_ip": clientIP(r),
				},
			})

			next.ServeHTTP(wrapped, r)

			// Exit event: emit AFTER the handler returns, regardless of
			// outcome (deferred in case the handler panics — RecoveryMiddleware
			// catches the panic upstream but the deferred emit still fires).
			d.Emit(r.Context(), dbg.Event{
				Op:   "http.request.exit",
				Kind: dbg.KindHTTP,
				GPSI: gpsi,
				SUPI: supi,
				Detail: map[string]any{
					"method":      r.Method,
					"path":        stripAPIversion(r.URL.Path),
					"status":      wrapped.statusCode,
					"duration_ms": time.Since(start).Milliseconds(),
				},
				Status: statusLabel(wrapped.statusCode),
			})
		})
	}
}

// extractSubscriber peeks at a JSON request body for known UE identity keys.
// Returns (gpsi, supi). It is best-effort: any parse error yields empty strings.
func extractSubscriber(body []byte) (string, string) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", ""
	}
	if v, ok := m["gpsi"].(string); ok {
		return v, ""
	}
	if v, ok := m["supi"].(string); ok {
		return "", v
	}
	return "", ""
}

// clientIP returns the best-effort client IP: X-Forwarded-For first, then
// RemoteAddr. Used only for debug telemetry — never for auth.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
