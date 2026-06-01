// Package postgres provides PostgreSQL data persistence for NSSAAF.
package postgres

import (
	"encoding/base64"
)

// encryptField encrypts a string value and returns base64-encoded ciphertext.
// Returns empty string for empty input (no-op).
func encryptField(enc *encryptor, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	ciphertext, err := enc.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptField decrypts a base64-encoded ciphertext back to plaintext.
// Returns empty string for empty input (no-op).
// Returns empty string, error if decryption fails.
func decryptField(enc *encryptor, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// encryptState encrypts raw bytes. Delegates to the encryptor's Encrypt method.
func encryptState(enc *encryptor, state []byte) ([]byte, error) {
	return enc.Encrypt(state)
}

// decryptState decrypts ciphertext bytes. Delegates to the encryptor's Decrypt method.
func decryptState(enc *encryptor, ciphertext []byte) ([]byte, error) {
	return enc.Decrypt(ciphertext)
}
