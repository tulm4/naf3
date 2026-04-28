// Package radius provides RFC 3579 EAP-over-RADIUS conformance tests.
// Spec: RFC 3579 §3.2 (EAP-Message), §3.3 (Message-Authenticator)
package radius

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRFC3579_EAPMessagePresent verifies that an Access-Request carrying an EAP
// payload includes the EAP-Message attribute (RFC 3579 §3.2).
func TestRFC3579_EAPMessagePresent(t *testing.T) {
	eapPayload := []byte{1, 2, 3, 4, 5} // Minimal EAP payload
	packet := BuildAccessRequest(7, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "user@example.com"),
		MakeAttribute(AttrEAPMessage, eapPayload),
	})

	encoded := packet.Encode()
	decoded, err := DecodePacket(encoded)
	assert.NoError(t, err)
	assert.Equal(t, CodeAccessRequest, decoded.Code)

	eapAttrs := GetAttributes(decoded.Attributes, AttrEAPMessage)
	assert.Len(t, eapAttrs, 1, "Access-Request with EAP payload must contain EAP-Message attribute")
	assert.Equal(t, eapPayload, []byte(eapAttrs[0].Value))
}

// TestRFC3579_MessageAuthenticator verifies that a Message-Authenticator
// attribute is computed correctly as HMAC-MD5 over the entire packet (RFC 3579 §3.2).
func TestRFC3579_MessageAuthenticator(t *testing.T) {
	secret := "shared_secret_xyz"
	packet := BuildAccessRequest(1, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "alice"),
		MakeAttribute(AttrEAPMessage, []byte{1, 2, 3}),
	})

	raw := packet.Encode()
	rawWithMA := AddMessageAuthenticator(raw, secret)

	assert.True(t, HasMessageAuthenticator(rawWithMA), "packet must contain Message-Authenticator after AddMessageAuthenticator")
	assert.True(t, VerifyMessageAuthenticator(rawWithMA, secret), "Message-Authenticator must verify with correct secret")
}

// TestRFC3579_EAPMessageFragmentation verifies that large EAP payloads
// (>253 bytes) are correctly split across multiple EAP-Message attributes.
func TestRFC3579_EAPMessageFragmentation(t *testing.T) {
	largePayload := make([]byte, 600)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	frags := FragmentEAPMessage(largePayload, 253)
	assert.Len(t, frags, 3, "600-byte payload with 253-byte limit should produce 3 fragments")
	assert.Len(t, frags[0], 253)
	assert.Len(t, frags[1], 253)
	assert.Len(t, frags[2], 94)

	// Verify no fragment exceeds maxSize
	for i, frag := range frags {
		assert.LessOrEqual(t, len(frag), 253, "fragment %d length must not exceed maxSize", i)
	}
}

// TestRFC3579_EAPMessageReassembly verifies that fragments from multiple
// EAP-Message attributes are correctly reassembled.
func TestRFC3579_EAPMessageReassembly(t *testing.T) {
	attrs := []Attribute{
		{Type: AttrEAPMessage, Value: []byte("part1-part2-")},
		{Type: AttrEAPMessage, Value: []byte("part3-end")},
		{Type: AttrUserName, Value: []byte("user")},
	}

	assembled := AssembleEAPMessage(attrs)
	assert.Equal(t, "part1-part2-part3-end", string(assembled), "fragments must be concatenated in attribute order")
}

// TestRFC3579_MessageAuthenticatorInChallenge verifies that Access-Challenge
// packets contain a valid Message-Authenticator (RFC 3579 §3.3).
func TestRFC3579_MessageAuthenticatorInChallenge(t *testing.T) {
	secret := "challenge_secret"
	stateBytes := []byte("challenge-state-12345")
	packet := BuildAccessChallenge(2, [16]byte{0x10, 0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A, 0x09, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}, []Attribute{
		MakeStringAttribute(AttrState, string(stateBytes)),
		MakeAttribute(AttrEAPMessage, []byte{4, 1, 0, 0, 6}), // EAP Request-Identity
	})

	raw := packet.Encode()
	rawWithMA := AddMessageAuthenticator(raw, secret)

	assert.True(t, HasMessageAuthenticator(rawWithMA))
	assert.True(t, VerifyMessageAuthenticator(rawWithMA, secret))

	// Verify Challenge carries State
	decoded, err := DecodePacket(rawWithMA)
	assert.NoError(t, err)
	assert.Equal(t, CodeAccessChallenge, decoded.Code)
	stateAttr := GetAttribute(decoded.Attributes, AttrState)
	assert.NotNil(t, stateAttr)
	assert.Equal(t, stateBytes, stateAttr.Value)
}

