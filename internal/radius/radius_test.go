// Package radius provides RADIUS client for AAA protocol interworking.
// Spec: TS 29.561 Ch.16, RFC 2865, RFC 3579
package radius

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Packet encode/decode
// ============================================================================

func TestPacketEncodeDecode(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "user"),
		MakeIntegerAttribute(AttrServiceType, 8),
	})

	encoded := packet.Encode()
	assert.Greater(t, len(encoded), 20)

	decoded, err := DecodePacket(encoded)
	assert.NoError(t, err)
	assert.Equal(t, uint8(CodeAccessRequest), decoded.Code)
	assert.Equal(t, uint8(1), decoded.Id)
	assert.Equal(t, auth, decoded.Vector)
	assert.Len(t, decoded.Attributes, 2)
}

func TestPacketEncodeDecodeWithVSA(t *testing.T) {
	auth := [16]byte{0x10, 0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A, 0x09, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}
	packet := BuildAccessRequest(5, auth, []Attribute{
		Make3GPPSNSSAIAttribute(1, "ABCDEF"),
	})

	encoded := packet.Encode()

	decoded, err := DecodePacket(encoded)
	assert.NoError(t, err)
	assert.Equal(t, uint8(CodeAccessRequest), decoded.Code)
	assert.Equal(t, uint8(5), decoded.Id)
	assert.Len(t, decoded.Attributes, 1)
}

func TestDecodePacketTooShort(t *testing.T) {
	_, err := DecodePacket([]byte{1, 2})
	assert.Error(t, err)
}

func TestDecodePacketLengthMismatch(t *testing.T) {
	data := make([]byte, 20)
	data[0] = CodeAccessRequest
	data[1] = 1
	data[2] = 0
	data[3] = 50 // declares 50 bytes but only 20 present
	_, err := DecodePacket(data)
	assert.Error(t, err)
}

func TestBuildAccessRequest(t *testing.T) {
	auth := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	packet := BuildAccessRequest(42, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "alice"),
	})

	assert.Equal(t, uint8(CodeAccessRequest), packet.Code)
	assert.Equal(t, uint8(42), packet.Id)
	assert.Equal(t, auth, packet.Vector)
	assert.Len(t, packet.Attributes, 1)
}

func TestBuildAccessAccept(t *testing.T) {
	auth := [16]byte{0x10, 0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A, 0x09, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}
	packet := BuildAccessAccept(1, auth, []Attribute{
		MakeStringAttribute(AttrReplyMessage, "Welcome"),
	})

	assert.Equal(t, uint8(CodeAccessAccept), packet.Code)
	assert.Equal(t, uint8(1), packet.Id)
}

func TestBuildAccessReject(t *testing.T) {
	auth := [16]byte{}
	packet := BuildAccessReject(1, auth, nil)

	assert.Equal(t, uint8(CodeAccessReject), packet.Code)
}

func TestBuildAccessChallenge(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessChallenge(3, auth, []Attribute{
		MakeStringAttribute(AttrState, "challenge-state"),
	})

	assert.Equal(t, uint8(CodeAccessChallenge), packet.Code)
	assert.Equal(t, uint8(3), packet.Id)
	assert.Len(t, packet.Attributes, 1)
}

func TestPacketString(t *testing.T) {
	packet := BuildAccessRequest(5, [16]byte{}, nil)
	s := packet.String()
	assert.Contains(t, s, "RADIUS")
	assert.Contains(t, s, "Code=1")
	assert.Contains(t, s, "Id=5")
}

// ============================================================================
// Attribute decode
// ============================================================================

func TestDecodeAttributes(t *testing.T) {
	// Build a []byte manually to avoid character literal expansion issues.
	data := make([]byte, 0, 20)
	// User-Name (type=1): type(1) + len(1) + "bob"(3) = 5 bytes
	data = append(data, 1, 5)
	data = append(data, []byte("bob")...)
	// NAS-Port-Type (type=61): type(1) + len(1) + value(4) = 6 bytes
	data = append(data, 61, 6, 0, 0, 0, 5)

	attrs, err := DecodeAttributes(data)
	assert.NoError(t, err)
	assert.Len(t, attrs, 2)
	assert.Equal(t, uint8(1), attrs[0].Type)
	assert.Equal(t, "bob", string(attrs[0].Value))
	assert.Equal(t, uint8(61), attrs[1].Type)
}

