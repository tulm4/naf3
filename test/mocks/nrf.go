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

// ServiceEndpointConfig holds the endpoint configuration for a service.
type ServiceEndpointConfig struct {
	IPv4Address string
	Port        int
}

// NRFMock is an httptest.Server implementing the NRF Nnrf_NFM API.
// Spec: TS 29.510 §6
type NRFMock struct {
	Server *httptest.Server

	mu sync.Mutex
	// nfStatus maps nfInstanceId → nfStatus value
	nfStatus map[string]string
	// profiles maps nfInstanceId → NFProfile JSON bytes
	profiles map[string][]byte
	// serviceEndpoints maps "NFType:serviceName" → endpoint config
	serviceEndpoints map[string]ServiceEndpointConfig
}

// NewNRFMock creates an NRF mock server with default UDM, AMF, AUSF, and AAA-GW profiles.
// Supports both Nnrf_NFDiscovery (/nnrf-disc/v1/) and Nnrf_NFManagement (/nnrf-nfm/v1/).
func NewNRFMock() *NRFMock {
	m := &NRFMock{
		nfStatus: map[string]string{
			"udm-001":    "REGISTERED",
			"amf-001":    "REGISTERED",
			"ausf-001":   "REGISTERED",
			"aaa-gw-001": "REGISTERED",
		},
		profiles:        map[string][]byte{},
		serviceEndpoints: map[string]ServiceEndpointConfig{},
	}
	// Use a custom mux so we can register different handlers for the same path
	// with different HTTP methods (ServeMux only allows one handler per path).
	mux := http.NewServeMux()
	// Discovery API base path: GET for discovery, POST for registration.
	// PUT for heartbeat on instance path.
	mux.HandleFunc("/nnrf-disc/v1/nf-instances", m.handleNfInstancesDisc)
	mux.HandleFunc("/nnrf-disc/v1/nf-instances/", m.handleNfInstancesDisc)
	mux.HandleFunc("/nnrf-disc/v1/subscriptions/", m.handleSubscription)
	// Management API: same dispatcher logic for Nnrf_NFManagement.
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances", m.handleNfInstancesNfm)
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances/", m.handleNfInstancesNfm)
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

// SetServiceEndpoint configures the endpoint for an NF's service.
// This allows E2E tests to point to container DNS names.
func (m *NRFMock) SetServiceEndpoint(nfType, serviceName, host string, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s:%s", nfType, serviceName)
	m.serviceEndpoints[key] = ServiceEndpointConfig{
		IPv4Address: host,
		Port:        port,
	}
}

// handleNfInstancesDisc dispatches Nnrf_NFDiscovery calls.
// GET → discovery (query params) or instance lookup
// POST → registration
// PUT → heartbeat
func (m *NRFMock) handleNfInstancesDisc(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/nnrf-disc/v1/nf-instances")
	// path is now "" (base with no trailing slash) or "/{id}" (instance)
	path = strings.TrimSuffix(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			// GET /nnrf-disc/v1/nf-instances?... → discovery query
			m.handleDiscovery(w, r)
		} else {
			// GET /nnrf-disc/v1/nf-instances/{id} → instance lookup
			id := strings.TrimPrefix(path, "/")
			m.handleGetInstance(w, r, id)
		}
	case http.MethodPost:
		m.handlePostInstance(w, r)
	case http.MethodPut:
		id := strings.TrimPrefix(path, "/")
		m.handlePutInstance(w, r, id)
	default:
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
	}
}

// handleNfInstancesNfm dispatches Nnrf_NFManagement calls.
func (m *NRFMock) handleNfInstancesNfm(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/nnrf-nfm/v1/nf-instances")
	path = strings.TrimSuffix(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			m.handleDiscovery(w, r)
		} else {
			id := strings.TrimPrefix(path, "/")
			m.handleGetInstance(w, r, id)
		}
	case http.MethodPost:
		m.handlePostInstance(w, r)
	case http.MethodPut:
		id := strings.TrimPrefix(path, "/")
		m.handlePutInstance(w, r, id)
	default:
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
	}
}

