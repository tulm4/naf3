// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"crypto/hmac"
	"crypto/md5"
)

// FindMessageAuthenticator finds the offset of the Message-Authenticator attribute in a packet.
// Returns -1 if not found.
func FindMessageAuthenticator(packet []byte) int {
	offset := 20 // Skip RADIUS header
	for offset+1 < len(packet) {
		attrType := packet[offset]
		attrLen := int(packet[offset+1])
		if attrLen < 2 {
			break
		}
		if attrType == AttrMessageAuthenticator {
			return offset
		}
		offset += attrLen
	}
	return -1
}

// HasMessageAuthenticator checks if a packet contains a Message-Authenticator attribute.
func HasMessageAuthenticator(packet []byte) bool {
	return FindMessageAuthenticator(packet) >= 0
}

// ZeroMessageAuthenticator sets the Message-Authenticator value to zeros without changing the packet length.
func ZeroMessageAuthenticator(packet []byte) []byte {
	result := make([]byte, len(packet))
	copy(result, packet)
	offset := FindMessageAuthenticator(packet)
	if offset < 0 {
		return result
	}
	// Zero out the MA value bytes (type+length at offset, value starts at offset+2)
	for i := offset + 2; i < offset+18 && i < len(result); i++ {
		result[i] = 0
	}
	return result
}

// ComputeMessageAuthenticator computes the Message-Authenticator value for a packet.
// The packet must include the MA attribute with a zeroed-out value field (16 bytes).
// RFC 3579 §3.2: MA = HMAC-MD5(packet, secret)
func ComputeMessageAuthenticator(packet []byte, secret string) []byte {
	// Zero the MA field first
	zeroed := ZeroMessageAuthenticator(packet)
	h := hmac.New(md5.New, []byte(secret))
	h.Write(zeroed)
	return h.Sum(nil)
}

// VerifyMessageAuthenticator verifies the Message-Authenticator in a packet.
// It uses the Request Authenticator from the packet as-is (doesn't modify the packet).
func VerifyMessageAuthenticator(packet []byte, secret string) bool {
	offset := FindMessageAuthenticator(packet)
	if offset < 0 {
		return false
	}
	// Must have at least type(1) + length(1) + value(16) = 18 bytes from offset
	if offset+18 > len(packet) {
		return false
	}
	// Extract the stored MA value
	storedMA := packet[offset+2 : offset+18]
	// Compute expected MA using a copy with MA value zeroed
	zeroed := make([]byte, len(packet))
	copy(zeroed, packet)
	// Zero out the MA value in the zeroed copy
	for i := 0; i < 16; i++ {
		zeroed[offset+2+i] = 0
	}
	// Compute expected MA - the packet's Request Authenticator is preserved as-is
	h := hmac.New(md5.New, []byte(secret))
	h.Write(zeroed)
	expectedMA := h.Sum(nil)
	// Compare
	for i := 0; i < 16; i++ {
		if storedMA[i] != expectedMA[i] {
			return false
		}
	}
	return true
}

// AddMessageAuthenticator adds or replaces the Message-Authenticator in a packet.
func AddMessageAuthenticator(packet []byte, secret string) []byte {
	offset := FindMessageAuthenticator(packet)
	if offset >= 0 {
		// Replace existing MA - compute on a copy with MA zeroed
		result := make([]byte, len(packet))
		copy(result, packet)
		// Zero out the existing MA value
		for i := 0; i < 16; i++ {
			result[offset+2+i] = 0
		}
		// Compute expected MA
		h := hmac.New(md5.New, []byte(secret))
		h.Write(result)
		ma := h.Sum(nil)
		copy(result[offset+2:offset+18], ma)
		return result
	}
	// Append new MA attribute: type(1) + length(1) + value(16) = 18 bytes
	newAttr := make([]byte, 18)
	newAttr[0] = AttrMessageAuthenticator
	newAttr[1] = 18
	// Compute MA over packet WITHOUT the new attribute
	h := hmac.New(md5.New, []byte(secret))
	h.Write(packet)
	ma := h.Sum(nil)
	copy(newAttr[2:], ma)
	return append(packet, newAttr...)
}

// RemoveMessageAuthenticator removes the Message-Authenticator attribute from a packet.
func RemoveMessageAuthenticator(packet []byte) []byte {
	offset := FindMessageAuthenticator(packet)
	if offset < 0 {
		return packet
	}
	attrLen := int(packet[offset+1])
	result := make([]byte, len(packet)-attrLen)
	copy(result[:offset], packet[:offset])
	copy(result[offset:], packet[offset+attrLen:])
	return result
}
