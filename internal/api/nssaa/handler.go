// Package nssaa provides HTTP handlers for the Nnssaaf_NSSAA service (N58 interface).
// Spec: TS 29.526 §7.2, TS 23.502 §4.2.9
//
// This package implements the oapi-codegen ServerInterface generated from
// TS29526_Nnssaaf_NSSAA.yaml. The generated router and middleware are
// in github.com/operator/nssAAF/oapi-gen/gen/nssaa.
package nssaa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/eap"
	"github.com/operator/nssAAF/internal/logging"
	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/storage"
	"github.com/operator/nssAAF/internal/udm"
	nssaanats "github.com/operator/nssAAF/oapi-gen/gen/nssaa"
	"github.com/operator/nssAAF/oapi-gen/gen/specs"
)

// AAARouter is the interface for forwarding EAP payloads to AAA-S.
// Aliased from eap.AAARouter for handler convenience.
// Spec: TS 29.561 §16-17
type AAARouter = eap.AAARouter

// AuthCtx represents a slice authentication context stored in NSSAAF.
type AuthCtx struct {
	AuthCtxID   string
	GPSI        string
	SnssaiSST   uint8
	SnssaiSD    string
	AmfInstance string
	ReauthURI   string
	RevocURI    string
	EapPayload  []byte
}

// AuthCtxStore manages slice authentication contexts.
// Phase 3 replaces InMemoryStore with Redis-backed implementation.
type AuthCtxStore interface {
	Load(ctx context.Context, id string) (*AuthCtx, error)
	Save(ctx context.Context, authCtx *AuthCtx) error
	Delete(ctx context.Context, id string) error
	Close() error
}

// ErrNotFound is returned when an authentication context is not found.
var ErrNotFound = errors.New("auth context not found")

// NssaaStore is the interface for NSSAA session persistence.
// Aliased from storage.NssaaStore for API convenience.
type NssaaStore = storage.NssaaStore

// authCtxToNssaaSession converts nssaa.AuthCtx → storage.NssaaSession.
func authCtxToNssaaSession(a *AuthCtx) *storage.NssaaSession {
	return &storage.NssaaSession{
		AuthCtxID:      a.AuthCtxID,
		GPSI:           a.GPSI,
		SnssaiSST:      a.SnssaiSST,
		SnssaiSD:       a.SnssaiSD,
		AmfInstance:    a.AmfInstance,
		ReauthURI:      a.ReauthURI,
		RevocURI:       a.RevocURI,
		EapPayload:     a.EapPayload,
		Status:         "PENDING",
		CallbackOwner:  "amf",
		HasAIWContext:  false,
		ExpiresAt:      time.Now().Add(5 * time.Minute),
	}
}

