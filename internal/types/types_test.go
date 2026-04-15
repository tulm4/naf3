// Package types provides 3GPP data types for NSSAAF.
package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnssaiValidate(t *testing.T) {
	valid := []Snssai{
		{SST: 0},
		{SST: 1},
		{SST: 255},
		{SST: 1, SD: "000001"},
		{SST: 128, SD: "abcdef"},
	}
	for _, s := range valid {
		err := s.Validate()
		assert.NoError(t, err, "expected %v to be valid", s)
	}

	invalid := []Snssai{
		{SST: 1, SD: "00000"},
		{SST: 1, SD: "0000001"},
		{SST: 1, SD: "GGGGGG"},
		{SST: 1, SD: "xyz123"},
	}
	for _, s := range invalid {
		err := s.Validate()
		assert.Error(t, err, "expected %v to be invalid", s)
	}
}

func TestSnssaiString(t *testing.T) {
	assert.Equal(t, "S-NSSAI{1}", Snssai{SST: 1}.String())
	assert.Equal(t, "S-NSSAI{128:000001}", Snssai{SST: 128, SD: "000001"}.String())
}

func TestSnssaiKey(t *testing.T) {
	assert.Equal(t, "1:*", Snssai{SST: 1}.Key())
	assert.Equal(t, "128:000001", Snssai{SST: 128, SD: "000001"}.Key())
	assert.Equal(t, "1:ABCDEF", Snssai{SST: 1, SD: "abcdef"}.Key())
}

func TestSnssaiEqual(t *testing.T) {
	assert.True(t, Snssai{SST: 1, SD: "000001"}.Equal(Snssai{SST: 1, SD: "000001"}))
	assert.False(t, Snssai{SST: 1, SD: "000001"}.Equal(Snssai{SST: 1}))
	assert.False(t, Snssai{SST: 1, SD: "000001"}.Equal(Snssai{SST: 2, SD: "000001"}))
}

