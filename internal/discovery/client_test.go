package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/operator/nssAAF/internal/nrf"
)

func TestHTTPRemoteDiscoveryClient_FindNF(t *testing.T) {
	t.Run("returns NF profile on 200 OK", func(t *testing.T) {
		profile := &nrf.NFProfile{
			NFInstanceID: "test-udm-instance",
			NFType:       nrf.NFTypeUDM,
			NFStatus:     nrf.NFStatusRegistered,
			FQDN:         "udm.operator.com",
		}

		var receivedMethod, receivedPath string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			receivedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(profile)
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		result, err := client.FindNF(context.Background(), "UDM")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.NFInstanceID != "test-udm-instance" {
			t.Errorf("NFInstanceID = %q, want %q", result.NFInstanceID, "test-udm-instance")
		}
		if receivedMethod != http.MethodGet {
			t.Errorf("method = %q, want %q", receivedMethod, http.MethodGet)
		}
		if receivedPath != "/internal/nf-discovery/UDM" {
			t.Errorf("path = %q, want %q", receivedPath, "/internal/nf-discovery/UDM")
		}
	})

	t.Run("returns error on 404 Not Found", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"title":  "NF_NOT_FOUND",
				"detail": "No serving UDM found in NRF",
			})
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		_, err := client.FindNF(context.Background(), "UDM")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		_, err := client.FindNF(context.Background(), "UDM")

		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {}
		}))
		defer ts.Close()

		client := NewClient(ts.URL)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := client.FindNF(ctx, "UDM")
		if err == nil {
			t.Fatal("expected error from cancelled context, got nil")
		}
	})
}