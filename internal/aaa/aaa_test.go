// Package aaa provides AAA proxy (AAA-P) functionality for routing between
// NSSAAF and NSS-AAA servers over RADIUS or Diameter.
// Spec: TS 29.561 §16-17
package aaa

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

func TestNewConfig(t *testing.T) {
	cfg := NewConfig()
	assert.NotNil(t, cfg.Snssai)
	assert.Equal(t, RoutingModeDirect, cfg.RoutingMode)
}

func TestConfigAddServer(t *testing.T) {
	cfg := NewConfig()

	cfg.AddServer(1, "ABCDEF", "aaa1.example.com", 1812, ProtocolRADIUS)
	cfg.AddServer(2, "*", "aaa2.example.com", 1812, ProtocolDIAMETER)

	// Exact match.
	server := cfg.Lookup(1, "ABCDEF")
	require.NotNil(t, server)
	assert.Equal(t, "aaa1.example.com", server.Host)
	assert.Equal(t, ProtocolRADIUS, server.Protocol)

	// SST-only match.
	server = cfg.Lookup(2, "XYZ999")
	require.NotNil(t, server)
	assert.Equal(t, "aaa2.example.com", server.Host)
	assert.Equal(t, ProtocolDIAMETER, server.Protocol)

	// No match → default (nil).
	cfg.Default = nil
	server = cfg.Lookup(99, "999999")
	assert.Nil(t, server)
}

func TestConfigLookupWithDefault(t *testing.T) {
	cfg := NewConfig()
	cfg.Default = &ServerConfig{
		Host:     "default.aaa.com",
		Port:     1812,
		Protocol: ProtocolRADIUS,
	}

	// No S-NSSAI config → default.
	server := cfg.Lookup(99, "nopatch")
	require.NotNil(t, server)
	assert.Equal(t, "default.aaa.com", server.Host)
}

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, ProtocolRADIUS, cfg.Protocol)
	assert.Equal(t, DefaultRadiusPort, cfg.Port)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
}

// ---------------------------------------------------------------------------
// RoutingMode / Protocol
// ---------------------------------------------------------------------------

func TestProxyModeString(t *testing.T) {
	assert.Equal(t, "DIRECT", ProxyModeDirect.String())
	assert.Equal(t, "PROXY", ProxyModeProxy.String())
	assert.Contains(t, ProxyMode(99).String(), "ProxyMode")
}

func TestProtocolString(t *testing.T) {
	assert.Equal(t, "RADIUS", ProtocolRADIUS.String())
	assert.Equal(t, "DIAMETER", ProtocolDIAMETER.String())
	assert.Contains(t, Protocol(99).String(), "Protocol")
}

func TestRoutingModeString(t *testing.T) {
	assert.Equal(t, "DIRECT", RoutingModeDirect.String())
	assert.Equal(t, "PROXY", RoutingModeProxy.String())
	assert.Contains(t, RoutingMode(99).String(), "RoutingMode")
}

// ---------------------------------------------------------------------------
// RouteDecision
// ---------------------------------------------------------------------------

func TestRouteDecision(t *testing.T) {
	rd := &RouteDecision{
		Mode:     ProxyModeDirect,
		Protocol: ProtocolRADIUS,
		Host:     "192.168.1.100",
		Port:     1812,
		Timeout:  15 * time.Second,
	}

	assert.Equal(t, ProxyModeDirect, rd.Mode)
	assert.Equal(t, ProtocolRADIUS, rd.Protocol)
	assert.Equal(t, "192.168.1.100", rd.Host)
	assert.Equal(t, 1812, rd.Port)
	assert.Equal(t, 15*time.Second, rd.Timeout)
}

// ---------------------------------------------------------------------------
// Router Construction
// ---------------------------------------------------------------------------

func TestNewRouter(t *testing.T) {
	cfg := SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "aaa.example.com",
			Port:     1812,
		},
	}

	logger := slog.Default()
	router := NewRouter(cfg, logger)

	assert.NotNil(t, router)
	assert.Equal(t, cfg, router.cfg)
}

