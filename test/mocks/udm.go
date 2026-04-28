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

// NudmUECMRegistration represents the Nudm_UECM_Get registration response.
// Spec: TS 29.526 §7.2.2, TS 29.571 §5.4.4
type NudmUECMRegistration struct {
	Supi          string        `json:"supi"`
	GPSI          string        `json:"gpsi,omitempty"`
	Suci          string        `json:"suci,omitempty"`
	AMFId         string        `json:"amfId,omitempty"`
	GuoCtrID      string        `json:"guamfId,omitempty"`
	Registrations []NudmRegItem `json:"registrations"`
}

// NudmRegItem represents a single registration within NudmUECMRegistration.
type NudmRegItem struct {
	PlmnID string `json:"plmnId"`
	Legacy bool   `json:"isLegacy"`
}

// UDMMock is an httptest.Server implementing the UDM Nudm_UECM API.
// Spec: TS 29.526 §7.2
type UDMMock struct {
	Server *httptest.Server

	mu sync.Mutex
	// registrations maps supi → registration data
	registrations map[string]*NudmUECMRegistration
}

// NewUDMMock creates a UDM mock server.
func NewUDMMock() *UDMMock {
	m := &UDMMock{
		registrations: make(map[string]*NudmUECMRegistration),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/nudm-uemm/v1/", m.handleRegistration)
	m.Server = httptest.NewServer(mux)
	return m
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
func (m *UDMMock) SetRegistration(supi string, reg *NudmUECMRegistration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registrations[supi] = reg
}

// SetGPSI sets the GPSI for a given SUPI, creating a default registration.
func (m *UDMMock) SetGPSI(supi, gpsi string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registrations[supi] = &NudmUECMRegistration{
		Supi: supi,
		GPSI: gpsi,
		Registrations: []NudmRegItem{
			{PlmnID: "00101", Legacy: false},
		},
	}
}

// handleRegistration handles GET /nudm-uemm/v1/{supi}/registration.
func (m *UDMMock) handleRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"cause":"METHOD_NOT_SUPPORTED"}`, http.StatusMethodNotAllowed)
		return
	}

	// Path: /nudm-uemm/v1/{supi}/registration
	// Strip prefix and trailing /registration
	path := strings.TrimPrefix(r.URL.Path, "/nudm-uemm/v1/")
	path = strings.TrimSuffix(path, "/registration")
	supi := strings.TrimSuffix(path, "/registration")
	supi = strings.Trim(supi, "/")

	if !strings.HasPrefix(supi, "imu-") && !strings.HasPrefix(supi, "5g-") {
		http.Error(w, `{"cause":"INVALID_SUPI"}`, http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	reg, ok := m.registrations[supi]
	m.mu.Unlock()

	if !ok {
		http.Error(w, fmt.Sprintf(`{"cause":"USER_NOT_FOUND","supi":"%s"}`, supi), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reg)
}
