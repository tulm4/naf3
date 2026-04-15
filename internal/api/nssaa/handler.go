// Package nssaa provides the Nnssaaf_NSSAA service operation handlers.
package nssaa

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

// Config holds runtime configuration for the NSSAA handler.
type Config struct {
	// BaseURL is the public base URL of the NSSAAF service.
	// Used to construct Location headers in 201 responses.
	BaseURL string
}

// Handler handles Nnssaaf_NSSAA service operations.
// Spec: TS 29.526 §7.2, N58 Interface
type Handler struct {
	cfg *Config
}

// NewHandler creates a new NSSAA handler.
func NewHandler(cfg *Config) *Handler {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://nssAAF.operator.com"
	}
	return &Handler{cfg: cfg}
}

// HandleCreateSliceAuthentication handles POST /nnssaaf-nssaa/v1/slice-authentications.
// Spec: TS 29.526 §7.2.2, Operation: CreateSliceAuthenticationContext
func (h *Handler) HandleCreateSliceAuthentication(w http.ResponseWriter, r *http.Request) {
	reqID := common.GetRequestID(r.Context())

	// 1. Read and parse request body
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		slog.Error("failed to read request body",
			"request_id", reqID, "error", err)
		common.WriteProblem(w, common.InternalServerProblem("failed to read request body"))
		return
	}

	info, err := ParseSliceAuthInfo(body)
	if err != nil {
		slog.Warn("failed to parse SliceAuthInfo",
			"request_id", reqID, "error", err)
		common.WriteProblem(w, common.NewProblem(400, types.CauseInvalidPayload,
			fmt.Sprintf("invalid request body: %v", err)))
		return
	}

	// 2. Validate request fields
	if errs := info.Validate(); len(errs) > 0 {
		detail := formatValidationErrors(errs)
		slog.Warn("request validation failed",
			"request_id", reqID, "gpsi", info.Gpsi, "errors", detail)
		common.WriteProblem(w, common.ValidationProblem("request", detail))
		return
	}

	// 3. Resolve AAA server configuration for this S-NSSAI
	// TODO(Phase 3): Load from storage, implement 3-level fallback
	slog.Info("AAA config lookup",
		"request_id", reqID,
		"gpsi", info.Gpsi,
		"snssai", info.Snssai.String(),
		"phase", "phase1-stub")
	common.WriteProblem(w, types.ErrAaaServerNotConfigured.ToProblemDetails())
}

// HandleConfirmSliceAuthentication handles PUT /nnssaaf-nssaa/v1/slice-authentications/{authCtxID}.
// Spec: TS 29.526 §7.2.3, Operation: ConfirmSliceAuthentication
func (h *Handler) HandleConfirmSliceAuthentication(w http.ResponseWriter, r *http.Request, authCtxID string) {
	reqID := common.GetRequestID(r.Context())

	if err := common.ValidateAuthCtxID(authCtxID); err != nil {
		slog.Warn("invalid authCtxID format",
			"request_id", reqID, "authCtxID", authCtxID)
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

	data, err := ParseSliceAuthConfirmationData(body)
	if err != nil {
		slog.Warn("failed to parse confirmation data",
			"request_id", reqID, "authCtxID", authCtxID, "error", err)
		common.WriteProblem(w, common.NewProblem(400, types.CauseInvalidPayload,
			fmt.Sprintf("invalid request body: %v", err)))
		return
	}

	if errs := data.Validate(); len(errs) > 0 {
		detail := formatValidationErrors(errs)
		slog.Warn("confirmation validation failed",
			"request_id", reqID, "authCtxID", authCtxID, "errors", detail)
		common.WriteProblem(w, common.ValidationProblem("request", detail))
		return
	}

	// TODO(Phase 3): Load from PostgreSQL + Redis cache
	slog.Info("session load",
		"request_id", reqID,
		"authCtxID", authCtxID,
		"phase", "phase1-stub")
	common.WriteProblem(w, types.ErrAuthContextNotFound.ToProblemDetails())
}

// GenerateAuthCtxID generates a new authentication context identifier.
// Uses UUID for time-sortable identifiers.
func GenerateAuthCtxID() string {
	return uuid.NewString()
}

// buildLocation builds the Location header URL for a newly created auth context.
func buildLocation(baseURL, authCtxID string) string {
	return fmt.Sprintf("%s/nnssaaf-nssaa/v1/slice-authentications/%s",
		strings.TrimSuffix(baseURL, "/"), authCtxID)
}

// formatValidationErrors formats multiple validation errors into a single string.
func formatValidationErrors(errs []error) string {
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
