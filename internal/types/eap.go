// Package types provides 3GPP data types for NSSAAF.
// Spec: TS 33.501 §5.13, RFC 3748, RFC 5216
package types

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// EAP message types used in NSSAA procedures.
// Spec: RFC 3748 §4, TS 33.501 §5.13
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

// EAP method types.
// Spec: RFC 3748 §5, TS 33.501 §5.13
const (
	EapMethodIdentity EapMethod = 1  // EAP-Identity/NAK
	EapMethodTLS      EapMethod = 13 // EAP-TLS (RFC 5216)
	EapMethodTTLS     EapMethod = 21 // EAP-TTLS
	EapMethodAKAPrime EapMethod = 50 // EAP-AKA' (RFC 5448)
	EapMethodPEAP     EapMethod = 26 // PEAP
)

// EapMethod represents an EAP authentication method.
type EapMethod uint8

// String implements fmt.Stringer.
func (m EapMethod) String() string {
	switch m {
	case EapMethodIdentity:
		return "EAP-Identity"
	case EapMethodTLS:
		return "EAP-TLS"
	case EapMethodTTLS:
		return "EAP-TTLS"
	case EapMethodAKAPrime:
		return "EAP-AKA'"
	case EapMethodPEAP:
		return "PEAP"
	default:
		return fmt.Sprintf("EAP-Method(%d)", m)
	}
}

// EapMessage is a Base64-encoded EAP payload as defined in RFC 3748.
// It is used in NSSAAF SBI requests and responses.
// Spec: TS 29.526 §7.2, RFC 3748 §4
type EapMessage string

// Validate checks that the EAP message is a non-empty, valid Base64 string.
func (m EapMessage) Validate() error {
	if string(m) == "" {
		return &ValidationError{
			Field:      "eapMessage",
			Reason:     "EAP message is required",
			HTTPStatus: 400,
			Cause:      CauseMissingEapPayload,
		}
	}
	_, err := base64.StdEncoding.DecodeString(string(m))
	if err != nil {
		return &ValidationError{
			Field:      "eapMessage",
			Reason:     "EAP message must be valid Base64 (RFC 3748)",
			HTTPStatus: 400,
			Cause:      CauseInvalidEapPayload,
		}
	}
	return nil
}

// Bytes decodes the Base64 EAP message to raw bytes.
func (m EapMessage) Bytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(string(m))
}

// NewEapMessage encodes raw bytes as a Base64 EAP message.
func NewEapMessage(data []byte) EapMessage {
	return EapMessage(base64.StdEncoding.EncodeToString(data))
}

// String implements fmt.Stringer.
func (m EapMessage) String() string { return string(m) }

// MarshalJSON implements json.Marshaler.
func (m EapMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(m))
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *EapMessage) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("failed to unmarshal EapMessage: %w", err)
	}
	*m = EapMessage(s)
	return nil
}