func TestDecodeAttributesEmpty(t *testing.T) {
	attrs, err := DecodeAttributes([]byte{})
	assert.NoError(t, err)
	assert.Len(t, attrs, 0)
}

func TestDecodeAttributesTruncated(t *testing.T) {
	// Type=1, Length=10 claims 8 bytes of value, but only 1 present.
	data := make([]byte, 3)
	data[0] = 1
	data[1] = 10
	data[2] = 'a'
	_, err := DecodeAttributes(data)
	assert.Error(t, err)
}

func TestDecodeAttributesInvalidLength(t *testing.T) {
	// Length field < 2 is invalid (minimum is type+length).
	_, err := DecodeAttributes([]byte{1, 1})
	assert.Error(t, err)
}

// ============================================================================
// Attribute helpers
// ============================================================================

func TestGetAttribute(t *testing.T) {
	attrs := []Attribute{
		MakeStringAttribute(AttrUserName, "bob"),
		MakeIntegerAttribute(AttrNASPortType, 19),
		MakeStringAttribute(AttrCallingStationID, "1234"),
	}

	userName := GetAttribute(attrs, AttrUserName)
	assert.NotNil(t, userName)
	assert.Equal(t, "bob", string(userName.Value))

	nasPort := GetAttribute(attrs, AttrNASPortType)
	assert.NotNil(t, nasPort)
	assert.Equal(t, uint32(19), GetInteger(nasPort))

	missing := GetAttribute(attrs, AttrAcctSessionID)
	assert.Nil(t, missing)
}

func TestGetAttributes(t *testing.T) {
	attrs := []Attribute{
		MakeAttribute(AttrEAPMessage, []byte("part1")),
		MakeAttribute(AttrEAPMessage, []byte("part2")),
		MakeAttribute(AttrUserName, []byte("user")),
	}

	eapAttrs := GetAttributes(attrs, AttrEAPMessage)
	assert.Len(t, eapAttrs, 2)

	none := GetAttributes(attrs, AttrAcctSessionID)
	assert.Len(t, none, 0)
}

func TestMakeStringAttribute(t *testing.T) {
	attr := MakeStringAttribute(AttrUserName, "alice")
	assert.Equal(t, uint8(1), attr.Type)
	assert.Equal(t, []byte("alice"), attr.Value)
}

func TestMakeIntegerAttribute(t *testing.T) {
	attr := MakeIntegerAttribute(AttrServiceType, 8)
	assert.Equal(t, uint8(AttrServiceType), attr.Type)
	assert.Equal(t, uint32(8), GetInteger(&attr))
}

func TestMakeIntegerAttributeLarge(t *testing.T) {
	attr := MakeIntegerAttribute(AttrNASPortType, 0x12345678)
	assert.Equal(t, uint32(0x12345678), GetInteger(&attr))
}

func TestGetString(t *testing.T) {
	assert.Equal(t, "", GetString(nil))
	attr := MakeStringAttribute(AttrUserName, "test")
	assert.Equal(t, "test", GetString(&attr))
}

func TestGetIntegerTruncated(t *testing.T) {
	attr := Attribute{Type: AttrServiceType, Value: []byte{1}}
	assert.Equal(t, uint32(0), GetInteger(&attr))
}

func TestEncodeAttributes(t *testing.T) {
	attrs := []Attribute{
		MakeStringAttribute(AttrUserName, "bob"),
		MakeIntegerAttribute(AttrNASPortType, 5),
	}
	data := EncodeAttributes(attrs)
	assert.NotEmpty(t, data)

	// Decode back
	decoded, err := DecodeAttributes(data)
	assert.NoError(t, err)
	assert.Len(t, decoded, 2)
	assert.Equal(t, "bob", string(decoded[0].Value))
	assert.Equal(t, uint32(5), GetInteger(&decoded[1]))
}

func TestEncodeAttributesEmpty(t *testing.T) {
	data := EncodeAttributes(nil)
	assert.Empty(t, data)
	data = EncodeAttributes([]Attribute{})
	assert.Empty(t, data)
}

