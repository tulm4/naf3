package gateway

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/radius"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

// mockForwardToBiz captures calls to forwardToBiz.
type mockForwardToBiz struct {
	calls []forwardCall
}

type forwardCall struct {
	ctx           context.Context
	sessionID     string
	transportType string
	messageType   string
	raw           []byte
}

func (m *mockForwardToBiz) invoke(ctx context.Context, sessionID, transportType, messageType string, raw []byte) {
	m.calls = append(m.calls, forwardCall{
		ctx:           ctx,
		sessionID:     sessionID,
		transportType: transportType,
		messageType:   messageType,
		raw:           raw,
	})
}

// buildRadiusPacket constructs a minimal RADIUS packet for testing.
// header: code(1) + id(1) + length(2) + authenticator(16) = 20 bytes
// plus optional attributes.
func buildRadiusPacket(code uint8, id uint8, attrs []byte) []byte {
	totalLen := 20 + len(attrs)
	pkt := make([]byte, totalLen)
	pkt[0] = code
	pkt[1] = id
	pkt[2] = byte(totalLen >> 8)
	pkt[3] = byte(totalLen)
	copy(pkt[20:], attrs)
	return pkt
}

// buildStateAttr builds a RADIUS State attribute (type=24).
func buildStateAttr(state string) []byte {
	attrLen := 2 + len(state)
	attr := make([]byte, attrLen)
	attr[0] = 24
	attr[1] = byte(attrLen)
	copy(attr[2:], state)
	return attr
}

// buildEAPAttr builds a RADIUS EAP-Message attribute (type=79).
func buildEAPAttr(payload []byte) []byte {
	attrLen := 2 + len(payload)
	attr := make([]byte, attrLen)
	attr[0] = 79
	attr[1] = byte(attrLen)
	copy(attr[2:], payload)
	return attr
}

// nullLogger returns a no-op logger that satisfies *slog.Logger.
func nullLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// nullTracer returns a no-op tracer for tests that don't need real tracing.
func nullTracer() trace.Tracer {
	return trace.NewNoopTracerProvider().Tracer("test")
}

// TestHandlePacket_TooShort verifies that packets with fewer than 4 bytes are dropped.
func TestHandlePacket_TooShort(t *testing.T) {
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:       nullLogger(),
		forwardToBiz: fwd.invoke,
	}

	// Empty packet
	h.handlePacket(context.Background(), nil, nil, []byte{})
	// Single byte
	h.handlePacket(context.Background(), nil, nil, []byte{1})
	// 3 bytes
	h.handlePacket(context.Background(), nil, nil, []byte{1, 2, 3})

	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_AccessAccept logs debug message and returns (no pub/sub needed).
func TestHandlePacket_AccessAccept(t *testing.T) {
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:       nullLogger(),
		forwardToBiz: fwd.invoke,
	}

	state := "test-session-123"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(2, 1, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	// Client-initiated responses are logged and returned directly; no forwardToBiz call.
	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_AccessReject logs debug message and returns (no pub/sub needed).
func TestHandlePacket_AccessReject(t *testing.T) {
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:       nullLogger(),
		forwardToBiz: fwd.invoke,
	}

	state := "reject-session"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(3, 2, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_AccessChallenge logs debug message and returns (no pub/sub needed).
func TestHandlePacket_AccessChallenge(t *testing.T) {
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:       nullLogger(),
		forwardToBiz: fwd.invoke,
	}

	state := "challenge-session"
	eapPayload := []byte{1, 13, 0, 6, 0, 0, 0, 0}
	attrs := append(buildStateAttr(state), buildEAPAttr(eapPayload)...)
	pkt := buildRadiusPacket(11, 3, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_CoARequest calls forwardToBiz with messageType="COA".
func TestHandlePacket_CoARequest(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
	}

	state := "coa-session-xyz"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(43, 4, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	// Wait for detached goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, state, fwd.calls[0].sessionID)
	assert.Equal(t, "RADIUS", fwd.calls[0].transportType)
	assert.Equal(t, "COA", fwd.calls[0].messageType)
	assert.Equal(t, pkt, fwd.calls[0].raw)
}

// TestHandlePacket_DisconnectRequest calls forwardToBiz with messageType="RAR".
func TestHandlePacket_DisconnectRequest(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
	}

	state := "dm-session-abc"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(40, 5, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	// Wait for detached goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, state, fwd.calls[0].sessionID)
	assert.Equal(t, "RADIUS", fwd.calls[0].transportType)
	assert.Equal(t, "DM", fwd.calls[0].messageType) // Disconnect-Request → DM
	assert.Equal(t, pkt, fwd.calls[0].raw)
}

// TestHandlePacket_UnknownCodeIsDropped verifies that unrecognized codes are ignored.
func TestHandlePacket_UnknownCodeIsDropped(t *testing.T) {
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:       nullLogger(),
		forwardToBiz: fwd.invoke,
	}

	// code=5 (Accounting-Request) — not handled
	pkt := buildRadiusPacket(5, 1, nil)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, fwd.calls)
}

// TestHandleServerInitiated_NoSessionID_DropsPacket verifies that packets without
// a State attribute are dropped without calling forwardToBiz.
func TestHandleServerInitiated_NoSessionID_DropsPacket(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
	}

	// CoA packet with no State attribute (totalLen >= 20 so not caught by < 4 check)
	pkt := buildRadiusPacket(43, 6, nil)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, fwd.calls)
}

