// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMessageAuthenticatorFunctions tests the core MA functions.
func TestMessageAuthenticatorFunctions(t *testing.T) {
	// Build a minimal packet with State attribute
	stateAttr := []byte{24, 9, 's', 't', 'a', 't', 'e', '1', '2'}
	
	// Build packet without MA first to compute correct length
	totalLen := 20 + len(stateAttr) + 18
	pkt := make([]byte, totalLen)
	pkt[0] = 43 // CoA-Request
	pkt[1] = 12 // ID
	pkt[2] = byte(totalLen >> 8)
	pkt[3] = byte(totalLen)
	
	// Zero the Request Authenticator (bytes 4-19) for CoA/DM per RFC 5176 §3.2
	for i := 4; i < 20; i++ {
		pkt[i] = 0
	}
	
	// Copy State attribute
	copy(pkt[20:], stateAttr)
	
	// Write Message-Authenticator attribute header: type=80, length=18
	maOffset := 20 + len(stateAttr)
	pkt[maOffset] = 80   // Message-Authenticator type
	pkt[maOffset+1] = 18 // Length
	
	// Verify HasMessageAuthenticator works
	assert.True(t, HasMessageAuthenticator(pkt), "should find MA")
	
	// Verify FindMessageAuthenticator returns correct offset
	idx := FindMessageAuthenticator(pkt)
	assert.Equal(t, maOffset, idx, "MA should be at offset %d", maOffset)
	
	// Compute MA (with zeroed Request Authenticator)
	ma := ComputeMessageAuthenticator(pkt, "secret")
	assert.Len(t, ma, 16, "MA should be 16 bytes")
	
	// Write MA to packet
	copy(pkt[maOffset+2:], ma)
	
	// Verify
	result := VerifyMessageAuthenticator(pkt, "secret")
	t.Logf("Verify result: %v", result)
	assert.True(t, result, "MA should verify with correct secret")
	assert.False(t, VerifyMessageAuthenticator(pkt, "wrong"), "MA should not verify with wrong secret")
	
	// Test that tampering with the packet fails verification
	pktCopy := make([]byte, len(pkt))
	copy(pktCopy, pkt)
	pktCopy[maOffset+2] ^= 0xFF // Flip all bits in first byte of MA
	assert.False(t, VerifyMessageAuthenticator(pktCopy, "secret"), "tampered MA should not verify")
}
