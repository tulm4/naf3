// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865
package radius

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// 3GPP Vendor ID as defined by IANA.
// Spec: TS 29.561 §16.3.2
const VendorID3GPP uint32 = 10415

// 3GPP Vendor-Specific attribute types.
const (
	VendorTypeSNSSAI     uint8 = 200 // 3GPP-S-NSSAI (TS 29.561 §16.3.2)
	VendorTypeNASFeature uint8 = 35  // NAS-Feature-Radius
)

// ErrInvalidVSA is returned when a VSA cannot be decoded.
var ErrInvalidVSA = errors.New("radius: invalid VSA")

// VSA represents a Vendor-Specific Attribute (Type 26).
// Spec: RFC 2865 §5.26
type VSA struct {
	VendorID   uint32
	VendorType uint8
	Data       []byte
}

// DecodeVSA decodes a Vendor-Specific attribute.
// Spec: RFC 2865 §5.26
//
// The VSA data format is:
//
//	[0-2]   Vendor-ID (3 bytes, little-endian)
//	[3]     Vendor-Type (1 byte)
//	[4...]  Vendor-Data (variable)
func DecodeVSA(attr *Attribute) (*VSA, error) {
	if attr == nil || attr.Type != AttrVendorSpecific {
		return nil, fmt.Errorf("%w: not a VSA", ErrInvalidVSA)
	}

	data := attr.Value
	if len(data) < 5 {
		return nil, fmt.Errorf("%w: VSA data too short: %d bytes (min 5)", ErrInvalidVSA, len(data))
	}

	// Vendor-ID is 3 bytes, little-endian (RFC 2865 §5.26).
	vendorID := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
	vendorType := data[3]
	vendorData := data[4:]

	return &VSA{
		VendorID:   vendorID,
		VendorType: vendorType,
		Data:       vendorData,
	}, nil
}

// EncodeVSA encodes a Vendor-Specific attribute.
// Spec: RFC 2865 §5.26
//
// The VSA data format is:
//
//	[0-2]   Vendor-ID (3 bytes LE)
//	[3]     Vendor-Type (1 byte)
//	[4...]  Vendor-Data (variable)
//
// EncodeVSA constructs an Attribute with:
//
//	Type = AttrVendorSpecific (26)
//	Value = [VID0, VID1, VID2, VendorType, VendorData...]
//
// DecodeVSA reads Vendor-ID from bytes 0-2 (LE), Vendor-Type from byte 3,
// and Vendor-Data from byte 4 onward.
func EncodeVSA(vendorID uint32, vendorType uint8, data []byte) Attribute {
	// Build the full VSA value: Vendor-ID (3 bytes LE) + Vendor-Type (1 byte) + Vendor-Data
	vsaValue := make([]byte, 3+1+len(data))
	vsaValue[0] = byte(vendorID)
	vsaValue[1] = byte(vendorID >> 8)
	vsaValue[2] = byte(vendorID >> 16)
	vsaValue[3] = vendorType
	copy(vsaValue[4:], data)

	return Attribute{
		Type:  AttrVendorSpecific,
		Value: vsaValue,
	}
}

// EncodeSnssaiVSA encodes a 3GPP S-NSSAI as VSA data (just the body, not the attribute wrapper).
// Spec: TS 29.561 §16.3.2
//
// Format: SST (1 byte) + SD (3 bytes), always exactly 4 bytes.
// SD is a hex string like "ABCDEF".
func EncodeSnssaiVSA(sst uint8, sd string) []byte {
	result := make([]byte, 4)
	result[0] = sst
	if sd != "" {
		sdBytes, err := hex.DecodeString(sd)
		if err == nil && len(sdBytes) == 3 {
			copy(result[1:], sdBytes)
		}
	}
	return result
}

// DecodeSnssaiVSA decodes a 3GPP S-NSSAI from VSA data.
// Spec: TS 29.561 §16.3.2
//
// Returns SD as uppercase hex string (e.g. "ABCDEF").
// Returns empty SD if data length < 4.
func DecodeSnssaiVSA(data []byte) (sst uint8, sd string, err error) {
	if len(data) < 1 {
		return 0, "", fmt.Errorf("radius: SNSSAI VSA data too short: %d bytes", len(data))
	}

	sst = data[0]

	if len(data) >= 4 {
		sd = strings.ToUpper(hex.EncodeToString(data[1:4]))
	}

	return sst, sd, nil
}

// Make3GPPSNSSAIAttribute creates a 3GPP-S-NSSAI VSA attribute.
func Make3GPPSNSSAIAttribute(sst uint8, sd string) Attribute {
	vsaData := EncodeSnssaiVSA(sst, sd)
	return EncodeVSA(VendorID3GPP, VendorTypeSNSSAI, vsaData)
}

// Is3GPPSNSSAI checks if the VSA is a 3GPP-S-NSSAI attribute.
func (v *VSA) Is3GPPSNSSAI() bool {
	return v.VendorID == VendorID3GPP && v.VendorType == VendorTypeSNSSAI
}

// Parse3GPPSNSSAI extracts S-NSSAI fields from the VSA data.
func (v *VSA) Parse3GPPSNSSAI() (sst uint8, sd string, err error) {
	if !v.Is3GPPSNSSAI() {
		return 0, "", fmt.Errorf("radius: VSA is not a 3GPP-S-NSSAI")
	}
	return DecodeSnssaiVSA(v.Data)
}
