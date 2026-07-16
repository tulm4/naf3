// Package nrfserver provides a production-ready NRF mock server.
// Imported by cmd/nrf-mock (containerized) and test/mocks/nrf.go (httptest).
// Spec: TS 29.510 §6
package nrfserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
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

// Close gracefully shuts down the server.
func (s *Server) Close() error {
	return s.httpSrv.Close()
}

// Shutdown gracefully shuts down the server with a context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

// ServeHTTP implements http.Handler so Server can be used with httptest.NewServer.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Log incoming request
	slog.Info("nrf mock request",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
	)
	s.httpSrv.Handler.ServeHTTP(w, r)
}

// handleNfInstancesDisc dispatches Nnrf_NFDiscovery calls.
// GET → discovery (query params) or instance lookup
// POST → registration
// PUT → update/heartbeat
// PATCH → patch update / heartbeat (TS 29.510 §5.2.2.3.1B)
// DELETE → deregistration
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
	case http.MethodPatch:
		id := strings.TrimPrefix(path, "/")
		s.handlePatchInstance(w, r, id)
	case http.MethodDelete:
		id := strings.TrimPrefix(path, "/")
		s.handleDeleteInstance(w, r, id)
	default:
		w.Header().Set("Content-Type", "application/json")
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
	case http.MethodPatch:
		id := strings.TrimPrefix(path, "/")
		s.handlePatchInstance(w, r, id)
	case http.MethodDelete:
		id := strings.TrimPrefix(path, "/")
		s.handleDeleteInstance(w, r, id)
	default:
		w.Header().Set("Content-Type", "application/json")
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
	nfStatusCopy := make(map[string]string, len(s.nfStatus))
	for k, v := range s.nfStatus {
		nfStatusCopy[k] = v
	}
	serviceEndpointsCopy := make(map[string]ServiceEndpointConfig, len(s.serviceEndpoints))
	for k, v := range s.serviceEndpoints {
		serviceEndpointsCopy[k] = v
	}
	s.mu.Unlock()

	instances := make([]map[string]interface{}, 0, len(nfStatusCopy))
	for id, status := range nfStatusCopy {
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
			if ep, ok := serviceEndpointsCopy[key]; ok {
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
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if wantedStatus != "" && status != wantedStatus {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	if profile, exists := s.profiles[id]; exists {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(profile)
		return
	}

	// Return NSSAAF-specific profile if that's the type
	nfType := s.nfTypeFromID(id)
	if nfType == NFTypeNSSAAF {
		profile := defaultNSSAAFProfile(id, status)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(profile)
		return
	}

	profile := defaultNFProfile(nfType, id, status)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}

// handlePutInstance handles PUT /nnrf-disc/v1/nf-instances/{id} — Nnrf_NFHeartBeat.
// Per TS 29.510 §5.2.2.2, returns 201 Created with HeartBeat-Interval header.
func (s *Server) handlePutInstance(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"cause":"INVALID_FORMAT"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	// Store profile
	s.profiles[id] = mustMarshal(payload)
	if status, ok := payload["nfStatus"].(string); ok {
		s.nfStatus[id] = status
	} else {
		s.nfStatus[id] = "REGISTERED"
	}
	s.mu.Unlock()

	// Return 201 Created with HeartBeat-Interval header per TS 29.510 §5.2.2.2
	w.Header().Set("HeartBeat-Interval", "300")
	w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, time.Now().UnixNano()))
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// handlePatchInstance handles PATCH /nnrf-disc/v1/nf-instances/{id} — TS 29.510 §5.2.2.3.1B.
// Real NRF returns 204 No Content with a fresh ETag header on successful patch.
func (s *Server) handlePatchInstance(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"cause":"NF_INSTANCE_NOT_FOUND"}`, http.StatusNotFound)
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"cause":"INVALID_FORMAT"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if status, ok := payload["nfStatus"].(string); ok {
		s.nfStatus[id] = status
	}
	s.mu.Unlock()
	w.Header().Set("ETag", fmt.Sprintf(`"etag-%d"`, time.Now().UnixNano()))
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteInstance handles DELETE /nnrf-disc/v1/nf-instances/{id} — Nnrf_NFDeregistration.
func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.nfStatus, id)
	delete(s.profiles, id)

	w.WriteHeader(http.StatusNoContent)
}

// handlePostInstance handles POST /nnrf-disc/v1/nf-instances — Nnrf_NFRegistration.
func (s *Server) handlePostInstance(w http.ResponseWriter, r *http.Request) {
	var profile map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&profile); err != nil {
		w.Header().Set("Content-Type", "application/json")
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
		w.Header().Set("Content-Type", "application/json")
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
	case strings.HasPrefix(id, "nssAAF-"):
		return NFTypeNSSAAF
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

// defaultNSSAProfile returns a valid NF profile for NSSAAF.
func defaultNSSAAFProfile(nfInstanceID, status string) map[string]interface{} {
	return map[string]interface{}{
		"nfInstanceId":   nfInstanceID,
		"nfType":         NFTypeNSSAAF,
		"nfStatus":       status,
		"heartBeatTimer": 300,
		"load":           0,
		"plmnList": []map[string]interface{}{
			{"mcc": "001", "mnc": "01"},
		},
		"nssaafInfo": map[string]interface{}{
			"supiRanges": []map[string]interface{}{
				{
					"start":  "imsi-001010000000001",
					"end":    "imsi-001019999999999",
					"pattern": "^imsi-00101[0-9]{8}$",
					"size":   "LARGE",
				},
			},
		},
		"nfServices": map[string]interface{}{
			"nnssaaf-nssaa": map[string]interface{}{
				"serviceName": "nnssaaf-nssaa",
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

// mustMarshal serializes v to JSON. Panics on error (use only in test/handlers).
func mustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
