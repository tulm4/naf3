//go:build e2e
// +build e2e

package scenarios

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/operator/nssAAF/test/mocks"
)

// TestUDM_AuthSubscription verifies UDM returns auth subscription.
// Spec: TS 29.526 §7.2.2
func TestUDM_AuthSubscription(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	udmMock := mocks.NewUDMMock()
	defer udmMock.Close()

	// Set auth subscription
	udmMock.SetAuthSubscription("imsi-208046000000001", "EAP_TLS", "radius://mock-aaa-s:1812")

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet,
		udmMock.URL()+"/nudm-uem/v1/subscribers/imsi-208046000000001/auth-contexts",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "EAP_TLS")
	assert.Contains(t, string(body), "radius://")
}

// TestUDM_SubscriberNotFound verifies 404 for unknown SUPI.
// Spec: TS 29.526 §7.2.2
func TestUDM_SubscriberNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	udmMock := mocks.NewUDMMock()
	defer udmMock.Close()

	// Do NOT set auth subscription for this SUPI

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet,
		udmMock.URL()+"/nudm-uem/v1/subscribers/imsi-999999999999999/auth-contexts",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestUDM_ErrorInjection verifies error response when configured.
// Spec: TS 29.526 §7.2.2
func TestUDM_ErrorInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	udmMock := mocks.NewUDMMock()
	defer udmMock.Close()

	// Configure error for SUPI
	udmMock.SetError("imsi-208046000000001", http.StatusGatewayTimeout)

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet,
		udmMock.URL()+"/nudm-uem/v1/subscribers/imsi-208046000000001/auth-contexts",
		nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusGatewayTimeout, resp.StatusCode)
}
