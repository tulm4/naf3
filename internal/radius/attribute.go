// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865
package radius

import (
	"encoding/binary"
	"fmt"
)

// RADIUS attribute types used by NSSAAF.
// Spec: RFC 2865 §5, RFC 2138, RFC 2139
const (
	AttrUserName             = 1  // RFC 2865 §5.1
	AttrUserPassword         = 2  // RFC 2865 §5.2
	AttrNASIPAddress         = 4  // RFC 2865 §5.4
	AttrNASPort              = 5  // RFC 2865 §5.5
	AttrServiceType          = 6  // RFC 2865 §5.6
	AttrReplyMessage         = 18 // RFC 2865 §5.3
	AttrState                = 24 // RFC 2865 §5.24
	AttrVendorSpecific       = 26 // RFC 2865 §5.26
	AttrCalledStationID      = 30 // RFC 2865 §5.3
	AttrCallingStationID     = 31 // RFC 2865 §5.3
	AttrNASIdentifier        = 32 // RFC 2865 §5.3
	AttrAcctSessionID        = 44 // RFC 2865 §5.3
	AttrAcctStatusType       = 40 // RFC 2865 §5.3
	AttrNASPortType          = 61 // RFC 2865 §5.5
	AttrEAPMessage           = 79 // RFC 3579 §3.2
	AttrMessageAuthenticator = 80 // RFC 3579 §3.2
	AttrNASFeature           = 35 // 3GPP
)

// Service-Type values relevant to NSSAAF.
const (
	ServiceTypeLogin             = 1
	ServiceTypeFramed            = 2
	ServiceTypeCallCheck         = 10
	ServiceTypeCallbackLogin     = 3
	ServiceTypeCallbackFramed    = 4
	ServiceTypeOutbound          = 5
	ServiceTypeAdministrative    = 6
	ServiceTypeNASPrompt         = 7
	ServiceTypeAuthenticateOnly  = 8
	ServiceTypeCallbackNASPrompt = 9
	ServiceTypeCallAuthorization = 11 // 3GPP NSSAA
	ServiceTypeVoice             = 12
	ServiceTypeFax               = 13
	ServiceTypeModemRelay        = 14
	ServiceTypeIAPP              = 15
	ServiceTypeHostLogin         = 17
)

// NAS-Port-Type values.
const (
	NASPortTypeAsync       = 0
	NASPortTypeSync        = 1
	NASPortTypeISDN        = 2
	NASPortTypeISDNV120    = 3
	NASPortTypeISDNV110    = 4
	NASPortTypeVirtual     = 5
	NASPortTypePIAFS       = 6
	NASPortTypeHDLCClear   = 7
	NASPortTypeSerialClear = 8
	NASPortTypeFrame       = 9
	NASPortTypeGPRSPDP     = 14
	NASPortTypeEPS         = 16
	NASPortTypeVirtual     = 19 // Virtual (used by NSSAAF)
)

// Attribute types for NSSAA.
const (
	Attr3GPPSNSSAI = 200 // 3GPP Vendor-Specific: S-NSSAI
)

// DecodeAttributes decodes all attributes from raw data.
// Spec: RFC 2865 §5
func DecodeAttributes(data []byte) ([]Attribute, error) {
	var attrs []Attribute
	offset := 0

	for offset < len(data) {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("radius: attribute truncated at offset %d", offset)
		}

		attrType := data[offset]
		attrLen := int(data[offset+1])

		if attrLen < 2 {
			return nil, fmt.Errorf("radius: attribute type=%d has invalid length %d", attrType, attrLen)
		}
		if offset+attrLen > len(data) {
			return nil, fmt.Errorf("radius: attribute type=%d extends beyond data: len=%d, remaining=%d",
				attrType, attrLen, len(data)-offset)
		}

		attrs = append(attrs, Attribute{
			Type:  attrType,
			Value: data[offset+2 : offset+attrLen],
		})

		offset += attrLen
	}

	return attrs, nil
}

// EncodeAttributes encodes a slice of attributes to wire format.
// Spec: RFC 2865 §5
func EncodeAttributes(attrs []Attribute) []byte {
	var result []byte
	for _, attr := range attrs {
		attrLen := 2 + len(attr.Value)
		buf := make([]byte, attrLen)
		buf[0] = attr.Type
		buf[1] = byte(attrLen)
		copy(buf[2:], attr.Value)
		result = append(result, buf...)
	}
	return result
}

// MakeAttribute creates an attribute with a byte value.
func MakeAttribute(attrType uint8, value []byte) Attribute {
	return Attribute{Type: attrType, Value: value}
}

// MakeStringAttribute creates a string-typed attribute.
func MakeStringAttribute(attrType uint8, value string) Attribute {
	return Attribute{Type: attrType, Value: []byte(value)}
}

// MakeIntegerAttribute creates a 4-byte integer attribute (network byte order).
func MakeIntegerAttribute(attrType uint8, value uint32) Attribute {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, value)
	return Attribute{Type: attrType, Value: buf}
}

// MakeIPv4Attribute creates an IPv4 address attribute.
func MakeIPv4Attribute(attrType uint8, ip []byte) Attribute {
	if len(ip) != 4 {
		ip = []byte{0, 0, 0, 0}
	}
	return Attribute{Type: attrType, Value: ip}
}

// GetAttribute returns the first attribute with the given type.
func GetAttribute(attrs []Attribute, attrType uint8) *Attribute {
	for i := range attrs {
		if attrs[i].Type == attrType {
			return &attrs[i]
		}
	}
	return nil
}

// GetAttributes returns all attributes with the given type.
func GetAttributes(attrs []Attribute, attrType uint8) []Attribute {
	var result []Attribute
	for i := range attrs {
		if attrs[i].Type == attrType {
			result = append(result, attrs[i])
		}
	}
	return result
}

// GetString extracts a string value from an attribute.
func GetString(attr *Attribute) string {
	if attr == nil {
		return ""
	}
	return string(attr.Value)
}

// GetInteger extracts a 4-byte integer value from an attribute.
func GetInteger(attr *Attribute) uint32 {
	if attr == nil || len(attr.Value) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(attr.Value)
}

// GetIPv4 extracts an IPv4 address from an attribute.
func GetIPv4(attr *Attribute) []byte {
	if attr == nil || len(attr.Value) < 4 {
		return nil
	}
	return attr.Value[:4]
}
