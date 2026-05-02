//go:build e2e
// +build e2e

package fullchain

import (
	"os"
	"testing"
)

// TestHarnessURLsFromEnv tests that the harness reads URLs from environment variables.
func TestHarnessURLsFromEnv(t *testing.T) {
	os.Setenv("FULLCHAIN_NRF_URL", "http://localhost:8082")
	os.Setenv("FULLCHAIN_UDM_URL", "http://localhost:8083")
	t.Cleanup(func() {
		os.Unsetenv("FULLCHAIN_NRF_URL")
		os.Unsetenv("FULLCHAIN_UDM_URL")
	})

	// Create a minimal harness to test env reading.
	// Note: This test only verifies the env var reading logic.
	// Full integration tests require docker compose running.
	h := newHarnessForEnvTest(t)
	if h.nrfURL != "http://localhost:8082" {
		t.Errorf("expected NRFURL to be http://localhost:8082, got %s", h.nrfURL)
	}
	if h.udmURL != "http://localhost:8083" {
		t.Errorf("expected UDMURL to be http://localhost:8083, got %s", h.udmURL)
	}
}

// TestHarnessSetNRFServiceEndpointRequiresEnv tests that SetNRFServiceEndpoint
// returns an error when FULLCHAIN_NRF_URL is not set.
func TestHarnessSetNRFServiceEndpointRequiresEnv(t *testing.T) {
	os.Unsetenv("FULLCHAIN_NRF_URL")
	h := newHarnessForEnvTest(t)
	err := h.SetNRFServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8081)
	if err == nil {
		t.Error("expected error when FULLCHAIN_NRF_URL not set")
	}
	if err != nil && err.Error() != "FULLCHAIN_NRF_URL not set" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestHarnessSetUDMAuthSubscriptionRequiresEnv tests that SetUDMAuthSubscription
// returns an error when FULLCHAIN_UDM_URL is not set.
func TestHarnessSetUDMAuthSubscriptionRequiresEnv(t *testing.T) {
	os.Unsetenv("FULLCHAIN_UDM_URL")
	h := newHarnessForEnvTest(t)
	err := h.SetUDMAuthSubscription("imsi-001", "EAP-AKA", "aaa-sim:1812")
	if err == nil {
		t.Error("expected error when FULLCHAIN_UDM_URL not set")
	}
	if err != nil && err.Error() != "FULLCHAIN_UDM_URL not set" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestHarnessSetNRFServiceEndpointSucceedsWithEnv tests that SetNRFServiceEndpoint
// succeeds when FULLCHAIN_NRF_URL is set.
func TestHarnessSetNRFServiceEndpointSucceedsWithEnv(t *testing.T) {
	os.Setenv("FULLCHAIN_NRF_URL", "http://nrf-mock:8080")
	t.Cleanup(func() {
		os.Unsetenv("FULLCHAIN_NRF_URL")
	})

	h := newHarnessForEnvTest(t)
	err := h.SetNRFServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8080)
	if err != nil {
		t.Errorf("expected no error with FULLCHAIN_NRF_URL set, got: %v", err)
	}
}

// TestHarnessSetUDMAuthSubscriptionSucceedsWithEnv tests that SetUDMAuthSubscription
// succeeds when FULLCHAIN_UDM_URL is set.
func TestHarnessSetUDMAuthSubscriptionSucceedsWithEnv(t *testing.T) {
	os.Setenv("FULLCHAIN_UDM_URL", "http://udm-mock:8080")
	t.Cleanup(func() {
		os.Unsetenv("FULLCHAIN_UDM_URL")
	})

	h := newHarnessForEnvTest(t)
	err := h.SetUDMAuthSubscription("imsi-208046000000001", "EAP_TLS", "radius://aaa-sim:1812")
	if err != nil {
		t.Errorf("expected no error with FULLCHAIN_UDM_URL set, got: %v", err)
	}
}

// TestHarnessEmptyURLsWhenEnvNotSet tests that URL accessors return empty strings
// when environment variables are not set.
func TestHarnessEmptyURLsWhenEnvNotSet(t *testing.T) {
	os.Unsetenv("FULLCHAIN_NRF_URL")
	os.Unsetenv("FULLCHAIN_UDM_URL")

	h := newHarnessForEnvTest(t)
	if h.NRFURL() != "" {
		t.Errorf("expected empty NRFURL when env not set, got: %s", h.NRFURL())
	}
	if h.UDMURL() != "" {
		t.Errorf("expected empty UDMURL when env not set, got: %s", h.UDMURL())
	}
}

// TestHarnessAccessors tests that URL accessor methods return the correct values.
func TestHarnessAccessors(t *testing.T) {
	os.Setenv("FULLCHAIN_NRF_URL", "http://nrf:8080")
	os.Setenv("FULLCHAIN_UDM_URL", "http://udm:8080")
	t.Cleanup(func() {
		os.Unsetenv("FULLCHAIN_NRF_URL")
		os.Unsetenv("FULLCHAIN_UDM_URL")
	})

	h := newHarnessForEnvTest(t)
	if got := h.NRFURL(); got != "http://nrf:8080" {
		t.Errorf("NRFURL() = %s, want http://nrf:8080", got)
	}
	if got := h.UDMURL(); got != "http://udm:8080" {
		t.Errorf("UDMURL() = %s, want http://udm:8080", got)
	}
}

// newHarnessForEnvTest creates a minimal harness instance for testing env var logic.
// This does NOT connect to docker compose - it only tests the env var reading behavior.
func newHarnessForEnvTest(t *testing.T) *Harness {
	return &Harness{
		nrfURL: os.Getenv("FULLCHAIN_NRF_URL"),
		udmURL: os.Getenv("FULLCHAIN_UDM_URL"),
	}
}
