package crypto

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
)

func DeriveKey(ikm, salt, info []byte, length int) ([]byte, error) {
	if len(ikm) == 0 {
		return nil, errors.New("DeriveKey: ikm cannot be empty")
	}
	if length <= 0 || length > 255 {
		return nil, errors.New("DeriveKey: length must be 1-255")
	}
	var saltToUse []byte
	if salt == nil {
		saltToUse = make([]byte, 32)
	} else {
		saltToUse = salt
	}
	prk := hkdf.Extract(sha256.New, ikm, saltToUse)
	return hkdf.Expand(sha256.New, prk, info, length)
}

func SessionKEK(masterKey []byte, authCtxId string) ([]byte, error) {
	info := []byte("nssaa-session-kek:" + authCtxId)
	return DeriveKey(masterKey, nil, info, 32)
}

func TLSExporter(masterSecret []byte, label string, context []byte, length int) ([]byte, error) {
	prk := hkdf.Extract(sha256.New, masterSecret, nil)
	return hkdf.Expand(sha256.New, prk, []byte(label), length)
}