// Handler implements nssaanats.ServerInterface.
// It receives HTTP requests validated by the oapi-codegen router and
// delegates to the business logic layer.
//
// Rate-limit policy (Rate limit gaps implementation plan, Task 3):
//   - Create path: AMF-scoped limiter using PerAmfPerSec (1-second window).
//     RL-POLICY-AMF: Each AMF instance is limited independently.
//   - Confirm path: Auth-context-scoped limiter using the per-minute policy.
//     RL-POLICY-AUTHCTX: Each authentication session is limited independently.
//   - All Redis errors: fail-open, request proceeds. RL-POLICY-FAIL-OPEN.
type Handler struct {
	store       NssaaStore
	aaa         AAARouter
	apiRoot     string
	nrfClient   interface {
		IsRegistered() bool
	}
	udmClient *udm.Client

	// amfRateLimiter enforces PerAmfPerSec with 1-second sliding window.
	// Nil is safe (no enforcement).
	amfRateLimiter *redis.RateLimiter

	// rateLimiter enforces the per-minute subscriber policy for confirm path.
	// Nil is safe (no enforcement).
	rateLimiter *redis.RateLimiter
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

// WithNRFClient sets the NRF client for service discovery.
func WithNRFClient(nrf interface {
	IsRegistered() bool
}) HandlerOption {
	return func(h *Handler) { h.nrfClient = nrf }
}

// WithUDMClient sets the UDM client for subscription data retrieval.
func WithUDMClient(udmClient *udm.Client) HandlerOption {
	return func(h *Handler) { h.udmClient = udmClient }
}

// WithRateLimiter sets the per-minute rate limiter for the NSSAA confirm path.
// Use amfRateLimiter for AMF-scoped per-second limiting.
func WithRateLimiter(rl *redis.RateLimiter) HandlerOption {
	return func(h *Handler) { h.rateLimiter = rl }
}

// WithAMFRateLimiter sets the per-second AMF rate limiter for the NSSAA create path.
func WithAMFRateLimiter(rl *redis.RateLimiter) HandlerOption {
	return func(h *Handler) { h.amfRateLimiter = rl }
}

// NewHandler creates a new NSSAA handler.
func NewHandler(store NssaaStore, opts ...HandlerOption) *Handler {
	h := &Handler{store: store}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeHTTP routes requests through the oapi-codegen chi handler.
// It satisfies the http.Handler interface so it can be used directly with httptest.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reqID := common.GetRequestID(r.Context())
	if reqID == "" {
		reqID = uuid.NewString()
	}
	r = r.WithContext(common.WithRequestID(r.Context(), reqID))
	nssaanats.Handler(h).ServeHTTP(w, r)
}

var _ http.Handler = (*Handler)(nil)

// CreateSliceAuthenticationContext handles POST /slice-authentications.
// Spec: TS 29.526 §7.2.2, TS 23.502 §4.2.9.2
//
// Procedure flow (TS 23.502 §4.2.9.2):
//  1. AMF sends Nnssaaf_NSSAA_Authenticate with GPSI, S-NSSAI, EAP-Response/Identity
//  2. NSSAAF creates auth context (authCtxId), forwards EAP to AAA-S
//  3. NSSAAF returns 201 with authCtxId and next EAP challenge
func (h *Handler) CreateSliceAuthenticationContext(w http.ResponseWriter, r *http.Request) {
	reqID := common.GetRequestID(r.Context())

	body, present, err := common.ReadRequestBody[nssaanats.SliceAuthInfo](w, r)
	if err != nil {
		return
	}
	snssaiPresent := present["snssai"]

	// Rate limit by AMF NF Instance ID (RL-G1, RL-POLICY-AMF).
	// SliceAuthInfo.AmfInstanceId is a UUID NF instance ID — use it directly.
	// Policy: PerAmfPerSec with 1-second sliding window.
	if h.amfRateLimiter != nil && body.AmfInstanceId != nil {
		allowed, rlErr := h.amfRateLimiter.AllowAMF(r.Context(), string(*body.AmfInstanceId))
		if rlErr != nil {
			slog.Warn("ratelimit: allow check failed",
				"service", "nssaa",
				"scope", "amf",
				"gpsi_hash", logging.HashGPSI(string(body.Gpsi)),
				"error", rlErr,
				"request_id", reqID,
			)
			metrics.RateLimitDecisionRequests.WithLabelValues("nssaa", "amf", "error").Inc()
		} else if !allowed {
			metrics.RateLimitRequests.WithLabelValues("nssaa", "limited").Inc()
			metrics.RateLimitDecisionRequests.WithLabelValues("nssaa", "amf", "limited").Inc()
			h.write429(w, r, 60)
			return
		} else {
			metrics.RateLimitDecisionRequests.WithLabelValues("nssaa", "amf", "allowed").Inc()
		}
	}

	if err := common.ValidateGPSI(string(body.Gpsi)); err != nil {
		var pd *common.ProblemDetails
		if errors.As(err, &pd) {
			common.WriteProblem(w, pd)
		} else {
			common.WriteProblem(w, common.ValidationProblem("gpsi", err.Error()))
		}
		return
	}

	// Inject GPSI into context so downstream WrapDB/WrapRedis picks it up
	// for per-UE debug event correlation.
	r = r.WithContext(debug.WithSubscriber(r.Context(), string(body.Gpsi), ""))

	sst := body.Snssai.Sst
	sd := body.Snssai.Sd
	if err := common.ValidateSnssai(int(sst), sd, !snssaiPresent); err != nil {
		var pd *common.ProblemDetails
		if errors.As(err, &pd) {
			common.WriteProblem(w, pd)
		} else {
			common.WriteProblem(w, common.ValidationProblem("snssai", err.Error()))
		}
		return
	}

	if body.EapIdRsp == nil || *body.EapIdRsp == "" {
		common.WriteProblem(w, common.ValidationProblem("eapIdRsp", "eapIdRsp is required"))
		return
	}

	// Validate that eapIdRsp is valid base64-encoded data.
	if _, err := base64.StdEncoding.DecodeString(*body.EapIdRsp); err != nil {
		common.WriteProblem(w, common.ValidationProblem("eapIdRsp", "eapIdRsp must be valid base64-encoded data"))
		return
	}

	// Use sst/sd from body (already checked above).
	authCtxID := uuid.NewString()

	var amfInstance string
	if body.AmfInstanceId != nil {
		amfInstance = string(*body.AmfInstanceId)
	}
	var reauthURI, revocURI string
	if body.ReauthNotifUri != nil {
		reauthURI = string(*body.ReauthNotifUri)
	}
	if body.RevocNotifUri != nil {
		revocURI = string(*body.RevocNotifUri)
	}

	authCtx := &AuthCtx{
		AuthCtxID:   authCtxID,
		GPSI:        string(body.Gpsi),
		SnssaiSST:   sst,
		SnssaiSD:    sd,
		AmfInstance: amfInstance,
		ReauthURI:   reauthURI,
		RevocURI:    revocURI,
		EapPayload:  []byte(*body.EapIdRsp),
	}

	// Convert API type to domain type before saving.
	session := authCtxToNssaaSession(authCtx)
	if err := h.store.Save(r.Context(), session); err != nil {
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to create auth context: %s", err)))
		return
	}

	// Phase 2: forward to AAA-S and get next EAP challenge.
	// Build the EAP session for the AAA call.
	eapSession := eap.NewSession(authCtxID, string(body.Gpsi)).
		WithSnssai(fmt.Sprintf("%d:%s", body.Snssai.Sst, body.Snssai.Sd))
	var nextEapBytes []byte
	if h.aaa != nil {
		nextEapBytes, err = h.aaa.SendEAP(r.Context(), eapSession, h.aaa.RoutingContext(eapSession), authCtx.EapPayload)
		if err != nil {
			slog.Warn("forward to AAA failed, falling back to echo",
				"auth_ctx_id", authCtxID, "error", err)
			nextEapBytes = authCtx.EapPayload
		}
	} else {
		// No AAA router configured; echo the EAP payload for testing scenarios.
		nextEapBytes = authCtx.EapPayload
	}
	nextEapStr := base64.StdEncoding.EncodeToString(nextEapBytes)

	resp := nssaanats.SliceAuthContext{
		AuthCtxId:  authCtxID,
		Gpsi:       body.Gpsi,
		Snssai:     body.Snssai,
		EapMessage: &nextEapStr,
	}

	location := fmt.Sprintf("%s/nnssaaf-nssaa/v1/slice-authentications/%s",
		h.apiRoot, authCtxID)

	w.Header().Set(common.HeaderLocation, location)
	w.Header().Set(common.HeaderXRequestID, reqID)
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// ConfirmSliceAuthentication handles PUT /slice-authentications/{authCtxId}.
// Spec: TS 29.526 §7.2.3, TS 23.502 §4.2.9.2 step 9
//
//nolint:revive // authCtxId matches the generated ServerInterface signature
func (h *Handler) ConfirmSliceAuthentication(w http.ResponseWriter, r *http.Request, authCtxId string) {
	reqID := common.GetRequestID(r.Context())

	// Rate limit by authCtxId (RL-G1, RL-POLICY-AUTHCTX).
	// Policy: per-minute subscriber policy per auth context.
	// Each authentication session is limited independently.
	if h.rateLimiter != nil {
		allowed, rlErr := h.rateLimiter.Allow(r.Context(), "authctx:"+authCtxId)
		if rlErr != nil {
			slog.Warn("ratelimit: allow check failed",
				"service", "nssaa",
				"scope", "authctx",
				"error", rlErr,
				"request_id", reqID,
			)
			metrics.RateLimitDecisionRequests.WithLabelValues("nssaa", "authctx", "error").Inc()
		} else if !allowed {
			metrics.RateLimitRequests.WithLabelValues("nssaa", "limited").Inc()
			metrics.RateLimitDecisionRequests.WithLabelValues("nssaa", "authctx", "limited").Inc()
			h.write429(w, r, 60)
			return
		} else {
			metrics.RateLimitDecisionRequests.WithLabelValues("nssaa", "authctx", "allowed").Inc()
		}
	}

	if err := common.ValidateAuthCtxID(authCtxId); err != nil {
		common.WriteProblem(w, common.ValidationProblem("authCtxId", err.Error()))
		return
	}

	body, present, err := common.ReadRequestBody[nssaanats.SliceAuthConfirmationData](w, r)
	if err != nil {
		return
	}
	snssaiPresent := present["snssai"]

	if err := common.ValidateGPSI(string(body.Gpsi)); err != nil {
		var pd *common.ProblemDetails
		if errors.As(err, &pd) {
			common.WriteProblem(w, pd)
		} else {
			common.WriteProblem(w, common.ValidationProblem("gpsi", err.Error()))
		}
		return
	}

	// Inject GPSI into context so downstream WrapDB/WrapRedis picks it up
	// for per-UE debug event correlation.
	r = r.WithContext(debug.WithSubscriber(r.Context(), string(body.Gpsi), ""))

	sst := body.Snssai.Sst
	sd := body.Snssai.Sd
	if err := common.ValidateSnssai(int(sst), sd, !snssaiPresent); err != nil {
		var pd *common.ProblemDetails
		if errors.As(err, &pd) {
			common.WriteProblem(w, pd)
		} else {
			common.WriteProblem(w, common.ValidationProblem("snssai", err.Error()))
		}
		return
	}

	if body.EapMessage == nil || *body.EapMessage == "" {
		common.WriteProblem(w, common.ValidationProblem("eapMessage", "eapMessage is required"))
		return
	}

	// Validate that eapMessage is valid base64-encoded data.
	if _, err := base64.StdEncoding.DecodeString(*body.EapMessage); err != nil {
		common.WriteProblem(w, common.ValidationProblem("eapMessage", "eapMessage must be valid base64-encoded data"))
		return
	}

	domSession, err := h.store.Load(r.Context(), authCtxId)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			common.WriteProblem(w, common.NotFoundProblem(
				fmt.Sprintf("slice authentication context %q not found", authCtxId)))
			return
		}
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to load auth context: %s", err)))
		return
	}

	if string(body.Gpsi) != domSession.GPSI {
		common.WriteProblem(w, common.ValidationProblem("gpsi",
			"GPSI does not match the authenticated GPSI for this session"))
		return
	}

	// Validate S-NSSAI matches the original session (TS 29.526 §7.2.3).
	// The AMF sends the same S-NSSAI used during CreateSession.
	if domSession.SnssaiSST != body.Snssai.Sst || domSession.SnssaiSD != body.Snssai.Sd {
		common.WriteProblem(w, common.ValidationProblem("snssai",
			"S-NSSAI does not match the original session"))
		return
	}

	eapPayload := []byte(*body.EapMessage)

	// Phase 2: forward to AAA-S and get next EAP challenge.
	// Build the EAP session for the AAA call.
	eapSession := eap.NewSession(authCtxId, string(body.Gpsi)).
		WithSnssai(fmt.Sprintf("%d:%s", domSession.SnssaiSST, domSession.SnssaiSD))
	var nextEapBytes []byte
	if h.aaa != nil {
		nextEapBytes, err = h.aaa.SendEAP(r.Context(), eapSession, h.aaa.RoutingContext(eapSession), eapPayload)
		if err != nil {
			slog.Warn("forward to AAA failed in confirm, falling back to echo",
				"auth_ctx_id", authCtxId, "error", err)
			nextEapBytes = eapPayload
		}
	} else {
		// No AAA router configured; echo the EAP payload for testing scenarios.
		nextEapBytes = eapPayload
	}

	// Update session state to mark confirmation complete.
	domSession.EapPayload = nextEapBytes
	domSession.Status = "CONFIRMED"
	if err := h.store.Save(r.Context(), domSession); err != nil {
		common.WriteProblem(w, common.InternalServerProblem(
			fmt.Sprintf("failed to update auth context: %s", err)))
		return
	}

	nextEapStr := base64.StdEncoding.EncodeToString(nextEapBytes)

	resp := nssaanats.SliceAuthConfirmationResponse{
		Gpsi:       body.Gpsi,
		Snssai:     body.Snssai,
		EapMessage: &nextEapStr,
		AuthResult: (*specs.AuthStatus)(nil),
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

// Compile-time check: Handler must implement nssaanats.ServerInterface.
var _ nssaanats.ServerInterface = (*Handler)(nil)
