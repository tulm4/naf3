//go:build e2e
// +build e2e

package fullchain_dev_diameter_radius

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

const (
	radiusAccessRequest = 1
	radiusAccessAccept  = 2

	attrUserName    = 1
	attrEAPMessage  = 79
	attrMessageAuth = 80
)

// sharedSecret matches compose/configs/aaa-gateway.yaml and the aaa-sim
// default (which falls back to "testing123" when AAA_SIM_SECRET is unset).
// We override via env in bringUp, but the aaa-sim container also reads its
// own secret; we hardcode "secret" here to match aaa-gateway.yaml.
const sharedSecret = "secret"

// buildAccessRequest constructs a RADIUS Access-Request packet with
// User-Name=testuser and EAP-Message=EAP-Response/Identity. Includes a
// Message-Authenticator (RFC 3579) computed over the packet header+attrs.
func buildAccessRequest() []byte {
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

	// Message-Authenticator placeholder (16 zero bytes); will be filled in below.
	attrs.WriteByte(attrMessageAuth)
	attrs.WriteByte(18)
	attrs.Write(make([]byte, 16))

	pkt := make([]byte, 20+attrs.Len())
	pkt[0] = radiusAccessRequest
	pkt[1] = 1 // Identifier
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	// Random Request Authenticator (16 bytes).
	for i := 4; i < 20; i++ {
		pkt[i] = byte(time.Now().UnixNano() >> uint(i))
	}
	copy(pkt[20:], attrs.Bytes())

	// Compute Message-Authenticator = HMAC-MD5(packet with MA zeroed, secret).
	maOffset := 20 + (2 + 2 + len("testuser")) + (2 + len(eap)) // offset of MA value
	for i := maOffset; i < maOffset+16; i++ {
		// already zero; keep zeroed
	}
	h := hmac.New(md5.New, []byte(sharedSecret))
	h.Write(pkt)
	copy(pkt[maOffset:maOffset+16], h.Sum(nil))
	return pkt
}

// validateAccessAccept parses a RADIUS Access-Accept and verifies the
// Response Authenticator (RFC 2865 §4) and EAP-Message contents.
func validateAccessAccept(t *testing.T, req, resp []byte, secret string) {
	t.Helper()
	if len(resp) < 20 {
		t.Fatalf("response too short: %d bytes", len(resp))
	}
	if resp[0] != radiusAccessAccept {
		t.Errorf("code = %d; want Access-Accept (2)", resp[0])
	}
	if resp[1] != req[1] {
		t.Errorf("Identifier = %d; want %d", resp[1], req[1])
	}
	respLen := binary.BigEndian.Uint16(resp[2:4])
	if int(respLen) != len(resp) {
		t.Errorf("Length = %d; got %d", respLen, len(resp))
	}

	// Response Authenticator = MD5(Code+ID+Length+RequestAuth+Attributes+Secret)
	h := md5.New()
	h.Write(resp[:4])
	h.Write(req[4:20])
	h.Write(resp[20:])
	h.Write([]byte(secret))
	expected := h.Sum(nil)
	if !bytes.Equal(expected, resp[4:20]) {
		t.Errorf("Response Authenticator mismatch:\n  got  %x\n  want %x", resp[4:20], expected)
	}

	// Locate EAP-Message attribute; assert value = [3,0,0,4] (EAP Success).
	eap := findAttr(t, resp, attrEAPMessage)
	wantEAP := []byte{3, 0, 0, 4}
	if len(eap) != len(wantEAP) {
		t.Errorf("EAP-Message len = %d; want %d (got %v)", len(eap), len(wantEAP), eap)
	} else {
		for i := range wantEAP {
			if eap[i] != wantEAP[i] {
				t.Errorf("EAP-Message[%d] = %d; want %d", i, eap[i], wantEAP[i])
			}
		}
	}
}

