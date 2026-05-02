package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// ErrDecryptFailed is returned when AEAD decryption detects tampering
// (wrong key, corrupted ciphertext, or truncated data).
var ErrDecryptFailed = errors.New("crypto: decryption failed")

// ErrKeyWrongLength is returned when the key is not 32 bytes for AES-256.
var ErrKeyWrongLength = errors.New("crypto: key must be 32 bytes for AES-256")

// EncryptConcat encrypts plaintext using AES-256-GCM and returns the result
// as a single concatenated byte string: nonce (12 bytes) || ciphertext || tag (16 bytes).
// This is the format expected by the PostgreSQL storage layer.
//
// Spec: NIST SP 800-38D (GCM mode).
func EncryptConcat(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrKeyWrongLength
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	// gcm.Seal returns nonce || ciphertext || tag
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptConcat decrypts ciphertext produced by EncryptConcat.
// The ciphertext must be nonce (12 bytes) || ciphertext || tag (16 bytes).
// Returns ErrDecryptFailed if the key is wrong or the ciphertext is tampered.
func DecryptConcat(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrKeyWrongLength
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize+gcm.Overhead() {
		return nil, ErrDecryptFailed
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// FromPassphrase derives a 32-byte key from a passphrase using SHA-256.
// This is a one-way key derivation — the passphrase cannot be recovered.
func FromPassphrase(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}
