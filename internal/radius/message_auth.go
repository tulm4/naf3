// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrInvalidMessageAuth is returned when Message-Authenticator verification fails.
var ErrInvalidMessageAuth = errors.New("radius: invalid Message-Authenticator")

// MessageAuthenticatorSize is the length of the HMAC-MD5 output in bytes.
const MessageAuthenticatorSize = 16

// ComputeMessageAuthenticator computes the HMAC-MD5 Message-Authenticator for a RADIUS packet.
// Spec: RFC 3579 §3.2
//
// The Message-Authenticator is computed as:
//   HMAC-MD5(Code + ID + Length + Request Authenticator + Attributes + Shared Secret)
//
// where:
//   - Code, ID, Length, Request Authenticator are from the original Access-Request
//   - Attributes do NOT include the Message-Authenticator attribute itself
//   - Shared Secret is a shared secret between NSSAAF and AAA-S
//
// RFC 3579 §3.2:
//   The Message-Authenticator attribute is set to the HMAC-MD5 hash of the entire
//   Access-Request packet, including the User-Password attribute (encrypted),
//   but with the Message-Authenticator field set to 16 zero octets.
func ComputeMessageAuthenticator(packet []byte, secret string) []byte {
	// Create a mutable copy of the packet.
	p := make([]byte, len(packet))
	copy(p, packet)

	// Zero out the Message-Authenticator attribute value.
	// We need to find the Message-Authenticator attribute and zero its value.
	p = zeroMessageAuthenticator(p)

	// Append the shared secret.
	// The HMAC is computed over: packet + secret
	auth := hmac.New(md5.New, []byte(secret))
	auth.Write(p)
	return auth.Sum(nil)
}

// VerifyMessageAuthenticator verifies a Message-Authenticator in a RADIUS packet.
// Spec: RFC 3579 §3.2
//
// For Access-Accept/Access-Reject/Access-Challenge responses:
//   Expected = HMAC-MD5(ResponseCode + ResponseID + ResponseLength + ResponseVector + Attributes + Secret)
//
// where ResponseVector is the Request Authenticator from the original Access-Request
// (for Access-Challenge) or a specially computed one (for Access-Accept/Reject).
func VerifyMessageAuthenticator(packet []byte, secret string) bool {
	// Find Message-Authenticator in the packet.
	offset := findMessageAuthenticator(packet)
	if offset < 0 {
		return false
	}

	// Extract the received MA.
	received := packet[offset : offset+18]
	expected := ComputeMessageAuthenticator(packet, secret)

	// Constant-time comparison.
	return hmac.Equal(received[2:], expected)
}

// zeroMessageAuthenticator finds the Message-Authenticator attribute in a RADIUS packet
// and zeros its value (16 bytes after the Type and Length).
func zeroMessageAuthenticator(packet []byte) []byte {
	offset := findMessageAuthenticator(packet)
	if offset < 0 {
		return packet
	}

	// Zero out the 16-byte value field.
	for i := 0; i < 16; i++ {
		packet[offset+2+i] = 0
	}
	return packet
}

// findMessageAuthenticator returns the byte offset of the Message-Authenticator attribute
// value within the packet, or -1 if not found.
func findMessageAuthenticator(packet []byte) int {
	if len(packet) < 20 {
		return -1
	}

	attrOffset := 20 // Start of attributes
	for attrOffset+1 < len(packet) {
		attrType := packet[attrOffset]
		attrLen := int(packet[attrOffset+1])

		if attrType == AttrMessageAuthenticator && attrLen == 18 {
			return attrOffset
		}

		attrOffset += attrLen
	}

	return -1
}

