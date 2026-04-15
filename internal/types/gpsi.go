// Package types provides 3GPP data types for NSSAAF.
// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.3
package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// GPSI patterns from TS 29.571 §5.4.4.3:
// - ^5[0-9]{8,14}$ — 9 to 15 total characters (leading 5 + 8 to 14 digits)
// - ^5-[0-9]{8,14}$ — same with optional dash separator (per TS 23.003)
var gpsiRegex = regexp.MustCompile(`^5-?[0-9]{8,14}$`)

// Gpsi represents a Generic Public Subscription Identifier.
// It uniquely identifies a subscriber's subscription.
// GPSI is required for all NSSAA procedures.
// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.3, TS 23.502 §4.2.9.1
type Gpsi string

// Validate checks that the GPSI conforms to the 3GPP pattern.
// Spec: TS 29.571 §5.4.4.3
func (g Gpsi) Validate() error {
	if string(g) == "" {
		return &ValidationError{
			Field:      "gpsi",
			Reason:     "GPSI is required for NSSAA (TS 23.502 §4.2.9.1)",
			HTTPStatus: 400,
			Cause:      CauseInvalidGpsi,
		}
	}
	if !gpsiRegex.MatchString(string(g)) {
		return &ValidationError{
			Field:      "gpsi",
			Reason:     "GPSI must match pattern ^5-?[0-9]{8,14}$ (TS 29.571 §5.4.4.3)",
			HTTPStatus: 400,
			Cause:      CauseInvalidGpsi,
		}
	}
	return nil
}

// Normalize returns the GPSI with the optional dash removed.
// Both "52080460000001" and "5-208046000000001" normalize to "52080460000001".
func (g Gpsi) Normalize() string {
	return strings.ReplaceAll(string(g), "-", "")
}

// String implements fmt.Stringer.
func (g Gpsi) String() string { return string(g) }

// MarshalJSON implements json.Marshaler.
func (g Gpsi) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(g))
}

// UnmarshalJSON implements json.Unmarshaler.
func (g *Gpsi) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("failed to unmarshal Gpsi: %w", err)
	}
	*g = Gpsi(s)
	return nil
}
