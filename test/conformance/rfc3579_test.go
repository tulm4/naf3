// Package conformance provides RFC 3579 conformance test suite for NSSAAF.
// Spec: RFC 3579 — RADIUS Support for Extensible Authentication Protocol (EAP)
package conformance

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
)

// RFC 3579 attribute types. RFC 2865, RFC 3579.
const (
	attrUserName       = 1
	attrNASIPAddress   = 4
	attrNASPort        = 5
	attrServiceType    = 6
	attrReplyMessage   = 18
	attrState          = 24
	attrClass          = 25
	attrCallingStation = 31
	attrNASIdentifier  = 32
	attrProxyState     = 33
	attrEAPMessage     = 79
	attrMessageAuth    = 80
)

// RADIUS codes. RFC 2865.
const (
	radiusAccessRequest   = 1
	radiusAccessAccept    = 2
	radiusAccessReject    = 3
	radiusAccessChallenge = 11
)

// TC-RADIUS-001: EAP-Message attribute present in Access-Request.
// Spec: RFC 3579 §3.1
func TestRFC3579_EAPMessagePresent(t *testing.T) {
	t.Parallel()
	// Build a minimal Access-Request with EAP-Message.
	packet := buildRADIUSRequest(nil)
	eapIdx := findRADIUSAttr(packet, attrEAPMessage)
	assert.GreaterOrEqual(t, eapIdx, 0, "TC-RADIUS-001: EAP-Message attribute must be present")
}

// TC-RADIUS-002: Message-Authenticator computed as HMAC-MD5 over entire packet.
// Spec: RFC 3579 §3.2
func TestRFC3579_MessageAuthHMACMD5(t *testing.T) {
	t.Parallel()
	secret := []byte("testing123")
	packet := buildRADIUSRequest(nil)
	packet = signPacketWithMA(packet, secret)

	// Verify MA is present and correct.
	valid := verifyRADIUSMessageAuth(packet, secret)
	assert.True(t, valid, "TC-RADIUS-002: Message-Authenticator must be valid HMAC-MD5")
}

// TC-RADIUS-003: EAP-Message fragmentation (>253 bytes split into multiple attributes).
// Spec: RFC 3579 §3.1
func TestRFC3579_EAPMessageFragmentation(t *testing.T) {
	t.Parallel()
	// Create a large EAP payload (300 bytes).
	eapPayload := make([]byte, 300)
	_, _ = rand.Read(eapPayload)

	packet := buildRADIUSRequest(eapPayload)

	// Count EAP-Message fragments.
	var count int
	offset := 20
	for offset+2 <= len(packet) {
		length := int(packet[offset+1])
		if length < 2 || offset+length > len(packet) {
			break
		}
		if packet[offset] == attrEAPMessage {
			count++
		}
		offset += length
	}

	// 300 bytes with max 251 bytes per attribute = 2 attributes.
	assert.Equal(t, 2, count, "TC-RADIUS-003: Large EAP payload must be fragmented into 2 attributes")
}

// TC-RADIUS-004: EAP-Message reassembly at receiver.
// Spec: RFC 3579 §3.1
func TestRFC3579_EAPMessageReassembly(t *testing.T) {
	t.Parallel()
	// Build packet with two EAP-Message fragments (251 + 49 = 300 bytes).
	eapFrag1 := make([]byte, 251)
	eapFrag2 := make([]byte, 49)
	_, _ = rand.Read(eapFrag1)
	_, _ = rand.Read(eapFrag2)

	packet := buildRADIUSRequestWithEAPFragments(eapFrag1, eapFrag2)
	reassembled := reassembleEAPMessage(packet)
	assert.Equal(t, 300, len(reassembled), "TC-RADIUS-004: Reassembled EAP message must be 300 bytes")
}

// TC-RADIUS-005: Message-Authenticator in Access-Challenge.
// Spec: RFC 3579 §3.2
func TestRFC3579_MessageAuthInAccessChallenge(t *testing.T) {
	t.Parallel()
	secret := []byte("testing123")
	req := buildRADIUSRequest(nil)
	resp := buildChallengeResponse(req, secret)

	maIdx := findRADIUSAttr(resp, attrMessageAuth)
	assert.GreaterOrEqual(t, maIdx, 0, "TC-RADIUS-005: Access-Challenge must include Message-Authenticator")

	valid := verifyRADIUSMessageAuth(resp, secret)
	assert.True(t, valid, "TC-RADIUS-005: Message-Authenticator in Challenge must be valid")
}

