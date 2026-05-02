// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/operator/nssAAF/internal/types"
)

// Time provider for testability.
var timeNow = time.Now

// Config holds configuration for the EAP engine.
type Config struct {
	// Session limits
	MaxRounds    int
	RoundTimeout time.Duration
	SessionTTL   time.Duration

	// Fragment reassembly
	FragmentTTLSeconds int64

	// TLS session resumption
	TLSSessionCacheSize int
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxRounds:           DefaultMaxRounds,
		RoundTimeout:        DefaultRoundTimeout,
		SessionTTL:          DefaultSessionTTL,
		FragmentTTLSeconds:  60,
		TLSSessionCacheSize: 1024,
	}
}

// Logger is the logging interface used by the EAP engine.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// defaultLogger wraps slog.Logger.
type defaultLogger struct {
	*slog.Logger
}

func (l *defaultLogger) Debug(msg string, args ...any) { l.Logger.Debug(msg, args...) }
func (l *defaultLogger) Info(msg string, args ...any)  { l.Logger.Info(msg, args...) }
func (l *defaultLogger) Warn(msg string, args ...any)  { l.Logger.Warn(msg, args...) }
func (l *defaultLogger) Error(msg string, args ...any) { l.Logger.Error(msg, args...) }

// Engine is the main EAP engine that processes authentication sessions.
// Spec: RFC 3748 §3, TS 33.501 §16.3
type Engine struct {
	cfg            Config
	sessionManager *sessionManager
	fragmentMgr    *FragmentManager
	aaaClient      AAARouter
	logger         Logger

	// TLS config for EAP-TLS
	tlsConfig *tls.Config
}

// NewEngine creates a new EAP engine with the given configuration.
func NewEngine(cfg Config, aaaClient AAARouter, logger *slog.Logger) *Engine {
	if cfg.MaxRounds == 0 {
		cfg.MaxRounds = DefaultMaxRounds
	}
	if cfg.RoundTimeout == 0 {
		cfg.RoundTimeout = DefaultRoundTimeout
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = DefaultSessionTTL
	}
	if cfg.FragmentTTLSeconds == 0 {
		cfg.FragmentTTLSeconds = 60
	}

	return &Engine{
		cfg:            cfg,
		sessionManager: newSessionManager(cfg.SessionTTL),
		fragmentMgr:    NewFragmentManager(cfg.FragmentTTLSeconds),
		aaaClient:      aaaClient,
		logger:         &defaultLogger{logger},
	}
}

// SetTLSConfig sets the TLS configuration for EAP-TLS.
func (e *Engine) SetTLSConfig(cfg *tls.Config) {
	e.tlsConfig = cfg
}

// StartSession creates a new EAP session for an authentication context.
func (e *Engine) StartSession(authCtxID, gpsi string) (*Session, error) {
	if authCtxID == "" {
		return nil, ErrMissingAuthCtxID
	}

	session := NewSession(authCtxID, gpsi)
	session.MaxRounds = e.cfg.MaxRounds
	session.Timeout = e.cfg.RoundTimeout

	e.sessionManager.put(session)
	e.logger.Info("eap_session_started",
		"auth_ctx_id", authCtxID,
		"gpsi", gpsi,
		"max_rounds", session.MaxRounds,
	)

	return session, nil
}

// GetSession returns an existing EAP session by authCtxID.
func (e *Engine) GetSession(authCtxID string) (*Session, error) {
	return e.sessionManager.get(authCtxID)
}