// ============================================================================
// VSA encode/decode
// ============================================================================

func TestEncodeVSA(t *testing.T) {
	// EncodeVSA: VendorID=10415 (0x28AF), VendorType=200 (0xC8), Data=[1,2,3,4]
	// Vendor-ID 3 bytes LE: [0xAF, 0x28, 0x00]
	// Outer attribute: type=26, length=10, value=[0xAF,0x28,0x00,0xC8,1,2,3,4]
	attr := EncodeVSA(VendorID3GPP, VendorTypeSNSSAI, []byte{1, 2, 3, 4})
	assert.Equal(t, uint8(AttrVendorSpecific), attr.Type)
	assert.Len(t, attr.Value, 8) // 3 VID + 1 VType + 4 Data

	decoded, err := DecodeVSA(&attr)
	assert.NoError(t, err)
	assert.Equal(t, uint32(VendorID3GPP), decoded.VendorID)
	assert.Equal(t, uint8(VendorTypeSNSSAI), decoded.VendorType)
	assert.Equal(t, []byte{1, 2, 3, 4}, decoded.Data)
}

func TestDecodeVSAInvalid(t *testing.T) {
	_, err := DecodeVSA(nil)
	assert.Error(t, err)

	notVSA := Attribute{Type: AttrUserName, Value: []byte{1, 2, 3, 4, 5}}
	_, err = DecodeVSA(&notVSA)
	assert.Error(t, err)

	shortVSA := Attribute{Type: AttrVendorSpecific, Value: []byte{1}}
	_, err = DecodeVSA(&shortVSA)
	assert.Error(t, err)
}

// ============================================================================
// 3GPP S-NSSAI VSA
// ============================================================================

func TestEncodeSnssaiVSA(t *testing.T) {
	// SST=1, SD="ABCDEF" → hex.DecodeString → [0xAB, 0xCD, 0xEF]
	vsaData := EncodeSnssaiVSA(1, "ABCDEF")
	assert.Len(t, vsaData, 4)
	assert.Equal(t, uint8(1), vsaData[0])
	assert.Equal(t, []byte{0xAB, 0xCD, 0xEF}, vsaData[1:])
}

func TestDecodeSnssaiVSA(t *testing.T) {
	// [SST=1, SD=0xAB,0xCD,0xEF] → decode → SST=1, SD="ABCDEF"
	sst, sd, err := DecodeSnssaiVSA([]byte{1, 0xAB, 0xCD, 0xEF})
	assert.NoError(t, err)
	assert.Equal(t, uint8(1), sst)
	assert.Equal(t, "ABCDEF", sd)
}

func TestEncodeSnssaiVSASDOnly(t *testing.T) {
	// Without SD, SST only → padded to 4 bytes with zeros.
	vsaData := EncodeSnssaiVSA(5, "")
	assert.Len(t, vsaData, 4)
	assert.Equal(t, uint8(5), vsaData[0])
	// SD bytes should be zero
	assert.Equal(t, []byte{0, 0, 0}, vsaData[1:])
}

func TestDecodeSnssaiVSASDOnly(t *testing.T) {
	// SST=5, SD=0 → should return empty SD (no SD bytes in input).
	sst, sd, err := DecodeSnssaiVSA([]byte{5})
	assert.NoError(t, err)
	assert.Equal(t, uint8(5), sst)
	assert.Equal(t, "", sd)
}

func TestDecodeSnssaiVSATooShort(t *testing.T) {
	_, _, err := DecodeSnssaiVSA([]byte{})
	assert.Error(t, err)
}

func TestSnssaiVSAEncodeDecode(t *testing.T) {
	// Encode SST=3, SD="123456" (hex: 0x12,0x34,0x56)
	vsaData := EncodeSnssaiVSA(3, "123456")
	sst, sd, err := DecodeSnssaiVSA(vsaData)
	assert.NoError(t, err)
	assert.Equal(t, uint8(3), sst)
	assert.Equal(t, "123456", sd)
}

