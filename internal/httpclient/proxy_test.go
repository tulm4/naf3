package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
)

func TestProxyClient_CallNRF(t *testing.T) {
	called := false
	gwServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/internal/nrf/v1/nf-instances" {
			t.Errorf("expected /internal/nrf/v1/nf-instances, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer gwServer.Close()

	client := NewProxyClient(gwServer.URL, config.RetryConfig{MaxAttempts: 1}, 10*time.Second)
	status, _, err := client.CallNRF(context.Background(), "GET", "/v1/nf-instances", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected status 200, got %d", status)
	}
	if !called {
		t.Error("expected proxy to be called")
	}
}

func TestProxyClient_CallUDM(t *testing.T) {
	called := false
	gwServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/internal/udm/v1/subscription-data/1234" {
			t.Errorf("expected /internal/udm/v1/subscription-data/1234, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer gwServer.Close()

	client := NewProxyClient(gwServer.URL, config.RetryConfig{MaxAttempts: 1}, 10*time.Second)
	status, _, err := client.CallUDM(context.Background(), "GET", "/v1/subscription-data/1234", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected status 200, got %d", status)
	}
	if !called {
		t.Error("expected proxy to be called")
	}
}

func TestProxyClient_CallAMF(t *testing.T) {
	called := false
	gwServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/internal/amf/v1/callback" {
			t.Errorf("expected /internal/amf/v1/callback, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer gwServer.Close()

	client := NewProxyClient(gwServer.URL, config.RetryConfig{MaxAttempts: 1}, 10*time.Second)
	status, _, err := client.CallAMF(context.Background(), "POST", "/v1/callback", []byte(`{}`))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected status 200, got %d", status)
	}
	if !called {
		t.Error("expected proxy to be called")
	}
}

func TestProxyClient_RetriesOn5xx(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewProxyClient(server.URL, config.RetryConfig{MaxAttempts: 3, BaseDelay: 1}, 10*time.Second)
	status, _, err := client.CallNRF(context.Background(), "GET", "/v1/test", nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected status 200, got %d", status)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestProxyClient_NoRetryOn4xx(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := NewProxyClient(server.URL, config.RetryConfig{MaxAttempts: 3, BaseDelay: 1}, 10*time.Second)
	status, _, _ := client.CallNRF(context.Background(), "GET", "/v1/test", nil)

	if status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", status)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", attempts)
	}
}
