package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/resilience"
)

func TestExtractProxyPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/internal/nrf/v1/nf-instances", "/v1/nf-instances"},
		{"/internal/udm/v1/subscription-data/1234", "/v1/subscription-data/1234"},
		{"/internal/amf/v1/callback", "/v1/callback"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractProxyPath(tt.input)
			if got != tt.expected {
				t.Errorf("extractProxyPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestProxyHandler_ProxiesToNRF(t *testing.T) {
	var receivedMethod, receivedPath string
	nrfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"nfInstances":[]}`))
	}))
	defer nrfServer.Close()

	h := NewProxyHandler(Config{
		NRFBaseURL: nrfServer.URL,
		Timeout:    10 * time.Second,
		RetryCfg:   resilience.RetryConfig{MaxAttempts: 1},
	})

	mux := http.NewServeMux()
	h.RegisterProxyRoutes(mux)

	req := httptest.NewRequest("GET", "/internal/nrf/nnrf-nfm/v1/nf-instances", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if receivedMethod != "GET" {
		t.Errorf("expected GET, got %s", receivedMethod)
	}
	if !strings.Contains(receivedPath, "/nnrf-nfm/v1/nf-instances") {
		t.Errorf("expected /nnrf-nfm/v1/nf-instances in path, got %s", receivedPath)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestProxyHandler_ProxiesToUDM(t *testing.T) {
	udmCalled := false
	udmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		udmCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer udmServer.Close()

	h := NewProxyHandler(Config{
		UDMBaseURL: udmServer.URL,
		Timeout:    10 * time.Second,
		RetryCfg:   resilience.RetryConfig{MaxAttempts: 1},
	})

	mux := http.NewServeMux()
	h.RegisterProxyRoutes(mux)

	req := httptest.NewRequest("POST", "/internal/udm/v1/subscription-data/123", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if !udmCalled {
		t.Error("expected UDM to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestProxyHandler_ProxiesToAMF(t *testing.T) {
	amfCalled := false
	amfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amfCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer amfServer.Close()

	h := NewProxyHandler(Config{
		AMFBaseURL: amfServer.URL,
		Timeout:    10 * time.Second,
		RetryCfg:   resilience.RetryConfig{MaxAttempts: 1},
	})

	mux := http.NewServeMux()
	h.RegisterProxyRoutes(mux)

	req := httptest.NewRequest("POST", "/internal/amf/v1/callback", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if !amfCalled {
		t.Error("expected AMF to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
