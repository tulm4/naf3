// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/types"
)

// ---------------------------------------------------------------------------
// Mock AAA Client
// ---------------------------------------------------------------------------

// mockAAAClient is a mock implementation of AAAClient for testing.
type mockAAAClient struct {
	mu         sync.Mutex
	responses  map[string][]byte // authCtxID → response bytes
	eapPayload []byte            // last received payload
	callCount  atomic.Int32

	// Configurable behavior.
	failNext     bool
	failNextErr  error
	delay        time.Duration
	responseCode uint8 // EAP code to return (Success=3, Failure=4, Request=1)
}

func newMockAAAClient() *mockAAAClient {
	return &mockAAAClient{
		responses:    make(map[string][]byte),
		responseCode: 1, // default: EAP-Request (continue)
	}
}

func (m *mockAAAClient) SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error) {
	m.mu.Lock()
	m.eapPayload = eapPayload
	m.callCount.Add(1)

	if m.failNext {
		m.failNext = false
		m.mu.Unlock()
		return nil, m.failNextErr
	}

	delay := m.delay
	responses := m.responses
	defaultResp := BuildRequest(1, EapMethodIdentity, []byte("server-challenge")).RawData

	// Release the lock before sleeping so other goroutines aren't blocked.
	m.mu.Unlock()

	// Use select to respect context cancellation even while sleeping.
	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			// delay elapsed; proceed.
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if resp, ok := responses[authCtxID]; ok {
		return resp, nil
	}
	return defaultResp, nil
}

func (m *mockAAAClient) SetResponse(authCtxID string, response []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[authCtxID] = response
}

func (m *mockAAAClient) SetNextFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = true
	m.failNextErr = err
}

func (m *mockAAAClient) SetResponseCode(code uint8) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responseCode = code
}

func (m *mockAAAClient) LastPayload() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.eapPayload
}

func (m *mockAAAClient) CallCount() int {
	return int(m.callCount.Load())
}

// ---------------------------------------------------------------------------
// Engine Construction
// ---------------------------------------------------------------------------

func TestNewEngine(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	cfg := DefaultConfig()

	engine := NewEngine(cfg, mock, logger)
	assert.NotNil(t, engine)
	assert.NotNil(t, engine.sessionManager)
	assert.NotNil(t, engine.fragmentMgr)
}

func TestNewEngineDefaults(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()

	engine := NewEngine(Config{}, mock, logger)
	assert.NotNil(t, engine)
	assert.Equal(t, DefaultMaxRounds, engine.cfg.MaxRounds)
	assert.Equal(t, DefaultRoundTimeout, engine.cfg.RoundTimeout)
	assert.Equal(t, DefaultSessionTTL, engine.cfg.SessionTTL)
	assert.Equal(t, int64(60), engine.cfg.FragmentTTLSeconds)
}

func TestNewEngineZeroFragmentTTL(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()

	cfg := Config{FragmentTTLSeconds: 0}
	engine := NewEngine(cfg, mock, logger)
	assert.Equal(t, int64(60), engine.cfg.FragmentTTLSeconds)
}

func TestSetTLSConfig(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	engine.SetTLSConfig(tlsCfg)
	assert.NotNil(t, engine.tlsConfig)
}

// ---------------------------------------------------------------------------
// StartSession
// ---------------------------------------------------------------------------

func TestStartSession(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	session, err := engine.StartSession("auth-001", "user@example.com")
	require.NoError(t, err)
	assert.NotNil(t, session)
	assert.Equal(t, "auth-001", session.AuthCtxID)
	assert.Equal(t, "user@example.com", session.Gpsi)
	assert.Equal(t, SessionStateInit, session.State)
	assert.Equal(t, DefaultMaxRounds, session.MaxRounds)
	assert.Equal(t, DefaultRoundTimeout, session.Timeout)
}