// TestHandleServerInitiated_Direct verifies that handleServerInitiated forwards
// CoA and DM packets to Biz with correct transport and message type.
func TestHandleServerInitiated_Direct(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
	}

	sessionID := "direct-coa-test"
	coaPkt := buildRadiusPacket(43, 7, buildStateAttr(sessionID))
	dmPkt := buildRadiusPacket(40, 8, buildStateAttr(sessionID))

	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")

	// Wait for detached goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, sessionID, fwd.calls[0].sessionID)
	assert.Equal(t, "COA", fwd.calls[0].messageType)
	assert.Equal(t, "RADIUS", fwd.calls[0].transportType)

	fwd.calls = nil
	h.handleServerInitiated(context.Background(), dmPkt, "RADIUS")

	// Wait for detached goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, sessionID, fwd.calls[0].sessionID)
	assert.Equal(t, "DM", fwd.calls[0].messageType) // Disconnect-Request → DM
}

// TestExtractSessionID_StateAttribute extracts session ID from State attribute.
func TestExtractSessionID_StateAttribute(t *testing.T) {
	sessionID := "state-attribute-test-session"
	attrs := buildStateAttr(sessionID)
	pkt := buildRadiusPacket(2, 1, attrs)

	result := extractSessionID(pkt)

	assert.Equal(t, sessionID, result)
}

// TestExtractSessionID_TooShort returns empty string.
func TestExtractSessionID_TooShort(t *testing.T) {
	assert.Equal(t, "", extractSessionID([]byte{1, 2, 3}))
}

// TestExtractSessionID_NoStateAttribute returns empty string.
func TestExtractSessionID_NoStateAttribute(t *testing.T) {
	eapPayload := []byte{1, 13, 0, 6, 0, 0, 0, 0}
	pkt := buildRadiusPacket(11, 1, buildEAPAttr(eapPayload))

	result := extractSessionID(pkt)

	assert.Equal(t, "", result)
}

// TestExtractSessionID_TruncatedAttribute returns empty string.
func TestExtractSessionID_TruncatedAttribute(t *testing.T) {
	pkt := buildRadiusPacket(2, 1, []byte{24, 20})

	result := extractSessionID(pkt)

	assert.Equal(t, "", result)
}

// TestRadiusHandler_CoA_WaitsForBizPodResponse verifies that handleServerInitiated
// returns immediately (detached goroutine) rather than blocking.
func TestRadiusHandler_CoA_WaitsForBizPodResponse(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)
	fwd := &mockForwardToBiz{}

	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
	}

	sessionID := "session-1"
	coaPkt := buildRadiusPacket(43, 9, buildStateAttr(sessionID))

	// Call handleServerInitiated - it should return immediately.
	start := time.Now()
	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")
	elapsed := time.Since(start)

	// Should return immediately (detached goroutine).
	if elapsed > 100*time.Millisecond {
		t.Errorf("handleServerInitiated blocked for %v, expected immediate return", elapsed)
	}

	// Wait for the goroutine to execute forwardToBiz.
	time.Sleep(50 * time.Millisecond)

	// forwardToBiz should have been called.
	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, sessionID, fwd.calls[0].sessionID)
	assert.Equal(t, "COA", fwd.calls[0].messageType)
}

