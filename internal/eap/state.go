// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"fmt"
)

// EAP packet codes as defined in RFC 3748 §4.
// Spec: RFC 3748 §4
const (
	EapCodeRequest  EapCode = 1
	EapCodeResponse EapCode = 2
	EapCodeSuccess  EapCode = 3
	EapCodeFailure  EapCode = 4
)

// EapCode represents the code of an EAP packet.
// Spec: RFC 3748 §4
type EapCode uint8

// String implements fmt.Stringer.
func (c EapCode) String() string {
	switch c {
	case EapCodeRequest:
		return "REQUEST"
	case EapCodeResponse:
		return "RESPONSE"
	case EapCodeSuccess:
		return "SUCCESS"
	case EapCodeFailure:
		return "FAILURE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", c)
	}
}

// IsValid reports whether c is a known EAP code.
func (c EapCode) IsValid() bool {
	return c >= EapCodeRequest && c <= EapCodeFailure
}

// EAP method types.
// Spec: RFC 3748 §5, TS 33.501 §5.13
const (
	EapMethodIdentity  EapMethod = 1  // EAP-Identity/NAK
	EapMethodNotification EapMethod = 2 // RFC 3748 §5.2
	EapMethodNak       EapMethod = 3  // RFC 3748 §5.3
	EapMethodTLS      EapMethod = 13 // EAP-TLS (RFC 5216)
	EapMethodTTLS     EapMethod = 21 // EAP-TTLS (RFC 5281)
	EapMethodPEAP     EapMethod = 26 // PEAP
	EapMethodAKAPrime EapMethod = 50 // EAP-AKA' (RFC 5448)
)

// EapMethod represents an EAP authentication method.
type EapMethod uint8

// String implements fmt.Stringer.
func (m EapMethod) String() string {
	switch m {
	case EapMethodIdentity:
		return "Identity"
	case EapMethodNotification:
		return "Notification"
	case EapMethodNak:
		return "NAK"
	case EapMethodTLS:
		return "EAP-TLS"
	case EapMethodTTLS:
		return "EAP-TTLS"
	case EapMethodPEAP:
		return "PEAP"
	case EapMethodAKAPrime:
		return "EAP-AKA'"
	default:
		return fmt.Sprintf("Method(%d)", m)
	}
}

// IsValid reports whether m is a known EAP method.
func (m EapMethod) IsValid() bool {
	switch m {
	case EapMethodIdentity, EapMethodNotification, EapMethodNak,
		EapMethodTLS, EapMethodTTLS, EapMethodPEAP, EapMethodAKAPrime:
		return true
	default:
		return false
	}
}

// IsTunneled reports whether m is a tunneled method that wraps other methods.
func (m EapMethod) IsTunneled() bool {
	switch m {
	case EapMethodTTLS, EapMethodPEAP:
		return true
	default:
		return false
	}
}

// EAP-TLS flags as defined in RFC 5216 §2.1.5.
// Spec: RFC 5216 §2.1.5
const (
	EapTlsFlagsMoreFrags  EapTlsFlags = 0x40 // More fragments follow
	EapTlsFlagsLength      EapTlsFlags = 0x20 // Total length field is present
	EapTlsFlagsReserved    EapTlsFlags = 0x1F // Reserved (must be zero)
)

// EapTlsFlags represents the flags field in EAP-TLS packets.
type EapTlsFlags uint8

// HasMoreFragments returns true if the More Fragments flag is set.
func (f EapTlsFlags) HasMoreFragments() bool { return f&EapTlsFlagsMoreFrags != 0 }

// HasLength returns true if the Length flag is set.
func (f EapTlsFlags) HasLength() bool { return f&EapTlsFlagsLength != 0 }

// EapResult describes the outcome of an EAP round.
type EapResult int

const (
	ResultContinue EapResult = iota // More rounds needed
	ResultSuccess                    // EAP-Success received
	ResultFailure                    // EAP-Failure received
	ResultIgnored                    // Message ignored (duplicate/out-of-order)
	ResultTimeout                    // No response within timeout
)

// String implements fmt.Stringer.
func (r EapResult) String() string {
	switch r {
	case ResultContinue:
		return "CONTINUE"
	case ResultSuccess:
		return "SUCCESS"
	case ResultFailure:
		return "FAILURE"
	case ResultIgnored:
		return "IGNORED"
	case ResultTimeout:
		return "TIMEOUT"
	default:
		return fmt.Sprintf("EapResult(%d)", r)
	}
}
