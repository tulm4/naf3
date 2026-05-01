// Package types provides 3GPP data types for NSSAAF.
// Spec: TS 23.003 §2.2, TS 29.571 §5.2.2
package types

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// GPSI patterns from TS 29.571 §5.2.2:
// Pattern: '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
//
// GPSI has 3 forms:
//  1. MSISDN-based: "msisdn-" + 5-15 decimal digits
//  2. External Identifier-based: "extid-" + <ext-id> + "@" + <realm>
//  3. Any other string (catch-all for backwards compatibility)
//
// Spec: TS 29.571 §5.2.2 (formerly §5.4.4.61 in older versions)
var gpsiRegex = regexp.MustCompile(`^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$`)

// Gpsi represents a Generic Public Subscription Identifier.
// It uniquely identifies a subscriber's subscription.
// GPSI is required for all NSSAA procedures.
// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.3, TS 23.502 §4.2.9.1
type Gpsi string

// Validate checks that the GPSI conforms to the 3GPP pattern.
// Spec: TS 29.571 §5.2.2
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
			Reason:     "GPSI must match pattern ^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$ (TS 29.571 §5.2.2)",
			HTTPStatus: 400,
			Cause:      CauseInvalidGpsi,
		}
	}
	return nil
}

// Normalize returns the GPSI as-is.
// GPSI can have various formats per TS 29.571 §5.2.2:
// - MSISDN-based: "msisdn-<digits>"
// - External Identifier: "extid-<id>@<realm>"
// - Any other string (catch-all)
func (g Gpsi) Normalize() string {
	return string(g)
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
