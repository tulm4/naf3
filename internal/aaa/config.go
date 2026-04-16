// Package aaa provides AAA proxy (AAA-P) functionality for routing between
// NSSAAF and NSS-AAA servers over RADIUS or Diameter.
// Spec: TS 29.561 §16-17
package aaa

import (
	"fmt"
	"time"
)

// Default port numbers.
const (
	DefaultRadiusPort    = 1812
	DefaultDiameterPort = 3868
)

// Config holds AAA proxy configuration.
type Config struct {
	// Default server configuration (used when no S-NSSAI-specific config).
	Default *ServerConfig

	// Per-S-NSSAI configuration.
	// Key format: "{sst}:{sd}" where sd is uppercase hex or "*".
	Snssai map[string]*ServerConfig

	// Routing mode.
	RoutingMode RoutingMode
}

// RoutingMode represents the overall routing mode.
type RoutingMode int

const (
	RoutingModeDirect RoutingMode = iota
	RoutingModeProxy
)

// String implements fmt.Stringer.
func (m RoutingMode) String() string {
	switch m {
	case RoutingModeDirect:
		return "DIRECT"
	case RoutingModeProxy:
		return "PROXY"
	default:
		return fmt.Sprintf("RoutingMode(%d)", m)
	}
}

// NewConfig creates a Config with default values.
func NewConfig() Config {
	return Config{
		Snssai:      make(map[string]*ServerConfig),
		RoutingMode: RoutingModeDirect,
	}
}

// AddServer adds an AAA server configuration for a specific S-NSSAI.
func (c *Config) AddServer(sst uint8, sd, host string, port int, protocol Protocol) {
	key := fmt.Sprintf("%d:%s", sst, sd)
	c.Snssai[key] = &ServerConfig{
		Protocol: protocol,
		Host:    host,
		Port:    port,
		Timeout: 10 * time.Second,
	}
}

// Lookup looks up AAA server configuration for an S-NSSAI.
// Uses 3-level fallback: exact → sst-only → default.
func (c *Config) Lookup(sst uint8, sd string) *ServerConfig {
	// Try exact match.
	key := fmt.Sprintf("%d:%s", sst, sd)
	if cfg, ok := c.Snssai[key]; ok {
		return cfg
	}

	// Try sst-only match.
	keySST := fmt.Sprintf("%d:*", sst)
	if cfg, ok := c.Snssai[keySST]; ok {
		return cfg
	}

	// Return default.
	return c.Default
}

// DefaultServerConfig returns a reasonable default server config.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Protocol: ProtocolRADIUS,
		Port:    DefaultRadiusPort,
		Timeout: 10 * time.Second,
	}
}
