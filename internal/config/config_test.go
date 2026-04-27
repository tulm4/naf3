// Package config provides configuration loading and management for nssAAF.
package config

import (
	"os"
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
	defer os.Remove(tmp.Name())

	_, err = tmp.WriteString(content)
	require.NoError(t, err)
	tmp.Close()

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
	os.Setenv("TEST_VAR", "test-value")
	defer os.Unsetenv("TEST_VAR")

	result := expandEnv("key=${TEST_VAR}")
	assert.Equal(t, "key=test-value", result)
}
