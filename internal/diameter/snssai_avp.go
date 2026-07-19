// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

// VendorID3GPP is the 3GPP vendor ID (10415).
const VendorID3GPP uint32 = 10415

// EncodeSnssaiAVP encodes a 3GPP-S-NSSAI as a vendor-specific AVP.
// Spec: TS 29.571 §5.4.4.60, TS 29.561 §17.4.1
//
// Format: SST(1 octet) + SD(3 octets, optional)
// The wire format is raw octets, NOT a grouped AVP.
//
// AVP Code 200, Vendor 10415, M-bit and V-bit set.
// Returns an error if sd is not a valid 6-character hex string.
func EncodeSnssaiAVP(sst uint8, sd string) (*diam.AVP, error) {
	var data []byte
	data = append(data, sst)

	if sd != "" {
		sdBytes, err := parseSDToBytes(sd)
		if err != nil {
			return nil, fmt.Errorf("diameter: invalid SNSSAI SD %q: %w", sd, err)
		}
		data = append(data, sdBytes...)
	}

	return diam.NewAVP(AVP3GPP_S_NSSAI, avp.Mbit|avp.Vbit, VendorID3GPP, datatype.OctetString(data)), nil
}

// AVP3GPP_S_NSSAI is the 3GPP-S-NSSAI AVP code per TS 29.561 Table 17.4-1.
// Spec: TS 29.561 §17.4.1
const AVP3GPP_S_NSSAI = 200

// EncodeEapPayloadAVP encodes an EAP payload as a Diameter AVP.
// Spec: RFC 4072, TS 29.561 §17
//
// AVP Code 209, no vendor.
func EncodeEapPayloadAVP(eapPayload []byte) *diam.AVP {
	return diam.NewAVP(209, 0, 0, datatype.OctetString(eapPayload))
}

// EncodeUserNameAVP encodes the user identity as a User-Name AVP.
// AVP Code 1, M-bit set.
func EncodeUserNameAVP(userName string) *diam.AVP {
	return diam.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(userName))
}

// EncodeSessionIDAVP encodes a session ID.
// AVP Code 263, M-bit set.
func EncodeSessionIDAVP(sessionID string) *diam.AVP {
	return diam.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionID))
}

// ErrInvalidSD indicates the Slice Differentiator is not a valid 6-character hex string.
var ErrInvalidSD = errors.New("SNSSAI SD must be exactly 6 hexadecimal characters")

// parseSDToBytes converts a 6-character hex SD string to 3 bytes.
func parseSDToBytes(sd string) ([]byte, error) {
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
	return b, nil
}
