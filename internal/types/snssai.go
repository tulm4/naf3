// snssai.go — S-NSSAI type with validation
// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
package types

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var snssaiSDRegex = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

// Snssai represents the Single-Network Slice Selection Assistance Information.
// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
type Snssai struct {
	// SST (Slice Service Type): identifies the network slice type.
	// Range: 0–255 (standard values 1–128, operator-specific 129–255)
	// Spec: TS 23.003 §3.2
	SST uint8 `json:"sst"`
	// SD (Slice Differentiator): optionally differentiates slices with the same SST.
	// Must be exactly 6 hexadecimal characters when present.
	// Empty string means default SD (wildcard).
	// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
	SD string `json:"sd,omitempty"`
}

// Validate checks that the S-NSSAI fields conform to 3GPP specifications.
// Spec: TS 23.003 §3.2, TS 29.571 §5.4.4.60
func (s Snssai) Validate() error {
	// SST is always required; valid range is 0–255.
	// JSON unmarshaling already guarantees SST is in [0,255]; this check
	// provides defense-in-depth for programmatic construction.
	//lint:ignore SA4003 JSON unmarshal guarantees SST ≤ 255; this is defense-in-depth for programmatic construction.
	if s.SST > 255 {
		return &ValidationError{
			Field:      "snssai.sst",
			Reason:     "SST must be in range 0-255 (TS 29.571 §5.4.4.60)",
			HTTPStatus: 400,
			Cause:      CauseInvalidSnssaiSst,
		}
	}
	// SD is optional (empty = default), but if present must be exactly 6 hex chars
	if s.SD != "" && !snssaiSDRegex.MatchString(s.SD) {
		return &ValidationError{
			Field:      "snssai.sd",
			Reason:     "SD must be exactly 6 hexadecimal characters (TS 29.571 §5.4.4.60)",
			HTTPStatus: 400,
			Cause:      CauseInvalidSnssaiSd,
		}
	}
	return nil
}

// String returns a human-readable representation of the S-NSSAI.
func (s Snssai) String() string {
	if s.SD == "" {
		return fmt.Sprintf("S-NSSAI{%d}", s.SST)
	}
	return fmt.Sprintf("S-NSSAI{%d:%s}", s.SST, s.SD)
}

// Key returns a normalized string key suitable for use as a map key or cache key.
// Format: "{sst}:{sd}" where sd is uppercase hex or "*" for wildcard.
func (s Snssai) Key() string {
	sd := s.SD
	if sd == "" {
		sd = "*"
	}
	return fmt.Sprintf("%d:%s", s.SST, strings.ToUpper(sd))
}

// Equal reports whether two S-NSSAIs are equal.
func (s Snssai) Equal(other Snssai) bool {
	return s.SST == other.SST && s.SD == other.SD
}

// SnssaiFromJSON parses a Snssai from JSON bytes.
func SnssaiFromJSON(data []byte) (*Snssai, error) {
	var snssai Snssai
	if err := json.Unmarshal(data, &snssai); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Snssai: %w", err)
	}
	return &snssai, nil
}

// MarshalJSON implements json.Marshaler.
func (s Snssai) MarshalJSON() ([]byte, error) {
	type alias Snssai
	return json.Marshal(alias(s))
}
