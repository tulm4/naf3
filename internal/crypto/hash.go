package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HashGPSI returns SHA-256 hash of GPSI, truncated to first 16 bytes (32 hex chars).
// Used in audit logs and telemetry to protect subscriber privacy.
func HashGPSI(gpsi string) string {
	h := sha256.Sum256([]byte(gpsi))
	return hex.EncodeToString(h[:16])
}

// HashSUPI returns SHA-256 hash of SUPI, truncated to first 16 bytes.
// Used in AIW audit logs.
func HashSUPI(supi string) string {
	h := sha256.Sum256([]byte(supi))
	return hex.EncodeToString(h[:16])
}

// HashMessage returns SHA-256 hash of EAP message for idempotency.
func HashMessage(msg []byte) string {
	h := sha256.Sum256(msg)
	return hex.EncodeToString(h[:])
}

// HMACSHA256 returns HMAC-SHA-256 of data with given key.
func HMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// VerifyHMAC checks if the provided MAC matches computed MAC using constant-time comparison.
func VerifyHMAC(key, data, mac []byte) bool {
	expected := HMACSHA256(key, data)
	return hmac.Equal(expected, mac)
}
