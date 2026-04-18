// Package nssaa provides HTTP routing for the Nnssaaf_NSSAA service (N58 interface).
// Spec: TS 29.526 §7.2, TS 23.502 §4.2.9
package nssaa

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/operator/nssAAF/internal/api/common"
	nssaanats "github.com/operator/nssAAF/oapi-gen/gen/nssaa"
)

// NewRouter creates a chi.Router that mounts the NSSAA API at /nnssaaf-nssaa/v1.
// RequestID is already injected by Handler.ServeHTTP before routing.
func NewRouter(handler *Handler, apiRoot string) http.Handler {
	r := chi.NewRouter()

	r.Use(common.LoggingMiddleware)
	r.Use(common.RecoveryMiddleware)

	return nssaanats.HandlerFromMuxWithBaseURL(handler, r, "/nnssaaf-nssaa/v1")
}
