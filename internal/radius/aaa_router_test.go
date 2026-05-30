// Package radius provides RADIUS client for AAA protocol interworking.
package radius

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/eap"
)

func TestRadiusAAARouter_RoutingContext_HashesGPSI(t *testing.T) {
	client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1", ServerPort: 1812}, nil)
	require.NoError(t, err)
	router := NewRadiusAAARouter(client)

	routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com"})
	// SHA-256("alice@example.com")[:16] hex
	expected := "ff8d9819fc0e12bf0d24892e45987e24"
	assert.Equal(t, expected, routing.GPSI)
}

func TestRadiusAAARouter_RoutingContext_DecodesSnssai_SstOnly(t *testing.T) {
	client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1"}, nil)
	require.NoError(t, err)
	router := NewRadiusAAARouter(client)

	routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", SnssaiKey: "1"})
	assert.Equal(t, uint8(1), routing.Sst)
	assert.Equal(t, "", routing.Sd)
}

func TestRadiusAAARouter_RoutingContext_DecodesSnssai_SstAndSd(t *testing.T) {
	client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1"}, nil)
	require.NoError(t, err)
	router := NewRadiusAAARouter(client)

	routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", SnssaiKey: "1-000001"})
	assert.Equal(t, uint8(1), routing.Sst)
	assert.Equal(t, "000001", routing.Sd)
}

func TestRadiusAAARouter_RoutingContext_AuthCtxID(t *testing.T) {
	client, err := NewRadiusClient(Config{ServerAddress: "127.0.0.1"}, nil)
	require.NoError(t, err)
	router := NewRadiusAAARouter(client)

	routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", AuthCtxID: "auth-123"})
	assert.Equal(t, "auth-123", routing.AuthCtxID)
}
