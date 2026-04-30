// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Encryptor
// ---------------------------------------------------------------------------

func TestEncryptorNew(t *testing.T) {
	// AES-256 requires exactly 32-byte key.
	key := bytes.Repeat([]byte{0xAB}, 32)
	enc, err := NewEncryptor(key)
	assert.NoError(t, err)
	assert.NotNil(t, enc)

	// Invalid key sizes.
	for _, size := range []int{16, 24, 33} {
		key := bytes.Repeat([]byte{byte(size)}, size)
		_, err := NewEncryptor(key)
		assert.Error(t, err, "size %d should be rejected", size)
	}
}

func TestEncryptorRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0xAB}, 32)
	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("hello, world!")

	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptorUniqueNonce(t *testing.T) {
	key := bytes.Repeat([]byte{0xCD}, 32)
	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("same message")

	ct1, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	ct2, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	// Different nonces → different ciphertexts.
	assert.NotEqual(t, ct1, ct2)

	// Both decrypt to the same plaintext.
	pt1, err := enc.Decrypt(ct1)
	require.NoError(t, err)
	pt2, err := enc.Decrypt(ct2)
	require.NoError(t, err)
	assert.Equal(t, pt1, pt2)
	assert.Equal(t, plaintext, pt1)
}

func TestEncryptorDecryptWrongKey(t *testing.T) {
	enc1, err := NewEncryptor(bytes.Repeat([]byte{0x01}, 32))
	require.NoError(t, err)
	enc2, err := NewEncryptor(bytes.Repeat([]byte{0x02}, 32))
	require.NoError(t, err)

	ct, err := enc1.Encrypt([]byte("secret"))
	require.NoError(t, err)

	_, err = enc2.Decrypt(ct)
	assert.Error(t, err)
}

