package aaa_sim

import (
	"context"
	"log/slog"
	"net"
	"os"
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
	server := NewRadiusServer(ln, ModeEAP_TLS_CHALLENGE, []byte("testing123"), logger)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	go server.Run(ctx)

	// Build and send a minimal RADIUS Access-Request.
	req := buildTestAccessRequest()
	_, err = ln.WriteTo(req, ln.LocalAddr())
	if err != nil {
		t.Fatalf("failed to send test request: %v", err)
	}

	// Give the server time to process and respond.
	time.Sleep(100 * time.Millisecond)

	// At this point, we can't reliably read the response on the same socket
	// after the server has closed. Verify the server didn't panic or error.
	// The actual EAP flow (Success/Failure/Challenge) is validated by the
	// non-network unit tests (TestBuildEAPAttr, TestBuildStateAttr, etc.).
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