func TestNewRouterWithOptions(t *testing.T) {
	cfg := SnssaiConfig{}
	logger := slog.Default()

	router := NewRouter(cfg, logger,
		WithMetrics(NewMetrics()),
	)

	assert.NotNil(t, router)
	assert.NotNil(t, router.metrics)
}

// ---------------------------------------------------------------------------
// ResolveRoute
// ---------------------------------------------------------------------------

func TestResolveRouteExactMatch(t *testing.T) {
	cfg := SnssaiConfig{
		Exact: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "exact.aaa.com",
			Port:     1812,
			Timeout:  20 * time.Second,
		},
	}

	router := NewRouter(cfg, slog.Default())
	decision := router.ResolveRoute(1, "ABCDEF")

	require.NotNil(t, decision)
	assert.Equal(t, ProtocolRADIUS, decision.Protocol)
	assert.Equal(t, "exact.aaa.com", decision.Host)
	assert.Equal(t, 1812, decision.Port)
	assert.Equal(t, 20*time.Second, decision.Timeout)
}

func TestResolveRouteSSTOnly(t *testing.T) {
	cfg := SnssaiConfig{
		SST: &ServerConfig{
			Protocol: ProtocolDIAMETER,
			Host:     "sst.aaa.com",
			Port:     3868,
		},
	}

	router := NewRouter(cfg, slog.Default())
	decision := router.ResolveRoute(5, "XYZ789")

	require.NotNil(t, decision)
	assert.Equal(t, ProtocolDIAMETER, decision.Protocol)
	assert.Equal(t, "sst.aaa.com", decision.Host)
}

func TestResolveRouteDefault(t *testing.T) {
	cfg := SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "default.aaa.com",
			Port:     1812,
			Timeout:  5 * time.Second,
		},
	}

	router := NewRouter(cfg, slog.Default())
	decision := router.ResolveRoute(99, "nopatch")

	require.NotNil(t, decision)
	assert.Equal(t, "default.aaa.com", decision.Host)
	assert.Equal(t, 5*time.Second, decision.Timeout)
}

func TestResolveRouteNoConfig(t *testing.T) {
	cfg := SnssaiConfig{}
	router := NewRouter(cfg, slog.Default())

	decision := router.ResolveRoute(1, "ABCDEF")
	assert.Nil(t, decision)
}

func TestResolveRoutePriority(t *testing.T) {
	// The implementation returns the first non-nil config field in priority order:
	// Exact → SST → Default. It does NOT perform S-NSSAI key matching.

	// Case 1: Only Default configured → Default is returned.
	cfg1 := SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "default.aaa.com",
			Port:     1812,
		},
	}
	router1 := NewRouter(cfg1, slog.Default())
	decision := router1.ResolveRoute(99, "nopatch")
	require.NotNil(t, decision)
	assert.Equal(t, "default.aaa.com", decision.Host)

	// Case 2: Only SST configured → SST is returned (Exact is nil, so SST is first).
	cfg2 := SnssaiConfig{
		SST: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "sst.aaa.com",
			Port:     1812,
		},
	}
	router2 := NewRouter(cfg2, slog.Default())
	decision = router2.ResolveRoute(5, "XYZ789")
	require.NotNil(t, decision)
	assert.Equal(t, "sst.aaa.com", decision.Host)

	// Case 3: Exact is set → Exact always wins regardless of S-NSSAI.
	cfg3 := SnssaiConfig{
		Exact: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "exact.aaa.com",
			Port:     1812,
		},
		SST: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "sst.aaa.com",
			Port:     1812,
		},
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "default.aaa.com",
			Port:     1812,
		},
	}
	router3 := NewRouter(cfg3, slog.Default())
	decision = router3.ResolveRoute(1, "ABCDEF")
	require.NotNil(t, decision)
	assert.Equal(t, "exact.aaa.com", decision.Host) // Exact always wins
}