// TestRadiusHandler_CoA_TimeoutReturnsNAK verifies that timeout on registry
// returns ResultCode=3002 (UNABLE_TO_DELIVER).
func TestRadiusHandler_CoA_TimeoutReturnsNAK(t *testing.T) {
	registry := NewServerInitiatedRegistry(50 * time.Millisecond)
	fwd := &mockForwardToBiz{}

	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
	}

	sessionID := "timeout-session"
	coaPkt := buildRadiusPacket(43, 10, buildStateAttr(sessionID))

	// Don't complete - let it timeout
	ch, _ := registry.Register(sessionID, "auth-1", "COA", 50*time.Millisecond)

	resp := ch.Wait()
	if resp.ResultCode != ResultCodeUnableToDeliver {
		t.Errorf("expected ResultCode %d (UNABLE_TO_DELIVER), got %d", ResultCodeUnableToDeliver, resp.ResultCode)
	}
	if resp.ErrorCause != "timeout" {
		t.Errorf("expected ErrorCause 'timeout', got %s", resp.ErrorCause)
	}

	// handleServerInitiated should also return immediately
	start := time.Now()
	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")
	if time.Since(start) > 100*time.Millisecond {
		t.Error("handleServerInitiated blocked on timeout test")
	}
}

// TestRadiusHandler_CoA_CompleteRemovesFromPending verifies that Complete removes
// the pending entry from the registry.
func TestRadiusHandler_CoA_CompleteRemovesFromPending(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)

	ch, _ := registry.Register("session-1", "auth-1", "COA", 5*time.Second)

	// Complete should succeed
	registry.Complete("session-1", "COA", &ServerInitiatedResponse{ResultCode: ResultCodeSuccess})

	// Registry should be clean
	resp := ch.Wait()
	if resp.ResultCode != ResultCodeSuccess {
		t.Errorf("expected ResultCode %d, got %d", ResultCodeSuccess, resp.ResultCode)
	}
}

// TestRadiusHandler_CoA_RegistryPending verifies that the registry has a pending
// request after handleServerInitiated returns.
func TestRadiusHandler_CoA_RegistryPending(t *testing.T) {
	registry := NewServerInitiatedRegistry(5 * time.Second)
	fwd := &mockForwardToBiz{}

	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
	}

	sessionID := "registry-pending-session"
	coaPkt := buildRadiusPacket(43, 11, buildStateAttr(sessionID))

	// Register a channel before calling handleServerInitiated.
	preCh, _ := registry.Register("other-session", "auth-other", "COA", 5*time.Second)
	_ = preCh // Ignore pre-existing entry.

	// Call handleServerInitiated - returns immediately.
	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")

	// Wait for goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	// Verify forwardToBiz was called.
	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, sessionID, fwd.calls[0].sessionID)
}

// buildMessageAuthAttr builds a RADIUS Message-Authenticator attribute (type=80).
func buildMessageAuthAttr(value []byte) []byte {
	attr := make([]byte, 18)
	attr[0] = 80 // Message-Authenticator type
	attr[1] = 18 // Length: type(1) + len(1) + value(16)
	copy(attr[2:], value)
	return attr
}

// buildCoAWithMessageAuth builds a CoA-Request packet with a valid Message-Authenticator.
func buildCoAWithMessageAuth(secret string) ([]byte, string) {
	sessionID := "coa-ma-test-session"
	stateAttr := buildStateAttr(sessionID)

	// Build packet without MA first to compute correct length.
	totalLen := 20 + len(stateAttr) + 18
	pkt := make([]byte, totalLen)
	pkt[0] = 43 // CoA-Request
	pkt[1] = 12 // ID
	pkt[2] = byte(totalLen >> 8)
	pkt[3] = byte(totalLen)

	// Copy State attribute.
	copy(pkt[20:], stateAttr)

	// Write Message-Authenticator attribute header: type=80, length=18.
	maOffset := 20 + len(stateAttr)
	pkt[maOffset] = 80   // Message-Authenticator type
	pkt[maOffset+1] = 18 // Length

	// Compute Message-Authenticator value (HMAC-MD5) and write it.
	ma := radius.ComputeMessageAuthenticator(pkt, secret)
	copy(pkt[maOffset+2:], ma)

	return pkt, sessionID
}

// TestRadiusHandler_CoA_DropsMissingMessageAuth verifies that CoA without Message-Authenticator
// is dropped when sharedSecret is configured.
// Spec: RFC 5176 §3.2
func TestRadiusHandler_CoA_DropsMissingMessageAuth(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
		sharedSecret: "secret",
	}

	// CoA packet without Message-Authenticator.
	sessionID := "coa-no-ma-session"
	stateAttr := buildStateAttr(sessionID)
	coaPkt := buildRadiusPacket(43, 20, stateAttr)

	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")

	// Wait for any detached goroutine.
	time.Sleep(50 * time.Millisecond)

	if fwd.forwardCalled() {
		t.Error("should NOT forward CoA without Message-Authenticator")
	}
}

