// Package crypto provides cryptographic utilities: TLS, EAP key derivation,
// and data-at-rest encryption.
// Spec: TS 33.501 §16.3-16.5
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

// ErrCiphertextTooShort is returned when ciphertext is shorter than nonce+tag.
var ErrCiphertextTooShort = errors.New("ciphertext too short")

// EncryptedData holds the result of AES-GCM encryption.
// Nonce is 12 bytes; Tag is 16 bytes.
type EncryptedData struct {
	Ciphertext []byte // encrypted data (without nonce/tag prepended)
	Nonce      []byte // 12-byte random nonce
	Tag        []byte // 16-byte GCM authentication tag
}

// Encrypt encrypts plaintext using AES-256-GCM with a random 12-byte nonce.
// The nonce is prepended to the ciphertext for storage convenience.
// AAD is additional authenticated data (nil allowed); it is not used in Phase 5
// but is included for future extensibility.
func Encrypt(plaintext, key, aad []byte) (EncryptedData, error) {
	if len(key) != 32 {
		return EncryptedData{}, errors.New("Encrypt: key must be 32 bytes for AES-256")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return EncryptedData{}, errors.New("Encrypt: failed to create cipher: " + err.Error())
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedData{}, errors.New("Encrypt: failed to create GCM: " + err.Error())
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedData{}, errors.New("Encrypt: failed to generate nonce: " + err.Error())
	}
	ct := gcm.Seal(nil, nonce, plaintext, aad)
	// gcm.Seal appends ciphertext+tag; split them
	n := len(ct) - gcm.Overhead()
	return EncryptedData{
		Ciphertext: ct[:n],
		Nonce:      nonce,
		Tag:        ct[n:],
	}, nil
}

// Decrypt decrypts AES-256-GCM ciphertext produced by Encrypt.
// aad must match the aad used during encryption (nil if none).
func Decrypt(ed EncryptedData, key, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("Decrypt: key must be 32 bytes for AES-256")
	}
	if len(ed.Nonce) != 12 {
		return nil, errors.New("Decrypt: nonce must be 12 bytes")
	}
	if len(ed.Tag) != 16 {
		return nil, errors.New("Decrypt: tag must be 16 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("Decrypt: failed to create cipher: " + err.Error())
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("Decrypt: failed to create GCM: " + err.Error())
	}
	// Reconstruct sealed data: nonce || ciphertext || tag
	sealed := append(ed.Nonce, ed.Ciphertext...)
	sealed = append(sealed, ed.Tag...)
	return gcm.Open(nil, ed.Nonce, sealed, aad)
}

// DecryptWithTag decrypts using raw bytes (nonce, ciphertext, tag) instead of EncryptedData.
func DecryptWithTag(ciphertext, nonce, tag, key, aad []byte) ([]byte, error) {
	return Decrypt(EncryptedData{Ciphertext: ciphertext, Nonce: nonce, Tag: tag}, key, aad)
}
