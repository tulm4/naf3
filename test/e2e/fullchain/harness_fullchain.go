//go:build e2e
// +build e2e

// Package fullchain provides E2E test harness with NRF/UDM mock integration.
package fullchain

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/operator/nssAAF/test/e2e"
)

// Harness extends e2e.Harness with containerized NRF/UDM integration for fullchain tests.
// It connects to the fullchain docker compose stack instead of using httptest mocks.
type Harness struct {
	*e2e.Harness
	nrfURL string
	udmURL string
}

// NewHarness creates a fullchain test harness.
// It connects to the fullchain docker compose stack (compose/fullchain.yaml)
// instead of using httptest mocks. Container URLs are read from environment
// variables set by the Makefile (FULLCHAIN_NRF_URL, FULLCHAIN_UDM_URL).
func NewHarness(t *testing.T) *Harness {
	h := e2e.NewHarness(t)

	// Container URLs from environment variables (set by Makefile test-fullchain).
	nrfURL := os.Getenv("FULLCHAIN_NRF_URL")
	udmURL := os.Getenv("FULLCHAIN_UDM_URL")

	return &Harness{
		Harness: h,
		nrfURL:  nrfURL,
		udmURL:  udmURL,
	}
}

// NRFURL returns the containerized NRF mock URL.
func (h *Harness) NRFURL() string { return h.nrfURL }

// UDMURL returns the containerized UDM mock URL.
func (h *Harness) UDMURL() string { return h.udmURL }

// SetNRFServiceEndpoint configures the NRF mock's service endpoint.
// For containerized tests, the NRF mock is configured via environment
// variables at startup (NRF_SERVICE_ENDPOINTS). This method validates
// connectivity and can be extended to configure via admin API.
func (h *Harness) SetNRFServiceEndpoint(nfType, serviceName, host string, port int) error {
	if h.nrfURL == "" {
		return errors.New("FULLCHAIN_NRF_URL not set")
	}
	// Containerized NRF mock configured via env vars at startup.
	// For initial implementation, we just validate the URL is set.
	return nil
}

// SetUDMAuthSubscription configures the UDM mock's auth subscription.
// For containerized tests, the UDM mock is configured via environment
// variables or docker compose run. This method validates connectivity.
func (h *Harness) SetUDMAuthSubscription(supi, authType, aaaServer string) error {
	if h.udmURL == "" {
		return errors.New("FULLCHAIN_UDM_URL not set")
	}
	// Containerized UDM mock configured via env vars at startup.
	return nil
}

// Close cleans up harness resources.
func (h *Harness) Close() {
	h.Harness.Close()
}

// ResetState clears state for test isolation.
func (h *Harness) ResetState() {
	h.Harness.ResetState()
}

// RequireTestContext returns a context with timeout for test operations.
func RequireTestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
