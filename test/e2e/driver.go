//go:build e2e
// +build e2e

// Package e2e provides an end-to-end test harness for the 3-component
// NSSAAF architecture: HTTP Gateway, Biz Pod, and AAA Gateway.
package e2e

import (
	"net/http"
	"net/http/httptest"

	"github.com/operator/nssAAF/test/mocks"
)

// Driver abstracts the backend for E2E tests.
//
// ContainerDriver routes to the containerized NRF/UDM/AAA-S services
// defined in compose/fullchain-dev.yaml. AMF and AUSF callbacks are
// mocked in-process via httptest.Server.
//
// Driver is selected at test startup via the E2E_PROFILE environment variable:
//   - "" or "fullchain": ContainerDriver + compose/fullchain-dev.yaml
//   - "mock":             MockDriver + in-process mocks (unit-level testing only)
type Driver interface {
	// SetupAMFMock starts an AMF callback mock server.
	// Returns nil if AMF is not mockable in this driver (e.g., containerized AMF).
	SetupAMFMock() AMFDriver

	// SetupAUSFMock starts an AUSF callback mock server.
	// Returns nil if AUSF is not mockable in this driver.
	SetupAUSFMock() AUSFDriver

	// NRFURL returns the URL of the NRF mock server.
	// Returns empty string if NRF is not available (e.g., mock driver uses in-process).
	NRFURL() string

	// UDMURL returns the URL of the UDM mock server.
	// Returns empty string if UDM is not available.
	UDMURL() string

	// AAASimURL returns the RADIUS/Diameter URL of the AAA-S simulator.
	// Returns empty string if AAA-S is not available.
	AAASimURL() string

	// Close cleans up driver resources.
	// For MockDriver: closes httptest.Server instances.
	// For ContainerDriver: closes HTTP clients (containers are managed by docker compose).
	Close()
}

// AMFDriver is the AMF callback mock interface.
type AMFDriver interface {
	// Server returns the underlying httptest.Server.
	Server() *httptest.Server

	// URL returns the mock AMF base URL.
	URL() string

	// GetNotifications returns all received NSSAA notifications.
	GetNotifications() []mocks.NssaaNotification

	// ClearNotifications resets received notifications.
	ClearNotifications()

	// SetFailureNext causes the next notification to return errorCode.
	SetFailureNext(errorCode int)

	// Close shuts down the mock server.
	Close()
}

// AUSFDriver is the AUSF callback mock interface.
type AUSFDriver interface {
	// Server returns the underlying httptest.Server.
	Server() *httptest.Server

	// URL returns the mock AUSF base URL.
	URL() string

	// Close shuts down the mock server.
	Close()
}

// tlsClient returns an http.Client for the given TLS CA certificate path.
// For the mock driver, this returns a plain http.Client.
// For the container driver, this returns a client configured with the self-signed CA.
func tlsClient() *http.Client {
	return &http.Client{
		Timeout: 30_000_000_000, // 30s
	}
}
