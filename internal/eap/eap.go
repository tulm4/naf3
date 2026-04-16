// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"time"
)

// nowUnixImpl returns the current Unix timestamp in seconds.
// Extracted to a separate function for testability.
func nowUnixImpl() int64 {
	return time.Now().Unix()
}
