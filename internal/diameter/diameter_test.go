// Package diameter provides Diameter client for AAA protocol interworking.
// Spec: TS 29.561 Ch.17, RFC 4072
package diameter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSDToBytes(t *testing.T) {
	tests := []struct {
		name    string
		sd      string
		wantNil bool
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
			result := parseSDToBytes(tt.sd)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Len(t, result, tt.wantLen)
			}
		})
	}
}

func TestEncodeSnssaiAVPWithSD(t *testing.T) {
	avp := EncodeSnssaiAVP(1, "123456")

	// Verify the AVP has the correct properties.
	assert.NotNil(t, avp)
	assert.Equal(t, uint32(310), avp.Code)
	assert.Equal(t, VendorID3GPP, avp.VendorID)
}

func TestEncodeSnssaiAVPWithoutSD(t *testing.T) {
	avp := EncodeSnssaiAVP(5, "")

	assert.NotNil(t, avp)
	assert.Equal(t, uint32(310), avp.Code)
	assert.Equal(t, VendorID3GPP, avp.VendorID)
}

func TestEncodeSnssaiAVPInvalidSD(t *testing.T) {
	// Should not panic even with invalid SD.
	avp := EncodeSnssaiAVP(2, "bad")
	assert.NotNil(t, avp)
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
	avp := EncodeSessionIdAVP("nssAAF;123;456")

	assert.NotNil(t, avp)
	assert.Equal(t, uint32(263), avp.Code)
}

func TestVendorID(t *testing.T) {
	assert.Equal(t, uint32(10415), VendorID3GPP)
}