func TestMake3GPPSNSSAIAttribute(t *testing.T) {
	attr := Make3GPPSNSSAIAttribute(3, "123456")
	assert.Equal(t, uint8(AttrVendorSpecific), attr.Type)

	vsa, err := DecodeVSA(&attr)
	assert.NoError(t, err)
	assert.True(t, vsa.Is3GPPSNSSAI())

	sst, sd, err := vsa.Parse3GPPSNSSAI()
	assert.NoError(t, err)
	assert.Equal(t, uint8(3), sst)
	assert.Equal(t, "123456", sd)
}

func TestMake3GPPSNSSAIAttributeNoSD(t *testing.T) {
	attr := Make3GPPSNSSAIAttribute(1, "")
	assert.Equal(t, uint8(AttrVendorSpecific), attr.Type)

	vsa, err := DecodeVSA(&attr)
	assert.NoError(t, err)
	assert.True(t, vsa.Is3GPPSNSSAI())

	sst, sd, err := vsa.Parse3GPPSNSSAI()
	assert.NoError(t, err)
	assert.Equal(t, uint8(1), sst)
	assert.Equal(t, "000000", sd) // zero-padded SD
}

func TestVSAIs3GPPSNSSAI(t *testing.T) {
	vsa := &VSA{VendorID: VendorID3GPP, VendorType: VendorTypeSNSSAI, Data: []byte{1, 2, 3, 4}}
	assert.True(t, vsa.Is3GPPSNSSAI())

	wrongVID := &VSA{VendorID: 9999, VendorType: VendorTypeSNSSAI, Data: []byte{1, 2, 3, 4}}
	assert.False(t, wrongVID.Is3GPPSNSSAI())

	wrongType := &VSA{VendorID: VendorID3GPP, VendorType: 99, Data: []byte{1, 2, 3, 4}}
	assert.False(t, wrongType.Is3GPPSNSSAI())
}

// ============================================================================
// Message Authenticator
// ============================================================================

func TestMessageAuthenticator(t *testing.T) {
	secret := "shared_secret"
	packet := BuildAccessRequest(1, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "test"),
	})

	raw := packet.Encode()
	raw = AddMessageAuthenticator(raw, secret)

	assert.True(t, hasMessageAuthenticator(raw))
	assert.True(t, VerifyMessageAuthenticator(raw, secret))
	assert.False(t, VerifyMessageAuthenticator(raw, "wrong_secret"))
}

func TestAddMessageAuthenticatorNoDuplicate(t *testing.T) {
	secret := "secret"
	packet := BuildAccessRequest(1, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "test"),
	})
	raw := packet.Encode()
	originalLen := len(raw)

	// Add twice — should not duplicate.
	raw = AddMessageAuthenticator(raw, secret)
	raw = AddMessageAuthenticator(raw, secret)

	assert.True(t, hasMessageAuthenticator(raw))
	assert.Equal(t, originalLen+18, len(raw))
}

func TestComputeMessageAuthenticatorSize(t *testing.T) {
	packet := []byte{1, 1, 0, 30}
	packet = append(packet, make([]byte, 16)...) // pad
	ma := ComputeMessageAuthenticator(packet, "secret")
	assert.Len(t, ma, 16)
}

func TestRemoveMessageAuthenticator(t *testing.T) {
	secret := "secret"
	packet := BuildAccessRequest(1, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "test"),
	})
	raw := packet.Encode()
	raw = AddMessageAuthenticator(raw, secret)
	originalLen := len(raw)

	clean := removeMessageAuthenticator(raw)
	assert.Equal(t, originalLen-18, len(clean)) // MA attribute removed
	assert.False(t, hasMessageAuthenticator(clean))
}

func TestHasMessageAuthenticatorFalse(t *testing.T) {
	packet := BuildAccessRequest(1, [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, []Attribute{
		MakeStringAttribute(AttrUserName, "test"),
	})
	raw := packet.Encode()
	assert.False(t, hasMessageAuthenticator(raw))
}

// ============================================================================
// Fragmentation
// ============================================================================

func TestFragmentEAPMessageShort(t *testing.T) {
	short := []byte("hello")
	frags := FragmentEAPMessage(short, 253)
	assert.Len(t, frags, 1)
	assert.Equal(t, short, frags[0])
}

func TestFragmentEAPMessageExact(t *testing.T) {
	exact := make([]byte, 253)
	for i := range exact {
		exact[i] = byte(i)
	}
	frags := FragmentEAPMessage(exact, 253)
	assert.Len(t, frags, 1)
}

func TestFragmentEAPMessageMultiple(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = byte(i % 256)
	}
	frags := FragmentEAPMessage(long, 253)
	assert.Len(t, frags, 2)
	assert.Len(t, frags[0], 253)
	assert.Len(t, frags[1], 247)
}

