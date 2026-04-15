// Package aiw provides the Nnssaaf_AIW service operation handlers.
// Spec: TS 29.526 §7.3
package aiw

import (
	"net/http"

	"github.com/operator/nssAAF/internal/api/common"
)

// Router handles HTTP routing for the Nnssaaf_AIW service.
type Router struct {
	handler *Handler
}

// NewRouter creates a new AIW API router.
func NewRouter(handler *Handler) *Router {
	return &Router{handler: handler}
}

// RegisterRoutes registers all AIW API routes on the given ServeMux.
// Base path: /nnssaaf-aiw/v1
func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	base := "/nnssaaf-aiw/v1"

	// POST /authentications
	mux.Handle(base+"/authentications", r)

	// PUT /authentications/{authCtxID}
	mux.Handle(base+"/authentications/", r)
}

// ServeHTTP routes requests to the appropriate handler based on method and path.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqID := common.GetRequestID(req.Context())

	switch {
	// POST /nnssaaf-aiw/v1/authentications
	case req.Method == http.MethodPost && req.URL.Path == "/nnssaaf-aiw/v1/authentications":
		r.handler.HandleCreateAuthentication(w, req.WithContext(common.WithRequestID(req.Context(), reqID)))

	// PUT /nnssaaf-aiw/v1/authentications/{authCtxID}
	case req.Method == http.MethodPut && len(req.URL.Path) > len("/nnssaaf-aiw/v1/authentications/"):
		authCtxID := req.URL.Path[len("/nnssaaf-aiw/v1/authentications/"):]
		r.handler.HandleConfirmAuthentication(w, req.WithContext(
			common.WithRequestID(req.Context(), reqID)), authCtxID)

	default:
		common.WriteProblem(w, common.NotFoundProblem("resource not found"))
	}
}
