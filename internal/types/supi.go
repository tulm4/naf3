// Package types provides 3GPP data types for NSSAAF.
// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.2
package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// IMSI-based SUPI: 'imsi-' followed by 5-15 digits (MCC + MNC + MSIN)
// Per TS 23.003 §2.2, an IMSI is 15 digits: MCC (3) + MNC (2-3) + MSIN (remaining)
// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.2
var supiIMSIRegex = regexp.MustCompile(`^imsi-[0-9]{5,15}$`)

// Supi represents a Subscription Permanent Identifier.
// It is the permanent identifier of a 5G subscription.
// Spec: TS 23.003 §2.2, TS 29.571 §5.4.4.2
type Supi string

// Validate checks that the SUPI conforms to the 3GPP pattern.
// Spec: TS 29.571 §5.4.4.2
func (s Supi) Validate() error {
	if string(s) == "" {
		return &ValidationError{
			Field:      "supi",
			Reason:     "SUPI is required",
			HTTPStatus: 400,
			Cause:      CauseInvalidSupi,
		}
	}
	if !supiIMSIRegex.MatchString(string(s)) {
		return &ValidationError{
			Field:      "supi",
			Reason:     "SUPI must match pattern ^imsi-[0-9]{5,15}$ (IMSI-based SUPI, TS 29.571 §5.4.4.2)",
			HTTPStatus: 400,
			Cause:      CauseInvalidSupi,
		}
	}
	return nil
}

// IsIMSI reports whether the SUPI is an IMSI-based SUPI.
func (s Supi) IsIMSI() bool {
	return strings.HasPrefix(string(s), "imsi-")
}

// IMSI returns the IMSI component of the SUPI (without the "imsi-" prefix).
// Only valid if IsIMSI() is true.
func (s Supi) IMSI() string {
	if !s.IsIMSI() {
		return ""
	}
	return strings.TrimPrefix(string(s), "imsi-")
}

// String implements fmt.Stringer.
func (s Supi) String() string { return string(s) }

// MarshalJSON implements json.Marshaler.
func (s Supi) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Supi) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("failed to unmarshal Supi: %w", err)
	}
	*s = Supi(str)
	return nil
}
