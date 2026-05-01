//go:build e2e
// +build e2e

// Package fullchain provides E2E test harness with NRF/UDM mock integration.
package fullchain

import (
	"context"
	"testing"
	"time"

	"github.com/operator/nssAAF/test/e2e"
	"github.com/operator/nssAAF/test/mocks"
)

// Harness extends e2e.Harness with NRF/UDM mock integration for fullchain tests.
type Harness struct {
	*e2e.Harness
	NRFMock *mocks.NRFMock
	UDMMock *mocks.UDMMock
}

// NewHarness creates a fullchain test harness.
func NewHarness(t *testing.T) *Harness {
	h := e2e.NewHarness(t)

	// Start NRF mock
	nrfMock := mocks.NewNRFMock()
	// Configure default endpoints pointing to mock containers
	nrfMock.SetServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8080)
	nrfMock.SetServiceEndpoint("AUSF", "nausf-auth", "ausf-mock", 8080)

	// Start UDM mock
	udmMock := mocks.NewUDMMock()
	// Configure default auth subscriptions
	udmMock.SetAuthSubscription("imsi-208046000000001", "EAP_TLS", "radius://mock-aaa-s:1812")

	return &Harness{
		Harness:  h,
		NRFMock: nrfMock,
		UDMMock: udmMock,
	}
}

// Close cleans up mock resources.
func (h *Harness) Close() {
	if h.NRFMock != nil {
		h.NRFMock.Close()
	}
	if h.UDMMock != nil {
		h.UDMMock.Close()
	}
	h.Harness.Close()
}

// ResetState clears state for both infrastructure and mocks.
func (h *Harness) ResetState() {
	h.Harness.ResetState()
	// Clear mock state by recreating
	h.NRFMock.Close()
	h.UDMMock.Close()
	h.NRFMock = mocks.NewNRFMock()
	h.NRFMock.SetServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8080)
	h.NRFMock.SetServiceEndpoint("AUSF", "nausf-auth", "ausf-mock", 8080)
	h.UDMMock = mocks.NewUDMMock()
	h.UDMMock.SetAuthSubscription("imsi-208046000000001", "EAP_TLS", "radius://mock-aaa-s:1812")
}

// RequireTestContext returns a context with timeout for test operations.
func RequireTestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
