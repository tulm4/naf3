// Package messages provides strongly-typed Diameter message structs for NSSAAF.
package messages

import (
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// STR represents a Session-Termination-Request (Command Code 275, AppID 0).
// Spec: RFC 6733 §5.4.6, TS 29.561 Ch.17
type STR struct {
	SessionID           string
	OriginHost         string
	OriginRealm        string
	DestinationHost    string
	DestinationRealm   string
	AuthApplicationID  uint32
	TerminationCause   uint32
	UserName           string
}

// NewSTR creates a new STR with default values.
func NewSTR(originHost, originRealm, destRealm string) *STR {
	return &STR{
		OriginHost:        originHost,
		OriginRealm:       originRealm,
		DestinationRealm:  destRealm,
		AuthApplicationID: 5, // Diameter EAP
	}
}

// Encode encodes the STR to a Diameter message.
func (s *STR) Encode(parser *dict.Parser) (*diam.Message, error) {
	m := diam.NewRequest(CmdSessionTermination, 0, parser)

	addAVPM := func(code uint32, vendor uint32, data datatype.Type) error {
		_, err := m.NewAVP(code, avp.Mbit, vendor, data)
		return err
	}

	// Required AVPs.
	if err := addAVPM(avp.SessionID, 0, datatype.UTF8String(s.SessionID)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginHost, 0, datatype.DiameterIdentity(s.OriginHost)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginRealm, 0, datatype.DiameterIdentity(s.OriginRealm)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.AuthApplicationID, 0, datatype.Unsigned32(s.AuthApplicationID)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.TerminationCause, 0, datatype.Unsigned32(s.TerminationCause)); err != nil {
		return nil, err
	}

	// Optional AVPs.
	if s.DestinationHost != "" {
		if err := addAVPM(avp.DestinationHost, 0, datatype.DiameterIdentity(s.DestinationHost)); err != nil {
			return nil, err
		}
	}
	if s.DestinationRealm != "" {
		if err := addAVPM(avp.DestinationRealm, 0, datatype.DiameterIdentity(s.DestinationRealm)); err != nil {
			return nil, err
		}
	}
	if s.UserName != "" {
		if err := addAVPM(avp.UserName, 0, datatype.UTF8String(s.UserName)); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// STA represents a Session-Termination-Answer (Command Code 275, AppID 0).
// Spec: RFC 6733 §5.4.6, TS 29.561 Ch.17
type STA struct {
	SessionID         string
	ResultCode       uint32
	OriginHost       string
	OriginRealm      string
}

// Decode decodes an STA from a received Diameter message.
func (s *STA) Decode(m *diam.Message) error {
	if avps, _ := m.FindAVPs(avp.SessionID, 0); len(avps) > 0 {
		if data, ok := avps[0].Data.(datatype.UTF8String); ok {
			s.SessionID = string(data)
		}
	}
	if avps, _ := m.FindAVPs(avp.ResultCode, 0); len(avps) > 0 {
		if data, ok := avps[0].Data.(datatype.Unsigned32); ok {
			s.ResultCode = uint32(data)
		}
	}
	if avps, _ := m.FindAVPs(avp.OriginHost, 0); len(avps) > 0 {
		if data, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			s.OriginHost = string(data)
		}
	}
	if avps, _ := m.FindAVPs(avp.OriginRealm, 0); len(avps) > 0 {
		if data, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			s.OriginRealm = string(data)
		}
	}
	return nil
}

// IsSuccess returns true if the result code indicates success.
func (s *STA) IsSuccess() bool {
	return s.ResultCode == ResultCodeSuccess
}
