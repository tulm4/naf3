// Package mocks provides httptest.Server implementations of 3GPP NF APIs for integration testing.
package mocks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// NRFMock is an httptest.Server implementing the NRF Nnrf_NFM API.
// Spec: TS 29.510 §6
type NRFMock struct {
	Server *httptest.Server

	mu sync.Mutex
	// nfStatus maps nfInstanceId → nfStatus value
	nfStatus map[string]string
	// profiles maps nfInstanceId → NFProfile JSON bytes
	profiles map[string][]byte
}

// NewNRFMock creates an NRF mock server with default UDM, AMF, AUSF, and AAA-GW profiles.
func NewNRFMock() *NRFMock {
	m := &NRFMock{
		nfStatus: map[string]string{
			"udm-001":    "REGISTERED",
			"amf-001":    "REGISTERED",
			"ausf-001":   "REGISTERED",
			"aaa-gw-001": "REGISTERED",
		},
		profiles: map[string][]byte{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances/", m.handleGetInstance)
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances", m.handlePostInstance)
	mux.HandleFunc("/nnrf-nfm/v1/subscriptions/", m.handleSubscription)
	m.Server = httptest.NewServer(mux)
	return m
}

// Close shuts down the mock server.
func (m *NRFMock) Close() {
	m.Server.Close()
}

// URL returns the mock server's base URL.
func (m *NRFMock) URL() string {
	return m.Server.URL
}

// SetNFStatus sets the nfStatus for a given NF instance ID.
func (m *NRFMock) SetNFStatus(nfInstanceID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nfStatus[nfInstanceID] = status
}

// SetProfile sets a custom NF profile JSON for a given NF instance ID.
func (m *NRFMock) SetProfile(nfInstanceID string, profileJSON []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.profiles[nfInstanceID] = profileJSON
}

// DefaultNFProfiles returns valid NF profile JSON for built-in NF types.
func defaultNFProfile(nfType, nfInstanceID, status string) map[string]interface{} {
	return map[string]interface{}{
		"nfInstanceId":   nfInstanceID,
		"nfType":         nfType,
		"nfStatus":       status,
		"heartBeatTimer": 300,
		"load":           0,
		"plmnId": map[string]interface{}{
			"mcc": "001",
			"mnc": "01",
		},
		"nsiList": []interface{}{},
		"nfServices": map[string]interface{}{
			serviceName(nfType): map[string]interface{}{
				"serviceName": serviceName(nfType),
				"versions": []map[string]interface{}{
					{"apiVersion": "v1"},
				},
				"ipEndPoints": []map[string]interface{}{
					{"ipv4Address": "127.0.0.1", "port": 8080},
				},
			},
		},
	}
}

func serviceName(nfType string) string {
	switch nfType {
	case "UDM":
		return "nudm-uem"
	case "AUSF":
		return "nausf-auth"
	case "AMF":
		return "namf-comm"
	default:
		return ""
	}
}

func (m *NRFMock) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	// Path: /nnrf-nfm/v1/nf-instances/{nfInstanceId}
	path := strings.TrimPrefix(r.URL.Path, "/nnrf-nfm/v1/nf-instances/")
	path = strings.TrimSuffix(path, "/")

	// Check for nfStatus query param filter
	wantedStatus := r.URL.Query().Get("nfStatus")

	m.mu.Lock()
	status, ok := m.nfStatus[path]
	if !ok {
		m.mu.Unlock()
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if wantedStatus != "" && status != wantedStatus {
		m.mu.Unlock()
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if profile, exists := m.profiles[path]; exists {
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(profile)
		return
	}
	m.mu.Unlock()

	// Return a default profile based on instance ID prefix
	var profile map[string]interface{}
	switch {
	case strings.HasPrefix(path, "udm-"):
		profile = defaultNFProfile("UDM", path, status)
	case strings.HasPrefix(path, "amf-"):
		profile = defaultNFProfile("AMF", path, status)
	case strings.HasPrefix(path, "ausf-"):
		profile = defaultNFProfile("AUSF", path, status)
	case strings.HasPrefix(path, "aaa-gw-"):
		profile = defaultNFProfile("AAA_GW", path, status)
	default:
		profile = defaultNFProfile("NSSAAF", path, status)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func (m *NRFMock) handlePostInstance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}
	var profile map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		http.Error(w, `{"cause":"INVALID_FORMAT"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

func (m *NRFMock) handleSubscription(w http.ResponseWriter, r *http.Request) {
	// PUT /nnrf-nfm/v1/subscriptions/{subscriptionId} — heartbeat
	if r.Method != http.MethodPut {
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
