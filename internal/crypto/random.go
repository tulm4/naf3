package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, errors.New("RandomBytes: " + err.Error())
	}
	return b, nil
}

func RandomHexString(n int) (string, error) {
	if n%2 != 0 {
		return "", errors.New("RandomHexString: n must be even")
	}
	b, err := RandomBytes(n / 2)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GCMNonce() ([]byte, error) {
	return RandomBytes(12)
}
