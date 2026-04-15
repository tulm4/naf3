// Package aiw provides the Nnssaaf_AIW service operation handlers.
package aiw

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/types"
)

// Config holds runtime configuration for the AIW handler.
type Config struct {
	BaseURL string
}

// Handler handles Nnssaaf_AIW service operations.
// Spec: TS 29.526 §7.3, N60 Interface
type Handler struct {
	cfg *Config
}

// NewHandler creates a new AIW handler.
func NewHandler(cfg *Config) *Handler {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://nssAAF.operator.com"
	}
	return &Handler{cfg: cfg}
}

// HandleCreateAuthentication handles POST /nnssaaf-aiw/v1/authentications.
// Spec: TS 29.526 §7.3.2, Operation: CreateAuthenticationContext
func (h *Handler) HandleCreateAuthentication(w http.ResponseWriter, r *http.Request) {
	reqID := common.GetRequestID(r.Context())

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		slog.Error("failed to read request body",
			"request_id", reqID, "error", err)
		common.WriteProblem(w, common.InternalServerProblem("failed to read request body"))
		return
	}

	info, err := ParseAuthInfo(body)
	if err != nil {
		slog.Warn("failed to parse AuthInfo",
			"request_id", reqID, "error", err)
		common.WriteProblem(w, common.NewProblem(400, types.CauseInvalidPayload,
			fmt.Sprintf("invalid request body: %v", err)))
		return
	}

	if errs := info.Validate(); len(errs) > 0 {
		detail := formatAiwErrors(errs)
		slog.Warn("AIW request validation failed",
			"request_id", reqID, "supi", info.Supi, "errors", detail)
		common.WriteProblem(w, common.ValidationProblem("request", detail))
		return
	}

	// TODO(Phase 3): Load AAA server config from storage
	slog.Info("AIW AAA config lookup",
		"request_id", reqID,
		"supi", info.Supi,
		"phase", "phase1-stub")
	common.WriteProblem(w, types.ErrAaaServerNotConfigured.ToProblemDetails())
}

// HandleConfirmAuthentication handles PUT /nnssaaf-aiw/v1/authentications/{authCtxID}.
// Spec: TS 29.526 §7.3.3, Operation: ConfirmAuthentication
func (h *Handler) HandleConfirmAuthentication(w http.ResponseWriter, r *http.Request, authCtxID string) {
	reqID := common.GetRequestID(r.Context())

	if err := common.ValidateAuthCtxID(authCtxID); err != nil {
		common.WriteProblem(w, common.NewProblem(400, types.CauseInvalidAuthCtxID,
			"authCtxID must be a non-empty string without control characters"))
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		slog.Error("failed to read request body",
			"request_id", reqID, "authCtxID", authCtxID, "error", err)
		common.WriteProblem(w, common.InternalServerProblem("failed to read request body"))
		return
	}

	data, err := ParseConfirmAuthData(body)
	if err != nil {
		slog.Warn("failed to parse confirm data",
			"request_id", reqID, "authCtxID", authCtxID, "error", err)
		common.WriteProblem(w, common.NewProblem(400, types.CauseInvalidPayload,
			fmt.Sprintf("invalid request body: %v", err)))
		return
	}

	if errs := data.Validate(); len(errs) > 0 {
		detail := formatAiwErrors(errs)
		slog.Warn("AIW confirmation validation failed",
			"request_id", reqID, "authCtxID", authCtxID, "errors", detail)
		common.WriteProblem(w, common.ValidationProblem("request", detail))
		return
	}

	// TODO(Phase 3): Load from PostgreSQL + Redis cache
	slog.Info("AIW session load",
		"request_id", reqID,
		"authCtxID", authCtxID,
		"phase", "phase1-stub")
	common.WriteProblem(w, types.ErrAuthContextNotFound.ToProblemDetails())
}

// GenerateAuthCtxID generates a new AIW authentication context identifier.
func GenerateAuthCtxID() string {
	return uuid.NewString()
}

// buildLocation builds the Location header URL for a newly created auth context.
func buildLocation(baseURL, authCtxID string) string {
	return fmt.Sprintf("%s/nnssaaf-aiw/v1/authentications/%s",
		strings.TrimSuffix(baseURL, "/"), authCtxID)
}

// formatAiwErrors formats multiple errors into a single string.
func formatAiwErrors(errs []error) string {
	if len(errs) == 0 {
		return "unknown validation error"
	}
	if len(errs) == 1 {
		return errs[0].Error()
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}
