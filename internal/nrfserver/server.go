// Package nrfserver provides a production-ready NRF mock server.
// Imported by cmd/nrf-mock (containerized) and test/mocks/nrf.go (httptest).
// Spec: TS 29.510 §6
package nrfserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// ServiceEndpointConfig holds the endpoint configuration for a service.
type ServiceEndpointConfig struct {
	IPv4Address string
	Port       int
}

// NF type constants.
const (
	NFTypeUDM    = "UDM"
	NFTypeAMF    = "AMF"
	NFTypeAUSF   = "AUSF"
	NFTypeAAAGW  = "AAA_GW"
	NFTypeNSSAAF = "NSSAAF"
)

// Server is an HTTP server implementing the NRF Nnrf_NFM API.
// Spec: TS 29.510 §6
type Server struct {
	httpSrv *http.Server

	mu sync.Mutex
	// nfStatus maps nfInstanceId → nfStatus value
	nfStatus map[string]string
	// profiles maps nfInstanceId → NFProfile JSON bytes
	profiles map[string][]byte
	// serviceEndpoints maps "NFType:serviceName" → endpoint config
	serviceEndpoints map[string]ServiceEndpointConfig
}

// NewServer creates an NRF server with default UDM, AMF, AUSF, and AAA-GW profiles.
// Supports both Nnrf_NFDiscovery (/nnrf-disc/v1/) and Nnrf_NFManagement (/nnrf-nfm/v1/).
func NewServer() *Server {
	s := &Server{
		nfStatus: map[string]string{
			"udm-001":    "REGISTERED",
			"amf-001":    "REGISTERED",
			"ausf-001":   "REGISTERED",
			"aaa-gw-001": "REGISTERED",
		},
		profiles:         map[string][]byte{},
		serviceEndpoints: map[string]ServiceEndpointConfig{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/nnrf-disc/v1/nf-instances", s.handleNfInstancesDisc)
	mux.HandleFunc("/nnrf-disc/v1/nf-instances/", s.handleNfInstancesDisc)
	mux.HandleFunc("/nnrf-disc/v1/subscriptions/", s.handleSubscription)
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances", s.handleNfInstancesNfm)
	mux.HandleFunc("/nnrf-nfm/v1/nf-instances/", s.handleNfInstancesNfm)
	mux.HandleFunc("/nnrf-nfm/v1/subscriptions/", s.handleSubscription)
	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// SetNFStatus sets the nfStatus for a given NF instance ID.
func (s *Server) SetNFStatus(nfInstanceID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nfStatus[nfInstanceID] = status
}

// SetProfile sets a custom NF profile JSON for a given NF instance ID.
func (s *Server) SetProfile(nfInstanceID string, profileJSON []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[nfInstanceID] = profileJSON
}

// SetServiceEndpoint configures the endpoint for an NF's service.
func (s *Server) SetServiceEndpoint(nfType, serviceName, host string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s", nfType, serviceName)
	s.serviceEndpoints[key] = ServiceEndpointConfig{
		IPv4Address: host,
		Port:        port,
	}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(addr string) error {
	s.httpSrv.Addr = addr
	return s.httpSrv.ListenAndServe()
}

// ServeHTTP implements http.Handler so Server can be used with httptest.NewServer.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.httpSrv.Handler.ServeHTTP(w, r)
}

// handleNfInstancesDisc dispatches Nnrf_NFDiscovery calls.
// GET → discovery (query params) or instance lookup
// POST → registration
// PUT → heartbeat
func (s *Server) handleNfInstancesDisc(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/nnrf-disc/v1/nf-instances")
	path = strings.TrimSuffix(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			s.handleDiscovery(w, r)
		} else {
			id := strings.TrimPrefix(path, "/")
			s.handleGetInstance(w, r, id)
		}
	case http.MethodPost:
		s.handlePostInstance(w, r)
	case http.MethodPut:
		id := strings.TrimPrefix(path, "/")
		s.handlePutInstance(w, r, id)
	default:
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
	}
}

// handleNfInstancesNfm dispatches Nnrf_NFManagement calls.
func (s *Server) handleNfInstancesNfm(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/nnrf-nfm/v1/nf-instances")
	path = strings.TrimSuffix(path, "/")

	switch r.Method {
	case http.MethodGet:
		if path == "" {
			s.handleDiscovery(w, r)
		} else {
			id := strings.TrimPrefix(path, "/")
			s.handleGetInstance(w, r, id)
		}
	case http.MethodPost:
		s.handlePostInstance(w, r)
	case http.MethodPut:
		id := strings.TrimPrefix(path, "/")
		s.handlePutInstance(w, r, id)
	default:
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
	}
}

// handleDiscovery handles GET /nnrf-disc/v1/nf-instances?... discovery queries.
// Spec: TS 29.510 §6.2.6 (Nnrf_NFDiscovery_Search).
func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	targetType := r.URL.Query().Get("target-nf-type")
	queryServiceName := r.URL.Query().Get("service-names")

	var prefixes []string
	switch targetType {
	case NFTypeUDM:
		prefixes = []string{"udm-"}
	case NFTypeAMF:
		prefixes = []string{"amf-"}
	case NFTypeAUSF:
		prefixes = []string{"ausf-"}
	case NFTypeAAAGW:
		prefixes = []string{"aaa-gw-"}
	case NFTypeNSSAAF:
		prefixes = []string{"nssAAF-"}
	default:
		if len(s.nfStatus) > 0 {
			prefixes = []string{""}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	instances := make([]map[string]interface{}, 0, len(s.nfStatus))
	for id, status := range s.nfStatus {
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
		nfType := s.nfTypeFromID(id)
		svcName := queryServiceName
		if svcName == "" {
			svcName = serviceNameForType(nfType)
		}
		profile := defaultNFProfile(nfType, id, status)
		if svcName != "" {
			key := nfType + ":" + svcName
			ipAddr := "127.0.0.1"
			port := 8080
			if ep, ok := s.serviceEndpoints[key]; ok {
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
func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request, id string) {
	wantedStatus := r.URL.Query().Get("nfStatus")

	s.mu.Lock()
	defer s.mu.Unlock()

	status, ok := s.nfStatus[id]
	if !ok {
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if wantedStatus != "" && status != wantedStatus {
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if profile, exists := s.profiles[id]; exists {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(profile)
		return
	}

	nfType := s.nfTypeFromID(id)
	profile := defaultNFProfile(nfType, id, status)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

// handlePutInstance handles PUT /nnrf-disc/v1/nf-instances/{id} — Nnrf_NFHeartBeat.
func (s *Server) handlePutInstance(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"cause":"INVALID_FORMAT"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if status, ok := payload["nfStatus"].(string); ok {
		s.nfStatus[id] = status
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// handlePostInstance handles POST /nnrf-disc/v1/nf-instances — Nnrf_NFRegistration.
func (s *Server) handlePostInstance(w http.ResponseWriter, r *http.Request) {
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
func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// nfTypeFromID derives the NF type string from an NF instance ID prefix.
func (s *Server) nfTypeFromID(id string) string {
	switch {
	case strings.HasPrefix(id, "udm-"):
		return NFTypeUDM
	case strings.HasPrefix(id, "amf-"):
		return NFTypeAMF
	case strings.HasPrefix(id, "ausf-"):
		return NFTypeAUSF
	case strings.HasPrefix(id, "aaa-gw-"):
		return NFTypeAAAGW
	default:
		return NFTypeNSSAAF
	}
}

// serviceNameForType returns the default service name for an NF type.
func serviceNameForType(nfType string) string {
	switch nfType {
	case NFTypeUDM:
		return "nudm-uem"
	case NFTypeAUSF:
		return "nausf-auth"
	case NFTypeAMF:
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
