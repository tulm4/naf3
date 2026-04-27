package crypto

import (
	"errors"
	"fmt"
)

var (
	ErrEnvelopeMalformed = errors.New("envelope: malformed encrypted DEK (must be exactly 28 bytes: 12-byte nonce + ciphertext + 16-byte GCM tag)")
	ErrDEKUnwrapFailed   = errors.New("envelope: DEK unwrap failed (wrong KEK?)")
)

// Envelope holds the result of envelope encryption.
// DEK is encrypted with KEK; ciphertext is encrypted with DEK.
// Both encryption steps use AES-256-GCM.
// Spec: docs/design/17_crypto.md §3.2
type Envelope struct {
	Ciphertext   []byte // encrypted with DEK
	EncryptedDEK []byte // DEK encrypted with KEK (nonce || ciphertext || tag = 12 + 32 + 16 = 60 bytes)
	Nonce        []byte // 12-byte nonce for data encryption
	DataTag      []byte // 16-byte GCM tag for ciphertext
	DEKTag       []byte // 16-byte GCM tag for encrypted DEK
	KEKVersion   int    // which KEK version encrypted the DEK
}

// EnvelopeEncrypt encrypts data with a fresh DEK, then encrypts the DEK with KEK.
// DEK is wrapped using AES-256-GCM ( Encrypt(DEK, KEK) ).
// Data is encrypted using AES-256-GCM ( Encrypt(plaintext, DEK) ).
func EnvelopeEncrypt(plaintext []byte, kek []byte, kekVersion int) (*Envelope, error) {
	// Generate per-item DEK
	dek, err := GenerateDEK()
	if err != nil {
		return nil, err
	}

	// Encrypt DEK with KEK (no AAD)
	dekEnc, err := Encrypt(dek, kek, nil)
	if err != nil {
		return nil, err
	}

	// Encrypt data with DEK (no AAD)
	dataEnc, err := Encrypt(plaintext, dek, nil)
	if err != nil {
		return nil, err
	}

	return &Envelope{
		Ciphertext:   dataEnc.Ciphertext,
		EncryptedDEK: append(dekEnc.Nonce, append(dekEnc.Ciphertext, dekEnc.Tag...)...),
		Nonce:        dataEnc.Nonce,
		DataTag:      dataEnc.Tag,
		DEKTag:       dekEnc.Tag,
		KEKVersion:   kekVersion,
	}, nil
}

// EnvelopeDecrypt decrypts an Envelope: first unwraps DEK with KEK, then decrypts data with DEK.
// Tries KEK of kekVersion; if unwrap fails, caller should retry with previous KEK version.
func EnvelopeDecrypt(env *Envelope, kek []byte) ([]byte, error) {
	// EncryptedDEK must be exactly 60 bytes: 12-byte nonce + 32-byte ciphertext (DEK) + 16-byte tag.
	// AES-256-GCM with a 32-byte DEK produces: nonce(12) + ciphertext(32) + tag(16) = 60 bytes.
	// Reject anything that is not exactly 60 bytes to avoid slice overlap bugs.
	if len(env.EncryptedDEK) != 60 {
		return nil, fmt.Errorf("envelope: EncryptedDEK must be exactly 60 bytes (12-byte nonce + 32-byte DEK ciphertext + 16-byte tag), got %d", len(env.EncryptedDEK))
	}
	dekNonce := env.EncryptedDEK[:12]
	dekTag := env.EncryptedDEK[12:28]
	dekCiphertext := env.EncryptedDEK[28:60] // 32 bytes (the encrypted DEK)

	dekEnc := EncryptedData{
		Ciphertext: dekCiphertext,
		Nonce:      dekNonce,
		Tag:        dekTag,
	}
	dek, err := Decrypt(dekEnc, kek, nil)
	if err != nil {
		return nil, ErrDEKUnwrapFailed
	}

	dataEnc := EncryptedData{
		Ciphertext: env.Ciphertext,
		Nonce:      env.Nonce,
		Tag:        env.DataTag,
	}
	return Decrypt(dataEnc, dek, nil)
}

// EnvelopeDecryptMulti attempts decryption with current KEK, then previous KEK.
// Used during KEK rotation overlap window.
func EnvelopeDecryptMulti(env *Envelope, currentKEK, previousKEK []byte) ([]byte, error) {
	pt, err := EnvelopeDecrypt(env, currentKEK)
	if err == nil {
		return pt, nil
	}
	if previousKEK != nil {
		pt, err = EnvelopeDecrypt(env, previousKEK)
		if err == nil {
			return pt, nil
		}
	}
	return nil, err
}
