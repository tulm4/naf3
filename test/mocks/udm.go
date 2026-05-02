// Package mocks provides httptest.Server implementations of 3GPP NF APIs for integration testing.
package mocks

import (
	"net/http/httptest"

	"github.com/operator/nssAAF/internal/udmserver"
)

// Re-export types for backward compatibility with existing tests.
type (
	NudmUECMRegistration = udmserver.NudmUECMRegistration
	NudmRegItem          = udmserver.NudmRegItem
	AuthSubscription     = udmserver.AuthSubscription
	AuthContextResponse  = udmserver.AuthContextResponse
)

// UDMMock is an httptest.Server implementing the UDM Nudm_UECM API.
// Spec: TS 29.526 §7.2
type UDMMock struct {
	*httptest.Server
	server *udmserver.Server
}

// NewUDMMock creates a UDM mock server.
func NewUDMMock() *UDMMock {
	srv := udmserver.NewServer()
	ts := httptest.NewServer(srv)
	return &UDMMock{Server: ts, server: srv}
}

// Close shuts down the mock server.
func (m *UDMMock) Close() {
	m.Server.Close()
}

// URL returns the mock server's base URL.
func (m *UDMMock) URL() string {
	return m.Server.URL
}

// SetRegistration sets the registration data for a given SUPI.
func (m *UDMMock) SetRegistration(supi string, reg *udmserver.NudmUECMRegistration) {
	m.server.SetRegistration(supi, reg)
}

// SetError configures an HTTP status code to return for a given SUPI.
// Useful for simulating timeouts (504) or other error conditions.
func (m *UDMMock) SetError(supi string, statusCode int) {
	m.server.SetError(supi, statusCode)
}

// SetAuthSubscription configures auth subscription for a SUPI.
func (m *UDMMock) SetAuthSubscription(supi, authType, aaaServer string) {
	m.server.SetAuthSubscription(supi, authType, aaaServer)
}

// SetGPSI sets the GPSI for a given SUPI, creating a default registration.
func (m *UDMMock) SetGPSI(supi, gpsi string) {
	m.server.SetGPSI(supi, gpsi)
}
