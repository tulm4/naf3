// Package messages provides strongly-typed Diameter message structs for NSSAAF.
package messages

import (
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// RAR represents a Re-Auth-Request (Command Code 258, AppID 0).
// Spec: RFC 6733 §5.4.5, TS 29.561 Ch.17
type RAR struct {
	SessionID         string
	OriginHost       string
	OriginRealm      string
	DestinationHost  string
	DestinationRealm string
	ReAuthRequestType uint32
	UserName         string
}

// NewRAR creates a new RAR with default values.
func NewRAR(originHost, originRealm, destRealm string) *RAR {
	return &RAR{
		OriginHost:        originHost,
		OriginRealm:       originRealm,
		DestinationRealm:  destRealm,
		ReAuthRequestType: ReAuthTypeAuthorizeAuthenticate,
	}
}

// Encode encodes the RAR to a Diameter message.
func (r *RAR) Encode(parser *dict.Parser) (*diam.Message, error) {
	m := diam.NewRequest(CmdReAuth, 0, parser)

	addAVPM := func(code uint32, vendor uint32, data datatype.Type) error {
		_, err := m.NewAVP(code, avp.Mbit, vendor, data)
		return err
	}

	if err := addAVPM(avp.SessionID, 0, datatype.UTF8String(r.SessionID)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginHost, 0, datatype.DiameterIdentity(r.OriginHost)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginRealm, 0, datatype.DiameterIdentity(r.OriginRealm)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.DestinationRealm, 0, datatype.DiameterIdentity(r.DestinationRealm)); err != nil {
		return nil, err
	}
	if r.DestinationHost != "" {
		if err := addAVPM(avp.DestinationHost, 0, datatype.DiameterIdentity(r.DestinationHost)); err != nil {
			return nil, err
		}
	}
	if err := addAVPM(avp.ReAuthRequestType, 0, datatype.Unsigned32(r.ReAuthRequestType)); err != nil {
		return nil, err
	}
	if r.UserName != "" {
		if err := addAVPM(avp.UserName, 0, datatype.UTF8String(r.UserName)); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// RAA represents a Re-Auth-Answer (Command Code 258, AppID 0).
// Spec: RFC 6733 §5.4.5, TS 29.561 Ch.17
type RAA struct {
	SessionID         string
	ResultCode       uint32
	OriginHost       string
	OriginRealm      string
	ReAuthRequestType uint32
	UserName         string
}

// Decode decodes an RAA from a received Diameter message.
func (r *RAA) Decode(m *diam.Message) error {
	if avps, _ := m.FindAVPs(avp.SessionID, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.UTF8String); ok {
			r.SessionID = string(s)
		}
	}
	if avps, _ := m.FindAVPs(avp.ResultCode, 0); len(avps) > 0 {
		if u, ok := avps[0].Data.(datatype.Unsigned32); ok {
			r.ResultCode = uint32(u)
		}
	}
	if avps, _ := m.FindAVPs(avp.OriginHost, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			r.OriginHost = string(s)
		}
	}
	if avps, _ := m.FindAVPs(avp.OriginRealm, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			r.OriginRealm = string(s)
		}
	}
	return nil
}

// IsSuccess returns true if the result code indicates success.
func (r *RAA) IsSuccess() bool {
	return r.ResultCode == ResultCodeSuccess
}
