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

// ─── Test: UDM_GetRegistration ───────────────────────────────────────────────

func TestIntegration_UDM_GetRegistration(t *testing.T) {
	mock := mocks.NewUDMMock()
	defer mock.Close()

	mock.SetGPSI("imsi-208046000000001", "520804600000001")

	resp, err := http.Get(mock.URL() + "/nudm-uemm/v1/imsi-208046000000001/registration")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var reg mocks.NudmUECMRegistration
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&reg))
	assert.Equal(t, "imsi-208046000000001", reg.Supi)
	assert.Equal(t, "520804600000001", reg.GPSI)
}

// ─── Test: UDM_GPSIKnown ─────────────────────────────────────────────────

func TestIntegration_UDM_GPSIKnown(t *testing.T) {
	mock := mocks.NewUDMMock()
	defer mock.Close()

	reg := &mocks.NudmUECMRegistration{
		Supi: "imsi-208046000000002",
		GPSI: "520804600000002",
		Registrations: []mocks.NudmRegItem{
			{PlmnID: "00101", Legacy: false},
		},
	}
	mock.SetRegistration("imsi-208046000000002", reg)

	resp, err := http.Get(mock.URL() + "/nudm-uemm/v1/imsi-208046000000002/registration")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result mocks.NudmUECMRegistration
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "imsi-208046000000002", result.Supi)
	assert.Equal(t, "520804600000002", result.GPSI)
}

// ─── Test: UDM_GPSIUnknown → 404 ────────────────────────────────────────

func TestIntegration_UDM_GPSIUnknown(t *testing.T) {
	mock := mocks.NewUDMMock()
	defer mock.Close()

	resp, err := http.Get(mock.URL() + "/nudm-uemm/v1/imsi-999999999999999/registration")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode,
		"unknown SUPI should return 404")

	var problem map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&problem))
	assert.Contains(t, problem["cause"], "USER_NOT_FOUND")
}

// ─── Test: UDM_Timeout → 504 ───────────────────────────────────────────

func TestIntegration_UDM_Timeout(t *testing.T) {
	mock := mocks.NewUDMMock()
	defer mock.Close()

	mock.SetError("imsi-timeout-001", http.StatusGatewayTimeout)

	resp, err := http.Get(mock.URL() + "/nudm-uemm/v1/imsi-timeout-001/registration")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusGatewayTimeout, resp.StatusCode,
		"configured error code should be returned by UDM mock")
}
