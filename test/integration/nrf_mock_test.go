// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/nrf"
	"github.com/operator/nssAAF/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test: NRF_Discovery ─────────────────────────────────────────────────────

func TestIntegration_NRF_Discovery(t *testing.T) {
	mock := mocks.NewNRFMock()
	defer mock.Close()

	client := nrf.NewClient(config.NRFConfig{
		BaseURL:         mock.URL(),
		DiscoverTimeout: 5 * time.Second,
	}, nfclient.NewFactory(nil))

	ctx := context.Background()
	endpoint, err := client.DiscoverUDM(ctx, "00101")
	require.NoError(t, err, "NRF discovery should succeed")
	assert.NotEmpty(t, endpoint, "UDM endpoint should be discovered")

	_ = client.IsRegistered()
}

// ─── Test: NRF_Registration ────────────────────────────────────────────────

func TestIntegration_NRF_Registration(t *testing.T) {
	mock := mocks.NewNRFMock()
	defer mock.Close()

	client := nrf.NewClient(config.NRFConfig{
		BaseURL:         mock.URL(),
		DiscoverTimeout: 5 * time.Second,
	}, nfclient.NewFactory(nil))

	ctx := context.Background()
	_, _, err := client.Register(ctx, nil)
	require.NoError(t, err, "NRF registration should succeed")
	assert.True(t, client.IsRegistered(), "client should be registered after Register")
}

// ─── Test: NRF_Heartbeat ─────────────────────────────────────────────────

func TestIntegration_NRF_Heartbeat(t *testing.T) {
	mock := mocks.NewNRFMock()
	defer mock.Close()

	client := nrf.NewClient(config.NRFConfig{
		BaseURL:         mock.URL(),
		DiscoverTimeout: 5 * time.Second,
	}, nfclient.NewFactory(nil))

	ctx := context.Background()
	_, _, _ = client.Register(ctx, nil)

	_, err := client.Heartbeat(ctx, client.NFInstanceID(), "test-etag")
	require.NoError(t, err, "NRF heartbeat should succeed")
}

// ─── Test: NRF_ServiceDiscovery ────────────────────────────────────────────

func TestIntegration_NRF_ServiceDiscovery(t *testing.T) {
	mock := mocks.NewNRFMock()
	defer mock.Close()

	client := nrf.NewClient(config.NRFConfig{
		BaseURL:         mock.URL(),
		DiscoverTimeout: 5 * time.Second,
	}, nfclient.NewFactory(nil))

	ctx := context.Background()

	endpoint, err := client.DiscoverUDM(ctx, "00101")
	require.NoError(t, err)
	assert.NotEmpty(t, endpoint, "service discovery should return UDM endpoint")

	_, err = client.DiscoverAMF(ctx, "amf-001")
	require.NoError(t, err, "AMF discovery should succeed")
}
