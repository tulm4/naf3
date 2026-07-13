// Server-initiated (RAR/ASR) RADIUS tests.
package aaa_sim

import (
	"bytes"
	"net"
	"testing"
	"time"
)

const (
	testRARSecret      = "testing123"
	testSessionID      = "session-rar-001"
	testSessionIDAbort = "session-asr-002"
)

// startTestRadiusServer starts a RadiusServer bound to a loopback UDP port
// and returns the server, its PacketConn, and the resolved clientAddr that
// the server should target for client→server packets.
func startTestRadiusServer(t *testing.T) (*RadiusServer, net.PacketConn, net.Addr) {
	t.Helper()
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := NewRadiusServer(ln, ModeEAP_TLS_SUCCESS, []byte(testRARSecret), testLogger())
	// Bind a separate "client" socket. We don't run a RADIUS client; we just
	// need a fixed clientAddr that the server can WriteTo and the test can ReadFrom.
	clientConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		_ = ln.Close()
		t.Fatalf("client listen: %v", err)
	}
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = ln.Close()
	})
	return srv, clientConn, clientConn.LocalAddr()
}

func TestSendRAR_RoundTrip(t *testing.T) {
	srv, clientConn, clientAddr := startTestRadiusServer(t)

	// Trigger the RAR.
	if err := srv.SendRAR(clientAddr, testSessionID); err != nil {
		t.Fatalf("SendRAR: %v", err)
	}

	// Read the RAR on the client side.
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 4096)
	n, _, err := clientConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read RAR: %v", err)
	}
	pkt := buf[:n]

	if got := pkt[0]; got != radiusCoARequest {
		t.Fatalf("RAR code = %d, want %d (RFC 5176 CoA-Request)", got, radiusCoARequest)
	}
	if len(pkt) < 20 {
		t.Fatalf("RAR too short: %d bytes", len(pkt))
	}
	// Length field sanity.
	if uint16(pkt[2])<<8|uint16(pkt[3]) != uint16(len(pkt)) {
		t.Errorf("RAR length field mismatch: hdr=%d pkt=%d",
			uint16(pkt[2])<<8|uint16(pkt[3]), len(pkt))
	}
	// State attribute must carry sessionID.
	stateOff := findAttrInResponse(pkt, attrState)
	if stateOff < 0 {
		t.Fatalf("RAR missing State attribute")
	}
	stateLen := int(pkt[stateOff+1])
	if stateOff+stateLen > len(pkt) {
		t.Fatalf("State attribute truncated")
	}
	if got := string(pkt[stateOff+2 : stateOff+stateLen]); got != testSessionID {
		t.Errorf("State value = %q, want %q", got, testSessionID)
	}
	// Message-Authenticator must be present and valid per RFC 5176 §3.
	if !verifyMessageAuth(pkt, []byte(testRARSecret)) {
		t.Errorf("RAR Message-Authenticator invalid")
	}
}

func TestSendASR_RoundTrip(t *testing.T) {
	srv, clientConn, clientAddr := startTestRadiusServer(t)

	if err := srv.SendASR(clientAddr, testSessionIDAbort); err != nil {
		t.Fatalf("SendASR: %v", err)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	buf := make([]byte, 4096)
	n, _, err := clientConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read ASR: %v", err)
	}
	pkt := buf[:n]

	if got := pkt[0]; got != radiusDisconnectRequest {
		t.Fatalf("ASR code = %d, want %d (RFC 5176 Disconnect-Request)", got, radiusDisconnectRequest)
	}
	stateOff := findAttrInResponse(pkt, attrState)
	if stateOff < 0 {
		t.Fatalf("ASR missing State attribute")
	}
	stateLen := int(pkt[stateOff+1])
	if got := string(pkt[stateOff+2 : stateOff+stateLen]); got != testSessionIDAbort {
		t.Errorf("State value = %q, want %q", got, testSessionIDAbort)
	}
	if !verifyMessageAuth(pkt, []byte(testRARSecret)) {
		t.Errorf("ASR Message-Authenticator invalid")
	}
}

func TestSendServerInitiated_RejectsInvalidCode(t *testing.T) {
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := NewRadiusServer(ln, ModeEAP_TLS_SUCCESS, []byte(testRARSecret), testLogger())
	if err := srv.SendServerInitiated(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, 1, "x"); err == nil {
		t.Errorf("SendServerInitiated(code=1) should reject")
	}
}

func TestSendRAR_RequestAuthenticatorNonZero(t *testing.T) {
	// RFC 5176 §3: Request Authenticator MUST be unpredictable. We assert it's
	// non-zero (the random read will produce non-zero bytes with overwhelming
	// probability) and that two successive RARs differ.
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := NewRadiusServer(ln, ModeEAP_TLS_SUCCESS, []byte(testRARSecret), testLogger())

	p1, err := srv.buildServerInitiatedPacket(radiusCoARequest, "s1")
	if err != nil {
		t.Fatalf("build packet 1: %v", err)
	}
	p2, err := srv.buildServerInitiatedPacket(radiusCoARequest, "s2")
	if err != nil {
		t.Fatalf("build packet 2: %v", err)
	}
	if bytes.Equal(p1[4:20], p2[4:20]) {
		t.Errorf("two consecutive Request Authenticators are identical (collision or non-random)")
	}
	if bytes.Equal(p1[4:20], make([]byte, 16)) {
		t.Errorf("Request Authenticator is all zeros")
	}
}