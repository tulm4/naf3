package gateway

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

// mockPublishResponse captures calls to publishResponse.
type mockPublishResponse struct {
	calls []publishCall
}

type publishCall struct {
	sessionID string
	raw       []byte
}

func (m *mockPublishResponse) invoke(sessionID string, raw []byte) {
	m.calls = append(m.calls, publishCall{sessionID: sessionID, raw: raw})
}

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
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	// Empty packet
	h.handlePacket(context.Background(), nil, nil, []byte{})
	// Single byte
	h.handlePacket(context.Background(), nil, nil, []byte{1})
	// 3 bytes
	h.handlePacket(context.Background(), nil, nil, []byte{1, 2, 3})

	assert.Empty(t, pub.calls)
	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_AccessAccept calls publishResponse.
func TestHandlePacket_AccessAccept(t *testing.T) {
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	state := "test-session-123"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(2, 1, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Len(t, pub.calls, 1)
	assert.Equal(t, state, pub.calls[0].sessionID)
	assert.Equal(t, pkt, pub.calls[0].raw)
	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_AccessReject calls publishResponse.
func TestHandlePacket_AccessReject(t *testing.T) {
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	state := "reject-session"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(3, 2, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Len(t, pub.calls, 1)
	assert.Equal(t, state, pub.calls[0].sessionID)
	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_AccessChallenge calls publishResponse.
func TestHandlePacket_AccessChallenge(t *testing.T) {
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	state := "challenge-session"
	eapPayload := []byte{1, 13, 0, 6, 0, 0, 0, 0}
	attrs := append(buildStateAttr(state), buildEAPAttr(eapPayload)...)
	pkt := buildRadiusPacket(11, 3, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Len(t, pub.calls, 1)
	assert.Equal(t, state, pub.calls[0].sessionID)
	assert.Empty(t, fwd.calls)
}

// TestHandlePacket_CoARequest calls forwardToBiz with messageType="COA".
func TestHandlePacket_CoARequest(t *testing.T) {
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		tracer:          nullTracer(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	state := "coa-session-xyz"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(43, 4, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, pub.calls)
	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, state, fwd.calls[0].sessionID)
	assert.Equal(t, "RADIUS", fwd.calls[0].transportType)
	assert.Equal(t, "COA", fwd.calls[0].messageType)
	assert.Equal(t, pkt, fwd.calls[0].raw)
}

// TestHandlePacket_DisconnectRequest calls forwardToBiz with messageType="RAR".
func TestHandlePacket_DisconnectRequest(t *testing.T) {
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		tracer:          nullTracer(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	state := "dm-session-abc"
	attrs := buildStateAttr(state)
	pkt := buildRadiusPacket(40, 5, attrs)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, pub.calls)
	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, state, fwd.calls[0].sessionID)
	assert.Equal(t, "RADIUS", fwd.calls[0].transportType)
	assert.Equal(t, "RAR", fwd.calls[0].messageType)
	assert.Equal(t, pkt, fwd.calls[0].raw)
}

// TestHandlePacket_UnknownCodeIsDropped verifies that unrecognized codes are ignored.
func TestHandlePacket_UnknownCodeIsDropped(t *testing.T) {
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	// code=5 (Accounting-Request) — not handled
	pkt := buildRadiusPacket(5, 1, nil)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, pub.calls)
	assert.Empty(t, fwd.calls)
}

// TestHandleServerInitiated_NoSessionID_DropsPacket verifies that packets without
// a State attribute are dropped without calling forwardToBiz.
func TestHandleServerInitiated_NoSessionID_DropsPacket(t *testing.T) {
	pub := &mockPublishResponse{}
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		publishResponse: pub.invoke,
		forwardToBiz:    fwd.invoke,
	}

	// CoA packet with no State attribute (totalLen >= 20 so not caught by < 4 check)
	pkt := buildRadiusPacket(43, 6, nil)

	h.handlePacket(context.Background(), nil, nil, pkt)

	assert.Empty(t, pub.calls)
	assert.Empty(t, fwd.calls)
}

// TestHandleServerInitiated_Direct verifies that handleServerInitiated forwards
// CoA and DM packets to Biz with correct transport and message type.
func TestHandleServerInitiated_Direct(t *testing.T) {
	fwd := &mockForwardToBiz{}
	h := &RadiusHandler{
		logger:          nullLogger(),
		tracer:          nullTracer(),
		publishResponse: func(string, []byte) {},
		forwardToBiz:    fwd.invoke,
	}

	sessionID := "direct-coa-test"
	coaPkt := buildRadiusPacket(43, 7, buildStateAttr(sessionID))
	dmPkt := buildRadiusPacket(40, 8, buildStateAttr(sessionID))

	h.handleServerInitiated(context.Background(), coaPkt, "RADIUS")

	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, sessionID, fwd.calls[0].sessionID)
	assert.Equal(t, "COA", fwd.calls[0].messageType)
	assert.Equal(t, "RADIUS", fwd.calls[0].transportType)

	fwd.calls = nil
	h.handleServerInitiated(context.Background(), dmPkt, "RADIUS")

	assert.Len(t, fwd.calls, 1)
	assert.Equal(t, sessionID, fwd.calls[0].sessionID)
	assert.Equal(t, "RAR", fwd.calls[0].messageType)
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
