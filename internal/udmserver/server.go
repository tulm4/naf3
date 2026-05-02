// Package udmserver provides a production-ready UDM mock server.
// Imported by cmd/udm-mock (containerized) and test/mocks/udm.go (httptest).
// Spec: TS 29.526 §7.2
package udmserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// AuthSubscription represents auth context returned by Nudm_UECM_Get for auth subscription.
type AuthSubscription struct {
	AuthType  string `json:"authType"`
	AAAServer string `json:"aaaServer"`
}

// AuthContextResponse is the response format for auth contexts endpoint.
type AuthContextResponse struct {
	AuthContexts []AuthSubscription `json:"authContexts"`
}

// Server is an HTTP server implementing the UDM Nudm_UECM API.
// Spec: TS 29.526 §7.2
type Server struct {
	httpSrv *http.Server

	mu sync.Mutex
	// registrations maps supi → registration data
	registrations map[string]*NudmUECMRegistration
	// errorCodes maps supi → HTTP status code for error injection
	errorCodes map[string]int
	// authSubscriptions maps supi → auth subscription data
	authSubscriptions map[string]*AuthSubscription
}

// NewServer creates a UDM server.
func NewServer() *Server {
	s := &Server{
		registrations:     make(map[string]*NudmUECMRegistration),
		errorCodes:        make(map[string]int),
		authSubscriptions: make(map[string]*AuthSubscription),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/nudm-uemm/v1/", s.handleRegistration)
	mux.HandleFunc("/nudm-uem/v1/subscribers/", s.handleAuthContexts)
	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// SetRegistration sets the registration data for a given SUPI.
func (s *Server) SetRegistration(supi string, reg *NudmUECMRegistration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrations[supi] = reg
}

// SetError configures an HTTP status code to return for a given SUPI.
// Useful for simulating timeouts (504) or other error conditions.
func (s *Server) SetError(supi string, statusCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorCodes[supi] = statusCode
}

// SetAuthSubscription configures auth subscription for a SUPI.
func (s *Server) SetAuthSubscription(supi, authType, aaaServer string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authSubscriptions[supi] = &AuthSubscription{
		AuthType:  authType,
		AAAServer: aaaServer,
	}
}

// SetGPSI sets the GPSI for a given SUPI, creating a default registration.
func (s *Server) SetGPSI(supi, gpsi string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrations[supi] = &NudmUECMRegistration{
		Supi: supi,
		GPSI: gpsi,
		Registrations: []NudmRegItem{
			{PlmnID: "00101", Legacy: false},
		},
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
	s.httpSrv.Handler.ServeHTTP(w, r)
}

// handleRegistration handles GET /nudm-uemm/v1/{supi}/registration.
func (s *Server) handleRegistration(w http.ResponseWriter, r *http.Request) {
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

	if !strings.HasPrefix(supi, "imsi-") {
		http.Error(w, `{"cause":"INVALID_SUPI"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	errorCode, hasError := s.errorCodes[supi]
	reg, ok := s.registrations[supi]
	s.mu.Unlock()

	if hasError {
		http.Error(w, fmt.Sprintf(`{"cause":"UDM_ERROR_%d"}`, errorCode), errorCode)
		return
	}

	if !ok {
		http.Error(w, fmt.Sprintf(`{"cause":"USER_NOT_FOUND","supi":"%s"}`, supi), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reg)
}

// handleAuthContexts handles GET /nudm-uem/v1/subscribers/{supi}/auth-contexts.
func (s *Server) handleAuthContexts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"cause":"METHOD_NOT_SUPPORTED"}`, http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/nudm-uem/v1/subscribers/")
	path = strings.TrimSuffix(path, "/auth-contexts")
	supi := strings.TrimSuffix(path, "/auth-contexts")
	supi = strings.Trim(supi, "/")

	if !strings.HasPrefix(supi, "imsi-") {
		http.Error(w, `{"cause":"INVALID_SUPI"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	errorCode, hasError := s.errorCodes[supi]
	authSub, ok := s.authSubscriptions[supi]
	s.mu.Unlock()

	if hasError {
		http.Error(w, fmt.Sprintf(`{"cause":"UDM_ERROR_%d"}`, errorCode), errorCode)
		return
	}

	if !ok {
		http.Error(w, `{"cause":"USER_NOT_FOUND","supi":"`+supi+`"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AuthContextResponse{
		AuthContexts: []AuthSubscription{*authSub},
	})
}
