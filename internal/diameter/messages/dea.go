// Package messages provides strongly-typed Diameter message structs for NSSAAF.
package messages

import (
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
)

// DEA represents a Diameter-EAP-Answer (Command Code 268, AppID 5).
// Spec: RFC 4072, TS 29.561 §17.2.2
type DEA struct {
	SessionID          string
	AuthApplicationID  uint32
	AuthRequestType     uint32
	ResultCode         uint32
	AuthSessionState    uint32
	OriginHost         string
	OriginRealm        string
	UserName            string
	EAPPayload         []byte
	NSSAIConfiguration *NSSAIConfiguration
	ErrorMessage        string
}

// NewDEA creates a new DEA with default values.
func NewDEA() *DEA {
	return &DEA{}
}

// Decode decodes a DEA from a received Diameter message.
func (d *DEA) Decode(m *diam.Message) error {
	// Session-ID.
	if avps, _ := m.FindAVPs(avp.SessionID, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.UTF8String); ok {
			d.SessionID = string(s)
		}
	}

	// Auth-Application-Id.
	if avps, _ := m.FindAVPs(avp.AuthApplicationID, 0); len(avps) > 0 {
		if u, ok := avps[0].Data.(datatype.Unsigned32); ok {
			d.AuthApplicationID = uint32(u)
		}
	}

	// Auth-Request-Type.
	if avps, _ := m.FindAVPs(avp.AuthRequestType, 0); len(avps) > 0 {
		if u, ok := avps[0].Data.(datatype.Unsigned32); ok {
			d.AuthRequestType = uint32(u)
		}
	}

	// Result-Code.
	if avps, _ := m.FindAVPs(avp.ResultCode, 0); len(avps) > 0 {
		if u, ok := avps[0].Data.(datatype.Unsigned32); ok {
			d.ResultCode = uint32(u)
		}
	}

	// Auth-Session-State.
	if avps, _ := m.FindAVPs(avp.AuthSessionState, 0); len(avps) > 0 {
		if u, ok := avps[0].Data.(datatype.Unsigned32); ok {
			d.AuthSessionState = uint32(u)
		}
	}

	// Origin-Host.
	if avps, _ := m.FindAVPs(avp.OriginHost, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			d.OriginHost = string(s)
		}
	}

	// Origin-Realm.
	if avps, _ := m.FindAVPs(avp.OriginRealm, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.DiameterIdentity); ok {
			d.OriginRealm = string(s)
		}
	}

	// User-Name.
	if avps, _ := m.FindAVPs(avp.UserName, 0); len(avps) > 0 {
		if s, ok := avps[0].Data.(datatype.UTF8String); ok {
			d.UserName = string(s)
		}
	}

	// EAP-Payload (code 209).
	if avps, _ := m.FindAVPs(209, 0); len(avps) > 0 {
		if o, ok := avps[0].Data.(datatype.OctetString); ok {
			d.EAPPayload = []byte(o)
		}
	}

	return nil
}

// IsSuccess returns true if the result code indicates success.
func (d *DEA) IsSuccess() bool {
	return d.ResultCode == ResultCodeSuccess || d.ResultCode == ResultCodeLimitedSuccess
}
