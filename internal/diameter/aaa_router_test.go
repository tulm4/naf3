// Package diameter provides Diameter client for AAA protocol interworking.
package diameter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/eap"
)

func TestDiameterAAARouter_RoutingContext_UnhashedGPSI(t *testing.T) {
	client, err := NewClient(Config{OriginHost: "nssAAF", OriginRealm: "test"}, nil)
	require.NoError(t, err)
	router := NewDiameterAAARouter(client)

	routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com"})
	// Diameter sends unhashed GPSI
	assert.Equal(t, "alice@example.com", routing.GPSI)
}

func TestDiameterAAARouter_RoutingContext_DecodesSnssai(t *testing.T) {
	client, err := NewClient(Config{OriginHost: "nssAAF", OriginRealm: "test"}, nil)
	require.NoError(t, err)
	router := NewDiameterAAARouter(client)

	routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", SnssaiKey: "2-abc123"})
	assert.Equal(t, uint8(2), routing.Sst)
	assert.Equal(t, "abc123", routing.Sd)
}

func TestDiameterAAARouter_RoutingContext_AuthCtxID(t *testing.T) {
	client, err := NewClient(Config{OriginHost: "nssAAF", OriginRealm: "test"}, nil)
	require.NoError(t, err)
	router := NewDiameterAAARouter(client)

	routing := router.RoutingContext(&eap.Session{Gpsi: "alice@example.com", AuthCtxID: "auth-456"})
	assert.Equal(t, "auth-456", routing.AuthCtxID)
}
