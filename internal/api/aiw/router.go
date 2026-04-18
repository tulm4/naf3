// Package aiw provides HTTP routing for the Nnssaaf_AIW service.
// Spec: TS 29.526 §7.3
package aiw

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/api/common"
	aiwnats "github.com/operator/nssAAF/oapi-gen/gen/aiw"
)

// NewRouter creates a chi.Router that mounts the AIW API at /nnssaaf-aiw/v1.
// RequestID is already injected by Handler.ServeHTTP before routing.
func NewRouter(handler *Handler, apiRoot string) http.Handler {
	r := chi.NewRouter()

	r.Use(common.LoggingMiddleware)
	r.Use(common.RecoveryMiddleware)

	return aiwnats.HandlerFromMuxWithBaseURL(handler, r, "/nnssaaf-aiw/v1")
}