// findAttr returns the value bytes of the first attribute of type attrType.
func findAttr(t *testing.T, pkt []byte, attrType byte) []byte {
	t.Helper()
	pos := 20
	for pos+2 <= len(pkt) {
		length := int(pkt[pos+1])
		if length < 2 || pos+length > len(pkt) {
			break
		}
		if pkt[pos] == attrType {
			return pkt[pos+2 : pos+length]
		}
		pos += length
	}
	t.Fatalf("attribute %d not found in packet of %d bytes", attrType, len(pkt))
	return nil
}

// TestRadius_AccessRequest_Success sends a RADIUS Access-Request to
// 172.0.3.14:1812 with a valid shared secret and asserts the response is
// Access-Accept with EAP Success.
//
// Spec: TS 29.561 §16.3, RFC 2865 §4 (Access-Request/Access-Accept).
func TestRadius_AccessRequest_Success(t *testing.T) {
	t.Cleanup(func() { tearDown(t, tcpComposeFile) })
	bringUp(t, tcpComposeFile, diameterNetworkTCP, map[string]string{"DIAMETER_TRANSPORT": "tcp"})

	conn, err := net.DialTimeout("udp", aaaSimAddr("radius"), 5*time.Second)
	if err != nil {
		t.Fatalf("Dial UDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	req := buildAccessRequest()
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("Write Access-Request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	resp := make([]byte, 4096)
	n, err := conn.Read(resp)
	if err != nil {
		t.Fatalf("Read Access-Accept: %v", err)
	}
	resp = resp[:n]

	validateAccessAccept(t, req, resp, sharedSecret)
}

// TestRadius_AccessRequest_BadSecret verifies that a RADIUS Access-Request
// sent with the wrong shared secret either:
//   - gets no response, or
//   - returns an Access-Accept with an invalid Response Authenticator.
// Either outcome confirms that aaa-sim does not silently accept forged requests.
//
// Spec: RFC 2865 §4 (Response Authenticator validation).
func TestRadius_AccessRequest_BadSecret(t *testing.T) {
	t.Cleanup(func() { tearDown(t, tcpComposeFile) })
	bringUp(t, tcpComposeFile, diameterNetworkTCP, map[string]string{"DIAMETER_TRANSPORT": "tcp"})

	conn, err := net.DialTimeout("udp", aaaSimAddr("radius"), 5*time.Second)
	if err != nil {
		t.Fatalf("Dial UDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Forge a request by overwriting the Message-Authenticator with a value
	// computed under a wrong secret.
	req := buildAccessRequest()
	const wrongSecret = "not-the-real-secret"
	// Rebuild MA under wrong secret.
	maOffset := 20 + (2 + 2 + len("testuser")) + (2 + 5)
	for i := maOffset; i < maOffset+16; i++ {
		req[i] = 0
	}
	h := hmac.New(md5.New, []byte(wrongSecret))
	h.Write(req)
	copy(req[maOffset:maOffset+16], h.Sum(nil))

	if _, err := conn.Write(req); err != nil {
		t.Fatalf("Write Access-Request: %v", err)
	}

	// Wait up to 3s for any response.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp := make([]byte, 4096)
	n, readErr := conn.Read(resp)
	if readErr != nil {
		// Timeout is acceptable: aaa-sim drops forged requests silently.
		t.Logf("no response received for bad-secret request (acceptable): %v", readErr)
		return
	}
	resp = resp[:n]
	t.Logf("received %d-byte response; verifying Response Authenticator under correct secret", n)

	// If a response was received, the Response Authenticator computed under
	// the correct secret MUST NOT match the response field. This proves the
	// reply is not from the legitimate aaa-sim (or aaa-sim is not validating,
	// which we surface as a failure).
	gotHash := resp[4:20]
	h2 := md5.New()
	h2.Write(resp[:4])
	h2.Write(req[4:20])
	h2.Write(resp[20:])
	h2.Write([]byte(sharedSecret))
	expected := h2.Sum(nil)
	if bytes.Equal(gotHash, expected) {
		t.Errorf("aaa-sim accepted Access-Request with wrong shared secret: Response Authenticator matches under correct secret")
	}
}