// TC-RADIUS-006: Message-Authenticator in Access-Accept.
// Spec: RFC 3579 §3.2
func TestRFC3579_MessageAuthInAccessAccept(t *testing.T) {
	t.Parallel()
	secret := []byte("testing123")
	req := buildRADIUSRequest(nil)
	resp := buildAccessResponse(req, secret, radiusAccessAccept)

	maIdx := findRADIUSAttr(resp, attrMessageAuth)
	assert.GreaterOrEqual(t, maIdx, 0, "TC-RADIUS-006: Access-Accept must include Message-Authenticator")

	valid := verifyRADIUSMessageAuth(resp, secret)
	assert.True(t, valid, "TC-RADIUS-006: Message-Authenticator in Accept must be valid")
}

// TC-RADIUS-007: Message-Authenticator in Access-Reject.
// Spec: RFC 3579 §3.2
func TestRFC3579_MessageAuthInAccessReject(t *testing.T) {
	t.Parallel()
	secret := []byte("testing123")
	req := buildRADIUSRequest(nil)
	resp := buildAccessResponse(req, secret, radiusAccessReject)

	maIdx := findRADIUSAttr(resp, attrMessageAuth)
	assert.GreaterOrEqual(t, maIdx, 0, "TC-RADIUS-007: Access-Reject must include Message-Authenticator")

	valid := verifyRADIUSMessageAuth(resp, secret)
	assert.True(t, valid, "TC-RADIUS-007: Message-Authenticator in Reject must be valid")
}

// TC-RADIUS-008: Invalid Message-Authenticator → packet dropped.
// Spec: RFC 3579 §3.2
func TestRFC3579_InvalidMessageAuthDrop(t *testing.T) {
	t.Parallel()
	secret := []byte("testing123")
	wrongSecret := []byte("wrongsecret")

	// Sign with wrong secret.
	packet := buildRADIUSRequest(nil)
	packet = signPacketWithMA(packet, wrongSecret)

	// Verify with correct secret should fail.
	valid := verifyRADIUSMessageAuth(packet, secret)
	assert.False(t, valid, "TC-RADIUS-008: Packet with wrong MA must be dropped")
}

// TC-RADIUS-009: Proxy-State attribute preserved end-to-end.
// Spec: RFC 3579 §3.3
func TestRFC3579_ProxyStatePreserved(t *testing.T) {
	t.Parallel()
	// Build a Proxy-State attribute and verify it can be found in a packet.
	proxyStateAttr := buildRADIUSAttribute(attrProxyState, []byte("proxy-001"))
	assert.Equal(t, 11, len(proxyStateAttr), "TC-RADIUS-009: Proxy-State attr must be 11 bytes")
	assert.Equal(t, uint8(attrProxyState), proxyStateAttr[0], "TC-RADIUS-009: Type must be Proxy-State")
	assert.Equal(t, uint8(11), proxyStateAttr[1], "TC-RADIUS-009: Length must be 11")
	assert.Equal(t, "proxy-001", string(proxyStateAttr[2:]), "TC-RADIUS-009: Value must be preserved")

	// Verify it can be found in a packet.
	packet := buildRADIUSRequest(nil)
	packet = append(packet, proxyStateAttr...)
	psIdx := findRADIUSAttr(packet, attrProxyState)
	assert.GreaterOrEqual(t, psIdx, 0, "TC-RADIUS-009: Proxy-State must be findable in packet")
}

// TC-RADIUS-010: User-Name attribute UTF-8 encoding.
// Spec: RFC 3579 §2.1, RFC 3579 §3.1
func TestRFC3579_UserNameUTF8(t *testing.T) {
	t.Parallel()
	// Build a User-Name attribute with UTF-8 characters.
	username := "用户" // Chinese characters (valid UTF-8).
	unAttr := buildRADIUSAttribute(attrUserName, []byte(username))

	// Verify attribute structure.
	assert.Equal(t, attrUserName, int(unAttr[0]), "TC-RADIUS-010: Type must be User-Name (1)")
	assert.Equal(t, 1+len(username)+1, len(unAttr), "TC-RADIUS-010: Length must be correct")
	assert.Equal(t, username, string(unAttr[2:]), "TC-RADIUS-010: Value must preserve UTF-8 bytes")
}

// ─── Low-level helpers ────────────────────────────────────────────────────

