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
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/operator/nssAAF/internal/api/common"
	aiwnats "github.com/operator/nssAAF/oapi-gen/gen/aiw"
)

// AAARouter forwards EAP payloads to AAA-S (RADIUS or Diameter).
type AAARouter interface {
	SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error)
}

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
	Load(id string) (*AuthContext, error)
	Save(ctx *AuthContext) error
	Delete(id string) error
	Close() error
}

// ErrNotFound is returned when an authentication context is not found.
var ErrNotFound = errors.New("auth context not found")

// eapPayloadFromPtr safely dereferences a nullable *EapMessage ([]byte alias) or returns empty.
func eapPayloadFromPtr(p *aiwnats.EapMessage) []byte {
	if p == nil {
		return nil
	}
	return *p
}

// InMemoryStore is a simple in-memory implementation of AuthCtxStore.
// Phase 3 replaces this with Redis-based storage.
type InMemoryStore struct {
	data map[string]*AuthContext
}

// NewInMemoryStore creates a new in-memory auth context store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{data: make(map[string]*AuthContext)}
}

// Load implements AuthCtxStore.
func (s *InMemoryStore) Load(id string) (*AuthContext, error) {
	if ctx, ok := s.data[id]; ok {
		return ctx, nil
	}
	return nil, ErrNotFound
}

// Save implements AuthCtxStore.
func (s *InMemoryStore) Save(ctx *AuthContext) error {
	s.data[ctx.AuthCtxID] = ctx
	return nil
}

// Delete implements AuthCtxStore.
func (s *InMemoryStore) Delete(id string) error {
	delete(s.data, id)
	return nil
}

// Close implements io.Closer. No-op for in-memory store, but required for
// API consistency when Phase 3 swaps this with a Redis-backed store.
func (s *InMemoryStore) Close() error {
	return nil
}

// Handler implements aiwnats.ServerInterface.
type Handler struct {
	store      AuthCtxStore
	aaa        AAARouter
	apiRoot    string
	ausfClient interface {
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

// NewHandler creates a new AIW handler.
func NewHandler(store AuthCtxStore, opts ...HandlerOption) *Handler {
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
	}

	if err := h.store.Save(authCtx); err != nil {
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

	authCtx, err := h.store.Load(authCtxId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			common.WriteProblem(w, common.NotFoundProblem(
				fmt.Sprintf("authentication context %q not found", authCtxId)))
			return
		}
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to load auth context: %s", err)))
		return
	}

	if string(body.Supi) != authCtx.Supi {
		common.WriteProblem(w, common.ValidationProblem("supi",
			"SUPI does not match the authenticated SUPI for this session"))
		return
	}

	// Store the Phase 2 EAP payload so it survives across round-trips.
	authCtx.EapPayload = eapPayloadFromPtr(body.EapMessage)
	if err := h.store.Save(authCtx); err != nil {
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to update auth context: %s", err)))
		return
	}

	// Phase 2: h.aaa.SendEAP(r.Context(), authCtxId, authCtx.EapPayload)
	// Phase 1: echo back the EAP message as the response.

	resp := aiwnats.AuthConfirmationResponse{
		Supi:       body.Supi,
		EapMessage: body.EapMessage,
		AuthResult: nil,
	}

	w.Header().Set(common.HeaderXRequestID, reqID)
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// Compile-time check: Handler must implement aiwnats.ServerInterface.
var _ aiwnats.ServerInterface = (*Handler)(nil)
