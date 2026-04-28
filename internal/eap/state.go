// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"fmt"
)

// EAP packet codes as defined in RFC 3748 §4.
// Spec: RFC 3748 §4
const (
	CodeRequest  Code = 1
	CodeResponse Code = 2
	CodeSuccess  Code = 3
	CodeFailure  Code = 4
)

// Code represents the code of an EAP packet.
// Spec: RFC 3748 §4
type Code uint8

// String implements fmt.Stringer.
func (c Code) String() string {
	switch c {
	case CodeRequest:
		return "REQUEST"
	case CodeResponse:
		return "RESPONSE"
	case CodeSuccess:
		return "SUCCESS"
	case CodeFailure:
		return "FAILURE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", c)
	}
}

// IsValid reports whether c is a known EAP code.
func (c Code) IsValid() bool {
	return c >= CodeRequest && c <= CodeFailure
}

// EAP method types.
// Spec: RFC 3748 §5, TS 33.501 §5.13
const (
	MethodIdentity     Method = 1  // EAP-Identity/NAK
	MethodNotification Method = 2  // RFC 3748 §5.2
	MethodNak          Method = 3  // RFC 3748 §5.3
	MethodTLS          Method = 13 // EAP-TLS (RFC 5216)
	MethodTTLS         Method = 21 // EAP-TTLS (RFC 5281)
	MethodPEAP         Method = 26 // PEAP
	MethodAKAPrime     Method = 50 // EAP-AKA' (RFC 5448)
)

// Method represents an EAP authentication method.
type Method uint8

// String implements fmt.Stringer.
func (m Method) String() string {
	switch m {
	case MethodIdentity:
		return "Identity"
	case MethodNotification:
		return "Notification"
	case MethodNak:
		return "NAK"
	case MethodTLS:
		return "EAP-TLS"
	case MethodTTLS:
		return "EAP-TTLS"
	case MethodPEAP:
		return "PEAP"
	case MethodAKAPrime:
		return "EAP-AKA'"
	default:
		return fmt.Sprintf("Method(%d)", m)
	}
}

// IsValid reports whether m is a known EAP method.
func (m Method) IsValid() bool {
	switch m {
	case MethodIdentity, MethodNotification, MethodNak,
		MethodTLS, MethodTTLS, MethodPEAP, MethodAKAPrime:
		return true
	default:
		return false
	}
}

// IsTunneled reports whether m is a tunneled method that wraps other methods.
func (m Method) IsTunneled() bool {
	switch m {
	case MethodTTLS, MethodPEAP:
		return true
	default:
		return false
	}
}

// EAP-TLS flags as defined in RFC 5216 §2.1.5.
// Spec: RFC 5216 §2.1.5
const (
	TLSFlagsMoreFrags TLSFlags = 0x40 // More fragments follow
	TLSFlagsLength    TLSFlags = 0x20 // Total length field is present
	TLSFlagsReserved  TLSFlags = 0x1F // Reserved (must be zero)
)

// TLSFlags represents the flags field in EAP-TLS packets.
type TLSFlags uint8

// HasMoreFragments returns true if the More Fragments flag is set.
func (f TLSFlags) HasMoreFragments() bool { return f&TLSFlagsMoreFrags != 0 }

// HasLength returns true if the Length flag is set.
func (f TLSFlags) HasLength() bool { return f&TLSFlagsLength != 0 }

// Result describes the outcome of an EAP round.
type Result int

const (
	// ResultContinue means more rounds are needed.
	ResultContinue Result = iota // More rounds needed
	// ResultSuccess means EAP authentication succeeded.
	// ResultSuccess means EAP authentication succeeded.
	ResultSuccess // EAP-Success received
	// ResultFailure means EAP authentication failed.
	ResultFailure // EAP-Failure received
	// ResultIgnored means the message was ignored (duplicate or out-of-order).
	ResultIgnored // Message ignored (duplicate/out-of-order)
	// ResultTimeout means no response was received within the timeout period.
	ResultTimeout // No response within timeout
)

// String implements fmt.Stringer.
func (r Result) String() string {
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
		return fmt.Sprintf("Result(%d)", r)
	}
}
