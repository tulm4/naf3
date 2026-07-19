// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
	"fmt"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

// AVP codes used by NSSAAF.
// Spec: RFC 4072, RFC 6733, TS 29.561 §17.4.1
const (
	// AVP code for EAP-Payload (RFC 4072).
	AVPCodeEAPPayload uint32 = 209
	// AVP code for Result-Code (RFC 6733).
	AVPCodeResultCode uint32 = 268
	// AVP code for Auth-Application-Id (RFC 6733).
	AVPCodeAuthApplicationID uint32 = 258
	// AVP code for 3GPP-S-NSSAI (TS 29.561 Table 17.4-1).
	// Format: SST(1 octet) + SD(3 octets, optional) as raw OctetString.
	AVPCodeSNSSAI uint32 = 200
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

// DecodeSnssaiAVP decodes a 3GPP-S-NSSAI AVP (code 200) from a message.
// Returns SST and SD; SD may be empty string if not present.
// Spec: TS 29.571 §5.4.4.60, TS 29.561 §17.4.1
//
// Format: SST(1 octet) + SD(3 octets, optional) as raw OctetString.
func DecodeSnssaiAVP(m *diam.Message) (sst uint8, sd string, err error) {
	avps, err := m.FindAVPs(AVPCodeSNSSAI, VendorID3GPP)
	if err != nil {
		return 0, "", fmt.Errorf("diameter: FindAVPs failed: %w", err)
	}
	if len(avps) == 0 {
		return 0, "", nil // not found — not an error
	}

	// 3GPP-S-NSSAI is a raw OctetString: SST(1 octet) + SD(3 octets, optional).
	os, ok := avps[0].Data.(datatype.OctetString)
	if !ok {
		return 0, "", fmt.Errorf("diameter: 3GPP-S-NSSAI AVP has unexpected type %T", avps[0].Data)
	}

	if len(os) < 1 {
		return 0, "", fmt.Errorf("diameter: 3GPP-S-NSSAI AVP too short: need at least 1 byte for SST")
	}

	sst = os[0]

	if len(os) >= 4 {
		sd = encodeHex([]byte(os[1:4]))
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
