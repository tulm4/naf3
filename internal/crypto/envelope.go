package crypto

import (
	"errors"
	"fmt"
)

var (
	ErrEnvelopeMalformed = errors.New("envelope: malformed encrypted DEK")
	ErrDEKUnwrapFailed  = errors.New("envelope: DEK unwrap failed (wrong KEK?)")
)

type Envelope struct {
	Ciphertext   []byte
	EncryptedDEK []byte
	Nonce        []byte
	DataTag      []byte
	DEKTag       []byte
	KEKVersion   int
}

func EnvelopeEncrypt(plaintext []byte, kek []byte, kekVersion int) (*Envelope, error) {
	dek, err := GenerateDEK()
	if err != nil {
		return nil, err
	}
	dekEnc, err := Encrypt(dek, kek, nil)
	if err != nil {
		return nil, err
	}
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

func EnvelopeDecrypt(env *Envelope, kek []byte) ([]byte, error) {
	if len(env.EncryptedDEK) != 60 {
		return nil, fmt.Errorf("envelope: EncryptedDEK must be exactly 60 bytes, got %d", len(env.EncryptedDEK))
	}
	dekNonce := env.EncryptedDEK[:12]
	dekCiphertext := env.EncryptedDEK[12:44]
	dekTag := env.EncryptedDEK[44:60]
	dekEnc := EncryptedData{Ciphertext: dekCiphertext, Nonce: dekNonce, Tag: dekTag}
	dek, err := Decrypt(dekEnc, kek, nil)
	if err != nil {
		return nil, ErrDEKUnwrapFailed
	}
	dataEnc := EncryptedData{Ciphertext: env.Ciphertext, Nonce: env.Nonce, Tag: env.DataTag}
	return Decrypt(dataEnc, dek, nil)
}

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
