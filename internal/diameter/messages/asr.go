// Package messages provides strongly-typed Diameter message structs for NSSAAF.
package messages

import (
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// ASR represents an Abort-Session-Request (Command Code 274, AppID 0).
// Spec: RFC 6733 §5.4.4, TS 29.561 Ch.17
type ASR struct {
	SessionID         string
	OriginHost       string
	OriginRealm      string
	DestinationHost  string
	DestinationRealm string
	AuthApplicationID uint32
	AuthRequestType   uint32
	UserName         string
}

// NewASR creates a new ASR with default values.
func NewASR(originHost, originRealm, destRealm string) *ASR {
	return &ASR{
		OriginHost:        originHost,
		OriginRealm:       originRealm,
		DestinationRealm:  destRealm,
		AuthApplicationID: 5, // Diameter EAP
	}
}

// Encode encodes the ASR to a Diameter message.
func (a *ASR) Encode(parser *dict.Parser) (*diam.Message, error) {
	m := diam.NewRequest(CmdAbortSession, 0, parser)

	addAVPM := func(code uint32, vendor uint32, data datatype.Type) error {
		_, err := m.NewAVP(code, avp.Mbit, vendor, data)
		return err
	}

	// Required AVPs.
	if err := addAVPM(avp.SessionID, 0, datatype.UTF8String(a.SessionID)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginHost, 0, datatype.DiameterIdentity(a.OriginHost)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginRealm, 0, datatype.DiameterIdentity(a.OriginRealm)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.DestinationRealm, 0, datatype.DiameterIdentity(a.DestinationRealm)); err != nil {
		return nil, err
	}

	// Optional AVPs.
	if a.DestinationHost != "" {
		if err := addAVPM(avp.DestinationHost, 0, datatype.DiameterIdentity(a.DestinationHost)); err != nil {
			return nil, err
		}
	}
	if a.AuthApplicationID != 0 {
		if err := addAVPM(avp.AuthApplicationID, 0, datatype.Unsigned32(a.AuthApplicationID)); err != nil {
			return nil, err
		}
	}
	if a.UserName != "" {
		if err := addAVPM(avp.UserName, 0, datatype.UTF8String(a.UserName)); err != nil {
			return nil, err
		}
	}

	return m, nil
}

// ASA represents an Abort-Session-Answer (Command Code 274, AppID 0).
// Spec: RFC 6733 §5.4.4, TS 29.561 Ch.17
type ASA struct {
	SessionID         string
	ResultCode       uint32
	OriginHost       string
	OriginRealm      string
}

// Decode decodes an ASA from a received Diameter message.
func (a *ASA) Decode(m *diam.Message) error {
	if avps, _ := m.FindAVPs(avp.SessionID, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.UTF8String); ok {
			a.SessionID = string(s)
		}
	}
	if avps, _ := m.FindAVPs(avp.ResultCode, 0); len(avps) > 0 {
		if u, ok := avps[0].Data.(datatype.Unsigned32); ok {
			a.ResultCode = uint32(u)
		}
	}
	if avps, _ := m.FindAVPs(avp.OriginHost, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			a.OriginHost = string(s)
		}
	}
	if avps, _ := m.FindAVPs(avp.OriginRealm, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			a.OriginRealm = string(s)
		}
	}
	return nil
}

// IsSuccess returns true if the result code indicates success.
func (a *ASA) IsSuccess() bool {
	return a.ResultCode == ResultCodeSuccess
}
