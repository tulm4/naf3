// Package diameter provides RFC 6733 Diameter base protocol conformance tests.
// Spec: RFC 6733, RFC 4072 (EAP), TS 29.561 §17
package diameter

import (
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiameter_CERCEAExchange tests the Capabilities-Exchange-Request and
// Capabilities-Exchange-Answer handshake per RFC 6733 §5.3.
func TestDiameter_CERCEAExchange(t *testing.T) {
	// CER is command code 257 (Capabilities-Exchange-Request)
	cer := diam.NewRequest(257, 0, dict.Default)

	// Add required Origin-Host and Origin-Realm AVPs
	_, err := cer.NewAVP(avp.OriginHost, avp.Mbit, 0, datatype.DiameterIdentity("nssAAF.operator.com"))
	require.NoError(t, err)
	_, err = cer.NewAVP(avp.OriginRealm, avp.Mbit, 0, datatype.DiameterIdentity("operator.com"))
	require.NoError(t, err)
	// Vendor-Id
	_, err = cer.NewAVP(avp.VendorID, avp.Mbit, 0, datatype.Unsigned32(10415))
	require.NoError(t, err)
	// Product-Name
	_, err = cer.NewAVP(avp.ProductName, 0, 0, datatype.UTF8String("NSSAAF"))
	require.NoError(t, err)

	// Verify CER is valid
	assert.Equal(t, uint8(1), cer.Header.Version)
	assert.Equal(t, uint32(257), cer.Header.CommandCode)

	// CEA must have same version, command code, R-bit cleared (answer flag = 0)
	cea := cer.Answer(0)
	assert.Equal(t, cer.Header.Version, cea.Header.Version)
	assert.Equal(t, cer.Header.CommandCode, cea.Header.CommandCode)
	assert.True(t, cea.Header.CommandFlags&0x80 == 0, "CEA must have R-bit cleared (answer flag)")
	assert.Equal(t, cer.Header.HopByHopID, cea.Header.HopByHopID)
	assert.Equal(t, cer.Header.EndToEndID, cea.Header.EndToEndID)
}

// TestDiameter_DERDEAExchange tests the Diameter-EAP-Request/Answer exchange
// used by NSSAAF for slice authentication.
func TestDiameter_DERDEAExchange(t *testing.T) {
	// DER is command code 268 with Application-Id 5 (Diameter EAP Application)
	der := diam.NewRequest(CmdDER, AppIDAAP, dict.Default)

	// Session-Id
	_, err := der.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String("nssAAF;auth-123;456"))
	require.NoError(t, err)
	// Auth-Application-Id
	_, err = der.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP))
	require.NoError(t, err)
	// Auth-Request-Type = AUTHORIZE_AUTHENTICATE (1)
	_, err = der.NewAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(1))
	require.NoError(t, err)
	// User-Name
	_, err = der.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String("520804600000001"))
	require.NoError(t, err)
	// EAP-Payload AVP
	eapBytes := []byte{1, 2, 3, 4, 5}
	_, err = der.NewAVP(AVPCodeEAPPayload, 0, 0, datatype.OctetString(eapBytes))
	require.NoError(t, err)

	// DER header must have correct values
	assert.Equal(t, uint8(1), der.Header.Version)
	assert.Equal(t, uint32(CmdDER), der.Header.CommandCode)
	assert.Equal(t, uint32(AppIDAAP), der.Header.ApplicationID)

	// Build DEA response
	dea := der.Answer(0)
	assert.Equal(t, der.Header.HopByHopID, dea.Header.HopByHopID)
	assert.Equal(t, der.Header.EndToEndID, dea.Header.EndToEndID)

	// DEA must have R-bit cleared (answer flag = 0)
	assert.True(t, dea.Header.CommandFlags&0x80 == 0, "DEA must have R-bit cleared")
}

// TestDiameter_AVPParsing tests that raw AVP bytes are correctly parsed into struct fields.
func TestDiameter_AVPParsing(t *testing.T) {
	// Build a message and add an AVP, then verify we can decode it
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	// Add EAP-Payload AVP with vendor-specific data
	eapPayload := []byte{0x02, 0x01, 0x00, 0x0a, 0x13, 0x01, 0x6c, 0x6f, 0x63, 0x61, 0x6c, 0x68, 0x6f, 0x73, 0x74}
	a, err := msg.NewAVP(AVPCodeEAPPayload, 0, 0, datatype.OctetString(eapPayload))
	require.NoError(t, err)
	assert.Equal(t, AVPCodeEAPPayload, a.Code)
	assert.Equal(t, uint32(0), a.VendorID)

	// Decode back
	decoded, err := DecodeEapPayloadAVP(msg)
	require.NoError(t, err)
	assert.Equal(t, eapPayload, decoded)
}