func TestSnssaiMarshalJSON(t *testing.T) {
	s := Snssai{SST: 1, SD: "000001"}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"sst":1`)
	assert.Contains(t, string(data), `"sd":"000001"`)

	var s2 Snssai
	err = json.Unmarshal(data, &s2)
	require.NoError(t, err)
	assert.Equal(t, s, s2)
}

func TestSnssaiFromJSON(t *testing.T) {
	data := []byte(`{"sst":128,"sd":"000001"}`)
	s, err := SnssaiFromJSON(data)
	require.NoError(t, err)
	assert.Equal(t, uint8(128), s.SST)
	assert.Equal(t, "000001", s.SD)
}

func TestGpsiValidate(t *testing.T) {
	tests := []struct {
		name    string
		gpsi    Gpsi
		wantErr bool
	}{
		{
			name:    "valid 9 chars",
			gpsi:    Gpsi("500000001"),
			wantErr: false,
		},
		{
			name:    "valid 10 chars",
			gpsi:    Gpsi("5208046000"),
			wantErr: false,
		},
		{
			name:    "valid 15 chars",
			gpsi:    Gpsi("52080460000001"),
			wantErr: false,
		},
		{
			name:    "valid with dash",
			gpsi:    Gpsi("5-20804600000001"),
			wantErr: false,
		},
		{
			name:    "invalid empty",
			gpsi:    Gpsi(""),
			wantErr: true,
		},
		{
			name:    "invalid too short",
			gpsi:    Gpsi("512345"),
			wantErr: true,
		},
		{
			name:    "invalid leading digit",
			gpsi:    Gpsi("0123456789012"),
			wantErr: true,
		},
		{
			name:    "invalid with letters",
			gpsi:    Gpsi("520abcde000001"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.gpsi.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGpsiNormalize(t *testing.T) {
	// Normalize removes the optional dash separator per TS 23.003
	result1 := Gpsi("52080460000001").Normalize()
	assert.Equal(t, "52080460000001", result1)
	result2 := Gpsi("5-20804600000001").Normalize()
	assert.Equal(t, "520804600000001", result2)
	result3 := Gpsi("5-0000001").Normalize()
	assert.Equal(t, "50000001", result3)
}

func TestSupiValidate(t *testing.T) {
	tests := []struct {
		name    string
		supi    Supi
		wantErr bool
	}{
		{
			name:    "valid IMSI",
			supi:    Supi("imu-208046000000001"),
			wantErr: false,
		},
		{
			name:    "invalid empty",
			supi:    Supi(""),
			wantErr: true,
		},
		{
			name:    "invalid prefix",
			supi:    Supi("abc-208046000000001"),
			wantErr: true,
		},
		{
			name:    "invalid too few digits",
			supi:    Supi("imu-208046"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.supi.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSupiHelpers(t *testing.T) {
	s := Supi("imu-208046000000001")
	assert.True(t, s.IsIMSI())
	assert.Equal(t, "208046000000001", s.IMSI())

	s2 := Supi("other-format")
	assert.False(t, s2.IsIMSI())
	assert.Equal(t, "", s2.IMSI())
}

func TestEapCodeString(t *testing.T) {
	assert.Equal(t, "REQUEST", EapCodeRequest.String())
	assert.Equal(t, "RESPONSE", EapCodeResponse.String())
	assert.Equal(t, "SUCCESS", EapCodeSuccess.String())
	assert.Equal(t, "FAILURE", EapCodeFailure.String())
	assert.Equal(t, "UNKNOWN(99)", EapCode(99).String())
}

func TestEapMethodString(t *testing.T) {
	assert.Equal(t, "EAP-Identity", EapMethodIdentity.String())
	assert.Equal(t, "EAP-TLS", EapMethodTLS.String())
	assert.Equal(t, "EAP-AKA'", EapMethodAKAPrime.String())
	assert.Equal(t, "EAP-Method(99)", EapMethod(99).String())
}

func TestEapMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		msg     EapMessage
		wantErr bool
	}{
		{
			name:    "valid base64",
			msg:     EapMessage("dXNlcgBleGFtcGxlLmNvbQ=="),
			wantErr: false,
		},
		{
			name:    "empty",
			msg:     EapMessage(""),
			wantErr: true,
		},
		{
			name:    "invalid base64",
			msg:     EapMessage("!!!not-valid-base64!!!"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEapMessageBytes(t *testing.T) {
	// "test" → base64 → bytes → "test"
	msg := EapMessage("dGVzdA==")
	b, err := msg.Bytes()
	require.NoError(t, err)
	assert.Equal(t, []byte("test"), b)

	_, err = EapMessage("!!!").Bytes()
	assert.Error(t, err)
}

func TestNewEapMessage(t *testing.T) {
	data := []byte("hello world")
	msg := NewEapMessage(data)
	b, err := msg.Bytes()
	require.NoError(t, err)
	assert.Equal(t, data, b)
}

func TestNssaaStatusValidate(t *testing.T) {
	valid := []NssaaStatus{
		NssaaStatusNotExecuted,
		NssaaStatusPending,
		NssaaStatusEapSuccess,
		NssaaStatusEapFailure,
	}
	for _, s := range valid {
		err := s.Validate()
		assert.NoError(t, err, "expected %s to be valid", s)
	}

	invalid := NssaaStatus("INVALID")
	err := invalid.Validate()
	assert.Error(t, err)
}

func TestNssaaStatusHelpers(t *testing.T) {
	assert.True(t, NssaaStatusEapSuccess.IsTerminal())
	assert.True(t, NssaaStatusEapFailure.IsTerminal())
	assert.False(t, NssaaStatusPending.IsTerminal())
	assert.False(t, NssaaStatusNotExecuted.IsTerminal())

	assert.False(t, NssaaStatusEapSuccess.IsPending())
	assert.True(t, NssaaStatusPending.IsPending())
}
