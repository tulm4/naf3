package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

type contextKey string

const claimsContextKey contextKey = "auth_claims"

// Config configures the auth middleware.
type Config struct {
	// Disabled skips all JWT validation. Use for E2E tests or trusted paths.
	Disabled bool `yaml:"disabled"`
}

// NewAuthMiddleware returns middleware that validates Bearer tokens.
// If cfg.Disabled is true (or NAF3_AUTH_DISABLED=1 env var), validation is skipped.
// Spec: D-01 — HTTP Gateway validates all inbound N58/N60 tokens; Biz Pod trusts gateway.
func NewAuthMiddleware(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Disabled || os.Getenv("NAF3_AUTH_DISABLED") == "1" {
				slog.Debug("auth: bypassed (E2E mode)")
				next.ServeHTTP(w, r)
				return
			}
			// Extract Bearer token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, "missing authorization header")
				return
			}
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeError(w, "invalid authorization scheme")
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == "" {
				writeError(w, "empty token")
				return
			}

			// Validate token
			claims, err := Validator().Validate(r.Context(), token, "")
			if err != nil {
				slog.Debug("token validation failed",
					"error", err,
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				writeError(w, "invalid token")
				return
			}

			// Inject claims into context
			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"type":   "https://tools.ietf.org/html/rfc9110#section-15.5.2",
		"title":  "Unauthorized",
		"status": http.StatusUnauthorized,
		"detail": message,
	})
}

// GetClaimsFromContext extracts TokenClaims from a request context.
// Returns nil if no claims are present (e.g., path bypasses auth middleware).
func GetClaimsFromContext(ctx context.Context) *TokenClaims {
	v := ctx.Value(claimsContextKey)
	if v == nil {
		return nil
	}
	return v.(*TokenClaims)
}

// TokenHash returns a SHA-256 hash of the token (for logging without exposing token).
// Only uses the first 8 bytes (base64url encoded) for brevity.
func TokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:8])
}

// MiddlewareOption configures the AuthMiddleware.
type MiddlewareOption func(*middlewareConfig)

type middlewareConfig struct {
	requiredScope string
	skipPaths     []string
}

// WithRequiredScope sets the required scope for the middleware.
func WithRequiredScope(scope string) MiddlewareOption {
	return func(c *middlewareConfig) {
		c.requiredScope = scope
	}
}

// WithSkipPaths sets paths that bypass authentication.
func WithSkipPaths(paths ...string) MiddlewareOption {
	return func(c *middlewareConfig) {
		c.skipPaths = paths
	}
}

// MiddlewareWithOptions is an alternative constructor with options.
func MiddlewareWithOptions(requiredScope string, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := &middlewareConfig{requiredScope: requiredScope}
	for _, opt := range opts {
		opt(cfg)
	}

	skipSet := make(map[string]bool)
	for _, p := range cfg.skipPaths {
		skipSet[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipSet[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			NewAuthMiddleware(Config{Disabled: false})(next).ServeHTTP(w, r)
		})
	}
}