// TestDiameter_AVPBuilder tests building AVPs from struct fields to wire bytes.
func TestDiameter_AVPBuilder(t *testing.T) {
	// Test EncodeSnssaiAVP (S-NSSAI AVP, code 310, vendor 10415)
	snssaiAVP, err := EncodeSnssaiAVP(1, "ABCDEF")
	require.NoError(t, err)
	assert.NotNil(t, snssaiAVP)
	assert.Equal(t, uint32(310), snssaiAVP.Code)
	assert.Equal(t, VendorID3GPP, snssaiAVP.VendorID)
	assert.True(t, snssaiAVP.Flags&avp.Vbit != 0, "V-bit must be set for vendor-specific AVP")

	// Test EncodeEapPayloadAVP (EAP-Payload AVP, code 209, no vendor)
	eapAvp := EncodeEapPayloadAVP([]byte("eap-test"))
	require.NotNil(t, eapAvp)
	assert.Equal(t, uint32(209), eapAvp.Code)
	assert.Equal(t, uint32(0), eapAvp.VendorID)

	// Test EncodeUserNameAVP (User-Name AVP, code 1)
	userAvp := EncodeUserNameAVP("testuser")
	require.NotNil(t, userAvp)
	assert.Equal(t, uint32(1), userAvp.Code)
	assert.True(t, userAvp.Flags&avp.Mbit != 0, "M-bit must be set")

	// Test EncodeSessionIDAVP (Session-Id AVP, code 263)
	sessAvp := EncodeSessionIDAVP("session-id-abc")
	require.NotNil(t, sessAvp)
	assert.Equal(t, uint32(263), sessAvp.Code)
	assert.True(t, sessAvp.Flags&avp.Mbit != 0)
}

// TestDiameter_ResultCodeAVP tests Result-Code AVP encoding and decoding.
func TestDiameter_ResultCodeAVP(t *testing.T) {
	testCases := []struct {
		code   uint32
		result string
	}{
		{2001, "DIAMETER_SUCCESS"},
		{2002, "DIAMETER_LIMITED_SUCCESS"},
		{5001, "DIAMETER_AUTHENTICATION_REJECTED"},
		{5012, "DIAMETER_EAP_AUTH_REJECTED"},
	}

	for _, tc := range testCases {
		t.Run(tc.result, func(t *testing.T) {
			m := diam.NewRequest(268, AppIDAAP, dict.Default)
			_, err := m.NewAVP(AVPCodeResultCode, avp.Mbit, 0, datatype.Unsigned32(tc.code))
			require.NoError(t, err)

			got := DecodeResultCodeAVP(m)
			assert.Equal(t, tc.code, got)
		})
	}
}

// TestDiameter_EAPPayloadAVP tests the 3GPP vendor-specific EAP-Payload AVP
// (Vendor-Id=10415, code=1265) per TS 29.561 §17.
func TestDiameter_EAPPayloadAVP(t *testing.T) {
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	// 3GPP EAP-Payload AVP: Vendor-Id=10415, AVP code=1265
	// Note: In the current implementation we use AVP code 209 (RFC 4072 standard).
	// This test verifies the standard EAP-Payload AVP is encoded correctly.
	eapPayload := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	_, err := msg.NewAVP(AVPCodeEAPPayload, 0, 0, datatype.OctetString(eapPayload))
	require.NoError(t, err)

	// Verify the AVP is present and decodes correctly
	decoded, err := DecodeEapPayloadAVP(msg)
	require.NoError(t, err)
	assert.Equal(t, eapPayload, decoded)

	// Verify Auth-Application-Id is extractable
	_, err = msg.NewAVP(AVPCodeAuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP))
	require.NoError(t, err)

	appID := DecodeAuthApplicationID(msg)
	assert.Equal(t, uint32(AppIDAAP), appID)
}

// TestDiameter_MultipleAVPsInMessage tests a DER message containing multiple AVPs
// (Result-Code + Auth-Application-Id + EAP-Payload).
func TestDiameter_MultipleAVPsInMessage(t *testing.T) {
	msg := diam.NewRequest(CmdDER, AppIDAAP, dict.Default)

	// Add multiple AVPs
	_, err := msg.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String("session-001"))
	require.NoError(t, err)
	_, err = msg.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP))
	require.NoError(t, err)
	_, err = msg.NewAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(1))
	require.NoError(t, err)
	_, err = msg.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String("alice@example.com"))
	require.NoError(t, err)
	_, err = msg.NewAVP(AVPCodeEAPPayload, 0, 0, datatype.OctetString([]byte("eap-data")))
	require.NoError(t, err)

	// Verify all AVPs are present
	eapDecoded, err := DecodeEapPayloadAVP(msg)
	require.NoError(t, err)
	assert.Equal(t, []byte("eap-data"), eapDecoded)

	appID := DecodeAuthApplicationID(msg)
	assert.Equal(t, uint32(AppIDAAP), appID)
}

// TestDiameter_MessageHeaderParsing tests parsing of the Diameter message header
// fields: version, command code, and flags per RFC 6733 §3.
func TestDiameter_MessageHeaderParsing(t *testing.T) {
	// Create a DER message and verify its header fields
	msg := diam.NewRequest(CmdDER, AppIDAAP, dict.Default)

	// Verify header structure
	assert.Equal(t, uint8(1), msg.Header.Version, "Diameter version must be 1")
	assert.Equal(t, uint32(CmdDER), msg.Header.CommandCode, "Command code must be 268 (Diameter-EAP-Request)")
	assert.Equal(t, uint32(AppIDAAP), msg.Header.ApplicationID)

	// Verify the request flag (R-bit) is set in request
	assert.True(t, msg.Header.CommandFlags&0x80 != 0, "R-bit must be set for request")

	// Create an answer and verify the R-bit is cleared
	answer := msg.Answer(0)
	assert.True(t, answer.Header.CommandFlags&0x80 == 0, "R-bit must be cleared for answer")
	assert.Equal(t, msg.Header.HopByHopID, answer.Header.HopByHopID)
	assert.Equal(t, msg.Header.EndToEndID, answer.Header.EndToEndID)
}