// Process processes an incoming EAP message from AMF.
// It determines whether this is a new request, a retry, or part of an ongoing session.
func (e *Engine) Process(ctx context.Context, authCtxID string, eapPayload []byte) (*types.EapMessage, types.AuthResult, error) {
	session, err := e.sessionManager.get(authCtxID)
	if err != nil {
		// No existing session — this should not happen for non-initial messages.
		return nil, types.AuthResultFailure, fmt.Errorf("no session found for authCtxID=%s: %w", authCtxID, err)
	}

	// Check for idle timeout.
	if session.IsTimedOut() {
		session.MarkTimeout()
		e.sessionManager.put(session)
		e.logger.Warn("eap_session_timeout",
			"auth_ctx_id", authCtxID,
			"last_activity", session.LastActivity,
		)
		return nil, types.AuthResultFailure, ErrSessionTimeout
	}

	// Parse EAP packet.
	packet, err := Parse(eapPayload)
	if err != nil {
		e.logger.Error("eap_parse_error",
			"auth_ctx_id", authCtxID,
			"error", err,
		)
		return nil, types.AuthResultFailure, err
	}

	// Idempotent retry detection.
	msgHash := sha256Hash(eapPayload)
	if bytesEqual(session.LastNonce, msgHash) && session.CachedResponse != nil {
		e.logger.Debug("eap_retry_detected",
			"auth_ctx_id", authCtxID,
			"id", packet.ID,
		)
		respMsg := types.NewEapMessage(session.CachedResponse)
		return &respMsg, types.AuthResultPending, nil
	}

	// Advance state machine.
	response, result, err := e.advanceState(ctx, session, packet, eapPayload)

	// For retry detection on next call: update LastNonce and cache BEFORE checking err.
	// This ensures that if advanceState returned an error (e.g. ID mismatch on
	// a genuine retry), the retry detection can intercept the next identical call.
	session.LastNonce = msgHash
	session.LastActivity = timeNow()

	if response != nil {
		session.CacheResponse(response)
	}
	e.sessionManager.put(session)

	if err != nil {
		// Do NOT return cached response here — the caller needs to know the error.
		return nil, types.AuthResultFailure, err
	}

	// Convert result to AuthResult.
	authResult := authResultFromEapResult(result)
	if response != nil {
		respMsg := types.NewEapMessage(response)
		return &respMsg, authResult, nil
	}
	return nil, authResult, nil
}

// advanceState drives the EAP state machine for a given session.
func (e *Engine) advanceState(ctx context.Context, session *Session, packet *Packet, rawPayload []byte) ([]byte, Result, error) {
	switch session.State {
	case SessionStateInit:
		return e.handleInit(ctx, session, packet, rawPayload)

	case SessionStateEapExchange:
		return e.handleExchange(ctx, session, packet, rawPayload)

	case SessionStateCompleting:
		return e.handleCompleting(ctx, session, packet, rawPayload)

	case SessionStateDone:
		return nil, ResultIgnored, ErrSessionAlreadyDone

	case SessionStateFailed:
		return nil, ResultIgnored, ErrSessionAlreadyDone

	case SessionStateTimeout:
		return nil, ResultTimeout, ErrSessionTimeout

	default:
		return nil, ResultIgnored, fmt.Errorf("%w: from state %s", ErrInvalidStateTransition, session.State)
	}
}

// handleInit processes the first EAP message from AMF.
func (e *Engine) handleInit(ctx context.Context, session *Session, packet *Packet, rawPayload []byte) ([]byte, Result, error) {
	if packet.Code != CodeResponse {
		return nil, ResultIgnored, fmt.Errorf("init: expected Response, got %s", packet.Code)
	}

	session.ExpectedID = packet.ID + 1
	session.Rounds = 1
	session.State = SessionStateEapExchange
	session.LastActivity = timeNow()

	// Detect EAP method from identity response.
	if packet.Type == byte(MethodIdentity) {
		method := e.detectEapMethodFromIdentity(packet.Data)
		session.Method = method
		e.logger.Info("eap_method_detected",
			"auth_ctx_id", session.AuthCtxID,
			"method", method,
		)
	}

	// Forward to AAA-S.
	resp, err := e.forwardToAAA(ctx, session, rawPayload)
	if resp == nil {
		return nil, ResultFailure, fmt.Errorf("aaa client returned nil response")
	}
	return resp, ResultContinue, err
}

