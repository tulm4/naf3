// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

// Session lifecycle errors.
var (
	ErrSessionNotFound        = errors.New("eap: session not found")
	ErrSessionAlreadyDone     = errors.New("eap: session already completed")
	ErrEapIDMismatch          = errors.New("eap: ID mismatch")
	ErrMaxRoundsExceeded      = errors.New("eap: maximum rounds exceeded")
	ErrSessionTimeout         = errors.New("eap: session timeout")
	ErrInvalidStateTransition = errors.New("eap: invalid state transition")
	ErrMissingAuthCtxID       = errors.New("eap: missing auth context ID")
)

// Default session limits.
const (
	DefaultMaxRounds    = 20
	DefaultRoundTimeout = 30 * time.Second
	DefaultSessionTTL   = 10 * time.Minute
)

// Session represents the state of an ongoing EAP authentication session.
// Spec: RFC 3748 §3, TS 33.501 §16.3
type Session struct {
	// Identity (immutable)
	AuthCtxID string // NSSAAF session identifier (maps to SliceAuthContext authCtxID)
	Gpsi      string // GPSI of the subscriber
	Supi      string // SUPI of the subscriber (optional, for logging only)

	// Routing context
	SnssaiKey string // S-NSSAI key used for AAA routing

	// Current state
	State  SessionState
	Method Method

	// Counters
	Rounds    int
	MaxRounds int

	// Sequence (RFC 3748 §4)
	ExpectedID uint8 // Next expected EAP ID for incoming Response

	// Cached response for idempotent retries
	LastNonce      []byte // SHA-256 hash of last processed EAP message
	CachedResponse []byte // Raw EAP response for retry deduplication

	// Timing
	CreatedAt    time.Time
	LastActivity time.Time
	Timeout      time.Duration

	// Method-specific state (typed, not interface{} for clarity)
	TLSState *TLSSessionState
}

// SessionState represents the current state of an EAP session.
// Spec: TS 33.501 §16.3, RFC 3748 §3
type SessionState int

const (
	// SessionStateIdle means the session has been created but not yet started.
	SessionStateIdle SessionState = iota
	// SessionStateInit is the initial state after session creation.
	// SessionStateInit is the initial state after session creation.
	SessionStateInit
	// SessionStateEapExchange is the active multi-round EAP authentication phase.
	SessionStateEapExchange // multi-round EAP authentication
	// SessionStateCompleting is the final round, waiting for AAA-S response.
	SessionStateCompleting // waiting for final AAA-S response
	// SessionStateDone is the successful terminal state.
	SessionStateDone
	// SessionStateFailed is the terminal failure state.
	SessionStateFailed
	// SessionStateTimeout is the terminal timeout state.
	SessionStateTimeout
)

// String implements fmt.Stringer.
func (s SessionState) String() string {
	switch s {
	case SessionStateIdle:
		return "IDLE"
	case SessionStateInit:
		return "INIT"
	case SessionStateEapExchange:
		return "EAP_EXCHANGE"
	case SessionStateCompleting:
		return "COMPLETING"
	case SessionStateDone:
		return "DONE"
	case SessionStateFailed:
		return "FAILED"
	case SessionStateTimeout:
		return "TIMEOUT"
	default:
		return fmt.Sprintf("SessionState(%d)", s)
	}
}

// IsTerminal returns true if the session is in a terminal state.
func (s SessionState) IsTerminal() bool {
	return s == SessionStateDone || s == SessionStateFailed || s == SessionStateTimeout
}

// NewSession creates a new EAP session with default limits.
func NewSession(authCtxID, gpsi string) *Session {
	now := time.Now()
	return &Session{
		AuthCtxID:    authCtxID,
		Gpsi:         gpsi,
		State:        SessionStateInit,
		MaxRounds:    DefaultMaxRounds,
		Timeout:      DefaultRoundTimeout,
		ExpectedID:   0, // Will be set on first Response
		CreatedAt:    now,
		LastActivity: now,
	}
}

