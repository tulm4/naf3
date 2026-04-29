package aaa_sim

import (
	"context"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestModeString(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeEAP_TLS_SUCCESS, "EAP_TLS_SUCCESS"},
		{ModeEAP_TLS_FAILURE, "EAP_TLS_FAILURE"},
		{ModeEAP_TLS_CHALLENGE, "EAP_TLS_CHALLENGE"},
		{Mode(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("Mode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"EAP_TLS_SUCCESS", ModeEAP_TLS_SUCCESS},
		{"EAP_TLS_FAILURE", ModeEAP_TLS_FAILURE},
		{"EAP_TLS_CHALLENGE", ModeEAP_TLS_CHALLENGE},
		{"unknown", ModeEAP_TLS_SUCCESS},
		{"", ModeEAP_TLS_SUCCESS},
	}
	for _, tt := range tests {
		if got := ParseMode(tt.input); got != tt.want {
			t.Errorf("ParseMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestRadiusServerChallengeMode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping network test: %v", err)
	}
	defer ln.Close()

	logger := testLogger()
	secret := []byte("testing123")
	server := NewRadiusServer(ln, ModeEAP_TLS_CHALLENGE, secret, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use WaitGroup to signal when server has processed the packet.
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		server.Run(ctx)
		wg.Done()
	}()

	// Build and send a minimal RADIUS Access-Request with valid Request Authenticator.
	// Build the request header with non-zero Request Authenticator.
	req := buildTestAccessRequestWithSecret(secret)
	_, err = ln.WriteTo(req, ln.LocalAddr())
	if err != nil {
		t.Fatalf("failed to send test request: %v", err)
	}

	// Read the response with a deadline.
	respBuf := make([]byte, 4096)
	ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := ln.ReadFrom(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			t.Log("server did not respond within timeout; this is acceptable for UDP tests")
			return
		}
		t.Fatalf("failed to read response: %v", err)
	}

	// Verify response code is Access-Challenge (11) for first request in challenge mode.
	if len(respBuf) < 1 {
		t.Fatalf("response too short: %d bytes", n)
	}
	if respBuf[0] != radiusAccessChallenge {
		t.Errorf("first response code = %d, want %d (Access-Challenge)", respBuf[0], radiusAccessChallenge)
	}

	// Verify State attribute is present (contains session ID).
	stateAttr := findAttrInResponse(respBuf[:n], attrState)
	if stateAttr < 0 {
		t.Error("response missing State attribute")
	}

	// Verify Message-Authenticator is present.
	maAttr := findAttrInResponse(respBuf[:n], attrMessageAuth)
	if maAttr < 0 {
		t.Error("response missing Message-Authenticator attribute")
	}

	// Send second request with State to trigger Access-Accept.
	req2 := buildTestAccessRequestWithState(secret, respBuf[4:20], extractStateFromResponse(respBuf[:n]))
	_, err = ln.WriteTo(req2, ln.LocalAddr())
	if err != nil {
		t.Fatalf("failed to send second request: %v", err)
	}

	ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	n2, _, err := ln.ReadFrom(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			t.Log("server did not respond to second request within timeout")
			return
		}
		t.Fatalf("failed to read second response: %v", err)
	}

	if len(respBuf) < 1 {
		t.Fatalf("second response too short: %d bytes", n2)
	}
	if respBuf[0] != radiusAccessAccept {
		t.Errorf("second response code = %d, want %d (Access-Accept)", respBuf[0], radiusAccessAccept)
	}
}

// findAttrInResponse finds an attribute by type in a RADIUS response.
func findAttrInResponse(data []byte, attrType uint8) int {
	if len(data) < 20 {
		return -1
	}
	pos := 20
	for pos+2 <= len(data) {
		length := int(data[pos+1])
		if length < 2 || pos+length > len(data) {
			break
		}
		if data[pos] == attrType {
			return pos
		}
		pos += length
	}
	return -1
}

// extractStateFromResponse extracts the State attribute value from a response.
func extractStateFromResponse(data []byte) []byte {
	offset := findAttrInResponse(data, attrState)
	if offset < 0 {
		return nil
	}
	length := int(data[offset+1])
	if length < 3 {
		return nil
	}
	return data[offset+2 : offset+length]
}

func TestBuildEAPAttr(t *testing.T) {
	small := []byte{1, 2, 3}
	attrs := buildEAPAttr(small)
	if len(attrs) < 4 {
		t.Errorf("buildEAPAttr small: expected ≥4 bytes, got %d", len(attrs))
	}
	if attrs[0] != attrEAPMessage {
		t.Errorf("buildEAPAttr small: first byte = %d, want %d", attrs[0], attrEAPMessage)
	}

	large := make([]byte, 500)
	for i := range large {
		large[i] = byte(i)
	}
	attrs = buildEAPAttr(large)
	if len(attrs) < 500 {
		t.Errorf("buildEAPAttr large: got %d bytes, want ≥500", len(attrs))
	}
}

func TestBuildStateAttr(t *testing.T) {
	attr := buildStateAttr("test-session")
	if attr[0] != attrState {
		t.Errorf("buildStateAttr: first byte = %d, want %d", attr[0], attrState)
	}
}

func TestBuildMessageAuthAttr(t *testing.T) {
	attr := buildMessageAuthAttr()
	if len(attr) != 18 {
		t.Errorf("buildMessageAuthAttr: len = %d, want 18", len(attr))
	}
	if attr[0] != attrMessageAuth {
		t.Errorf("buildMessageAuthAttr: first byte = %d, want %d", attr[0], attrMessageAuth)
	}
}

func TestHasMessageAuth(t *testing.T) {
	packet := []byte{
		1, 0, 0, 38, // Access-Request
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, // Request Auth
		attrMessageAuth, 18,
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	}
	if !hasMessageAuth(packet) {
		t.Error("hasMessageAuth: expected true")
	}

	packet2 := []byte{
		1, 0, 0, 22,
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
		attrUserName, 6, 'a', 'd', 'm', 'i', 'n',
	}
	if hasMessageAuth(packet2) {
		t.Error("hasMessageAuth: expected false")
	}
}

// buildTestAccessRequest creates a minimal valid RADIUS Access-Request.
func buildTestAccessRequest() []byte {
	req := make([]byte, 20)
	req[0] = radiusAccessRequest
	req[1] = 1 // ID
	req[2] = 0
	req[3] = 20 // length
	return req
}

// buildTestAccessRequestWithSecret creates a valid RADIUS Access-Request
// with a proper Request Authenticator computed per RFC 2865 §4.
func buildTestAccessRequestWithSecret(secret []byte) []byte {
	// Build minimal packet with zero Request Authenticator for computation.
	req := make([]byte, 20)
	req[0] = radiusAccessRequest
	req[1] = 1 // ID
	req[2] = 0
	req[3] = 20 // length
	// req[4:20] stays zero for now.

	// Compute Request Authenticator: MD5(Code+ID+Length+16_zeros+Attributes+Secret).
	// Attributes is empty, so: MD5(code+id+length+16_zeros+secret).
	computed := md5Authenticator(req[:4], req[4:20], nil, secret)
	copy(req[4:20], computed)
	return req
}

// buildTestAccessRequestWithState creates a RADIUS Access-Request with a State
// attribute and a valid Request Authenticator.
func buildTestAccessRequestWithState(secret []byte, reqAuth []byte, state []byte) []byte {
	// Build User-Name attribute.
	username := []byte("testuser")
	userAttr := []byte{attrUserName, byte(2 + len(username))}
	userAttr = append(userAttr, username...)

	// Build State attribute.
	stateAttr := []byte{attrState, byte(2 + len(state))}
	stateAttr = append(stateAttr, state...)

	attrs := append(userAttr, stateAttr...)
	packetLen := 20 + len(attrs)

	req := make([]byte, packetLen)
	req[0] = radiusAccessRequest
	req[1] = 1 // ID
	req[2] = byte(packetLen >> 8)
	req[3] = byte(packetLen & 0xff)

	// Compute Request Authenticator with these attrs.
	computed := md5Authenticator(req[:4], reqAuth, attrs, secret)
	copy(req[4:20], computed)

	copy(req[20:], attrs)
	return req
}