// handleExchange processes a mid-session EAP Response from AMF.
func (e *Engine) handleExchange(ctx context.Context, session *Session, packet *Packet, rawPayload []byte) ([]byte, Result, error) {
	// Validate ID matches expected.
	if packet.ID != session.ExpectedID {
		e.logger.Warn("eap_id_mismatch",
			"auth_ctx_id", session.AuthCtxID,
			"expected_id", session.ExpectedID,
			"got_id", packet.ID,
		)
		return nil, ResultIgnored, ErrEapIDMismatch
	}

	// Check round limit.
	if session.Rounds >= session.MaxRounds {
		e.logger.Warn("eap_max_rounds_exceeded",
			"auth_ctx_id", session.AuthCtxID,
			"rounds", session.Rounds,
			"max", session.MaxRounds,
		)
		session.MarkFailed()
		return nil, ResultFailure, ErrMaxRoundsExceeded
	}

	session.Rounds++
	session.ExpectedID = packet.ID + 1
	session.LastActivity = timeNow()

	// Forward to AAA-S.
	response, err := e.forwardToAAA(ctx, session, rawPayload)
	if err != nil {
		return nil, ResultFailure, err
	}
	if response == nil {
		return nil, ResultFailure, fmt.Errorf("aaa client returned nil response")
	}

	// Parse AAA response.
	respPacket, parseErr := Parse(response)
	if parseErr != nil {
		return nil, ResultFailure, parseErr
	}

	// Cache for retry detection.
	session.LastNonce = sha256Hash(rawPayload)
	session.CacheResponse(response)

	switch respPacket.Code {
	case CodeSuccess:
		session.MarkDone()
		e.logger.Info("eap_auth_success",
			"auth_ctx_id", session.AuthCtxID,
			"rounds", session.Rounds,
		)
		return response, ResultSuccess, nil

	case CodeFailure:
		session.MarkFailed()
		e.logger.Info("eap_auth_failure",
			"auth_ctx_id", session.AuthCtxID,
			"rounds", session.Rounds,
		)
		return response, ResultFailure, nil

	case CodeRequest:
		// Continue exchange.
		return response, ResultContinue, nil

	default:
		return response, ResultContinue, nil
	}
}

// handleCompleting processes a final EAP message from AMF.
func (e *Engine) handleCompleting(ctx context.Context, session *Session, packet *Packet, rawPayload []byte) ([]byte, Result, error) {
	session.Rounds++
	session.ExpectedID = packet.ID + 1
	resp, err := e.forwardToAAA(ctx, session, rawPayload)
	if resp == nil {
		return nil, ResultFailure, fmt.Errorf("aaa client returned nil response")
	}
	return resp, ResultContinue, err
}

// forwardToAAA forwards an EAP message to AAA-S and returns the response.
func (e *Engine) forwardToAAA(ctx context.Context, session *Session, eapPayload []byte) ([]byte, error) {
	if e.aaaClient == nil {
		return nil, errors.New("aaa client not configured")
	}

	e.logger.Debug("eap_forward_to_aaa",
		"auth_ctx_id", session.AuthCtxID,
		"snssai_key", session.SnssaiKey,
		"method", session.Method,
		"rounds", session.Rounds,
	)

	response, err := e.aaaClient.SendEAP(ctx, session, eapPayload)
	if err != nil {
		e.logger.Error("eap_aaa_error",
			"auth_ctx_id", session.AuthCtxID,
			"error", err,
		)
		return nil, fmt.Errorf("aaa client error: %w", err)
	}

	return response, nil
}

// detectEapMethodFromIdentity inspects the identity string to determine the EAP method.
// This is a heuristic; AAA-S may override with a NAK response.
func (e *Engine) detectEapMethodFromIdentity(data []byte) Method {
	if len(data) == 0 {
		return MethodIdentity
	}
	// Check for common EAP method prefixes in identity.
	// This is a simplified heuristic; real implementation may consult AAA config.
	return MethodTLS // Default to EAP-TLS for enterprise slices.
}

// DeleteSession removes a session from the manager.
func (e *Engine) DeleteSession(authCtxID string) {
	e.sessionManager.delete(authCtxID)
	e.logger.Debug("eap_session_deleted", "auth_ctx_id", authCtxID)
}

// Stats returns engine statistics.
func (e *Engine) Stats() EngineStats {
	return EngineStats{
		ActiveSessions:   e.sessionManager.size(),
		FragmentBuffers:  e.fragmentMgr.Size(),
		MaxRounds:        e.cfg.MaxRounds,
		RoundTimeoutSecs: int(e.cfg.RoundTimeout.Seconds()),
	}
}

// EngineStats holds operational statistics for the EAP engine.
type EngineStats struct {
	ActiveSessions   int
	FragmentBuffers  int
	MaxRounds        int
	RoundTimeoutSecs int
}

// authResultFromEapResult converts a Result to an AuthResult.
func authResultFromEapResult(r Result) types.AuthResult {
	switch r {
	case ResultSuccess:
		return types.AuthResultSuccess
	case ResultFailure:
		return types.AuthResultFailure
	case ResultContinue, ResultIgnored:
		return types.AuthResultPending
	case ResultTimeout:
		return types.AuthResultFailure
	default:
		return types.AuthResultFailure
	}
}
