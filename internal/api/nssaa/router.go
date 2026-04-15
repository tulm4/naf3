// Package nssaa provides the Nnssaaf_NSSAA service operation handlers.
// Spec: TS 29.526 §7.2
package nssaa

import (
	"net/http"

	"github.com/operator/nssAAF/internal/api/common"
)

// Router handles HTTP routing for the Nnssaaf_NSSAA service.
type Router struct {
	handler *Handler
}

// NewRouter creates a new NSSAA API router.
func NewRouter(handler *Handler) *Router {
	return &Router{handler: handler}
}

// RegisterRoutes registers all NSSAA API routes on the given ServeMux.
// Base path: /nnssaaf-nssaa/v1
func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	base := "/nnssaaf-nssaa/v1"

	// POST /slice-authentications — Create a new slice authentication context
	mux.Handle(base+"/slice-authentications", r)

	// PUT /slice-authentications/{authCtxID} — Advance EAP round
	mux.Handle(base+"/slice-authentications/", r)
}

// ServeHTTP routes requests to the appropriate handler based on method and path.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqID := common.GetRequestID(req.Context())

	switch {
	// POST /nnssaaf-nssaa/v1/slice-authentications
	case req.Method == http.MethodPost && req.URL.Path == "/nnssaaf-nssaa/v1/slice-authentications":
		r.handler.HandleCreateSliceAuthentication(w, req.WithContext(common.WithRequestID(req.Context(), reqID)))

	// PUT /nnssaaf-nssaa/v1/slice-authentications/{authCtxID}
	case req.Method == http.MethodPut && len(req.URL.Path) > len("/nnssaaf-nssaa/v1/slice-authentications/"):
		authCtxID := req.URL.Path[len("/nnssaaf-nssaa/v1/slice-authentications/"):]
		r.handler.HandleConfirmSliceAuthentication(w, req.WithContext(
			common.WithRequestID(req.Context(), reqID)), authCtxID)

	default:
		common.WriteProblem(w, common.NotFoundProblem("resource not found"))
	}
}