func TestResolveRouteTimeoutDefault(t *testing.T) {
	cfg := SnssaiConfig{
		Default: &ServerConfig{
			Protocol: ProtocolRADIUS,
			Host:     "aaa.com",
			Port:     1812,
			Timeout:  0, // zero → should default to 10s
		},
	}

	router := NewRouter(cfg, slog.Default())
	decision := router.ResolveRoute(1, "ABCDEF")

	require.NotNil(t, decision)
	assert.Equal(t, 10*time.Second, decision.Timeout)
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

func TestRouterStats(t *testing.T) {
	cfg := SnssaiConfig{
		Default: &ServerConfig{Protocol: ProtocolRADIUS, Host: "aaa.com", Port: 1812},
	}
	router := NewRouter(cfg, slog.Default())

	stats := router.Stats()
	assert.False(t, stats.HasRadius)
	assert.False(t, stats.HasDiameter)

	// Set clients (nil is acceptable for Stats test).
	router.SetRadiusClient(nil)
	stats = router.Stats()
	assert.False(t, stats.HasRadius) // nil → false

	router.SetDiameterClient(nil)
	stats = router.Stats()
	assert.False(t, stats.HasDiameter) // nil → false
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

func TestNewMetrics(t *testing.T) {
	m := NewMetrics()
	assert.NotNil(t, m.requests)
	assert.NotNil(t, m.latencies)
	assert.NotNil(t, m.counters)
}

func TestMetricsRecordRequest(t *testing.T) {
	m := NewMetrics()

	m.RecordAAARequest("RADIUS", "aaa1.com", "success")
	m.RecordAAARequest("RADIUS", "aaa1.com", "success")
	m.RecordAAARequest("RADIUS", "aaa1.com", "failure")
	m.RecordAAARequest("DIAMETER", "aaa2.com", "success")

	stats := m.Stats()
	assert.Equal(t, int64(2), stats.Requests["RADIUS"]["aaa1.com"]["success"])
	assert.Equal(t, int64(1), stats.Requests["RADIUS"]["aaa1.com"]["failure"])
	assert.Equal(t, int64(1), stats.Requests["DIAMETER"]["aaa2.com"]["success"])
}

func TestMetricsRecordLatency(t *testing.T) {
	m := NewMetrics()

	m.RecordAAALatency("RADIUS", "aaa.com", 100*time.Millisecond)
	m.RecordAAALatency("RADIUS", "aaa.com", 200*time.Millisecond)

	avg := m.AverageLatency("RADIUS", "aaa.com")
	assert.Equal(t, 150.0, avg) // 300ms total / 2 = 150ms
}

func TestMetricsAverageLatencyNoData(t *testing.T) {
	m := NewMetrics()

	assert.Equal(t, 0.0, m.AverageLatency("RADIUS", "nonexistent.com"))
	assert.Equal(t, 0.0, m.AverageLatency("DIAMETER", "nonexistent.com"))
}

func TestMetricsRequestRate(t *testing.T) {
	m := NewMetrics()

	// RecordAAARequest updates the requests map.
	m.RecordAAARequest("RADIUS", "aaa.com", "success")
	m.RecordAAARequest("RADIUS", "aaa.com", "failure")
	m.RecordAAARequest("DIAMETER", "aaa.com", "success")

	// RequestRate uses m.counters, which is only updated by RecordAAALatency.
	// So we record latency too (which also increments the counter).
	m.RecordAAALatency("RADIUS", "aaa.com", 10*time.Millisecond)
	m.RecordAAALatency("RADIUS", "aaa.com", 20*time.Millisecond)

	rate := m.RequestRate("RADIUS", "aaa.com")
	assert.Equal(t, float64(2), rate) // 2 latency recordings = 2 requests

	assert.Equal(t, float64(0), m.RequestRate("DIAMETER", "aaa.com"))
	assert.Equal(t, float64(0), m.RequestRate("RADIUS", "unknown.com"))
}

func TestMetricsStats(t *testing.T) {
	m := NewMetrics()
	m.RecordAAARequest("RADIUS", "aaa.com", "success")

	stats := m.Stats()
	_, ok := stats.Requests["RADIUS"]
	assert.True(t, ok)
}

// ---------------------------------------------------------------------------
// Default Ports
// ---------------------------------------------------------------------------

func TestDefaultPorts(t *testing.T) {
	assert.Equal(t, 1812, DefaultRadiusPort)
	assert.Equal(t, 3868, DefaultDiameterPort)
}
