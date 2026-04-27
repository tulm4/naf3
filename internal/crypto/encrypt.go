package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

var ErrCiphertextTooShort = errors.New("ciphertext too short")

type EncryptedData struct {
	Ciphertext []byte
	Nonce      []byte
	Tag        []byte
}

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
	// gcm.Seal(nil, nonce, plaintext, aad) produces ciphertext || tag (nonce passed separately to Open)
	sealed := gcm.Seal(nil, nonce, plaintext, aad)
	tagLen := gcm.Overhead()
	ctLen := len(sealed) - tagLen
	if ctLen < 0 {
		ctLen = 0
	}
	return EncryptedData{
		Ciphertext: sealed[:ctLen],
		Nonce:      nonce,
		Tag:        sealed[ctLen:],
	}, nil
}

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
	// Reconstruct ct||tag from separate Ciphertext and Tag fields
	ctTag := make([]byte, len(ed.Ciphertext)+len(ed.Tag))
	copy(ctTag, ed.Ciphertext)
	copy(ctTag[len(ed.Ciphertext):], ed.Tag)
	return gcm.Open(nil, ed.Nonce, ctTag, aad)
}

func DecryptWithTag(ciphertext, nonce, tag, key, aad []byte) ([]byte, error) {
	return Decrypt(EncryptedData{Ciphertext: ciphertext, Nonce: nonce, Tag: tag}, key, aad)
}
