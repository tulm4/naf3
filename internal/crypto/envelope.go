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
	// EncryptedDEK format: nonce(12) || Ciphertext || Tag(16)
	encDEK := make([]byte, 0, 12+len(dekEnc.Ciphertext)+16)
	encDEK = append(encDEK, dekEnc.Nonce...)
	encDEK = append(encDEK, dekEnc.Ciphertext...)
	encDEK = append(encDEK, dekEnc.Tag...)
	return &Envelope{
		Ciphertext:   dataEnc.Ciphertext,
		EncryptedDEK: encDEK,
		Nonce:        dataEnc.Nonce,
		DataTag:      dataEnc.Tag,
		DEKTag:       dekEnc.Tag,
		KEKVersion:   kekVersion,
	}, nil
}

func EnvelopeDecrypt(env *Envelope, kek []byte) ([]byte, error) {
	// EncryptedDEK format: nonce(12) || Ciphertext || Tag(16)
	if len(env.EncryptedDEK) < 28 { // minimum: 12 (nonce) + 0 (ciphertext) + 16 (tag)
		return nil, fmt.Errorf("envelope: EncryptedDEK too short: %d bytes", len(env.EncryptedDEK))
	}
	dekNonce := env.EncryptedDEK[:12]
	dekRemainder := env.EncryptedDEK[12:] // ciphertext || tag(16)
	dekTag := dekRemainder[len(dekRemainder)-16:]
	dekCiphertext := dekRemainder[:len(dekRemainder)-16]
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
