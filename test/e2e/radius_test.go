//go:build e2e
// +build e2e

// RADIUS Access-Request / Access-Accept verification.
// aaa-sim listens on host UDP port 18120 (mapped from container 1812).
// Tests send UDP packets from the host process, then assert log content.
//
// Spec: TS 29.561 §16.3, RFC 2865 §4 (Access-Request/Access-Accept).
package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

const (
	radiusAccessRequest = 1
	radiusAccessAccept  = 2
	radiusAccessReject  = 3

	attrUserName    = 1
	attrEAPMessage  = 79
	attrMessageAuth = 80

	// sharedSecret matches compose/configs/aaa-gateway.yaml (RadiusSharedSecret).
	// The aaa-sim container also reads AAA_SIM_RADIUS_SECRET; in default config
	// it is "secret".
	radiusSharedSecret = "secret"
)

// buildRadiusAccessRequest constructs a minimal RADIUS Access-Request with
// User-Name=testuser, EAP-Message=EAP-Response/Identity, and a Message-Authenticator
// (RFC 3579) computed over (header || attrs) using secret.
func buildRadiusAccessRequest(secret string) []byte {
	// EAP-Response/Identity: Code=2, Id=0, Length=5, Type=1
	eap := []byte{0x02, 0x00, 0x00, 0x05, 0x01}

	var attrs bytes.Buffer
	// User-Name = "testuser"
	attrs.WriteByte(attrUserName)
	attrs.WriteByte(byte(2 + len("testuser")))
	attrs.WriteString("testuser")

	// EAP-Message = eap
	attrs.WriteByte(attrEAPMessage)
	attrs.WriteByte(byte(2 + len(eap)))
	attrs.Write(eap)

	// Message-Authenticator placeholder (16 zero bytes)
	attrs.WriteByte(attrMessageAuth)
	attrs.WriteByte(18)
	attrs.Write(make([]byte, 16))

	pkt := make([]byte, 20+attrs.Len())
	pkt[0] = radiusAccessRequest
	pkt[1] = 1 // Identifier
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	// Random Request Authenticator
	for i := 4; i < 20; i++ {
		pkt[i] = byte(time.Now().UnixNano() >> uint(i))
	}
	copy(pkt[20:], attrs.Bytes())

	// Compute Message-Authenticator = HMAC-MD5(packet with MA zeroed, secret)
	maOffset := 20 + (2 + 2 + len("testuser")) + (2 + len(eap))
	for i := maOffset; i < maOffset+16; i++ {
		pkt[i] = 0
	}
	h := hmac.New(md5.New, []byte(secret))
	h.Write(pkt)
	copy(pkt[maOffset:maOffset+16], h.Sum(nil))
	return pkt
}

// radiusAddress returns the host UDP address where aaa-sim's RADIUS server is reachable.
func radiusAddress() string {
	if v := os.Getenv("FULLCHAIN_AAA_SIM_RADIUS_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:18120"
}

// TestRadius_AccessRequest_Success sends a RADIUS Access-Request with the
// correct shared secret and asserts aaa-sim logs show Access-Accept.
func TestRadius_AccessRequest_Success(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil")
	}

	addr, err := net.ResolveUDPAddr("udp", radiusAddress())
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()

	req := buildRadiusAccessRequest(radiusSharedSecret)
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("Write Access-Request: %v", err)
	}

	// Read response (best-effort; we assert via logs, not packet inspection).
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	n, readErr := conn.Read(buf)
	if readErr == nil && n >= 20 {
		gotCode := buf[0]
		if gotCode != radiusAccessAccept {
			t.Logf("response code = %d (expected Access-Accept=2)", gotCode)
		}
	} else {
		t.Logf("no UDP response within 5s (readErr=%v); assertion is log-based", readErr)
	}

	logs, err := drv.Logs("aaa-sim", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-sim): %v", err)
	}
	if !containsAny(logs, "Access-Accept", "AccessAccept", "EAP-Success", "access-accept") {
		t.Errorf("aaa-sim logs do not show Access-Accept; logs:\n%s", logs)
	}
}

// TestRadius_AccessRequest_BadSecret sends an Access-Request with a wrong
// shared secret and asserts aaa-sim logs show rejection or no response.
func TestRadius_AccessRequest_BadSecret(t *testing.T) {
	skipIfNotE2E(t)
	drv := NewContainerDriver()
	if drv == nil {
		t.Fatal("ContainerDriver is nil")
	}

	addr, err := net.ResolveUDPAddr("udp", radiusAddress())
	if err != nil {
		t.Fatalf("ResolveUDPAddr: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()

	req := buildRadiusAccessRequest("definitely-wrong-secret")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("Write Access-Request: %v", err)
	}

	// Best-effort read; bad-secret requests are typically dropped silently.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	_, _ = conn.Read(buf)

	// Give aaa-sim a moment to log the rejection attempt.
	time.Sleep(1 * time.Second)
	logs, err := drv.Logs("aaa-sim", 200)
	if err != nil {
		t.Fatalf("Logs(aaa-sim): %v", err)
	}
	// Either: explicit rejection log, OR no Access-Accept log (silent drop).
	hasReject := containsAny(logs, "Access-Reject", "AccessReject",
		"bad authenticator", "invalid authenticator", "shared secret mismatch")
	hasAccept := containsAny(logs, "Access-Accept", "AccessAccept")
	if !hasReject && hasAccept {
		t.Errorf("aaa-sim accepted request with wrong secret; logs:\n%s", logs)
	}
	// If hasReject: pass. If neither: pass (silent drop is also acceptable).
	_ = fmt.Sprintf // keep fmt import even if unused
}
