// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
	"fmt"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

// AVP codes used by NSSAAF.
// Spec: RFC 4072, RFC 6733, TS 29.571
const (
	// AVP code for EAP-Payload (RFC 4072).
	AVPCodeEAPPayload uint32 = 209
	// AVP code for Result-Code (RFC 6733).
	AVPCodeResultCode uint32 = 268
	// AVP code for Auth-Application-Id (RFC 6733).
	AVPCodeAuthApplicationID uint32 = 258
	// AVP code for 3GPP-S-NSSAI (TS 29.571 §5.4.4.60).
	AVPCodeSNSSAI uint32 = 310
	// AVP code for Slice/Service Type (child of SNSSAI).
	AVPCodeSliceServiceType uint32 = 259
	// AVP code for Slice Differentiator (child of SNSSAI).
	AVPCodeSliceDifferentiator uint32 = 260
)

// DecodeEapPayloadAVP decodes an EAP-Payload AVP from a Diameter message.
// Spec: RFC 4072
func DecodeEapPayloadAVP(m *diam.Message) ([]byte, error) {
	// FindAVPs uses the dictionary and may fail for non-standard AVP codes
	// (e.g. EAP-Payload, code 209, is not in dict.Default). Fall back to
	// direct iteration so we can always find it regardless of dictionary coverage.
	for _, avp := range m.AVP {
		if avp.Code == AVPCodeEAPPayload && avp.VendorID == 0 {
			os, ok := avp.Data.(datatype.OctetString)
			if !ok {
				return nil, fmt.Errorf("diameter: EAP-Payload AVP has unexpected type %T", avp.Data)
			}
			return []byte(os), nil
		}
	}
	return nil, nil
}

// DecodeSnssaiAVP decodes a 3GPP-S-NSSAI AVP (code 310) from a message.
// Returns SST and SD; SD may be empty string if not present.
// Spec: TS 29.571 §5.4.4.60
func DecodeSnssaiAVP(m *diam.Message) (sst uint8, sd string, err error) {
	avps, err := m.FindAVPs(AVPCodeSNSSAI, VendorID3GPP)
	if err != nil {
		return 0, "", fmt.Errorf("diameter: FindAVPs failed: %w", err)
	}
	if len(avps) == 0 {
		return 0, "", nil // not found — not an error
	}

	// 3GPP-S-NSSAI is a grouped AVP.
	g, ok := avps[0].Data.(*diam.GroupedAVP)
	if !ok {
		// Fallback: try as raw octet string (SST only).
		if os, ok := avps[0].Data.(datatype.OctetString); ok && len(os) >= 1 {
			return uint8(os[0]), "", nil
		}
		return 0, "", fmt.Errorf("diameter: 3GPP-S-NSSAI AVP has unexpected type %T", avps[0].Data)
	}

	for _, child := range g.AVP {
		switch child.Code {
		case AVPCodeSliceServiceType:
			if ui, ok := child.Data.(datatype.Unsigned32); ok {
				sst = uint8(ui)
			}
		case AVPCodeSliceDifferentiator:
			if os, ok := child.Data.(datatype.OctetString); ok && len(os) >= 3 {
				sd = encodeHex([]byte(os))
			}
		}
	}
	return sst, sd, nil
}

// encodeHex converts a byte slice to uppercase hex string.
func encodeHex(b []byte) string {
	const hexChars = "0123456789ABCDEF"
	result := make([]byte, len(b)*2)
	for i, v := range b {
		result[i*2] = hexChars[v>>4]
		result[i*2+1] = hexChars[v&0x0F]
	}
	return string(result)
}

// DecodeResultCodeAVP extracts the Result-Code AVP from a Diameter message.
// Returns 0 if not found or cannot be decoded.
func DecodeResultCodeAVP(m *diam.Message) uint32 {
	avps, err := m.FindAVPs(AVPCodeResultCode, 0)
	if err != nil || len(avps) == 0 {
		return 0
	}
	if rc, ok := avps[0].Data.(datatype.Unsigned32); ok {
		return uint32(rc)
	}
	return 0
}

// DecodeAuthApplicationID extracts the Auth-Application-Id AVP value from a message.
func DecodeAuthApplicationID(m *diam.Message) uint32 {
	avps, err := m.FindAVPs(AVPCodeAuthApplicationID, 0)
	if err != nil || len(avps) == 0 {
		return 0
	}
	if id, ok := avps[0].Data.(datatype.Unsigned32); ok {
		return uint32(id)
	}
	return 0
}
