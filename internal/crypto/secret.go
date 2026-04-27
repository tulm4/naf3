package crypto

import (
	"context"
	"time"
)

type EncryptedSecret struct {
	ID            string    	AaaConfigID  string    	Ciphertext   []byte    	Nonce        []byte    	Tag          []byte    	EncryptedDEK []byte    	DEKVersion  int       	Version      int       	CreatedAt    time.Time 	ExpiresAt    time.Time 	IsActive     bool      }

func EncryptSecret(ctx context.Context, plaintextSecret string, km KeyManager) (*EncryptedSecret, error) {
	ver, err := km.GetKeyVersion(ctx)
	if err != nil {
		return nil, err
	}

	dek, err := GenerateDEK()
	if err != nil {
		return nil, err
	}

	dataEnc, err := Encrypt([]byte(plaintextSecret), dek, nil)
	if err != nil {
		return nil, err
	}

	wrappedDEK, _, err := km.Wrap(ctx, dek)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	return &EncryptedSecret{
		Ciphertext:   dataEnc.Ciphertext,
		Nonce:        dataEnc.Nonce,
		Tag:          dataEnc.Tag,
		EncryptedDEK: wrappedDEK,
		DEKVersion:  ver,
		Version:      1,
		CreatedAt:    now,
		ExpiresAt:    now.Add(90 * 24 * time.Hour),
		IsActive:     true,
	}, nil
}

func DecryptSecret(ctx context.Context, es *EncryptedSecret, km KeyManager) (string, error) {
	dek, err := km.Unwrap(ctx, es.EncryptedDEK)
	if err != nil {
		return "", err
	}

	raw, err := Decrypt(EncryptedData{
		Ciphertext: es.Ciphertext,
		Nonce:     es.Nonce,
		Tag:       es.Tag,
	}, dek, nil)
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func RotateSecret(ctx context.Context, plaintextSecret string, km KeyManager) (*EncryptedSecret, error) {
	enc, err := EncryptSecret(ctx, plaintextSecret, km)
	if err != nil {
		return nil, err
	}
	enc.Version++
	return enc, nil
}
