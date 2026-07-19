// Package messages provides strongly-typed Diameter message structs for NSSAAF.
package messages

import (
	"encoding/hex"
	"errors"
	"fmt"
)

var (
	// ErrInvalidSST indicates the Slice/Service Type is out of range.
	ErrInvalidSST = errors.New("SNSSAI SST must be in range 0-255")
	// ErrInvalidSD indicates the Slice Differentiator is not 6 hex characters.
	ErrInvalidSD = errors.New("SNSSAI SD must be exactly 6 hexadecimal characters")
)

// SNSSAI represents S-NSSAI (Single Network Slice Selection Assistance Information).
// Spec: TS 29.571 §5.4.4.60
//
// Wire format: SST (1 octet) + SD (3 octets, optional)
// SD is only included when HasSD is true.
type SNSSAI struct {
	SST   uint8    // Slice/Service Type (0-255)
	SD    [3]byte  // Slice Differentiator
	HasSD bool     // Whether SD is present
}

// NewSNSSAI creates a new SNSSAI from SST and optional SD string.
// sd should be 6 uppercase hex chars (e.g., "1A2B3C") or empty.
func NewSNSSAI(sst uint8, sd string) (*SNSSAI, error) {
	if sst > 255 {
		return nil, ErrInvalidSST
	}

	s := &SNSSAI{SST: sst}

	if sd != "" {
		if len(sd) != 6 {
			return nil, ErrInvalidSD
		}
		b, err := hex.DecodeString(sd)
		if err != nil {
			return nil, ErrInvalidSD
		}
		if len(b) != 3 {
			return nil, ErrInvalidSD
		}
		copy(s.SD[:], b)
		s.HasSD = true
	}

	return s, nil
}

// MarshalBinary encodes SNSSAI to wire format.
// Format: SST (1 byte) + SD (3 bytes, only if HasSD).
// Spec: TS 29.571 §5.4.4.60
func (s *SNSSAI) MarshalBinary() ([]byte, error) {
	data := make([]byte, 1)
	data[0] = s.SST

	if s.HasSD {
		data = append(data, s.SD[:]...)
	}

	return data, nil
}

// UnmarshalBinary decodes SNSSAI from wire format.
func (s *SNSSAI) UnmarshalBinary(data []byte) error {
	if len(data) < 1 {
		return fmt.Errorf("SNSSAI: need at least 1 byte for SST")
	}

	s.SST = data[0]

	if len(data) >= 4 {
		s.HasSD = true
		copy(s.SD[:], data[1:4])
	}

	return nil
}

// SDString returns the SD as a 6-character hex string.
func (s *SNSSAI) SDString() string {
	if !s.HasSD {
		return ""
	}
	return hex.EncodeToString(s.SD[:])
}

// String returns the string representation (e.g., "1-1A2B3C" or "255").
func (s *SNSSAI) String() string {
	if s.HasSD {
		return fmt.Sprintf("%d-%s", s.SST, s.SDString())
	}
	return fmt.Sprintf("%d", s.SST)
}
