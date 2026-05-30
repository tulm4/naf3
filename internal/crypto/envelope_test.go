package crypto

import (
	"bytes"
	"testing"
)

// TestEnvelopeDecryptCorruptedCiphertext verifies that EnvelopeDecrypt returns an error
// when the encrypted DEK (Ciphertext portion) has been tampered with.
func TestEnvelopeDecryptCorruptedCiphertext(t *testing.T) {
	kek := bytes.Repeat([]byte{0xCC}, 32)
	plaintext := []byte("test data for corruption")

	env, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	// Corrupt the encrypted DEK (tamper with the DEK ciphertext portion)
	corruptedDEK := make([]byte, len(env.EncryptedDEK))
	copy(corruptedDEK, env.EncryptedDEK)
	// EncryptedDEK format: nonce(12) || ciphertext || tag(16)
	// Corrupt a byte in the ciphertext portion (after nonce, before tag)
	if len(corruptedDEK) > 28 {
		corruptedDEK[12] ^= 0xFF
	}
	env.EncryptedDEK = corruptedDEK

	_, err = EnvelopeDecrypt(env, kek)
	if err == nil {
		t.Error("EnvelopeDecrypt with corrupted DEK ciphertext: expected error, got nil")
	}
}

// TestEnvelopeDecryptCorruptedDataCiphertext verifies that EnvelopeDecrypt returns an error
// when the data ciphertext has been tampered with.
func TestEnvelopeDecryptCorruptedDataCiphertext(t *testing.T) {
	kek := bytes.Repeat([]byte{0xDD}, 32)
	plaintext := []byte("data ciphertext corruption test")

	env, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	// Corrupt the data ciphertext
	corruptedCiphertext := make([]byte, len(env.Ciphertext))
	copy(corruptedCiphertext, env.Ciphertext)
	corruptedCiphertext[0] ^= 0xFF
	env.Ciphertext = corruptedCiphertext

	_, err = EnvelopeDecrypt(env, kek)
	if err == nil {
		t.Error("EnvelopeDecrypt with corrupted data ciphertext: expected error, got nil")
	}
}

// TestEnvelopeDecryptCorruptedDataTag verifies that EnvelopeDecrypt returns an error
// when the data authentication tag has been tampered with.
func TestEnvelopeDecryptCorruptedDataTag(t *testing.T) {
	kek := bytes.Repeat([]byte{0xEE}, 32)
	plaintext := []byte("tag corruption test")

	env, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	// Corrupt the data tag
	corruptedTag := make([]byte, len(env.DataTag))
	copy(corruptedTag, env.DataTag)
	corruptedTag[0] ^= 0xFF
	env.DataTag = corruptedTag

	_, err = EnvelopeDecrypt(env, kek)
	if err == nil {
		t.Error("EnvelopeDecrypt with corrupted tag: expected error, got nil")
	}
}

// TestEnvelopeDecryptMultiBothFail verifies that EnvelopeDecryptMulti returns an error
// when both current and previous KEKs fail to decrypt.
func TestEnvelopeDecryptMultiBothFail(t *testing.T) {
	currentKEK := bytes.Repeat([]byte{0x11}, 32)
	previousKEK := bytes.Repeat([]byte{0x22}, 32)
	wrongKEK := bytes.Repeat([]byte{0x33}, 32)
	plaintext := []byte("both fail test")

	env, err := EnvelopeEncrypt(plaintext, wrongKEK, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	_, err = EnvelopeDecryptMulti(env, currentKEK, previousKEK)
	if err == nil {
		t.Error("EnvelopeDecryptMulti with both wrong KEKs: expected error, got nil")
	}
}

// TestEnvelopeDecryptTruncatedEncryptedDEK verifies that EnvelopeDecrypt returns an error
// when the EncryptedDEK field is too short (below minimum of 28 bytes).
func TestEnvelopeDecryptTruncatedEncryptedDEK(t *testing.T) {
	kek := bytes.Repeat([]byte{0x55}, 32)
	plaintext := []byte("truncated test")

	env, err := EnvelopeEncrypt(plaintext, kek, 1)
	if err != nil {
		t.Fatalf("EnvelopeEncrypt: %v", err)
	}

	// Truncate the encrypted DEK to be too short (minimum is 28 bytes: 12 nonce + 0 ciphertext + 16 tag)
	env.EncryptedDEK = env.EncryptedDEK[:20]

	_, err = EnvelopeDecrypt(env, kek)
	if err == nil {
		t.Error("EnvelopeDecrypt with truncated EncryptedDEK: expected error, got nil")
	}
}

// TestEnvelopeEncryptVariousSizes verifies envelope encryption/decryption with various plaintext sizes
// including block-aligned and non-block-aligned sizes.
func TestEnvelopeEncryptVariousSizes(t *testing.T) {
	kek := bytes.Repeat([]byte{0x66}, 32)
	testCases := []struct {
		name     string
		plaintext []byte
	}{
		{"16 bytes", bytes.Repeat([]byte{0x01}, 16)},
		{"64 bytes", bytes.Repeat([]byte{0x02}, 64)},
		{"1KB", bytes.Repeat([]byte{0x03}, 1024)},
		{"exact block size (32 bytes)", bytes.Repeat([]byte{0x04}, 32)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env, err := EnvelopeEncrypt(tc.plaintext, kek, 1)
			if err != nil {
				t.Fatalf("EnvelopeEncrypt: %v", err)
			}

			decrypted, err := EnvelopeDecrypt(env, kek)
			if err != nil {
				t.Fatalf("EnvelopeDecrypt: %v", err)
			}
			if !bytes.Equal(decrypted, tc.plaintext) {
				t.Errorf("round-trip mismatch for %s: got %d bytes, want %d", tc.name, len(decrypted), len(tc.plaintext))
			}
		})
	}
}
