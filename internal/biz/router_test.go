package biz

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/proto"
)

// TestBuildForwardRequest_RADIUS verifies that S-NSSAI with RADIUS protocol
// creates an AaaForwardRequest with TransportType=RADIUS.
func TestBuildForwardRequest_RADIUS(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "aaa-server.operator.com",
			Port:     1812,
		},
	}, tLogger())

	req, err := r.BuildForwardRequest("auth-ctx-001", []byte{1, 2, 3}, 1, "01A2B3")

	require.NoError(t, err)
	require.NotNil(t, req)

	assert.Equal(t, proto.TransportRADIUS, req.TransportType)
	assert.Equal(t, proto.DirectionClientInitiated, req.Direction)
	assert.Equal(t, "auth-ctx-001", req.AuthCtxID)
	assert.Equal(t, uint8(1), req.Sst)
	assert.Equal(t, "01A2B3", req.Sd)
	assert.Equal(t, []byte{1, 2, 3}, req.Payload)
	assert.NotEmpty(t, req.SessionID)
	assert.Contains(t, req.SessionID, "nssAAF;")
}

// TestBuildForwardRequest_DIAMETER verifies that S-NSSAI with DIAMETER protocol
// creates an AaaForwardRequest with TransportType=DIAMETER.
func TestBuildForwardRequest_DIAMETER(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolDIAMETER,
			Host:     "aaa-server.operator.com",
			Port:     3868,
		},
	}, tLogger())

	req, err := r.BuildForwardRequest("auth-ctx-002", []byte{4, 5, 6}, 2, "")

	require.NoError(t, err)
	require.NotNil(t, req)

	assert.Equal(t, proto.TransportDIAMETER, req.TransportType)
	assert.Equal(t, proto.DirectionClientInitiated, req.Direction)
	assert.Equal(t, "auth-ctx-002", req.AuthCtxID)
	assert.Equal(t, uint8(2), req.Sst)
	assert.Equal(t, "", req.Sd)
}

// TestBuildForwardRequest_NoRouteConfigured verifies that when no AAA server
// is configured for the given S-NSSAI, BuildForwardRequest returns an error.
func TestBuildForwardRequest_NoRouteConfigured(t *testing.T) {
	r := NewRouter(SnssaiConfig{}, tLogger())

	req, err := r.BuildForwardRequest("auth-ctx-003", []byte{7, 8, 9}, 255, "FFFFFF")

	assert.Error(t, err)
	assert.Nil(t, req)
	assert.Contains(t, err.Error(), "no route configured")
}

// TestBuildForwardRequest_3LevelLookup_exactMatch takes precedence over SST-only.
func TestBuildForwardRequest_3LevelLookup_exactMatch(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Exact: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "exact-host.operator.com",
			Port:     1812,
		},
		SST: &ServerConfig{
			Protocol: ProtocolDIAMETER,
			Host:     "sst-host.operator.com",
			Port:     3868,
		},
	}, tLogger())

	req, err := r.BuildForwardRequest("auth-ctx-004", []byte{1}, 1, "01A2B3")

	require.NoError(t, err)
	assert.Equal(t, proto.TransportRADIUS, req.TransportType)
}

// TestBuildForwardRequest_3LevelLookup_sstOnly takes precedence over default.
func TestBuildForwardRequest_3LevelLookup_sstOnly(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		SST: &ServerConfig{
			Protocol: ProtocolDIAMETER,
			Host:     "sst-host.operator.com",
			Port:     3868,
		},
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "default-host.operator.com",
			Port:     1812,
		},
	}, tLogger())

	req, err := r.BuildForwardRequest("auth-ctx-005", []byte{2}, 3, "FFFFFF")

	require.NoError(t, err)
	assert.Equal(t, proto.TransportDIAMETER, req.TransportType)
}

// TestResolveRoute_NoConfig returns nil RouteDecision.
func TestResolveRoute_NoConfig(t *testing.T) {
	r := NewRouter(SnssaiConfig{}, tLogger())

	decision := r.ResolveRoute(1, "01A2B3")

	assert.Nil(t, decision)
}

// TestResolveRoute_DefaultConfig returns the default routing decision.
func TestResolveRoute_DefaultConfig(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "default.operator.com",
			Port:     1812,
			Timeout:  5 * time.Second,
		},
	}, tLogger())

	decision := r.ResolveRoute(99, "FFFFFF")

	require.NotNil(t, decision)
	assert.Equal(t, ProxyModeDirect, decision.Mode)
	assert.Equal(t, ProtocolRADIUS, decision.Protocol)
	assert.Equal(t, "default.operator.com", decision.Host)
	assert.Equal(t, 1812, decision.Port)
	assert.Equal(t, 5*time.Second, decision.Timeout)
}

// TestResolveRoute_DefaultTimeout applies 10-second default when Timeout=0.
func TestResolveRoute_DefaultTimeout(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "default.operator.com",
			Port:     1812,
		},
	}, tLogger())

	decision := r.ResolveRoute(1, "")

	require.NotNil(t, decision)
	assert.Equal(t, 10*time.Second, decision.Timeout)
}

// TestStats_AlwaysFalse verifies that Biz Pod Stats() always returns false
// for HasRadius and HasDiameter (Biz Pod has no direct AAA clients).
func TestStats_AlwaysFalse(t *testing.T) {
	r := NewRouter(SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "aaa-server.operator.com",
			Port:     1812,
		},
	}, tLogger())

	stats := r.Stats()

	assert.False(t, stats.HasRadius)
	assert.False(t, stats.HasDiameter)
}

// tLogger returns a no-op logger for tests.
func tLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
