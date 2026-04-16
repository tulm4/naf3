// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072, RFC 6733
package diameter

import (
	"encoding/hex"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

// VendorID3GPP is the 3GPP vendor ID (10415).
const VendorID3GPP uint32 = 10415

// EncodeSnssaiAVP encodes a 3GPP-S-NSSAI as a grouped AVP.
// Spec: TS 29.571 §5.4.4.60, TS 29.561 §17
//
// Format:
//   3GPP-S-NSSAI ::= <AVP Header: 310, Vendor: 10415>
//                    { Slice/Service Type }
//                    [ Slice Differentiator ]
//
// AVP Code 310, Vendor 10415, M-bit and V-bit set.
func EncodeSnssaiAVP(sst uint8, sd string) *diam.AVP {
	sstAVP := diam.NewAVP(259, avp.Mbit|avp.Vbit, VendorID3GPP, datatype.Unsigned32(sst))

	group := &diam.GroupedAVP{AVP: []*diam.AVP{sstAVP}}

	if sd != "" {
		sdBytes := parseSDToBytes(sd)
		if sdBytes != nil {
			sdAVP := diam.NewAVP(260, avp.Mbit|avp.Vbit, VendorID3GPP, datatype.OctetString(sdBytes))
			group.AVP = append(group.AVP, sdAVP)
		}
	}

	return diam.NewAVP(310, avp.Mbit|avp.Vbit, VendorID3GPP, group)
}

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

// EncodeSessionIdAVP encodes a session ID.
// AVP Code 263, M-bit set.
func EncodeSessionIdAVP(sessionId string) *diam.AVP {
	return diam.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionId))
}

// parseSDToBytes converts a 6-character hex SD string to 3 bytes.
func parseSDToBytes(sd string) []byte {
	if len(sd) != 6 {
		return nil
	}
	b, err := hex.DecodeString(sd)
	if err != nil || len(b) != 3 {
		return nil
	}
	return b
}
