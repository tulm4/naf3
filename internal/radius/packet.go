// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865
package radius

import (
	"encoding/binary"
	"fmt"
)

// RADIUS packet codes as defined in RFC 2865.
// Spec: RFC 2865 §3
const (
	CodeAccessRequest     uint8 = 1
	CodeAccessAccept      uint8 = 2
	CodeAccessReject      uint8 = 3
	CodeAccessChallenge   uint8 = 11
	CodeDisconnectRequest uint8 = 40
	CodeDisconnectACK     uint8 = 41
	CodeDisconnectNAK     uint8 = 42
)

// Packet represents a RADIUS packet as defined in RFC 2865.
// Spec: RFC 2865 §3
type Packet struct {
	Code       uint8
	Id         uint8
	Length     uint16
	Vector     [16]byte // Authenticator or Response Authenticator
	Attributes []Attribute
}

// Attribute represents a RADIUS attribute (TLV format).
// Spec: RFC 2865 §5
type Attribute struct {
	Type  uint8
	Value []byte
}

// DecodePacket decodes a raw RADIUS packet from wire format.
// Spec: RFC 2865 §3
func DecodePacket(data []byte) (*Packet, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("radius: packet too short: %d bytes (min 20)", len(data))
	}

	code := data[0]
	id := data[1]
	length := binary.BigEndian.Uint16(data[2:4])

	if int(length) != len(data) {
		return nil, fmt.Errorf("radius: length mismatch: header=%d, actual=%d", length, len(data))
	}

	if length < 20 {
		return nil, fmt.Errorf("radius: length too short: %d", length)
	}

	var vector [16]byte
	copy(vector[:], data[4:20])

	attrs, err := DecodeAttributes(data[20:])
	if err != nil {
		return nil, fmt.Errorf("radius: failed to decode attributes: %w", err)
	}

	return &Packet{
		Code:       code,
		Id:         id,
		Length:     length,
		Vector:     vector,
		Attributes: attrs,
	}, nil
}

// Encode encodes a RADIUS packet to wire format.
// Spec: RFC 2865 §3
func (p *Packet) Encode() []byte {
	attrData := EncodeAttributes(p.Attributes)
	length := 20 + len(attrData)

	data := make([]byte, length)
	data[0] = p.Code
	data[1] = p.Id
	binary.BigEndian.PutUint16(data[2:4], uint16(length))
	copy(data[4:20], p.Vector[:])
	copy(data[20:], attrData)

	p.Length = uint16(length)
	return data
}

// BuildAccessRequest creates an Access-Request packet with the given authenticator.
// Spec: RFC 2865 §3.1
func BuildAccessRequest(id uint8, authenticator [16]byte, attrs []Attribute) *Packet {
	return &Packet{
		Code:       CodeAccessRequest,
		Id:         id,
		Length:     20,
		Vector:     authenticator,
		Attributes: attrs,
	}
}

// BuildAccessAccept creates an Access-Accept packet.
// Spec: RFC 2865 §3.2
func BuildAccessAccept(id uint8, responseAuth [16]byte, attrs []Attribute) *Packet {
	return &Packet{
		Code:       CodeAccessAccept,
		Id:         id,
		Length:     20,
		Vector:     responseAuth,
		Attributes: attrs,
	}
}

// BuildAccessReject creates an Access-Reject packet.
// Spec: RFC 2865 §3.3
func BuildAccessReject(id uint8, responseAuth [16]byte, attrs []Attribute) *Packet {
	return &Packet{
		Code:       CodeAccessReject,
		Id:         id,
		Length:     20,
		Vector:     responseAuth,
		Attributes: attrs,
	}
}

// BuildAccessChallenge creates an Access-Challenge packet.
// Spec: RFC 2865 §3.4
func BuildAccessChallenge(id uint8, responseAuth [16]byte, attrs []Attribute) *Packet {
	return &Packet{
		Code:       CodeAccessChallenge,
		Id:         id,
		Length:     20,
		Vector:     responseAuth,
		Attributes: attrs,
	}
}

// String implements fmt.Stringer.
func (p *Packet) String() string {
	return fmt.Sprintf("RADIUS[Code=%d, Id=%d, Len=%d, Attrs=%d]",
		p.Code, p.Id, p.Length, len(p.Attributes))
}
