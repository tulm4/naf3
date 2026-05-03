//go:build e2e
// +build e2e

package e2e

import (
	"net/http/httptest"
	"os"

	"github.com/operator/nssAAF/test/mocks"
)

// ContainerDriver routes to containerized NRF, UDM, and AAA-S services
// from the fullchain docker compose stack (compose/fullchain-dev.yaml).
//
// This driver is used when E2E_PROFILE=fullchain. The Biz Pod is configured
// via environment variables in compose/fullchain-dev.yaml to point to the containerized
// services:
//
//	NRF_URL=http://nrf-mock:8081
//	UDM_URL=http://udm-mock:8081
//
// The URLs returned by NRFURL(), UDMURL(), and AAASimURL() are the host:port
// of the docker published ports (e.g., http://localhost:8082 for NRF),
// set by the Makefile via FULLCHAIN_NRF_URL, FULLCHAIN_UDM_URL env vars.
//
// ContainerDriver provides in-process AMF and AUSF mocks via httptest.Server
// because the fullchain compose does not include AMF/AUSF containers.
type ContainerDriver struct {
	nrfURL   string
	udmURL   string
	aaaSimURL string

	amfMock  *containerAMFDriver
	ausfMock *containerAUSFDriver
}

// NewContainerDriver creates a ContainerDriver from environment variables.
// Returns nil if required environment variables are not set.
func NewContainerDriver() *ContainerDriver {
	nrfURL := os.Getenv("FULLCHAIN_NRF_URL")
	udmURL := os.Getenv("FULLCHAIN_UDM_URL")
	aaaSimURL := os.Getenv("FULLCHAIN_AAA_SIM_URL")

	if nrfURL == "" {
		return nil
	}

	return &ContainerDriver{
		nrfURL:    nrfURL,
		udmURL:    udmURL,
		aaaSimURL: aaaSimURL,
		amfMock:  &containerAMFDriver{},
		ausfMock: &containerAUSFDriver{},
	}
}

// SetupAMFMock starts an in-process AMF mock for ContainerDriver.
// AMF/AUSF are not containerized in fullchain compose, so we use httptest mocks.
func (d *ContainerDriver) SetupAMFMock() AMFDriver {
	if d.amfMock == nil {
		d.amfMock = &containerAMFDriver{}
	}
	return d.amfMock
}

// SetupAUSFMock starts an in-process AUSF mock for ContainerDriver.
func (d *ContainerDriver) SetupAUSFMock() AUSFDriver {
	if d.ausfMock == nil {
		d.ausfMock = &containerAUSFDriver{}
	}
	return d.ausfMock
}

// NRFURL returns the containerized NRF mock URL (from FULLCHAIN_NRF_URL env var).
func (d *ContainerDriver) NRFURL() string {
	return d.nrfURL
}

// UDMURL returns the containerized UDM mock URL (from FULLCHAIN_UDM_URL env var).
func (d *ContainerDriver) UDMURL() string {
	return d.udmURL
}

// AAASimURL returns the AAA-S simulator URL (from FULLCHAIN_AAA_SIM_URL env var).
func (d *ContainerDriver) AAASimURL() string {
	return d.aaaSimURL
}

// Close cleans up driver resources.
// Containers are managed by docker compose, not by this driver.
func (d *ContainerDriver) Close() {
	if d.amfMock != nil {
		d.amfMock.Close()
		d.amfMock = nil
	}
	if d.ausfMock != nil {
		d.ausfMock.Close()
		d.ausfMock = nil
	}
}

// SetNRFServiceEndpoint configures a service endpoint in the containerized NRF mock.
//
// NOTE: The containerized NRF mock (nrf-mock) is configured via environment
// variables at container startup (NRF_SERVICE_ENDPOINTS in compose/fullchain-dev.yaml).
// This method is a stub for future admin API support.
//
// For programmatic per-test configuration, the NRF mock should expose an admin API:
//
//	PUT /admin/nf-instances/{nfType}/services/{serviceName}
//	Body: { "host": "...", "port": N }
//
// Until then, configure NRF endpoints via FULLCHAIN_NRF_URL and env vars.
func (d *ContainerDriver) SetNRFServiceEndpoint(nfType, serviceName, host string, port int) error {
	// Stub: NRF configured via env vars at startup
	// TODO: Implement admin API on nrf-mock container
	return nil
}

// SetUDMAuthSubscription configures auth subscription for a SUPI in the containerized UDM mock.
//
// NOTE: The containerized UDM mock (udm-mock) is configured via environment
// variables at container startup (FULLCHAIN_UDM_AUTH_SUBSCRIPTIONS in compose/fullchain-dev.yaml).
// This method is a stub for future admin API support.
//
// For programmatic per-test configuration, the UDM mock should expose an admin API:
//
//	PUT /admin/subscribers/{supi}/auth-subscription
//	Body: { "authType": "...", "aaaServer": "radius://..." }
//
// Until then, configure UDM subscriptions via FULLCHAIN_UDM_URL and env vars.
func (d *ContainerDriver) SetUDMAuthSubscription(supi, authType, aaaServer string) error {
	// Stub: UDM configured via env vars at startup
	// TODO: Implement admin API on udm-mock container
	return nil
}

// containerAMFDriver wraps mocks.AMFMock for ContainerDriver.
type containerAMFDriver struct {
	mock *mocks.AMFMock
}

func (d *containerAMFDriver) Server() *httptest.Server {
	if d.mock == nil {
		d.mock = mocks.NewAMFMock()
	}
	return d.mock.Server()
}

func (d *containerAMFDriver) URL() string {
	if d.mock == nil {
		d.mock = mocks.NewAMFMock()
	}
	return d.mock.URL()
}

func (d *containerAMFDriver) GetNotifications() []mocks.NssaaNotification {
	if d.mock == nil {
		return nil
	}
	return d.mock.GetNotifications()
}

func (d *containerAMFDriver) ClearNotifications() {
	if d.mock != nil {
		d.mock.ClearNotifications()
	}
}

func (d *containerAMFDriver) SetFailureNext(errorCode int) {
	if d.mock != nil {
		d.mock.SetFailureNext(errorCode)
	}
}

func (d *containerAMFDriver) Close() {
	if d.mock != nil {
		d.mock.Close()
		d.mock = nil
	}
}

// containerAUSFDriver wraps mocks.AUSFMock for ContainerDriver.
type containerAUSFDriver struct {
	mock *mocks.AUSFMock
}

func (d *containerAUSFDriver) Server() *httptest.Server {
	if d.mock == nil {
		d.mock = mocks.NewAUSFMock()
	}
	return d.mock.Server()
}

func (d *containerAUSFDriver) URL() string {
	if d.mock == nil {
		d.mock = mocks.NewAUSFMock()
	}
	return d.mock.URL()
}

func (d *containerAUSFDriver) Close() {
	if d.mock != nil {
		d.mock.Close()
		d.mock = nil
	}
}

// Verify ContainerDriver satisfies Driver interface at compile time.
var _ Driver = (*ContainerDriver)(nil)

// Verify containerAMFDriver satisfies AMFDriver at compile time.
var _ AMFDriver = (*containerAMFDriver)(nil)

// Verify containerAUSFDriver satisfies AUSFDriver at compile time.
var _ AUSFDriver = (*containerAUSFDriver)(nil)
