package config

import (
	"os"
	"testing"
	"time"
)

func TestNRFConfigExtended(t *testing.T) {
	// Configure master key so crypto.Validate does not reject the test load.
	// (NRF config extension has no crypto dependency of its own.)
	t.Setenv("MASTER_KEY_HEX", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	content := `
nrf:
  baseURL: "https://nrf.operator.com"
  discoverTimeout: 10s
  cacheTtl: 5m
  instanceId: "550e8400-e29b-41d4-a716-446655440000"
  profilePath: "/etc/nssAAF/nf-profile.yaml"
  accessToken:
    enabled: true
    authServer: "https://nrf.operator.com/oauth2/token"
    clientId: "nssAAF-client"
    clientSecret: "${NRF_CLIENT_SECRET}"
    scope: "nnrf-nfm"
  heartbeat:
    initialIntervalSeconds: 300
    acceptNegotiatedInterval: true
    maxConsecutiveFailures: 3
  discoveryCache:
    enabled: true
    defaultTTLSeconds: 3600
`
	tmp, err := os.CreateTemp("", "nrf-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	cfg, err := Load(tmp.Name())
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	// Check extended NRFConfig fields
	if cfg.NRF.InstanceID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("InstanceID mismatch: got %s", cfg.NRF.InstanceID)
	}

	if cfg.NRF.ProfilePath != "/etc/nssAAF/nf-profile.yaml" {
		t.Errorf("ProfilePath mismatch: got %s", cfg.NRF.ProfilePath)
	}

	if !cfg.NRF.AccessToken.Enabled {
		t.Errorf("AccessToken.Enabled should be true")
	}

	if cfg.NRF.AccessToken.AuthServer != "https://nrf.operator.com/oauth2/token" {
		t.Errorf("AuthServer mismatch")
	}

	if cfg.NRF.Heartbeat.MaxConsecutiveFailures != 3 {
		t.Errorf("MaxConsecutiveFailures mismatch: got %d", cfg.NRF.Heartbeat.MaxConsecutiveFailures)
	}

	if cfg.NRF.Heartbeat.InitialInterval != 5*time.Minute {
		t.Errorf("InitialInterval mismatch: got %v", cfg.NRF.Heartbeat.InitialInterval)
	}
}