// WithSnssai configures the S-NSSAI routing key.
func (s *Session) WithSnssai(snssaiKey string) *Session {
	s.SnssaiKey = snssaiKey
	return s
}

// WithMaxRounds sets the maximum number of EAP rounds.
func (s *Session) WithMaxRounds(n int) *Session {
	s.MaxRounds = n
	return s
}

// WithTimeout sets the per-round timeout.
func (s *Session) WithTimeout(d time.Duration) *Session {
	s.Timeout = d
	return s
}

// AdvanceToExchange transitions the session to the EAP exchange phase.
func (s *Session) AdvanceToExchange() error {
	if s.State != SessionStateInit && s.State != SessionStateIdle {
		return fmt.Errorf("%w: cannot advance from %s", ErrInvalidStateTransition, s.State)
	}
	s.State = SessionStateEapExchange
	s.Rounds = 1
	s.LastActivity = time.Now()
	return nil
}

// RecordResponse records an incoming Response and updates expected ID.
func (s *Session) RecordResponse(id uint8, payload []byte) {
	s.ExpectedID = id + 1
	s.Rounds++
	s.LastActivity = time.Now()
	h := sha256.Sum256(payload)
	s.LastNonce = h[:]
}

// CacheResponse stores the response for idempotent retry handling.
func (s *Session) CacheResponse(response []byte) {
	s.CachedResponse = response
}

// MarkDone transitions the session to a terminal state.
func (s *Session) MarkDone() {
	s.State = SessionStateDone
	s.LastActivity = time.Now()
}

// MarkFailed transitions the session to failed state.
func (s *Session) MarkFailed() {
	s.State = SessionStateFailed
	s.LastActivity = time.Now()
}

// MarkTimeout transitions the session to timeout state.
func (s *Session) MarkTimeout() {
	s.State = SessionStateTimeout
	s.LastActivity = time.Now()
}

// IsExpired returns true if the session has exceeded its TTL.
func (s *Session) IsExpired(ttl time.Duration) bool {
	return time.Since(s.CreatedAt) > ttl
}

// IsTimedOut returns true if no activity has occurred within the round timeout.
func (s *Session) IsTimedOut() bool {
	return time.Since(s.LastActivity) > s.Timeout
}

// String implements fmt.Stringer.
func (s *Session) String() string {
	return fmt.Sprintf("EapSession{AuthCtxID=%s, State=%s, Method=%s, Rounds=%d/%d}",
		s.AuthCtxID, s.State, s.Method, s.Rounds, s.MaxRounds)
}

// TLSSessionState holds EAP-TLS specific state.
// Spec: RFC 5216 §2
type TLSSessionState struct {
	// TLS version negotiated
	TLSVersion uint16 // 0x0303 = TLS 1.2, 0x0304 = TLS 1.3

	// Handshake status
	HandshakeComplete bool

	// Client random
	ClientRandom []byte

	// Server certificate (raw DER)
	ServerCertificate []byte

	// Server certificate verified (after successful verification)
	ServerCertVerified bool

	// MSK derived from TLS master secret (RFC 5216 §2.1.4)
	// MSK[0:31] = EMSK (part 1), MSK[32:63] = MSK (part 2)
	MSK []byte

	// Flags from most recent EAP-TLS packet
	Flags TLSFlags

	// Accumulated TLS data across fragments
	TLSData []byte
}

// NewTLSSessionState creates a new TLS session state.
func NewTLSSessionState() *TLSSessionState {
	return &TLSSessionState{}
}

// AppendTLSData appends TLS data to the fragment buffer.
func (s *TLSSessionState) AppendTLSData(data []byte) {
	s.TLSData = append(s.TLSData, data...)
}

// ResetTLSData clears the accumulated TLS data.
func (s *TLSSessionState) ResetTLSData() {
	s.TLSData = nil
}
