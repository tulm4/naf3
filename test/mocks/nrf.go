// Package mocks provides httptest.Server implementations of 3GPP NF APIs for integration testing.
package mocks

import (
	"net/http/httptest"

	"github.com/operator/nssAAF/internal/nrfserver"
)

// NRFMock is an httptest.Server wrapper around nrfserver.Server.
type NRFMock struct {
	httpServer *httptest.Server
	server     *nrfserver.Server
}

// NewNRFMock creates an NRF mock server with default profiles.
func NewNRFMock() *NRFMock {
	srv := nrfserver.NewServer()
	ts := httptest.NewServer(srv)
	return &NRFMock{httpServer: ts, server: srv}
}

// Close shuts down the mock server.
func (m *NRFMock) Close() {
	m.httpServer.Close()
}

// URL returns the mock server's base URL.
func (m *NRFMock) URL() string {
	return m.httpServer.URL
}

// Server returns the underlying httptest.Server.
func (m *NRFMock) Server() *httptest.Server {
	return m.httpServer
}

// SetNFStatus forwards to the underlying server.
func (m *NRFMock) SetNFStatus(nfInstanceID, status string) {
	m.server.SetNFStatus(nfInstanceID, status)
}

// SetProfile sets a custom NF profile JSON for a given NF instance ID.
func (m *NRFMock) SetProfile(nfInstanceID string, profileJSON []byte) {
	m.server.SetProfile(nfInstanceID, profileJSON)
}

// SetServiceEndpoint forwards to the underlying server.
func (m *NRFMock) SetServiceEndpoint(nfType, serviceName, host string, port int) {
	m.server.SetServiceEndpoint(nfType, serviceName, host, port)
}
