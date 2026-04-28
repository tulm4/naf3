// Package mocks provides httptest.Server implementations of 3GPP NF APIs for integration testing.
package mocks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// UeAuthData represents UE authentication data from AUSF.
// Spec: TS 29.518 §6.1.3.2.2, TS 29.526 §7.3
type UeAuthData struct {
	AuthType                string `json:"authType"` // e.g., "5G_AKA", "EAP"
	AuthSubscribed          string `json:"authSubscribed"`
	KDFNegotiationSupported bool   `json:"kdfNegotiationSupported,omitempty"`
	SequenceNumber          string `json:"sequenceNumber,omitempty"`
	auts                    string `json:"auts,omitempty"`
}

// AUSFMock is an httptest.Server implementing the AUSF N60 API.
// Spec: TS 29.526 §7.3, TS 29.518 §6.1.3.2
type AUSFMock struct {
	Server *httptest.Server

	mu sync.Mutex
	// authData maps GPSI → UeAuthData
	authData map[string]*UeAuthData
	// errorCodes maps GPSI → HTTP status code for error injection
	errorCodes map[string]int
}

// NewAUSFMock creates an AUSF mock server.
func NewAUSFMock() *AUSFMock {
	m := &AUSFMock{
		authData:   make(map[string]*UeAuthData),
		errorCodes: make(map[string]int),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/nausf-auth/v1/ue-identities/", m.handleUEAuth)
	m.Server = httptest.NewServer(mux)
	return m
}

// Close shuts down the mock server.
func (m *AUSFMock) Close() {
	m.Server.Close()
}

// URL returns the mock server's base URL.
func (m *AUSFMock) URL() string {
	return m.Server.URL
}

// SetUEAuthData configures the UE authentication data for a given GPSI.
func (m *AUSFMock) SetUEAuthData(gpsi string, data *UeAuthData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authData[gpsi] = data
}

// SetError configures an error response for a given GPSI.
func (m *AUSFMock) SetError(gpsi string, statusCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorCodes[gpsi] = statusCode
}

func (m *AUSFMock) handleUEAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}

	// Path: /nausf-auth/v1/ue-identities/{gpsi}
	gpsi := strings.TrimPrefix(r.URL.Path, "/nausf-auth/v1/ue-identities/")

	m.mu.Lock()
	statusCode, hasError := m.errorCodes[gpsi]
	authData, hasData := m.authData[gpsi]
	m.mu.Unlock()

	if hasError {
		http.Error(w, fmt.Sprintf(`{"cause":"AUSF_ERROR_%d"}`, statusCode), statusCode)
		return
	}

	if !hasData {
		http.Error(w, `{"cause":"UE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(authData)
}
