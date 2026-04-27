package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

// RandomBytes returns n cryptographically secure random bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, errors.New("RandomBytes: failed to read random bytes: " + err.Error())
	}
	return b, nil
}

// RandomHexString returns a random hex string of length n (n must be even).
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

// GCMNonce returns a random 12-byte nonce for AES-GCM.
func GCMNonce() ([]byte, error) {
	return RandomBytes(12)
}