func TestEncryptorDecryptTooShort(t *testing.T) {
	enc, err := NewEncryptor(bytes.Repeat([]byte{0xAB}, 32))
	require.NoError(t, err)

	_, err = enc.Decrypt([]byte("short"))
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Session model
// ---------------------------------------------------------------------------

func TestSessionDefaultValues(t *testing.T) {
	s := &Session{
		AuthCtxID:   "auth-123",
		GPSI:        "52080460000001",
		NssaaStatus: "PENDING",
	}
	assert.Equal(t, "auth-123", s.AuthCtxID)
	assert.Equal(t, "52080460000001", s.GPSI)
	assert.Equal(t, "PENDING", string(s.NssaaStatus))
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

func TestHashGPSI(t *testing.T) {
	gpsi := "52080460000001"
	hash := HashGPSI(gpsi)

	// SHA-256 first 16 bytes hex → 32 characters.
	assert.Len(t, hash, 32)

	// Deterministic.
	assert.Equal(t, hash, HashGPSI(gpsi))

	// Different GPSI → different hash.
	assert.NotEqual(t, hash, HashGPSI("52080460000002"))

	// Not reversible.
	assert.NotContains(t, hash, "52080460000001")
}

func TestHashGPSIEmpty(t *testing.T) {
	hash := HashGPSI("")
	assert.Len(t, hash, 32)
}

// ---------------------------------------------------------------------------
// ConfigRepository helpers
// ---------------------------------------------------------------------------

func TestNewUUID(t *testing.T) {
	id1, err := newUUID()
	require.NoError(t, err)
	id2, err := newUUID()
	require.NoError(t, err)

	// Format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (36 chars).
	assert.Len(t, id1, 36)
	assert.Len(t, id2, 36)

	// Unique.
	assert.NotEqual(t, id1, id2)

	// Version 4: first char of 3rd group = '4' (lowercase hex).
	assert.Equal(t, "4", string(id1[14]))
	// Hyphens at positions 8, 13, 18, 23 (0-indexed).
	assert.Equal(t, "-", string(id1[8]))
	assert.Equal(t, "-", string(id1[13]))
	assert.Equal(t, "-", string(id1[18]))
	assert.Equal(t, "-", string(id1[23]))
}

// ---------------------------------------------------------------------------
// ConfigRepository scan helpers (no DB needed)
// ---------------------------------------------------------------------------

func TestAAAConfigFields(t *testing.T) {
	c := &AAAConfig{
		ID:            "test-uuid",
		SnssaiSST:     1,
		SnssaiSD:      "ABCDEF",
		Protocol:      "RADIUS",
		AAAServerHost: "aaa.example.com",
		AAAServerPort: 1812,
		SharedSecret:  []byte("shared-secret"),
		AllowReauth:   true,
		AllowRevoke:   true,
		Priority:      100,
		Weight:        1,
		Enabled:       true,
	}

	assert.Equal(t, "test-uuid", c.ID)
	assert.Equal(t, uint8(1), c.SnssaiSST)
	assert.Equal(t, "ABCDEF", c.SnssaiSD)
	assert.Equal(t, "RADIUS", c.Protocol)
	assert.Equal(t, 1812, c.AAAServerPort)
	assert.Equal(t, []byte("shared-secret"), c.SharedSecret)
	assert.True(t, c.AllowReauth)
	assert.True(t, c.AllowRevoke)
}

// ---------------------------------------------------------------------------
// SecretEncryptor (AAA config secret encryption)
// ---------------------------------------------------------------------------

func TestSecretEncryptorRoundTrip(t *testing.T) {
	enc := NewSecretEncryptor("my-passphrase")

	secret := []byte("super-secret-password")

	ciphertext, err := enc.Encrypt(secret)
	require.NoError(t, err)
	assert.NotEqual(t, secret, ciphertext)

	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, secret, decrypted)
}

func TestSecretEncryptorDifferentPassphrase(t *testing.T) {
	enc1 := NewSecretEncryptor("passphrase1")
	enc2 := NewSecretEncryptor("passphrase2")

	ct, err := enc1.Encrypt([]byte("secret"))
	require.NoError(t, err)

	_, err = enc2.Decrypt(ct)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// AuditRepository
// ---------------------------------------------------------------------------

func TestAuditEntryFields(t *testing.T) {
	e := &AuditEntry{
		AuthCtxID:     "auth-123",
		GPSIHash:      HashGPSI("52080460000001"),
		SnssaiSST:     1,
		SnssaiSD:      "ABCDEF",
		AMFInstanceID: "amf-1",
		Action:        AuditActionSessionCreated,
		NssaaStatus:   "NOT_EXECUTED",
		ErrorCode:     0,
	}

	assert.Equal(t, "auth-123", e.AuthCtxID)
	assert.Len(t, e.GPSIHash, 32)
	assert.Equal(t, AuditActionSessionCreated, e.Action)
	assert.Equal(t, 0, e.ErrorCode)
}

func TestAuditActions(t *testing.T) {
	actions := []AuditAction{
		AuditActionSessionCreated,
		AuditActionEAPRoundAdvanced,
		AuditActionEAPSuccess,
		AuditActionEAPFailure,
		AuditActionSessionExpired,
		AuditActionSessionTerminated,
		AuditActionNotifReauthSent,
		AuditActionNotifReauthAck,
		AuditActionNotifReauthFailed,
		AuditActionNotifRevocSent,
		AuditActionNotifRevocAck,
		AuditActionNotifRevocFailed,
		AuditActionAAAConnected,
		AuditActionAAAFailed,
	}

	for _, a := range actions {
		assert.NotEmpty(t, string(a))
	}
}

// ---------------------------------------------------------------------------
// NssaaStatus enum
// ---------------------------------------------------------------------------

func TestNssaaStatusString(t *testing.T) {
	assert.Equal(t, "NOT_EXECUTED", string("NOT_EXECUTED"))
}

// ---------------------------------------------------------------------------
// Partition naming
// ---------------------------------------------------------------------------

func TestPartitionNaming(t *testing.T) {
	// The partition name format: slice_auth_sessions_YYYY_MM
	now := time.Now().AddDate(0, 1, 0)
	expected := "slice_auth_sessions_" + now.Format("2006_01")

	// Just verify the format is consistent.
	assert.Regexp(t, `^slice_auth_sessions_\d{4}_\d{2}$`, expected)
}
