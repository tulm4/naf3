// Package aiw provides HTTP handlers for the Nnssaaf_AIW service.
// Spec: TS 29.526 §7.3
//
// This package implements the oapi-codegen ServerInterface generated from
// TS29526_Nnssaaf_AIW.yaml. The generated router and middleware are
// in github.com/operator/nssAAF/oapi-gen/gen/aiw.
package aiw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/operator/nssAAF/internal/api/common"
	ratelimit "github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/eap"
	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/storage"
	aiwnats "github.com/operator/nssAAF/oapi-gen/gen/aiw"
	"github.com/operator/nssAAF/oapi-gen/gen/specs"
)

// AAARouter is the interface for forwarding EAP payloads to AAA-S.
// Aliased from eap.AAARouter for handler convenience.
// Spec: TS 29.561 §16-17
type AAARouter = eap.AAARouter

// AuthContext represents an AIW authentication context.
// Spec: TS 29.526 §7.3
// Design: docs/design/04_data_model.md §3.6
type AuthContext struct {
	AuthCtxID  string
	Supi       string
	EapPayload []byte
	TtlsInner  []byte

	// MSK: Master Session Key from EAP-TLS (RFC 5216 §2.1.4)
	// Stored encrypted; NULL if not EAP-TLS or on Failure
	MSK []byte

	// PvsInfo: Privacy-Violating Servers info from AAA-S (TS 29.526 §7.3.3)
	// JSON array: [{"serverType":"PROSE","serverId":"pvs-001"},...]
	PvsInfo []byte // JSONB stored as []byte

	// AusfID: AUSF instance that triggered this authentication
	AusfID string

	// Supported features echo (from request)
	SupportedFeatures string

	// Auth result
	Status     string
	AuthResult string

	// Session metadata
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ExpiresAt  time.Time
	CompletedAt *time.Time
}

// AuthCtxStore manages AIW authentication contexts.
// Phase 3 replaces InMemoryStore with Redis-backed implementation.
type AuthCtxStore interface {
	Load(ctx context.Context, id string) (*AuthContext, error)
	Save(ctx context.Context, authCtx *AuthContext) error
	Delete(ctx context.Context, id string) error
	Close() error
}

// ErrNotFound is returned when an authentication context is not found.
var ErrNotFound = errors.New("auth context not found")

// AiwStore is the interface for AIW session persistence.
// Aliased from storage.AiwStore for API convenience.
type AiwStore = storage.AiwStore

// authContextToAiwSession converts aiw.AuthContext → storage.AiwSession.
func authContextToAiwSession(a *AuthContext) *storage.AiwSession {
	expiresAt := a.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(24 * time.Hour)
	}
	return &storage.AiwSession{
		AuthCtxID:         a.AuthCtxID,
		Supi:              a.Supi,
		EapPayload:        a.EapPayload,
		TtlsInner:         a.TtlsInner,
		MSK:               a.MSK,
		PvsInfo:           a.PvsInfo,
		AusfID:            a.AusfID,
		SupportedFeatures:  a.SupportedFeatures,
		Status:            a.Status,
		AuthResult:        a.AuthResult,
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
		ExpiresAt:         expiresAt,
		CompletedAt:       a.CompletedAt,
	}
}

// eapPayloadFromPtr safely dereferences a nullable *EapMessage ([]byte alias) or returns empty.
func eapPayloadFromPtr(p *aiwnats.EapMessage) []byte {
	if p == nil {
		return nil
	}
	return *p
}

// InMemoryStore is a simple in-memory implementation of AiwStore.
// Phase 3 replaces this with Redis-based storage.
type InMemoryStore struct {
	data map[string]*storage.AiwSession
}

// NewInMemoryStore creates a new in-memory auth context store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{data: make(map[string]*storage.AiwSession)}
}

// Load implements AiwStore.
func (s *InMemoryStore) Load(_ context.Context, id string) (*storage.AiwSession, error) {
	if ctx, ok := s.data[id]; ok {
		return ctx, nil
	}
	return nil, storage.ErrSessionNotFound
}

// Save implements AiwStore.
func (s *InMemoryStore) Save(_ context.Context, ctx *storage.AiwSession) error {
	s.data[ctx.AuthCtxID] = ctx
	return nil
}

// Delete implements AiwStore.
func (s *InMemoryStore) Delete(_ context.Context, id string) error {
	delete(s.data, id)
	return nil
}

// Close implements io.Closer. No-op for in-memory store, but required for
// API consistency when Phase 3 swaps this with a Redis-backed store.
func (s *InMemoryStore) Close() error {
	return nil
}

