// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072
package diameter

import (
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSDToBytes(t *testing.T) {
	tests := []struct {
		name    string
		sd      string
		wantErr bool
		wantLen int
	}{
		{"valid", "ABCDEF", false, 3},
		{"valid zeros", "000000", false, 3},
		{"too short", "ABC", true, 0},
		{"too long", "ABCDEFG", true, 0},
		{"invalid chars", "GGGGGG", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSDToBytes(tt.sd)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				assert.Len(t, result, tt.wantLen)
			}
		})
	}
}

func TestEncodeSnssaiAVPWithSD(t *testing.T) {
	avp, err := EncodeSnssaiAVP(1, "123456")

	require.NoError(t, err)
	assert.NotNil(t, avp)
	assert.Equal(t, uint32(310), avp.Code)
	assert.Equal(t, VendorID3GPP, avp.VendorID)
}

func TestEncodeSnssaiAVPWithoutSD(t *testing.T) {
	avp, err := EncodeSnssaiAVP(5, "")

	require.NoError(t, err)
	assert.NotNil(t, avp)
	assert.Equal(t, uint32(310), avp.Code)
	assert.Equal(t, VendorID3GPP, avp.VendorID)
}

func TestEncodeSnssaiAVPInvalidSD(t *testing.T) {
	// Invalid SD length (3 chars) → error.
	_, err := EncodeSnssaiAVP(2, "bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SNSSAI SD")

	// Non-hex characters.
	_, err = EncodeSnssaiAVP(2, "GGGGGG")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SNSSAI SD")
}

func TestEncodeEapPayloadAVP(t *testing.T) {
	eapData := []byte{1, 2, 3, 4, 5}
	avp := EncodeEapPayloadAVP(eapData)

	assert.NotNil(t, avp)
	assert.Equal(t, uint32(209), avp.Code)
	assert.Equal(t, uint32(0), avp.VendorID)
}

func TestEncodeUserNameAVP(t *testing.T) {
	avp := EncodeUserNameAVP("user@example.com")

	assert.NotNil(t, avp)
	assert.Equal(t, uint32(1), avp.Code)
}

func TestEncodeSessionIdAVP(t *testing.T) {
	avp := EncodeSessionIDAVP("nssAAF;123;456")

	assert.NotNil(t, avp)
	assert.Equal(t, uint32(263), avp.Code)
}

func TestVendorID(t *testing.T) {
	assert.Equal(t, uint32(10415), VendorID3GPP)
}

// ---------------------------------------------------------------------------
// Decode function tests
// ---------------------------------------------------------------------------

func TestDecodeEapPayloadAVP(t *testing.T) {
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	eapBytes := []byte{1, 2, 3, 4, 5}
	_, err := msg.NewAVP(AVPCodeEAPPayload, 0, 0, datatype.OctetString(eapBytes))
	require.NoError(t, err)

	got, err := DecodeEapPayloadAVP(msg)
	require.NoError(t, err)
	assert.Equal(t, eapBytes, got)
}

func TestDecodeEapPayloadAVPNotFound(t *testing.T) {
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	got, err := DecodeEapPayloadAVP(msg)
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeResultCodeAVP(t *testing.T) {
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	_, err := msg.NewAVP(AVPCodeResultCode, 0, 0, datatype.Unsigned32(2001))
	require.NoError(t, err)

	got := DecodeResultCodeAVP(msg)
	assert.Equal(t, uint32(2001), got)
}

func TestDecodeResultCodeAVPNotFound(t *testing.T) {
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	got := DecodeResultCodeAVP(msg)
	assert.Equal(t, uint32(0), got)
}

func TestDecodeAuthApplicationId(t *testing.T) {
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	_, err := msg.NewAVP(AVPCodeAuthApplicationID, 0, 0, datatype.Unsigned32(5))
	require.NoError(t, err)

	got := DecodeAuthApplicationID(msg)
	assert.Equal(t, uint32(5), got)
}

func TestDecodeAuthApplicationIdNotFound(t *testing.T) {
	msg := diam.NewRequest(268, AppIDAAP, dict.Default)

	got := DecodeAuthApplicationID(msg)
	assert.Equal(t, uint32(0), got)
}
