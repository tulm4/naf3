// Package types provides 3GPP data types for NSSAAF.
// Spec: TS 29.571 §5.4.4.60, TS 23.502 §4.2.9
package types

import (
	"encoding/json"
	"fmt"
)

// NSSAA authentication status values.
// Spec: TS 29.571 §5.4.4.60
const (
	// NssaaStatusNotExecuted means NSSAA has not been executed for this S-NSSAI yet.
	NssaaStatusNotExecuted NssaaStatus = "NOT_EXECUTED"
	// NssaaStatusPending means NSSAA is in progress (EAP exchange ongoing).
	NssaaStatusPending NssaaStatus = "PENDING"
	// NssaaStatusEapSuccess means EAP authentication completed successfully.
	NssaaStatusEapSuccess NssaaStatus = "EAP_SUCCESS"
	// NssaaStatusEapFailure means EAP authentication failed.
	NssaaStatusEapFailure NssaaStatus = "EAP_FAILURE"
)

// NssaaStatus represents the status of NSSAA for a specific S-NSSAI.
// Spec: TS 29.571 §5.4.4.60, TS 23.502 §4.2.9
type NssaaStatus string

// Validate checks that the status is a known NssaaStatus value.
func (s NssaaStatus) Validate() error {
	switch s {
	case NssaaStatusNotExecuted, NssaaStatusPending,
		NssaaStatusEapSuccess, NssaaStatusEapFailure:
		return nil
	default:
		return &ValidationError{
			Field:      "nssaaStatus",
			Reason:     fmt.Sprintf("unknown NssaaStatus value: %q", s),
			HTTPStatus: 400,
			Cause:      CauseInvalidStatus,
		}
	}
}

// IsTerminal reports whether this is a terminal (final) state.
func (s NssaaStatus) IsTerminal() bool {
	return s == NssaaStatusEapSuccess || s == NssaaStatusEapFailure
}

// IsPending reports whether the session is still in progress.
func (s NssaaStatus) IsPending() bool {
	return s == NssaaStatusPending
}

// String implements fmt.Stringer.
func (s NssaaStatus) String() string { return string(s) }

// MarshalJSON implements json.Marshaler.
func (s NssaaStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *NssaaStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("failed to unmarshal NssaaStatus: %w", err)
	}
	*s = NssaaStatus(str)
	return nil
}

// NotificationType values for slice re-authentication and revocation.
// Spec: TS 29.526 §7.2.4, TS 23.502 §4.2.9.2-3
const (
	NotificationTypeSliceReAuth NotificationType = "SLICE_RE_AUTH"
	NotificationTypeSliceRevoc  NotificationType = "SLICE_REVOCATION"
)

// NotificationType represents the type of asynchronous notification from NSSAAF.
type NotificationType string

// String implements fmt.Stringer.
func (n NotificationType) String() string { return string(n) }

// AuthResult is the final authentication result returned to the AMF.
// Spec: TS 29.526 §7.2.3
type AuthResult string

const (
	// AuthResultSuccess indicates successful slice authentication.
	AuthResultSuccess AuthResult = "EAP_SUCCESS"
	// AuthResultFailure indicates failed slice authentication.
	AuthResultFailure AuthResult = "EAP_FAILURE"
	// AuthResultPending indicates EAP exchange is still in progress.
	AuthResultPending AuthResult = "PENDING"
)
