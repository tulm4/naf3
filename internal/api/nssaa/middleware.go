// Package nssaa provides the Nnssaaf_NSSAA service operation handlers.
// Spec: TS 29.526 §7.2
package nssaa

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/types"
)

// AuthMiddleware validates OAuth2 bearer tokens for NSSAA API requests.
// For Phase 1 this is a stub that extracts the X-Request-ID and logs the request.
// Spec: TS 29.526 §7.2, OAuth2 Client Credentials
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract bearer token from Authorization header
		authHdr := r.Header.Get(common.HeaderAuthorization)
		if authHdr == "" {
			// No auth header — for Phase 1 allow unauthenticated requests
			// TODO(Phase 5): Enforce OAuth2 token validation
			slog.Warn("NSSAA request without Authorization header",
				"path", r.URL.Path,
				"method", r.Method,
				"request_id", common.GetRequestID(r.Context()),
			)
			next.ServeHTTP(w, r)
			return
		}

		// Parse Bearer token
		if !strings.HasPrefix(authHdr, "Bearer ") {
			common.WriteProblem(w, common.UnauthorizedProblem("invalid authorization scheme"))
			return
		}

		token := strings.TrimPrefix(authHdr, "Bearer ")
		if token == "" {
			common.WriteProblem(w, common.UnauthorizedProblem("missing bearer token"))
			return
		}

		// TODO(Phase 5): Validate JWT token, check scope "nnssaaf-nssaa"
		// For now, attach the token to context for downstream use
		ctx := context.WithValue(r.Context(), contextKeyBearerToken{}, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type contextKeyBearerToken struct{}

// GetBearerToken retrieves the bearer token from context.
func GetBearerToken(ctx context.Context) string {
	if v := ctx.Value(contextKeyBearerToken{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ValidateAmfRequest validates AMF-related request data.
// For Phase 1 this is a stub. In production it would validate:
// - AMF TLS certificate CN matches amfInstanceID
// - AMF is authorized for this S-NSSAI
func ValidateAmfRequest(ctx context.Context, amfInstanceID string) error {
	if amfInstanceID == "" {
		return nil // optional field
	}
	// TODO(Phase 5): Validate AMF certificate CN against amfInstanceID
	return nil
}

// ValidateNssaiForGpsi validates that the requested S-NSSAI is permitted for this GPSI.
// This would check the UE subscription data from UDM.
// Spec: TS 23.502 §4.2.9.1
func ValidateNssaiForGpsi(ctx context.Context, gpsi string, snssai types.Snssai) error {
	// TODO(Phase 6): Call UDM to verify GPSI subscription includes this S-NSSAI
	_ = gpsi
	_ = snssai
	return nil
}
