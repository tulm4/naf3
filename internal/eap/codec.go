// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrInvalidPacket is returned when an EAP packet cannot be parsed.
var ErrInvalidPacket = errors.New("eap: invalid packet")

// ErrUnexpectedLength is returned when packet length doesn't match header.
var ErrUnexpectedLength = errors.New("eap: unexpected length")

// ErrPayloadTooLarge is returned when an EAP payload exceeds maximum size.
var ErrPayloadTooLarge = errors.New("eap: payload too large")

// MaxEAPSize is the maximum allowed EAP packet size (64KB - 1).
const MaxEAPSize = 65535

// Packet represents a decoded EAP packet as defined in RFC 3748 §4.
// Spec: RFC 3748 §4
type Packet struct {
	Code    EapCode
	Id      uint8
	Length  uint16 // Total packet length including header
	Type    uint8  // Only valid for Request/Response packets
	Data    []byte // Type-specific data (may be empty)
	RawData []byte // Original wire-format data (for debugging/tracing)
}

// Parse decodes a raw EAP packet from wire format.
// Returns an error if the packet is malformed or shorter than the minimum header.
// Spec: RFC 3748 §4
func Parse(data []byte) (*Packet, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: need at least 4 bytes, got %d", ErrInvalidPacket, len(data))
	}

	code := EapCode(data[0])
	id := data[1]
	length := binary.BigEndian.Uint16(data[2:4])

	if int(length) != len(data) {
		return nil, fmt.Errorf("%w: header says %d, got %d bytes",
			ErrUnexpectedLength, length, len(data))
	}

	if length > MaxEAPSize {
		return nil, fmt.Errorf("%w: %d bytes exceeds max %d",
			ErrInvalidPacket, length, MaxEAPSize)
	}

	if !code.IsValid() {
		return nil, fmt.Errorf("%w: code=%d", ErrInvalidPacket, code)
	}

	p := &Packet{
		Code:   code,
		Id:     id,
		Length: length,
		RawData: data,
	}

	// Type field is only present for Request (1) and Response (2) packets.
	// Success (3), Failure (4) have no Type field.
	switch code {
	case EapCodeRequest, EapCodeResponse:
		if len(data) < 5 {
			return nil, fmt.Errorf("%w: missing type field", ErrInvalidPacket)
		}
		p.Type = data[4]
		p.Data = data[5:]
	default:
		// Success, Failure have no Type or Data beyond the 4-byte header.
		p.Data = data[4:]
	}

	return p, nil
}

// Encode serializes an EAP packet to wire format.
// Spec: RFC 3748 §4
func Encode(p *Packet) []byte {
	// Calculate total length.
	// Request/Response always have a Type byte; Success/Failure do not.
	length := 4
	if p.Code == EapCodeRequest || p.Code == EapCodeResponse {
		length++
	}
	length += len(p.Data)

	// Clamp to maximum.
	if length > MaxEAPSize {
		length = MaxEAPSize
	}

	buf := make([]byte, length)
	buf[0] = byte(p.Code)
	buf[1] = p.Id
	binary.BigEndian.PutUint16(buf[2:4], uint16(length))

	if p.Code == EapCodeRequest || p.Code == EapCodeResponse {
		buf[4] = p.Type
		copy(buf[5:], p.Data)
	} else {
		copy(buf[4:], p.Data)
	}

	return buf
}

// BuildRequest creates an EAP-Request packet with the given type and optional data.
// Spec: RFC 3748 §4
func BuildRequest(id uint8, method EapMethod, data []byte) *Packet {
	p := &Packet{
		Code:   EapCodeRequest,
		Id:     id,
		Type:   byte(method),
		Data:   data,
		Length: 5 + uint16(len(data)),
	}
	p.RawData = Encode(p)
	return p
}

// BuildResponse creates an EAP-Response packet with the given type and data.
// Spec: RFC 3748 §4
func BuildResponse(id uint8, method EapMethod, data []byte) *Packet {
	p := &Packet{
		Code:   EapCodeResponse,
		Id:     id,
		Type:   byte(method),
		Data:   data,
		Length: 5 + uint16(len(data)),
	}
	p.RawData = Encode(p)
	return p
}

// BuildSuccess creates an EAP-Success packet.
// Spec: RFC 3748 §4.2
func BuildSuccess(id uint8) *Packet {
	p := &Packet{
		Code:   EapCodeSuccess,
		Id:     id,
		Length: 4,
		Data:   nil,
	}
	p.RawData = Encode(p)
	return p
}

// BuildFailure creates an EAP-Failure packet.
// Spec: RFC 3748 §4.2
func BuildFailure(id uint8) *Packet {
	p := &Packet{
		Code:   EapCodeFailure,
		Id:     id,
		Length: 4,
		Data:   nil,
	}
	p.RawData = Encode(p)
	return p
}

// String implements fmt.Stringer.
func (p *Packet) String() string {
	return fmt.Sprintf("EAP[Code=%s, Id=%d, Type=%s, Len=%d]",
		p.Code, p.Id, EapMethod(p.Type), p.Length)
}

// Clone returns a deep copy of the packet.
func (p *Packet) Clone() *Packet {
	dataCopy := make([]byte, len(p.Data))
	copy(dataCopy, p.Data)
	return &Packet{
		Code:    p.Code,
		Id:      p.Id,
		Length:  p.Length,
		Type:    p.Type,
		Data:    dataCopy,
		RawData: bytes.Clone(p.RawData),
	}
}
