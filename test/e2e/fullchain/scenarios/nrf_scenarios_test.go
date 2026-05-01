//go:build e2e
// +build e2e

package scenarios

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/operator/nssAAF/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNRF_UDMDiscovery verifies NRF returns correct UDM endpoint.
// Spec: TS 29.510 §6.2.6
func TestNRF_UDMDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	// Configure default UDM endpoint
	nrfMock.SetServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8080)

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	instances, ok := result["nfInstances"].([]interface{})
	require.True(t, ok, "nfInstances must be an array")
	assert.NotEmpty(t, instances, "should return at least one UDM instance")

	first := instances[0].(map[string]interface{})
	services, ok := first["nfServices"].(map[string]interface{})
	require.True(t, ok)
	nudmService, ok := services["nudm-uem"].(map[string]interface{})
	require.True(t, ok)
	_, ok = nudmService["ipEndPoints"].([]interface{})
	require.True(t, ok)
}

// TestNRF_CustomEndpoint verifies SetServiceEndpoint changes discovery response.
// Spec: TS 29.510 §6.2.6
func TestNRF_CustomEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	// Set custom endpoint
	nrfMock.SetServiceEndpoint("UDM", "nudm-uem", "custom-udm", 9090)

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	// Re-encode to inspect string content for custom values
	out, err := json.Marshal(result)
	require.NoError(t, err)
	bodyStr := string(out)
	assert.Contains(t, bodyStr, "custom-udm", "should return custom endpoint host")
	assert.Contains(t, bodyStr, "9090", "should return custom port")
}

// TestNRF_NotRegistered verifies unregistered NFs are excluded from discovery.
// Spec: TS 29.510 §6.2.6
func TestNRF_NotRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	// Set UDM to NOT_REGISTERED
	nrfMock.SetNFStatus("udm-001", "NOT_REGISTERED")

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	instances, _ := result["nfInstances"].([]interface{})
	assert.Empty(t, instances, "should not return unregistered NF")
}

// TestNRF_AllRegistered verifies all registered NFs are returned when no filter.
// Spec: TS 29.510 §6.2.6
func TestNRF_AllRegistered(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	ctx := context.Background()
	nrfMock := mocks.NewNRFMock()
	defer nrfMock.Close()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		nrfMock.URL()+"/nnrf-disc/v1/nf-instances",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	instances, ok := result["nfInstances"].([]interface{})
	require.True(t, ok, "nfInstances must be an array")
	assert.NotEmpty(t, instances, "should return registered NFs")
}