func TestAssembleEAPMessage(t *testing.T) {
	attrs := []Attribute{
		{Type: AttrEAPMessage, Value: []byte("part1part2")},
		{Type: AttrEAPMessage, Value: []byte("part3")},
		{Type: AttrUserName, Value: []byte("user")},
	}
	assembled := AssembleEAPMessage(attrs)
	assert.Equal(t, "part1part2part3", string(assembled))
}

func TestAssembleEAPMessageEmpty(t *testing.T) {
	attrs := []Attribute{{Type: AttrUserName, Value: []byte("user")}}
	assembled := AssembleEAPMessage(attrs)
	assert.Empty(t, assembled)
}

// ============================================================================
// Transport helpers
// ============================================================================

func TestGenerateRandomAuthenticator(t *testing.T) {
	auth1, err := GenerateRandomAuthenticator()
	assert.NoError(t, err)
	assert.Len(t, auth1, 16)

	auth2, err := GenerateRandomAuthenticator()
	assert.NoError(t, err)

	// Should be random.
	assert.NotEqual(t, auth1, auth2)
}

// ============================================================================
// Round-trip: full packet with all attributes
// ============================================================================

func TestFullPacketRoundTrip(t *testing.T) {
	auth := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	packet := BuildAccessRequest(1, auth, []Attribute{
		MakeStringAttribute(AttrUserName, "alice@example.com"),
		MakeIntegerAttribute(AttrServiceType, ServiceTypeAuthenticateOnly),
		MakeIntegerAttribute(AttrNASPortType, NASPortTypeNASVirtual),
		MakeStringAttribute(AttrCallingStationID, "alice@example.com"),
		Make3GPPSNSSAIAttribute(1, "ABCDEF"),
		MakeAttribute(AttrEAPMessage, []byte("eap-data")),
	})

	encoded := packet.Encode()
	decoded, err := DecodePacket(encoded)
	assert.NoError(t, err)
	assert.Equal(t, uint8(CodeAccessRequest), decoded.Code)
	assert.Len(t, decoded.Attributes, 6)

	// Check User-Name.
	un := GetAttribute(decoded.Attributes, AttrUserName)
	assert.NotNil(t, un)
	assert.Equal(t, "alice@example.com", GetString(un))

	// Check Service-Type.
	st := GetAttribute(decoded.Attributes, AttrServiceType)
	assert.NotNil(t, st)
	assert.Equal(t, uint32(ServiceTypeAuthenticateOnly), GetInteger(st))

	// Check S-NSSAI VSA.
	vsa := GetAttribute(decoded.Attributes, AttrVendorSpecific)
	assert.NotNil(t, vsa)
	decodedVSA, err := DecodeVSA(vsa)
	assert.NoError(t, err)
	assert.True(t, decodedVSA.Is3GPPSNSSAI())
	sst, sd, _ := decodedVSA.Parse3GPPSNSSAI()
	assert.Equal(t, uint8(1), sst)
	assert.Equal(t, "ABCDEF", sd)

	// Check EAP message.
	eapAttrs := GetAttributes(decoded.Attributes, AttrEAPMessage)
	assert.Len(t, eapAttrs, 1)
	assert.Equal(t, []byte("eap-data"), eapAttrs[0].Value)
}

func TestGetAttributeBytes(t *testing.T) {
	packet := BuildAccessRequest(1, [16]byte{}, []Attribute{
		MakeStringAttribute(AttrState, "state-data"),
	})
	raw := packet.Encode()

	state := GetAttributeBytes(raw, AttrState)
	assert.Equal(t, []byte("state-data"), state)

	missing := GetAttributeBytes(raw, AttrUserName)
	assert.Nil(t, missing)
}
