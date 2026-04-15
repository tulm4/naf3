// Package common provides HTTP middleware for the NSSAAF SBI API layer.
// Spec: TS 29.500 §5, RFC 7230
package common

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
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
