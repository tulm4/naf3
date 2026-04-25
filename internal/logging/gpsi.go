// Package logging provides structured logging utilities for NSSAAF.
// REQ-16: GPSI hashed in logs (SHA256, first 8 bytes, base64url) — never log raw GPSI.
package logging

import (
	"crypto/sha256"
	"encoding/base64"
)

// HashGPSI returns a hash of the GPSI for logging purposes.
// Format: SHA256(gpsi)[0:8], base64url encoded.
// Per REQ-16 and docs/design/19_observability.md §3.1.
func HashGPSI(gpsi string) string {
	h := sha256.Sum256([]byte(gpsi))
	return base64.RawURLEncoding.EncodeToString(h[:8])
}