func TestStartSessionWithCustomConfig(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()

	cfg := Config{
		MaxRounds:    10,
		RoundTimeout: 5 * time.Second,
		SessionTTL:   20 * time.Minute,
	}
	engine := NewEngine(cfg, mock, logger)

	session, err := engine.StartSession("auth-002", "user2@test")
	require.NoError(t, err)
	assert.Equal(t, 10, session.MaxRounds)
	assert.Equal(t, 5*time.Second, session.Timeout)
}

func TestStartSessionEmptyAuthCtxId(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	session, err := engine.StartSession("", "user@test")
	assert.Nil(t, session)
	assert.ErrorIs(t, err, ErrMissingAuthCtxID)
}

// ---------------------------------------------------------------------------
// GetSession
// ---------------------------------------------------------------------------

func TestGetSession(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	created, err := engine.StartSession("auth-get", "user@test")
	require.NoError(t, err)

	got, err := engine.GetSession("auth-get")
	require.NoError(t, err)
	assert.Equal(t, created, got)
}

func TestGetSessionNotFound(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.GetSession("nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

// ---------------------------------------------------------------------------
// Process — Init flow
// ---------------------------------------------------------------------------

func TestProcessFirstResponse(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	// First: start session.
	_, err := engine.StartSession("auth-proc", "user@test")
	require.NoError(t, err)

	// Build EAP-Response/Identity from AMF.
	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	eapData := eapResp.RawData

	// Process.
	eapMsg, result, err := engine.Process(context.Background(), "auth-proc", eapData)
	require.NoError(t, err)
	assert.Equal(t, types.AuthResultPending, result)
	assert.NotNil(t, eapMsg)

	// AAA client was called.
	assert.Equal(t, 1, mock.CallCount())
	assert.Equal(t, eapData, mock.LastPayload())
}

func TestProcessNoSession(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, _, err := engine.Process(context.Background(), "nonexistent", []byte{0x02, 0x01, 0x00, 0x05, 0x01, 0x41})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Process — Timeout detection
// ---------------------------------------------------------------------------

func TestProcessSessionTimeout(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()

	cfg := Config{
		MaxRounds:    20,
		RoundTimeout: 10 * time.Millisecond,
		SessionTTL:   1 * time.Hour,
	}
	engine := NewEngine(cfg, mock, logger)

	_, err := engine.StartSession("auth-timeout", "user@test")
	require.NoError(t, err)

	// Wait for idle timeout.
	time.Sleep(20 * time.Millisecond)

	// Process should detect timeout.
	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, err = engine.Process(context.Background(), "auth-timeout", eapResp.RawData)
	assert.ErrorIs(t, err, ErrSessionTimeout)
}

// ---------------------------------------------------------------------------
// Process — Retry (idempotent)
// ---------------------------------------------------------------------------

func TestProcessRetry(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-retry", "user@test")
	require.NoError(t, err)

	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	eapData := eapResp.RawData

	// First call.
	_, _, err = engine.Process(context.Background(), "auth-retry", eapData)
	require.NoError(t, err)
	require.Equal(t, 1, mock.CallCount())

	// Same payload again → retry → no new call to AAA.
	_, result, err := engine.Process(context.Background(), "auth-retry", eapData)
	require.NoError(t, err)
	assert.Equal(t, types.AuthResultPending, result)
	assert.Equal(t, 1, mock.CallCount()) // still 1
}

// ---------------------------------------------------------------------------
// Process — EAP Success
// ---------------------------------------------------------------------------

func TestProcessEAPSuccess(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-success", "user@test")
	require.NoError(t, err)

	// Set up: first response triggers a second exchange, second response is Success.
	mock.SetResponse("auth-success", BuildRequest(2, EapMethodTLS, []byte{0x00}).RawData)
	mock.SetResponse("auth-success", BuildSuccess(2).RawData)

	eapResp1 := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, err = engine.Process(context.Background(), "auth-success", eapResp1.RawData)
	require.NoError(t, err)

	eapResp2 := BuildResponse(2, EapMethodTLS, []byte{0x01, 0x02})
	eapMsg, result, err := engine.Process(context.Background(), "auth-success", eapResp2.RawData)
	require.NoError(t, err)
	assert.Equal(t, types.AuthResultSuccess, result)
	assert.NotNil(t, eapMsg)
}

// ---------------------------------------------------------------------------
// Process — EAP Failure
// ---------------------------------------------------------------------------

func TestProcessEAPFailure(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-fail", "user@test")
	require.NoError(t, err)

	// AAA returns EAP-Failure packet. The engine forwards it to AMF.
	// Note: the engine returns AuthResultPending (not AuthResultFailure) because
	// the Failure packet is a valid response that must be delivered to AMF.
	// AMF translates the EAP-Failure into the final NSSAA result.
	mock.SetResponse("auth-fail", BuildFailure(1).RawData)

	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	eapMsg, result, err := engine.Process(context.Background(), "auth-fail", eapResp.RawData)
	require.NoError(t, err)
	assert.Equal(t, types.AuthResultPending, result)
	assert.NotNil(t, eapMsg) // the EAP-Failure packet is returned to AMF

	// Verify the returned packet is actually an EAP-Failure.
	eapBytes, decodeErr := eapMsg.Bytes()
	require.NoError(t, decodeErr, "eapMsg should contain valid base64")
	eapPkt, parseErr := Parse(eapBytes)
	require.NoError(t, parseErr)
	assert.Equal(t, CodeFailure, eapPkt.Code)
}

// ---------------------------------------------------------------------------
// Process — AAA client error
// ---------------------------------------------------------------------------

func TestProcessAAAError(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-aaa-err", "user@test")
	require.NoError(t, err)

	// Configure AAA to fail on next call.
	mock.SetNextFailure(errors.New("AAA connection refused"))

	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, err = engine.Process(context.Background(), "auth-aaa-err", eapResp.RawData)
	assert.Error(t, err)
}

// Regression: forwardToAAA returning nil must not cause a panic in handleExchange.
func TestProcessNilResponseFromAAA(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-nil-resp", "user@test")
	require.NoError(t, err)

	// Set AAA to return a nil response.
	mock.SetResponse("auth-nil-resp", nil)

	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, processErr := engine.Process(context.Background(), "auth-nil-resp", eapResp.RawData)
	// Should return an error, not nil. The old code would panic on nil dereference.
	require.Error(t, processErr, "nil response should produce an error, not panic")
}

// ---------------------------------------------------------------------------
// Process — Context cancellation
// ---------------------------------------------------------------------------

func TestProcessContextCancelled(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	mock.delay = 500 * time.Millisecond

	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-cancel", "user@test")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, err = engine.Process(ctx, "auth-cancel", eapResp.RawData)
	assert.Error(t, err) // either timeout or context error
}

// ---------------------------------------------------------------------------
// Process — Parse error
// ---------------------------------------------------------------------------

func TestProcessParseError(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-parse-err", "user@test")
	require.NoError(t, err)

	// Invalid EAP packet (too short).
	_, _, err = engine.Process(context.Background(), "auth-parse-err", []byte{0x01})
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Process — Max rounds exceeded
// ---------------------------------------------------------------------------

func TestProcessMaxRoundsExceeded(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()

	cfg := Config{MaxRounds: 3}
	engine := NewEngine(cfg, mock, logger)

	_, err := engine.StartSession("auth-max-rounds", "user@test")
	require.NoError(t, err)

	// Keep sending responses until max rounds.
	for i := uint8(1); i <= 4; i++ {
		eapResp := BuildResponse(i, EapMethodTLS, []byte{0x01})
		_, _, _ = engine.Process(context.Background(), "auth-max-rounds", eapResp.RawData)
	}

	// After max rounds, session should be failed.
	session, err := engine.GetSession("auth-max-rounds")
	require.NoError(t, err)
	assert.Equal(t, SessionStateFailed, session.State)
}

// ---------------------------------------------------------------------------
// Process — ID mismatch
// ---------------------------------------------------------------------------

func TestProcessIdMismatch(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-id-mismatch", "user@test")
	require.NoError(t, err)

	// First response sets ExpectedId=2.
	eapResp1 := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, err = engine.Process(context.Background(), "auth-id-mismatch", eapResp1.RawData)
	require.NoError(t, err)

	// Second response with Id=5 (not 2).
	eapResp2 := BuildResponse(5, EapMethodTLS, []byte{0x01})
	_, _, err = engine.Process(context.Background(), "auth-id-mismatch", eapResp2.RawData)
	assert.ErrorIs(t, err, ErrEapIDMismatch)
}

// ---------------------------------------------------------------------------
// Process — Already done / failed sessions
// ---------------------------------------------------------------------------

func TestProcessDoneSession(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	session, err := engine.StartSession("auth-done", "user@test")
	require.NoError(t, err)
	session.MarkDone()

	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, err = engine.Process(context.Background(), "auth-done", eapResp.RawData)
	assert.ErrorIs(t, err, ErrSessionAlreadyDone)
}

func TestProcessFailedSession(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	session, err := engine.StartSession("auth-failed", "user@test")
	require.NoError(t, err)
	session.MarkFailed()

	eapResp := BuildResponse(1, EapMethodIdentity, []byte("user@test"))
	_, _, err = engine.Process(context.Background(), "auth-failed", eapResp.RawData)
	assert.ErrorIs(t, err, ErrSessionAlreadyDone)
}

// ---------------------------------------------------------------------------
// DeleteSession
// ---------------------------------------------------------------------------

func TestDeleteSession(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	_, err := engine.StartSession("auth-del", "user@test")
	require.NoError(t, err)

	engine.DeleteSession("auth-del")

	_, err = engine.GetSession("auth-del")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestDeleteSessionNotFound(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	// Should not panic.
	engine.DeleteSession("nonexistent")
}

// ---------------------------------------------------------------------------
// Engine Stats
// ---------------------------------------------------------------------------

func TestEngineStats(t *testing.T) {
	logger := slog.Default()
	mock := newMockAAAClient()
	engine := NewEngine(DefaultConfig(), mock, logger)

	stats := engine.Stats()
	assert.Equal(t, 0, stats.ActiveSessions)
	assert.Equal(t, 0, stats.FragmentBuffers)
	assert.Equal(t, DefaultMaxRounds, stats.MaxRounds)
	assert.Equal(t, 30, stats.RoundTimeoutSecs)

	// Start a session.
	_, _ = engine.StartSession("auth-stats", "user@test")
	stats = engine.Stats()
	assert.Equal(t, 1, stats.ActiveSessions)
}

// ---------------------------------------------------------------------------
// Session Manager Stats
// ---------------------------------------------------------------------------

func TestSessionManagerStats(t *testing.T) {
	mgr := newSessionManager(5 * time.Minute)
	stats := mgr.stats()

	assert.Equal(t, 0, stats.ActiveSessions)
	assert.Equal(t, 5*time.Minute, stats.TTL)

	mgr.put(NewSession("auth-1", "user1@test"))
	mgr.put(NewSession("auth-2", "user2@test"))
	stats = mgr.stats()
	assert.Equal(t, 2, stats.ActiveSessions)
}

// ---------------------------------------------------------------------------
// DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 20, cfg.MaxRounds)
	assert.Equal(t, 30*time.Second, cfg.RoundTimeout)
	assert.Equal(t, 10*time.Minute, cfg.SessionTTL)
	assert.Equal(t, int64(60), cfg.FragmentTTLSeconds)
	assert.Equal(t, 1024, cfg.TLSSessionCacheSize)
}

// ---------------------------------------------------------------------------
// DefaultLogger
// ---------------------------------------------------------------------------

func TestDefaultLogger(t *testing.T) {
	logger := slog.Default()
	dl := &defaultLogger{Logger: logger}

	// Should not panic.
	dl.Debug("debug msg")
	dl.Info("info msg")
	dl.Warn("warn msg")
	dl.Error("error msg")
}