// ComputeResponseAuthenticator computes the Response Authenticator for an Access-Accept/Reject.
// Spec: RFC 2865 §3.3
//
// ResponseAuth = MD5(Code+ID+Length+RequestAuth+Attributes+Secret)
//
// For Access-Accept/Reject responses, the Response Authenticator is:
//   MD5(Code + ID + Length + Request Authenticator + Attributes + Secret)
//
// For Access-Challenge, the Request Authenticator from the original request is used.
func ComputeResponseAuthenticator(code, id uint8, length uint16, requestAuth [16]byte, attrs []byte, secret string) [16]byte {
	// Build: Code(1) + ID(1) + Length(2) + RequestAuth(16) + Attributes
	buf := make([]byte, 0, 20+len(attrs))
	buf = append(buf, code, id)
	binary.BigEndian.PutUint16(buf[2:4], length)
	buf = append(buf, requestAuth[:]...)
	buf = append(buf, attrs...)

	// MD5 with secret appended.
	h := md5.New()
	h.Write(buf)
	h.Write([]byte(secret))
	var result [16]byte
	copy(result[:], h.Sum(nil))
	return result
}

// AddMessageAuthenticator adds a Message-Authenticator attribute to a RADIUS packet.
// Returns the modified packet.
func AddMessageAuthenticator(packet []byte, secret string) []byte {
	// Find existing MA and remove it first (to avoid duplicate).
	packet = removeMessageAuthenticator(packet)

	// Append MA attribute: Type(1) + Length(1) + Value(16) = 18 bytes.
	maAttr := make([]byte, 18)
	maAttr[0] = AttrMessageAuthenticator
	maAttr[1] = 18
	// Value (bytes 2-17) set to zero, will be filled by ComputeMessageAuthenticator.

	// Append MA attribute to packet.
	packet = append(packet, maAttr...)

	// Update packet length.
	newLen := uint16(len(packet))
	binary.BigEndian.PutUint16(packet[2:4], newLen)

	// Compute and set the actual MA.
	ma := ComputeMessageAuthenticator(packet, secret)
	copy(packet[len(packet)-16:], ma)

	return packet
}

// removeMessageAuthenticator removes the Message-Authenticator attribute from a packet.
func removeMessageAuthenticator(packet []byte) []byte {
	if len(packet) < 20 {
		return packet
	}

	result := packet[:20] // Header
	attrOffset := 20

	for attrOffset+1 < len(packet) {
		attrType := packet[attrOffset]
		attrLen := int(packet[attrOffset+1])

		if attrType == AttrMessageAuthenticator && attrLen == 18 {
			// Skip this attribute.
			attrOffset += attrLen
			continue
		}

		// Copy the attribute.
		if attrOffset+attrLen <= len(packet) {
			result = append(result, packet[attrOffset:attrOffset+attrLen]...)
		}
		attrOffset += attrLen
	}

	// Update length.
	if len(result) >= 4 {
		binary.BigEndian.PutUint16(result[2:4], uint16(len(result)))
	}

	return result
}

// ValidateAccessChallenge validates an Access-Challenge packet.
// Returns the State attribute if valid, or an error.
func ValidateAccessChallenge(packet []byte, requestAuth [16]byte, secret string) ([]byte, error) {
	if len(packet) < 20 {
		return nil, fmt.Errorf("radius: packet too short")
	}

	code := packet[0]
	if code != CodeAccessChallenge {
		return nil, fmt.Errorf("radius: expected Access-Challenge (11), got %d", code)
	}

	// Verify Message-Authenticator if present.
	if !VerifyMessageAuthenticator(packet, secret) {
		return nil, ErrInvalidMessageAuth
	}

	// Extract State attribute.
	state := GetAttributeBytes(packet, AttrState)
	return state, nil
}

// GetAttributeBytes returns the raw bytes of the first attribute with the given type.
func GetAttributeBytes(packet []byte, attrType uint8) []byte {
	if len(packet) < 20 {
		return nil
	}

	offset := 20
	for offset+1 < len(packet) {
		attrLen := int(packet[offset+1])
		if packet[offset] == attrType && offset+attrLen <= len(packet) {
			return packet[offset+2 : offset+attrLen]
		}
		offset += attrLen
	}
	return nil
}
