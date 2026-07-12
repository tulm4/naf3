// Package config provides configuration loading and management for nssAAF.
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Create a minimal config file
	content := `
server:
  addr: ":8080"
database:
  host: "localhost"
  port: 5432
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	tmp, err := os.CreateTemp("", "config-*.yaml")
	require.NoError(t, err)
	defer func() { _ = os.Remove(tmp.Name()) }()

	_, err = tmp.WriteString(content)
	require.NoError(t, err)
	_ = tmp.Close()

	cfg, err := Load(tmp.Name())
	require.NoError(t, err)

	// Defaults applied
	assert.Equal(t, 10*time.Second, cfg.Server.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.Server.WriteTimeout)
	assert.Equal(t, 120*time.Second, cfg.Server.IdleTimeout)
	assert.Equal(t, 20, cfg.EAP.MaxRounds)
	assert.Equal(t, 30*time.Second, cfg.EAP.RoundTimeout)
	assert.Equal(t, 5*time.Minute, cfg.EAP.SessionTTL)
	assert.Equal(t, 10*time.Second, cfg.AAA.ResponseTimeout)
	assert.Equal(t, 3, cfg.AAA.MaxRetries)
	assert.Equal(t, 5, cfg.AAA.FailureThreshold)
	assert.Equal(t, 30*time.Second, cfg.AAA.RecoveryTimeout)
	assert.Equal(t, "/metrics", cfg.Metrics.Path)
	assert.Equal(t, 50, cfg.Redis.PoolSize)
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	assert.Error(t, err)
}

func TestExpandEnv(t *testing.T) {
	_ = os.Setenv("TEST_VAR", "test-value")
	defer func() { _ = os.Unsetenv("TEST_VAR") }()

	result := expandEnv("key=${TEST_VAR}")
	assert.Equal(t, "key=test-value", result)
}

func TestAAAgwConfig_DiameterTransport_DefaultsToTCP(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "aaa-gateway.yaml")
	yaml := `
component: aaa-gateway
version: "0.1.0"
server:
  addr: ":9090"
redis:
  addr: "localhost:6379"
aaaGateway:
  bizServiceUrl: "http://localhost:8080"
crypto:
  keyManager: "soft"
  masterKeyHex: "0102030405060708091011121314151617181920212223242526272829303132"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AAAgw == nil {
		t.Fatal("AAAgw is nil after Load")
	}
	if cfg.AAAgw.DiameterTransport != "tcp" {
		t.Errorf("default DiameterTransport = %q; want %q", cfg.AAAgw.DiameterTransport, "tcp")
	}
}

// TestDebugConfig_DefaultsOff verifies the per-UE debug subsystem configuration
// is parsed and surfaced on cfg.Debug. When no debug section is present, the
// zero value (Enabled=false) is expected.
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §6
func TestDebugConfig_DefaultsOff(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "biz.yaml")
	yamlData := `
component: biz
version: "0.1.0"
biz:
  aaaGatewayUrl: "http://localhost:9090"
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	if err := os.WriteFile(cfgPath, []byte(yamlData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Debug.Enabled {
		t.Fatal("expected Debug.Enabled=false by default when section is absent")
	}
	if cfg.Debug.RedisAddr != "" {
		t.Fatalf("expected empty Debug.RedisAddr when section is absent, got %q", cfg.Debug.RedisAddr)
	}
}

// TestDebugConfig_EnabledAndRedisAddr verifies the per-UE debug subsystem
// configuration parses the enabled flag and redis address fields.
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §6
func TestDebugConfig_EnabledAndRedisAddr(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "biz.yaml")
	yamlData := `
component: biz
version: "0.1.0"
biz:
  aaaGatewayUrl: "http://localhost:9090"
debug:
  enabled: true
  redisAddr: "127.0.0.1:6379"
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	if err := os.WriteFile(cfgPath, []byte(yamlData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Debug.Enabled {
		t.Fatal("expected Debug.Enabled=true")
	}
	if cfg.Debug.RedisAddr != "127.0.0.1:6379" {
		t.Fatalf("unexpected redis addr: %s", cfg.Debug.RedisAddr)
	}
}