// RateLimiter is the interface for rate limiting decisions.
// Aliased from the concrete redis implementation for ergonomic use.
// Allows injection of real *ratelimit.RateLimiter or test doubles.
type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

var _ RateLimiter = (*ratelimit.RateLimiter)(nil) // compile-time check

// Handler implements aiwnats.ServerInterface.
type Handler struct {
	store       AiwStore
	aaa         AAARouter
	apiRoot     string
	rateLimiter RateLimiter
	ausfClient  interface {
		ForwardMSK(ctx context.Context, authCtxID string, msk []byte) error
	}
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithAAA sets the AAA router.
func WithAAA(aaa AAARouter) HandlerOption {
	return func(h *Handler) { h.aaa = aaa }
}

// WithAPIRoot sets the API root URL for Location header generation.
func WithAPIRoot(apiRoot string) HandlerOption {
	return func(h *Handler) { h.apiRoot = apiRoot }
}

// WithAUSFClient sets the AUSF client for MSK forwarding.
func WithAUSFClient(ausf interface {
	ForwardMSK(ctx context.Context, authCtxID string, msk []byte) error
}) HandlerOption {
	return func(h *Handler) { h.ausfClient = ausf }
}

// WithRateLimiter sets the rate limiter for the AIW handler.
func WithRateLimiter(rl RateLimiter) HandlerOption {
	return func(h *Handler) { h.rateLimiter = rl }
}

// NewHandler creates a new AIW handler.
func NewHandler(store AiwStore, opts ...HandlerOption) *Handler {
	h := &Handler{store: store}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeHTTP routes requests through the oapi-codegen handler.
// It satisfies the http.Handler interface so it can be used directly with httptest.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := common.GetRequestID(r.Context())
	if reqID == "" {
		reqID = uuid.NewString()
	}
	r = r.WithContext(common.WithRequestID(r.Context(), reqID))
	aiwnats.Handler(h).ServeHTTP(w, r)
}

var _ http.Handler = (*Handler)(nil)

// CreateAuthenticationContext handles POST /authentications.
// Spec: TS 29.526 §7.3.2
func (h *Handler) CreateAuthenticationContext(w http.ResponseWriter, r *http.Request) {
	reqID := common.GetRequestID(r.Context())

	var body aiwnats.AuthInfo
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		common.WriteProblem(w, common.ValidationProblem("body", err.Error()))
		return
	}

	// Rate limit by SUPI (RL-G1).
	if h.rateLimiter != nil {
		allowed, err := h.rateLimiter.Allow(r.Context(), "aiw:supi:"+string(body.Supi))
		if err != nil {
			slog.Warn("ratelimit: allow check failed",
				"service", "aiw",
				"scope", "supi",
				"error", err,
				"request_id", reqID,
			)
			metrics.RateLimitDecisionRequests.WithLabelValues("aiw", "supi", "error").Inc()
		} else if !allowed {
			metrics.RateLimitRequests.WithLabelValues("aiw", "limited").Inc()
			metrics.RateLimitDecisionRequests.WithLabelValues("aiw", "supi", "limited").Inc()
			h.write429(w, r, 60)
			return
		} else {
			metrics.RateLimitDecisionRequests.WithLabelValues("aiw", "supi", "allowed").Inc()
		}
	}

	if err := common.ValidateSUPI(string(body.Supi)); err != nil {
		var pd *common.ProblemDetails
		if errors.As(err, &pd) {
			common.WriteProblem(w, pd)
		} else {
			common.WriteProblem(w, common.ValidationProblem("supi", err.Error()))
		}
		return
	}

	// Note: eapIdRsp is decoded as base64 automatically by JSON unmarshaling
	// into []byte. Invalid base64 causes JSON decode error above, so no
	// explicit base64 validation is needed here (unlike NSSAA's string field).
	authCtxID := uuid.NewString()

	authCtx := &AuthContext{
		AuthCtxID:  authCtxID,
		Supi:       string(body.Supi),
		EapPayload: eapPayloadFromPtr(body.EapIdRsp),
		Status:     "PENDING", // Initial AIW session state per TS 29.526 §7.3
	}

	session := authContextToAiwSession(authCtx)
	session.Status = "PENDING"
	if err := h.store.Save(r.Context(), session); err != nil {
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to create auth context: %s", err)))
		return
	}

	resp := aiwnats.AuthContext{
		Supi:      body.Supi,
		AuthCtxId: authCtxID,
	}

	if body.EapIdRsp != nil {
		resp.EapMessage = body.EapIdRsp
	}

	location := fmt.Sprintf("%s/nnssaaf-aiw/v1/authentications/%s",
		h.apiRoot, authCtxID)

	w.Header().Set(common.HeaderLocation, location)
	w.Header().Set(common.HeaderXRequestID, reqID)
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// ConfirmAuthentication handles PUT /authentications/{authCtxId}.
// Spec: TS 29.526 §7.3.3
//
//nolint:revive // authCtxId matches the generated ServerInterface signature
func (h *Handler) ConfirmAuthentication(w http.ResponseWriter, r *http.Request, authCtxId string) {
	reqID := common.GetRequestID(r.Context())

	if err := common.ValidateAuthCtxID(authCtxId); err != nil {
		common.WriteProblem(w, common.ValidationProblem("authCtxId", err.Error()))
		return
	}

	// Rate limit by auth context (RL-G1 / confirm path).
	// Scope: "aiw:authctx:{authCtxId}" — per-minute sliding window.
	// Policy: fail-open (allow request through on Redis error).
	if h.rateLimiter != nil {
		allowed, rlErr := h.rateLimiter.Allow(r.Context(), "aiw:authctx:"+authCtxId)
		if rlErr != nil {
			slog.Warn("ratelimit: allow check failed",
				"service", "aiw",
				"scope", "authctx",
				"error", rlErr,
				"request_id", reqID,
			)
			metrics.RateLimitDecisionRequests.WithLabelValues("aiw", "authctx", "error").Inc()
		} else if !allowed {
			metrics.RateLimitRequests.WithLabelValues("aiw", "limited").Inc()
			metrics.RateLimitDecisionRequests.WithLabelValues("aiw", "authctx", "limited").Inc()
			h.write429(w, r, 60)
			return
		} else {
			metrics.RateLimitDecisionRequests.WithLabelValues("aiw", "authctx", "allowed").Inc()
		}
	}

	var body aiwnats.AuthConfirmationData
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		common.WriteProblem(w, common.ValidationProblem("body", err.Error()))
		return
	}

	if err := common.ValidateSUPI(string(body.Supi)); err != nil {
		var pd *common.ProblemDetails
		if errors.As(err, &pd) {
			common.WriteProblem(w, pd)
		} else {
			common.WriteProblem(w, common.ValidationProblem("supi", err.Error()))
		}
		return
	}

	if body.EapMessage == nil || len(*body.EapMessage) == 0 {
		common.WriteProblem(w, common.ValidationProblem("eapMessage", "eapMessage is required"))
		return
	}

	// Note: eapMessage is []byte alias in generated types, so JSON auto-decodes base64.

	domSession, err := h.store.Load(r.Context(), authCtxId)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			common.WriteProblem(w, common.NotFoundProblem(
				fmt.Sprintf("authentication context %q not found", authCtxId)))
			return
		}
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to load auth context: %s", err)))
		return
	}

	if string(body.Supi) != domSession.Supi {
		common.WriteProblem(w, common.ValidationProblem("supi",
			"SUPI does not match the authenticated SUPI for this session"))
		return
	}

	// Store the Phase 2 EAP payload so it survives across round-trips.
	domSession.EapPayload = eapPayloadFromPtr(body.EapMessage)
	if err := h.store.Save(r.Context(), domSession); err != nil {
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to update auth context: %s", err)))
		return
	}

	// Phase 2: h.aaa.SendEAP(r.Context(), authCtxId, authCtx.EapPayload)
	// Phase 1: echo back the EAP message as the response.
	// PvsInfo is not available in Phase 1 (requires real AAA-S integration).
	// Return an empty list per TS 29.526 §7.3 which requires pvsInfo with minItems=1.
	// This is a Phase 1 stub — the field is present but empty.
	resp := aiwnats.AuthConfirmationResponse{
		Supi:       body.Supi,
		EapMessage: body.EapMessage,
		AuthResult: nil,
		PvsInfo:    &[]specs.ServerAddressingInfo{},
	}

	w.Header().Set(common.HeaderXRequestID, reqID)
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// write429 writes a 429 Too Many Requests response with Retry-After header.
func (h *Handler) write429(w http.ResponseWriter, r *http.Request, retryAfter int) {
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.WriteHeader(http.StatusTooManyRequests)
	common.WriteProblem(w, common.NewProblem(
		http.StatusTooManyRequests,
		"rate-limit-exceeded",
		"Rate limit exceeded for this request",
	))
}

// Compile-time check: Handler must implement aiwnats.ServerInterface.
var _ aiwnats.ServerInterface = (*Handler)(nil)