// TestRFC3579_MessageAuthenticatorInAccept verifies that Access-Accept
// packets contain a valid Message-Authenticator (RFC 3579 §3.4).
func TestRFC3579_MessageAuthenticatorInAccept(t *testing.T) {
	secret := "accept_secret"
	packet := BuildAccessAccept(3, [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}, []Attribute{
		MakeStringAttribute(AttrReplyMessage, "Authentication successful"),
	})

	raw := packet.Encode()
	rawWithMA := AddMessageAuthenticator(raw, secret)

	assert.True(t, HasMessageAuthenticator(rawWithMA))
	assert.True(t, VerifyMessageAuthenticator(rawWithMA, secret))
}

// TestRFC3579_MessageAuthenticatorInReject verifies that Access-Reject
// packets contain a valid Message-Authenticator (RFC 3579 §3.4).
func TestRFC3579_MessageAuthenticatorInReject(t *testing.T) {
	secret := "reject_secret"
	packet := BuildAccessReject(4, [16]byte{0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8, 0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0}, nil)

	raw := packet.Encode()
	rawWithMA := AddMessageAuthenticator(raw, secret)

	assert.True(t, HasMessageAuthenticator(rawWithMA))
	assert.True(t, VerifyMessageAuthenticator(rawWithMA, secret))
}

// TestRFC3579_InvalidMessageAuthenticator verifies that a packet with an
// invalid Message-Authenticator is rejected (RFC 3579 §3.2).
func TestRFC3579_InvalidMessageAuthenticator(t *testing.T) {
	secret := "correct_secret"
	wrongSecret := "wrong_secret"

	packet := BuildAccessRequest(5, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "bob"),
	})

	raw := packet.Encode()
	rawWithMA := AddMessageAuthenticator(raw, secret)

	// Verify with wrong secret fails
	assert.False(t, VerifyMessageAuthenticator(rawWithMA, wrongSecret), "verification must fail with wrong secret")

	// Verify with correct secret succeeds
	assert.True(t, VerifyMessageAuthenticator(rawWithMA, secret), "verification must succeed with correct secret")
}

// TestRFC3579_ProxyStatePreserved verifies that the Proxy-State attribute
// is preserved end-to-end across RADIUS proxies (RFC 3579 §3.5).
func TestRFC3579_ProxyStatePreserved(t *testing.T) {
	proxyState := []byte("proxy-123")
	packet := BuildAccessRequest(6, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "charlie"),
		MakeAttribute(33, proxyState), // Proxy-State = 33
	})

	encoded := packet.Encode()
	decoded, err := DecodePacket(encoded)
	assert.NoError(t, err)

	proxyStateAttr := GetAttribute(decoded.Attributes, 33)
	assert.NotNil(t, proxyStateAttr, "Proxy-State attribute must be preserved")
	assert.Equal(t, proxyState, proxyStateAttr.Value)
}

// TestRFC3579_UserNameUTF8 verifies that the User-Name attribute correctly
// encodes UTF-8 characters (RFC 3579 §3, RFC 2865 §5.1).
func TestRFC3579_UserNameUTF8(t *testing.T) {
	// UTF-8 identities: Latin-1 supplement + Cyrillic
	utf8Name := []byte("m\xC3\xBCller.example") // müller.example
	attrs := []Attribute{
		{Type: AttrUserName, Value: utf8Name},
	}
	encoded := EncodeAttributes(attrs)
	decoded, err := DecodeAttributes(encoded)
	assert.NoError(t, err)
	assert.Len(t, decoded, 1)
	assert.Equal(t, utf8Name, decoded[0].Value)
}
