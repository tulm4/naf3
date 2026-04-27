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
	sealed := gcm.Seal(nil, nonce, plaintext, aad)
	nonceLen := len(nonce)
	tagLen := gcm.Overhead()
	ctLen := len(sealed) - nonceLen - tagLen
	return EncryptedData{
		Ciphertext: sealed[nonceLen : nonceLen+ctLen],
		Nonce:      nonce,
		Tag:        sealed[nonceLen+ctLen:],
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
	n, c, t := len(ed.Nonce), len(ed.Ciphertext), len(ed.Tag)
	sealed := make([]byte, n+c+t)
	copy(sealed[:n], ed.Nonce)
	copy(sealed[n:n+c], ed.Ciphertext)
	copy(sealed[n+c:], ed.Tag)
	return gcm.Open(nil, ed.Nonce, sealed, aad)
}

func DecryptWithTag(ciphertext, nonce, tag, key, aad []byte) ([]byte, error) {
	return Decrypt(EncryptedData{Ciphertext: ciphertext, Nonce: nonce, Tag: tag}, key, aad)
}