// buildRADIUSRequest builds a minimal RADIUS Access-Request with EAP-Message and MA.
func buildRADIUSRequest(eapPayload []byte) []byte {
	secret := []byte("testing123")
	if eapPayload == nil {
		eapPayload = []byte{1, 13, 0, 6, 0, 0, 0, 0} // EAP Request Identity
	}

	// Header: 20 bytes.
	packet := make([]byte, 20)
	packet[0] = radiusAccessRequest
	packet[1] = 1
	binary.BigEndian.PutUint16(packet[2:4], 20)

	// Request Authenticator: 16 bytes random.
	reqAuth := make([]byte, 16)
	_, _ = rand.Read(reqAuth)
	copy(packet[4:20], reqAuth)

	// User-Name attribute.
	packet = append(packet, buildRADIUSAttribute(attrUserName, []byte("user@example.com"))...)

	// NAS-IP-Address attribute.
	packet = append(packet, buildRADIUSAttribute(attrNASIPAddress, []byte{127, 0, 0, 1})...)

	// EAP-Message attributes (may be fragmented).
	packet = append(packet, buildEAPMessageAttrs(eapPayload)...)

	// Message-Authenticator placeholder (18 bytes).
	maAttr := buildRADIUSAttribute(attrMessageAuth, make([]byte, 16))
	packet = append(packet, maAttr...)

	// Update length.
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))

	// Compute and fill MA value.
	packet = signPacketWithMA(packet, secret)

	return packet
}

// buildRADIUSRequestWithEAPFragments builds a packet with specific EAP fragments.
func buildRADIUSRequestWithEAPFragments(frag1, frag2 []byte) []byte {
	secret := []byte("testing123")

	packet := make([]byte, 20)
	packet[0] = radiusAccessRequest
	packet[1] = 1
	binary.BigEndian.PutUint16(packet[2:4], 20)

	reqAuth := make([]byte, 16)
	_, _ = rand.Read(reqAuth)
	copy(packet[4:20], reqAuth)

	packet = append(packet, buildRADIUSAttribute(attrUserName, []byte("user"))...)
	packet = append(packet, buildEAPMessageAttrs(frag1)...)
	packet = append(packet, buildEAPMessageAttrs(frag2)...)

	maAttr := buildRADIUSAttribute(attrMessageAuth, make([]byte, 16))
	packet = append(packet, maAttr...)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))

	packet = signPacketWithMA(packet, secret)
	return packet
}

// buildChallengeResponse creates an Access-Challenge for a given request.
func buildChallengeResponse(req []byte, secret []byte) []byte {
	// Build response header (20 bytes).
	resp := make([]byte, 20)
	resp[0] = radiusAccessChallenge
	resp[1] = req[1] // same ID
	binary.BigEndian.PutUint16(resp[2:4], 20)

	// Response Authenticator: MD5(Code+ID+Length+RequestAuth+Attributes+Secret).
	attrs := buildEAPMessageAttrs([]byte{3, 0, 0, 4}) // EAP Success
	attrs = append(attrs, buildRADIUSAttribute(attrState, []byte("challenge-state"))...)

	respAuthData := make([]byte, 20+len(attrs))
	respAuthData[0] = radiusAccessChallenge
	respAuthData[1] = req[1]
	binary.BigEndian.PutUint16(respAuthData[2:4], uint16(20+len(attrs)))
	copy(respAuthData[4:20], req[4:20]) // Request Authenticator
	copy(respAuthData[20:], attrs)
	h := md5.New()
	h.Write(respAuthData)
	h.Write(secret)
	copy(resp[4:20], h.Sum(nil))

	// Append attributes.
	resp = append(resp, attrs...)

	// MA placeholder + compute.
	maAttr := buildRADIUSAttribute(attrMessageAuth, make([]byte, 16))
	resp = append(resp, maAttr...)
	binary.BigEndian.PutUint16(resp[2:4], uint16(len(resp)))

	return signPacketWithMA(resp, secret)
}

// buildAccessResponse creates an Access-Accept or Access-Reject for a given request.
func buildAccessResponse(req []byte, secret []byte, code uint8) []byte {
	// Collect attributes from request (excluding MA).
	var attrs []byte
	offset := 20
	for offset+2 <= len(req) {
		attrLen := int(req[offset+1])
		if attrLen < 2 || offset+attrLen > len(req) {
			break
		}
		if req[offset] != attrMessageAuth {
			attrs = append(attrs, req[offset:offset+attrLen]...)
		}
		offset += attrLen
	}

	// Build response header.
	resp := make([]byte, 20)
	resp[0] = code
	resp[1] = req[1]
	binary.BigEndian.PutUint16(resp[2:4], uint16(20+len(attrs)))

	// Response Authenticator.
	respAuthData := make([]byte, 20+len(attrs))
	respAuthData[0] = code
	respAuthData[1] = req[1]
	binary.BigEndian.PutUint16(respAuthData[2:4], uint16(20+len(attrs)))
	copy(respAuthData[4:20], req[4:20]) // Request Authenticator
	copy(respAuthData[20:], attrs)
	h := md5.New()
	h.Write(respAuthData)
	h.Write(secret)
	copy(resp[4:20], h.Sum(nil))

	// Append attributes.
	resp = append(resp, attrs...)

	// MA placeholder + compute.
	maAttr := buildRADIUSAttribute(attrMessageAuth, make([]byte, 16))
	resp = append(resp, maAttr...)
	binary.BigEndian.PutUint16(resp[2:4], uint16(len(resp)))

	return signPacketWithMA(resp, secret)
}

