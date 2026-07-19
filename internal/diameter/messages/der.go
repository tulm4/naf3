// Package messages provides strongly-typed Diameter message structs for NSSAAF.
// Spec: RFC 4072 (DER/DEA), RFC 6733 (ASR/ASA, RAR/RAA, STR/STA), TS 29.561 Ch.17
package messages

import (
	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// Command codes per RFC/3GPP.
const (
	CmdDiameterEAP      uint32 = 268 // DER/DEA
	CmdAbortSession     uint32 = 274 // ASR/ASA
	CmdReAuth           uint32 = 258 // RAR/RAA
	CmdSessionTermination uint32 = 275 // STR/STA
)

// Auth request types.
const (
	AuthRequestTypeAuthenticate      uint32 = 1
	AuthRequestTypeAuthorizeAuth     uint32 = 2
)

// Auth session state.
const (
	AuthSessionStateMaintainSession uint32 = 0
	AuthSessionStateNoState        uint32 = 1
)

// Re-Auth request types (RAR).
const (
	ReAuthTypeAuthorizeOnly        uint32 = 0
	ReAuthTypeAuthorizeAuthenticate uint32 = 1
)

// Termination causes (STR).
const (
	TerminationCauseLogout           uint32 = 1
	TerminationCauseServiceNA       uint32 = 2
	TerminationCauseSessionTimeout   uint32 = 3
	TerminationCauseAdminReset      uint32 = 4
	TerminationCauseAdminReboot      uint32 = 5
	TerminationCauseProtocolError    uint32 = 6
	TerminationCauseMaintenance     uint32 = 7
	TerminationCauseHostNotResponding uint32 = 8
)

// Result codes.
const (
	ResultCodeSuccess              uint32 = 2001
	ResultCodeLimitedSuccess       uint32 = 2002
	ResultCodeDIAMETERMULTIROUNDAUTH uint32 = 1041
)

// DER represents a Diameter-EAP-Request (Command Code 268, AppID 5).
// Spec: RFC 4072, TS 29.561 §17.2.1
type DER struct {
	SessionID          string
	OriginHost         string
	OriginRealm        string
	DestinationHost    string
	DestinationRealm   string
	AuthApplicationID  uint32
	AuthRequestType     uint32
	AuthSessionState    uint32
	OriginStateID      uint64
	UserName            string
	CallingStationID    string
	ExternalIdentifier  string
	EAPPayload         []byte
	SNSSAI             *SNSSAI
	NSSAIConfiguration *NSSAIConfiguration
	AAAServerName       string
}

// NewDER creates a DER with default values.
func NewDER(originHost, originRealm, destRealm string) *DER {
	return &DER{
		OriginHost:        originHost,
		OriginRealm:       originRealm,
		DestinationRealm:  destRealm,
		AuthApplicationID: 5, // Diameter EAP
		AuthRequestType:   AuthRequestTypeAuthorizeAuth,
		AuthSessionState:  AuthSessionStateNoState,
	}
}

// AddSNSSAI sets the S-NSSAI for the request.
func (d *DER) AddSNSSAI(sst uint8, sd string) error {
	s, err := NewSNSSAI(sst, sd)
	if err != nil {
		return err
	}
	d.SNSSAI = s
	return nil
}

// AddEAPPayload sets the EAP payload.
func (d *DER) AddEAPPayload(payload []byte) {
	d.EAPPayload = payload
}

// Encode encodes the DER to a Diameter message using the provided dictionary.
func (d *DER) Encode(dict *dict.Parser) (*diam.Message, error) {
	m := diam.NewRequest(CmdDiameterEAP, d.AuthApplicationID, dict)

	addAVP := func(code uint32, flags uint8, vendor uint32, data datatype.Type) error {
		_, err := m.NewAVP(code, flags, vendor, data)
		return err
	}

	addAVPM := func(code uint32, vendor uint32, data datatype.Type) error {
		return addAVP(code, avp.Mbit, vendor, data)
	}

	// Required AVPs.
	if err := addAVPM(avp.SessionID, 0, datatype.UTF8String(d.SessionID)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.AuthApplicationID, 0, datatype.Unsigned32(d.AuthApplicationID)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.AuthRequestType, 0, datatype.Unsigned32(d.AuthRequestType)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.AuthSessionState, 0, datatype.Unsigned32(d.AuthSessionState)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginHost, 0, datatype.DiameterIdentity(d.OriginHost)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginRealm, 0, datatype.DiameterIdentity(d.OriginRealm)); err != nil {
		return nil, err
	}
	if err := addAVPM(avp.OriginStateID, 0, datatype.Unsigned32(d.OriginStateID)); err != nil {
		return nil, err
	}
	if d.DestinationHost != "" {
		if err := addAVPM(avp.DestinationHost, 0, datatype.DiameterIdentity(d.DestinationHost)); err != nil {
			return nil, err
		}
	}
	if err := addAVPM(avp.DestinationRealm, 0, datatype.DiameterIdentity(d.DestinationRealm)); err != nil {
		return nil, err
	}

	// Optional AVPs.
	if d.UserName != "" {
		if err := addAVPM(avp.UserName, 0, datatype.UTF8String(d.UserName)); err != nil {
			return nil, err
		}
	}
	if d.CallingStationID != "" {
		if err := addAVPM(31, 0, datatype.UTF8String(d.CallingStationID)); err != nil {
			return nil, err
		}
	}
	if d.ExternalIdentifier != "" {
		if err := addAVPM(606, 10415, datatype.UTF8String(d.ExternalIdentifier)); err != nil {
			return nil, err
		}
	}
	if len(d.EAPPayload) > 0 {
		if err := addAVPM(209, 0, datatype.OctetString(d.EAPPayload)); err != nil {
			return nil, err
		}
	}
	if d.SNSSAI != nil {
		data, err := d.SNSSAI.MarshalBinary()
		if err != nil {
			return nil, err
		}
		if err := addAVPM(200, 10415, datatype.OctetString(data)); err != nil {
			return nil, err
		}
	}

	return m, nil
}