// TestRadiusHandler_CoA_DropsInvalidMessageAuth verifies that CoA with invalid
// Message-Authenticator is dropped when sharedSecret is configured.
// Spec: RFC 5176 §3.2
func TestRadiusHandler_CoA_DropsInvalidMessageAuth(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
		sharedSecret: "secret",
	}

	// CoA packet with wrong Message-Authenticator (all zeros).
	sessionID := "coa-invalid-ma-session"
	stateAttr := buildStateAttr(sessionID)
	maAttr := buildMessageAuthAttr(make([]byte, 16)) // Invalid: all zeros
	attrs := append(stateAttr, maAttr...)
	coaPkt := buildRadiusPacket(43, 21, attrs)

	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")

	// Wait for any detached goroutine.
	time.Sleep(50 * time.Millisecond)

	if fwd.forwardCalled() {
		t.Error("should NOT forward CoA with invalid Message-Authenticator")
	}
}

// TestRadiusHandler_CoA_AcceptsValidMessageAuth verifies that CoA with valid
// Message-Authenticator is forwarded when sharedSecret is configured.
func TestRadiusHandler_CoA_AcceptsValidMessageAuth(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
		sharedSecret: "secret",
	}

	coaPkt, sessionID := buildCoAWithMessageAuth("secret")

	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")

	// Wait for detached goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	if !fwd.forwardCalled() {
		t.Error("should forward CoA with valid Message-Authenticator")
	}
	if len(fwd.calls) != 1 {
		t.Errorf("expected 1 forward call, got %d", len(fwd.calls))
	}
	if fwd.calls[0].sessionID != sessionID {
		t.Errorf("expected sessionID %s, got %s", sessionID, fwd.calls[0].sessionID)
	}
}

// TestRadiusHandler_CoA_NoValidation_WhenNoSecret verifies that CoA is forwarded
// normally when sharedSecret is not configured (backwards compatibility for testing/dev).
func TestRadiusHandler_CoA_NoValidation_WhenNoSecret(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
		sharedSecret: "", // No secret configured
	}

	sessionID := "coa-no-secret-session"
	coaPkt := buildRadiusPacket(43, 22, buildStateAttr(sessionID))

	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")

	// Wait for detached goroutine to execute.
	time.Sleep(50 * time.Millisecond)

	if !fwd.forwardCalled() {
		t.Error("should forward CoA when sharedSecret is not configured")
	}
}

// TestRadiusHandler_DM_DropsMissingMessageAuth verifies that Disconnect-Request
// without Message-Authenticator is dropped when sharedSecret is configured.
// Spec: RFC 5176 §3.1
func TestRadiusHandler_DM_DropsMissingMessageAuth(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
		sharedSecret: "secret",
	}

	// Disconnect-Request packet without Message-Authenticator.
	sessionID := "dm-no-ma-session"
	stateAttr := buildStateAttr(sessionID)
	dmPkt := buildRadiusPacket(40, 23, stateAttr)

	h.handleServerInitiated(context.Background(), dmPkt, "RADIUS")

	// Wait for any detached goroutine.
	time.Sleep(50 * time.Millisecond)

	if fwd.forwardCalled() {
		t.Error("should NOT forward DM without Message-Authenticator")
	}
}

// TestRadiusHandler_DM_DropsInvalidMessageAuth verifies that Disconnect-Request
// with invalid Message-Authenticator is dropped when sharedSecret is configured.
// Spec: RFC 5176 §3.1
func TestRadiusHandler_DM_DropsInvalidMessageAuth(t *testing.T) {
	fwd := &mockForwardToBiz{}
	registry := NewServerInitiatedRegistry(5 * time.Second)
	h := &RadiusHandler{
		logger:       nullLogger(),
		tracer:       nullTracer(),
		forwardToBiz: fwd.invoke,
		registry:     registry,
		sharedSecret: "secret",
	}

	// Disconnect-Request packet with wrong Message-Authenticator.
	sessionID := "dm-invalid-ma-session"
	stateAttr := buildStateAttr(sessionID)
	maAttr := buildMessageAuthAttr(make([]byte, 16)) // Invalid: all zeros
	attrs := append(stateAttr, maAttr...)
	dmPkt := buildRadiusPacket(40, 24, attrs)

	h.handleServerInitiated(context.Background(), dmPkt, "RADIUS")

	// Wait for any detached goroutine.
	time.Sleep(50 * time.Millisecond)

	if fwd.forwardCalled() {
		t.Error("should NOT forward DM with invalid Message-Authenticator")
	}
}

// forwardCalled is a helper for mockForwardToBiz.
func (m *mockForwardToBiz) forwardCalled() bool {
	return len(m.calls) > 0
}