// signPacketWithMA computes and fills the Message-Authenticator attribute.
func signPacketWithMA(packet []byte, secret []byte) []byte {
	// Find MA attribute position.
	maIdx := -1
	offset := 20
	for offset+2 <= len(packet) {
		attrLen := int(packet[offset+1])
		if attrLen < 2 || offset+attrLen > len(packet) {
			break
		}
		if packet[offset] == attrMessageAuth {
			maIdx = offset
			break
		}
		offset += attrLen
	}
	if maIdx < 0 {
		return packet
	}

	// Zero the MA value bytes.
	for i := 0; i < 16; i++ {
		packet[maIdx+2+i] = 0
	}

	// Compute HMAC-MD5 over header + attributes + MA attr header.
	maDataLen := maIdx + 18
	maData := make([]byte, maDataLen)
	copy(maData, packet[:maDataLen])
	h := hmac.New(md5.New, secret)
	h.Write(maData)
	copy(packet[maIdx+2:maIdx+18], h.Sum(nil))

	return packet
}

// verifyRADIUSMessageAuth verifies the Message-Authenticator in a packet.
func verifyRADIUSMessageAuth(packet []byte, secret []byte) bool {
	maIdx := -1
	offset := 20
	for offset+2 <= len(packet) {
		attrLen := int(packet[offset+1])
		if attrLen < 2 || offset+attrLen > len(packet) {
			break
		}
		if packet[offset] == attrMessageAuth {
			maIdx = offset
			break
		}
		offset += attrLen
	}
	if maIdx < 0 || maIdx+18 > len(packet) {
		return false
	}

	// Extract expected value.
	expected := make([]byte, 16)
	copy(expected, packet[maIdx+2:maIdx+18])

	// Zero and recompute.
	for i := 0; i < 16; i++ {
		packet[maIdx+2+i] = 0
	}
	maDataLen := maIdx + 18
	maData := make([]byte, maDataLen)
	copy(maData, packet[:maDataLen])
	h := hmac.New(md5.New, secret)
	h.Write(maData)
	computed := h.Sum(nil)

	return hmac.Equal(expected, computed)
}

// buildRADIUSAttribute builds a RADIUS attribute: Type (1) + Length (1) + Value.
func buildRADIUSAttribute(attrType uint8, value []byte) []byte {
	attr := make([]byte, 2+len(value))
	attr[0] = attrType
	attr[1] = byte(2 + len(value))
	copy(attr[2:], value)
	return attr
}

// buildEAPMessageAttrs splits an EAP payload into RADIUS attributes (max 251 bytes each).
func buildEAPMessageAttrs(payload []byte) []byte {
	var attrs []byte
	for i := 0; i < len(payload); i += 251 {
		end := i + 251
		if end > len(payload) {
			end = len(payload)
		}
		attrs = append(attrs, buildRADIUSAttribute(attrEAPMessage, payload[i:end])...)
	}
	return attrs
}

// findRADIUSAttr finds the first occurrence of an attribute type in a packet.
func findRADIUSAttr(packet []byte, attrType uint8) int {
	offset := 20
	for offset+2 <= len(packet) {
		length := int(packet[offset+1])
		if length < 2 || offset+length > len(packet) {
			break
		}
		if packet[offset] == attrType {
			return offset
		}
		offset += length
	}
	return -1
}

// reassembleEAPMessage concatenates all EAP-Message fragments in a packet.
func reassembleEAPMessage(packet []byte) []byte {
	var result []byte
	offset := 20
	for offset+2 <= len(packet) {
		length := int(packet[offset+1])
		if length < 2 || offset+length > len(packet) {
			break
		}
		if packet[offset] == attrEAPMessage {
			result = append(result, packet[offset+2:offset+length]...)
		}
		offset += length
	}
	return result
}
