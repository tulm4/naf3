package gen

import (
	"fmt"

	"layeh.com/radius"
)

// NSSAI represents a Single Network Slice Selection Assistance Information.
// Spec: 3GPP TS 29.561 §16.3.2
// Layout: Type(1) + Length(1) + SST(1) + SD(3) = 6 bytes
type NSSAI struct {
	SST uint8   // Slice/Service Type (0-255)
	SD  [3]byte // Slice Differentiator (zero if SST-only)
}

// Pack encodes NSSAI into the VSA sub-TLV format.
func (n *NSSAI) Pack() []byte {
	// 3GPP sub-type: 200, length: 6 (SST + SD)
	b := make([]byte, 6)
	b[0] = 200 // 3GPP sub-type: 3GPP-S-NSSAI
	b[1] = 6   // Length: SST(1) + SD(3) = 4, but per 3GPP spec length field includes Type+Length bytes
	b[2] = n.SST
	copy(b[3:6], n.SD[:])
	return b
}

// Unpack decodes NSSAI from VSA sub-TLV format.
// Expects the full 6-byte sub-TLV: Type(1) + Length(1) + SST(1) + SD(3).
func (n *NSSAI) Unpack(b []byte) error {
	if len(b) < 6 {
		return fmt.Errorf("NSSAI: expected 6 bytes, got %d", len(b))
	}
	if b[0] != 200 {
		return fmt.Errorf("NSSAI: expected type 200, got %d", b[0])
	}
	n.SST = b[2]
	copy(n.SD[:], b[3:6])
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
