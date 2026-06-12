// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"crypto/md5"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Response Authenticator (RFC 2865 §3)
// ============================================================================

func TestVerifyResponseAuthenticator_Valid(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build a response with correct authenticator
	// Response header: Code(1) + ID(1) + Length(2) + Vector(16)
	resp := make([]byte, 20)
	resp[0] = CodeAccessAccept
	resp[1] = 1
	resp[2] = 0
	resp[3] = 20 // length = 20 (header only, no attributes)

	// Compute correct Response Authenticator per RFC 2865 §3
	// ResponseAuth = MD5(Code+ID+Length+RequestAuth+Attributes+Secret)
	h := md5.New()
	h.Write(resp[0:4])       // Code + ID + Length
	h.Write(reqAuth[:])      // Request Authenticator
	h.Write([]byte{})        // Attributes (empty)
	h.Write([]byte(secret))  // Secret
	correctAuth := h.Sum(nil)
	copy(resp[4:20], correctAuth)

	// Verify should pass
	valid := VerifyResponseAuthenticator(resp, reqAuth, secret)
	assert.True(t, valid, "expected valid response authenticator")
}

func TestVerifyResponseAuthenticator_Invalid(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Response with wrong authenticator (all zeros instead of correct value)
	resp := make([]byte, 20)
	resp[0] = CodeAccessAccept
	resp[1] = 1
	resp[2] = 0
	resp[3] = 20

	valid := VerifyResponseAuthenticator(resp, reqAuth, secret)
	assert.False(t, valid, "expected invalid response authenticator")
}

func TestVerifyResponseAuthenticator_Tampered(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build valid response
	resp := make([]byte, 20)
	resp[0] = CodeAccessAccept
	resp[1] = 1
	resp[2] = 0
	resp[3] = 20

	// Compute correct authenticator
	h := md5.New()
	h.Write(resp[0:4])
	h.Write(reqAuth[:])
	h.Write([]byte{})
	h.Write([]byte(secret))
	correctAuth := h.Sum(nil)
	copy(resp[4:20], correctAuth)

	// Verify valid
	assert.True(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected valid")

	// Tamper with response code
	resp[0] = CodeAccessReject

	// Verify tampered
	assert.False(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected invalid after tampering")
}

func TestVerifyResponseAuthenticator_TamperedAttributes(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build response with attributes
	resp := make([]byte, 30)
	resp[0] = CodeAccessAccept
	resp[1] = 1
	resp[2] = 0
	resp[3] = 30 // length includes attributes
	resp[20] = 1 // User-Name attribute
	resp[21] = 5
	resp[22] = 'a'
	resp[23] = 'l'
	resp[24] = 'i'
	resp[25] = 'c'
	resp[26] = 'e'
	// 4 bytes of padding (27-30)

	// Compute correct authenticator with attributes
	h := md5.New()
	h.Write(resp[0:4])
	h.Write(reqAuth[:])
	h.Write(resp[20:30])
	h.Write([]byte(secret))
	correctAuth := h.Sum(nil)
	copy(resp[4:20], correctAuth)

	// Verify valid
	assert.True(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected valid")

	// Tamper with attribute
	resp[22] = 'b' // Change 'a' to 'b'

	// Verify tampered
	assert.False(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected invalid after attribute tampering")
}

func TestVerifyResponseAuthenticator_WrongSecret(t *testing.T) {
	secret := "testing123"
	wrongSecret := "wrongsecret"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build response with correct secret
	resp := make([]byte, 20)
	resp[0] = CodeAccessAccept
	resp[1] = 1
	resp[2] = 0
	resp[3] = 20

	h := md5.New()
	h.Write(resp[0:4])
	h.Write(reqAuth[:])
	h.Write([]byte{})
	h.Write([]byte(secret))
	correctAuth := h.Sum(nil)
	copy(resp[4:20], correctAuth)

	// Verify with wrong secret should fail
	assert.False(t, VerifyResponseAuthenticator(resp, reqAuth, wrongSecret), "expected invalid with wrong secret")
}

func TestVerifyResponseAuthenticator_TooShort(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Packet too short (less than 20 bytes)
	shortResp := []byte{1, 2, 3}
	assert.False(t, VerifyResponseAuthenticator(shortResp, reqAuth, secret), "expected invalid for short packet")

	// Exactly 20 bytes is valid header
	resp := make([]byte, 20)
	resp[0] = CodeAccessAccept
	resp[1] = 1
	resp[2] = 0
	resp[3] = 20
	// Leave authenticator as zeros
	assert.False(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "zero authenticator should fail")
}

func TestVerifyResponseAuthenticator_LengthMismatch(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Response claims longer than it is
	resp := make([]byte, 20)
	resp[0] = CodeAccessAccept
	resp[1] = 1
	resp[2] = 0
	resp[3] = 50 // Claims 50 bytes
	// But actually only 20 bytes

	assert.False(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected invalid for length mismatch")
}

func TestVerifyResponseAuthenticator_AccessReject(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build Access-Reject with correct authenticator
	resp := make([]byte, 20)
	resp[0] = CodeAccessReject
	resp[1] = 1
	resp[2] = 0
	resp[3] = 20

	h := md5.New()
	h.Write(resp[0:4])
	h.Write(reqAuth[:])
	h.Write([]byte{})
	h.Write([]byte(secret))
	correctAuth := h.Sum(nil)
	copy(resp[4:20], correctAuth)

	// Verify should pass for Access-Reject too
	assert.True(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected valid for Access-Reject")
}

func TestVerifyResponseAuthenticator_AccessChallenge(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build Access-Challenge with correct authenticator
	resp := make([]byte, 20)
	resp[0] = CodeAccessChallenge
	resp[1] = 1
	resp[2] = 0
	resp[3] = 20

	h := md5.New()
	h.Write(resp[0:4])
	h.Write(reqAuth[:])
	h.Write([]byte{})
	h.Write([]byte(secret))
	correctAuth := h.Sum(nil)
	copy(resp[4:20], correctAuth)

	// Verify should pass for Access-Challenge too
	assert.True(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected valid for Access-Challenge")
}

// ============================================================================
// ComputeResponseAuthenticator (round-trip tests)
// ============================================================================

func TestComputeResponseAuthenticatorRoundTrip(t *testing.T) {
	secret := "testing123"
	reqAuth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// Build a response packet
	code := CodeAccessAccept
	id := uint8(1)
	length := uint16(20)
	attrs := []byte{}

	// Compute the Response Authenticator
	respAuth := ComputeResponseAuthenticator(code, id, length, reqAuth, attrs, secret)

	// Build the response packet with the computed authenticator
	resp := make([]byte, 20)
	resp[0] = code
	resp[1] = id
	resp[2] = byte(length >> 8)
	resp[3] = byte(length & 0xFF)
	copy(resp[4:20], respAuth[:])

	// Verify the response authenticator
	assert.True(t, VerifyResponseAuthenticator(resp, reqAuth, secret), "expected valid round-trip")
}
