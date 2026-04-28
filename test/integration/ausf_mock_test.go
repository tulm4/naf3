// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/operator/nssAAF/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_AUSF_GetUeAuthData verifies that the AUSF mock returns
// correct UE authentication data for a known GPSI.
func TestIntegration_AUSF_GetUeAuthData(t *testing.T) {
	mock := mocks.NewAUSFMock()
	defer mock.Close()

	mock.SetUEAuthData("520804600000001", &mocks.UeAuthData{
		AuthType:   "EAP",
		AuthSubscribed: "EAP-TLS",
		KDFNegotiationSupported: true,
		SequenceNumber:   "00001",
	})

	resp, err := http.Get(mock.URL() + "/nausf-auth/v1/ue-identities/520804600000001")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var data mocks.UeAuthData
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&data))
	assert.Equal(t, "EAP", data.AuthType)
	assert.Equal(t, "EAP-TLS", data.AuthSubscribed)
	assert.True(t, data.KDFNegotiationSupported)
}

// TestIntegration_AUSF_UnknownGPSI verifies that the AUSF mock returns 404
// for an unknown GPSI.
func TestIntegration_AUSF_UnknownGPSI(t *testing.T) {
	mock := mocks.NewAUSFMock()
	defer mock.Close()

	resp, err := http.Get(mock.URL() + "/nausf-auth/v1/ue-identities/520999999999999")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"unknown GPSI should return 404")

	var problem map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&problem))
	assert.Contains(t, problem["cause"], "UE_NOT_FOUND")
}

// TestIntegration_AUSF_Timeout verifies that the AUSF mock correctly returns
// the configured error code for error injection (simulating timeout).
func TestIntegration_AUSF_Timeout(t *testing.T) {
	mock := mocks.NewAUSFMock()
	defer mock.Close()

	// Configure mock to return 504 for this GPSI.
	mock.SetError("520804600000099", http.StatusGatewayTimeout)

	resp, err := http.Get(mock.URL() + "/nausf-auth/v1/ue-identities/520804600000099")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusGatewayTimeout, resp.StatusCode,
		"configured error code should be returned by AUSF mock")
}