// handleDiscovery handles GET /nnrf-disc/v1/nf-instances?... discovery queries.
// Spec: TS 29.510 §6.2.6 (Nnrf_NFDiscovery_Search).
func (m *NRFMock) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	targetType := r.URL.Query().Get("target-nf-type")
	queryServiceName := r.URL.Query().Get("service-names")

	// Map target-nf-type to instance ID prefixes.
	var prefixes []string
	switch targetType {
	case "UDM":
		prefixes = []string{"udm-"}
	case "AMF":
		prefixes = []string{"amf-"}
	case "AUSF":
		prefixes = []string{"ausf-"}
	case "AAA_GW":
		prefixes = []string{"aaa-gw-"}
	case "NSSAAF":
		prefixes = []string{"nssAAF-"}
	default:
		// No target-nf-type → match all (return first registered NF as fallback).
		if len(m.nfStatus) > 0 {
			prefixes = []string{""}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	instances := make([]map[string]interface{}, 0, len(m.nfStatus))
	for id, status := range m.nfStatus {
		match := false
		for _, p := range prefixes {
			if p == "" || strings.HasPrefix(id, p) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if status != "REGISTERED" {
			continue
		}
		var nfType string
		switch {
		case strings.HasPrefix(id, "udm-"):
			nfType = "UDM"
		case strings.HasPrefix(id, "amf-"):
			nfType = "AMF"
		case strings.HasPrefix(id, "ausf-"):
			nfType = "AUSF"
		case strings.HasPrefix(id, "aaa-gw-"):
			nfType = "AAA_GW"
		default:
			nfType = "NSSAAF"
		}
		svcName := queryServiceName
		if svcName == "" {
			svcName = serviceNameForType(nfType)
		}
		profile := defaultNFProfile(nfType, id, status)
		if svcName != "" {
			key := nfType + ":" + svcName
			var ipAddr string = "127.0.0.1"
			var port int = 8080
			if ep, ok := m.serviceEndpoints[key]; ok {
				ipAddr = ep.IPv4Address
				port = ep.Port
			}
			profile["nfServices"] = map[string]interface{}{
				svcName: map[string]interface{}{
					"serviceName": svcName,
					"versions": []map[string]interface{}{
						{"apiVersion": "v1"},
					},
					"ipEndPoints": []map[string]interface{}{
						{"ipv4Address": ipAddr, "port": port},
					},
				},
			}
		}
		instances = append(instances, profile)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"nfInstances": instances})
}

// handleGetInstance handles GET /nnrf-disc/v1/nf-instances/{id}.
func (m *NRFMock) handleGetInstance(w http.ResponseWriter, r *http.Request, id string) {
	wantedStatus := r.URL.Query().Get("nfStatus")

	m.mu.Lock()
	defer m.mu.Unlock()

	status, ok := m.nfStatus[id]
	if !ok {
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if wantedStatus != "" && status != wantedStatus {
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if profile, exists := m.profiles[id]; exists {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(profile)
		return
	}

	var profile map[string]interface{}
	switch {
	case strings.HasPrefix(id, "udm-"):
		profile = defaultNFProfile("UDM", id, status)
	case strings.HasPrefix(id, "amf-"):
		profile = defaultNFProfile("AMF", id, status)
	case strings.HasPrefix(id, "ausf-"):
		profile = defaultNFProfile("AUSF", id, status)
	case strings.HasPrefix(id, "aaa-gw-"):
		profile = defaultNFProfile("AAA_GW", id, status)
	default:
		profile = defaultNFProfile("NSSAAF", id, status)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

// handlePutInstance handles PUT /nnrf-disc/v1/nf-instances/{id} — Nnrf_NFHeartBeat.
func (m *NRFMock) handlePutInstance(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"cause":"INVALID_FORMAT"}`, http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	if status, ok := payload["nfStatus"].(string); ok {
		m.nfStatus[id] = status
	}
	m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// handlePostInstance handles POST /nnrf-disc/v1/nf-instances — Nnrf_NFRegistration.
func (m *NRFMock) handlePostInstance(w http.ResponseWriter, r *http.Request) {
	var profile map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		http.Error(w, `{"cause":"INVALID_FORMAT"}`, http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

// handleSubscription handles PUT /nnrf-disc/v1/subscriptions/{id} — heartbeat subscription.
func (m *NRFMock) handleSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serviceNameForType returns the default service name for an NF type.
func serviceNameForType(nfType string) string {
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

// defaultNFProfile returns a valid NF profile for built-in NF types.
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
			serviceNameForType(nfType): map[string]interface{}{
				"serviceName": serviceNameForType(nfType),
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
