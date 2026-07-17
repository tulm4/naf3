package gen

import (
	"fmt"

	"layeh.com/radius"
)

// NSSAI represents a Single Network Slice Selection Assistance Information.
// Spec: 3GPP TS 29.561 §16.3.2
type NSSAI struct {
	SST uint8   // Slice/Service Type (0-255)
	SD  [3]byte // Slice Differentiator (zero if SST-only)
}

// Pack encodes NSSAI into the data portion of the 3GPP-S-NSSAI sub-TLV.
// The generated ThreeGPPSNSSAI_Add helper wraps this in Type+Length.
// Spec: TS 29.561 §16.3.2
func (n *NSSAI) Pack() []byte {
	b := make([]byte, 4)
	b[0] = n.SST
	copy(b[1:4], n.SD[:])
	return b
}

// Unpack decodes NSSAI from the data portion of the 3GPP-S-NSSAI sub-TLV.
// The generated ThreeGPPSNSSAI_Gets helper strips Type+Length before passing data.
// Per TS 29.561 §16.3.2:
//   - Length=3: SST only (1 byte data)
//   - Length=6: SST + SD (4 byte data)
func (n *NSSAI) Unpack(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("NSSAI: empty data")
	}
	n.SST = b[0]
	if len(b) >= 4 {
		copy(n.SD[:], b[1:4])
	} else if len(b) > 1 {
		// Partial SD — treat as zeros (SST-only variant)
		n.SD = [3]byte{0, 0, 0}
	}
	return nil
}

// AddNSSAIAttribute adds 3GPP-S-NSSAI to a RADIUS packet.
// Uses the generated ThreeGPPSNSSAI_Add helper.
func AddNSSAIAttribute(pkt *radius.Packet, nssai NSSAI) error {
	return ThreeGPPSNSSAI_Add(pkt, nssai.Pack())
}

// GetNSSAIAttributes extracts all 3GPP-S-NSSAI from a RADIUS packet.
func GetNSSAIAttributes(pkt *radius.Packet) ([]NSSAI, error) {
	raw, err := ThreeGPPSNSSAI_Gets(pkt)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}

	result := make([]NSSAI, 0, len(raw))
	for _, b := range raw {
		var n NSSAI
		if err := n.Unpack(b); err != nil {
			continue // skip malformed sub-TLVs
		}
		result = append(result, n)
	}
	return result, nil
}
