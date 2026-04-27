package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

func HashGPSI(gpsi string) string {
	h := sha256.Sum256([]byte(gpsi))
	return hex.EncodeToString(h[:16])
}

func HashSUPI(supi string) string {
	h := sha256.Sum256([]byte(supi))
	return hex.EncodeToString(h[:16])
}

func HashMessage(msg []byte) string {
	h := sha256.Sum256(msg)
	return hex.EncodeToString(h[:])
}

func HMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func VerifyHMAC(key, data, mac []byte) bool {
	expected := HMACSHA256(key, data)
	return hmac.Equal(expected, mac)
}
