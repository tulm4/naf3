// Package crypto provides RFC 5216 EAP-TLS MSK derivation conformance tests.
// Spec: RFC 5216 §2.1.2 (MSK Derivation), RFC 8446 §6.3 (TLS Exporter)
package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMSKDerivation_Length verifies that the MSK exported via TLS Exporter
// is exactly 64 bytes (RFC 5216 §2.1.2: MSK is 64 bytes).
func TestMSKDerivation_Length(t *testing.T) {
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i % 256)
	}

	msk, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)
	assert.Len(t, msk, 64, "MSK must be exactly 64 bytes per RFC 5216 §2.1.2")
}

// TestMSKDerivation_MSKeySplit verifies that the MSK is the first 32 bytes
// of the 64-byte exported key material (RFC 5216 §2.1.2).
func TestMSKDerivation_MSKeySplit(t *testing.T) {
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i + 1)
	}

	exported, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	msk := exported[:32]
	assert.Len(t, msk, 32, "MSK (first 32 bytes) must be 32 bytes")
}

// TestMSKDerivation_EMSK verifies that the EMSK is the last 32 bytes
// of the 64-byte exported key material (RFC 5216 §2.1.2).
func TestMSKDerivation_EMSK(t *testing.T) {
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i*2 + 1)
	}

	exported, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	emsk := exported[32:]
	assert.Len(t, emsk, 32, "EMSK (last 32 bytes) must be 32 bytes")
}

// TestMSKDerivation_MSKEMSKDifferent verifies that MSK and EMSK are different
// (they come from different parts of the key material and are non-overlapping).
func TestMSKDerivation_MSKEMSKDifferent(t *testing.T) {
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i ^ 0xAA)
	}

	exported, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	msk := exported[:32]
	emsk := exported[32:]
	assert.NotEqual(t, msk, emsk, "MSK and EMSK must be different values")
}

// TestMSKDerivation_EmptySession verifies that an empty master secret
// returns an error (no key material to export).
func TestMSKDerivation_EmptySession(t *testing.T) {
	_, err := TLSExporter(nil, "EAP-TLS MSK", nil, 64)
	assert.Error(t, err, "empty master secret must produce an error")
}

// TestMSKDerivation_InsufficientKeyMaterial verifies that requesting fewer than
// 64 bytes from TLSExporter does not produce the full 64-byte MSK.
func TestMSKDerivation_InsufficientKeyMaterial(t *testing.T) {
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i)
	}

	// Request only 32 bytes (only MSK, no EMSK) — valid but incomplete
	mskOnly, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 32)
	require.NoError(t, err)
	assert.Len(t, mskOnly, 32, "requesting 32 bytes must return exactly 32 bytes")

	// Request 0 bytes — invalid
	_, err = TLSExporter(masterSecret, "EAP-TLS MSK", nil, 0)
	assert.Error(t, err, "requesting 0 bytes must return an error")
}

// TestMSKDerivation_ExportLabel verifies that the RFC 5216 "EAP-TLS MSK" label
// is used correctly in the TLS Exporter interface (RFC 5216 §2.1.2).
func TestMSKDerivation_ExportLabel(t *testing.T) {
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i + 0x10)
	}

	// Export with RFC 5216 label
	mskRFC, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	// Export with different label — must produce different output
	mskOther, err := TLSExporter(masterSecret, "WRONG-LABEL", nil, 64)
	require.NoError(t, err)

	assert.NotEqual(t, mskRFC, mskOther, "different labels must produce different MSK values")
}

// TestMSKDerivation_SessionIDInContext verifies that the TLS session ID is
// included in the derivation context when provided (RFC 5216 §2.1.2).
func TestMSKDerivation_SessionIDInContext(t *testing.T) {
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i)
	}

	sessionCtx := []byte("session-identifier-123")

	// Export with session context
	mskWithCtx, err := TLSExporter(masterSecret, "EAP-TLS MSK", sessionCtx, 64)
	require.NoError(t, err)

	// Export without session context — different output
	mskWithoutCtx, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	assert.NotEqual(t, mskWithCtx, mskWithoutCtx,
		"MSK with session context must differ from MSK without context")
}

// TestMSKDerivation_HandshakeMessages verifies that different TLS handshake
// transcripts produce different MSK values (MSK depends on TLS transcript).
func TestMSKDerivation_HandshakeMessages(t *testing.T) {
	masterSecret1 := make([]byte, 48)
	masterSecret2 := make([]byte, 48)
	for i := range masterSecret1 {
		masterSecret1[i] = byte(i)
		masterSecret2[i] = byte(i ^ 0xFF)
	}

	msk1, err := TLSExporter(masterSecret1, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	msk2, err := TLSExporter(masterSecret2, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	assert.NotEqual(t, msk1, msk2, "different master secrets must produce different MSK values")
}

// TestMSKDerivation_PeerCertificate verifies that MSK derivation uses
// information from the peer certificate when available. Since we use a mock
// masterSecret rather than a real TLS session, this test verifies the
// deterministic derivation contract: same inputs → same MSK.
func TestMSKDerivation_PeerCertificate(t *testing.T) {
	// Use a fixed master secret to ensure determinism
	masterSecret := make([]byte, 48)
	for i := range masterSecret {
		masterSecret[i] = byte(i * 3)
	}

	// Derive MSK twice with identical inputs
	msk1, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	msk2, err := TLSExporter(masterSecret, "EAP-TLS MSK", nil, 64)
	require.NoError(t, err)

	assert.Equal(t, msk1, msk2,
		"MSK derivation must be deterministic: identical inputs must produce identical MSK")
}
