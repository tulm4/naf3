package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
)

func TestNativeBizClient_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(server.URL, cfg)

	respBody, status, err := client.ForwardRequest(context.Background(), "/test", "POST", []byte(`{}`), "")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200, got: %d", status)
	}
	if len(respBody) == 0 {
		t.Fatal("expected non-empty response body")
	}
}

func TestNativeBizClient_RetryOn502(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(server.URL, cfg)

	respBody, status, err := client.ForwardRequest(context.Background(), "/test", "POST", []byte(`{}`), "")

	if err != nil {
		t.Fatalf("expected no error after retry, got: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status 200 after retry, got: %d", status)
	}
	if attempt < 2 {
		t.Fatalf("expected at least 2 attempts, got: %d", attempt)
	}
	_ = respBody // ignore unused variable
}

func TestNativeBizClient_NoRetryOn400(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(server.URL, cfg)

	_, status, err := client.ForwardRequest(context.Background(), "/test", "POST", []byte(`{}`), "")

	if err != nil {
		t.Fatalf("expected no error for 400, got: %v", err)
	}
	if status != http.StatusBadRequest {
		t.Fatalf("expected status 400, got: %d", status)
	}
	if attempt != 1 {
		t.Fatalf("expected exactly 1 attempt for 400, got: %d", attempt)
	}
}

func TestNativeBizClient_CircuitBreakerOpen(t *testing.T) {
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	cfg := config.NativeCommConfig{}
	client := newNativeBizClient(failServer.URL, cfg)

	// Trip the circuit breaker (5 failures)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		client.ForwardRequest(ctx, "/test", "POST", []byte(`{}`), "")
	}

	// Next request should be rejected by circuit breaker
	_, status, err := client.ForwardRequest(ctx, "/test", "POST", []byte(`{}`), "")

	if err == nil {
		t.Fatal("expected error when circuit breaker is open")
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 for circuit breaker open, got: %d", status)
	}
}

func TestNativeBizClient_UsesConfigurableTimeout(t *testing.T) {
	cfg := config.NativeCommConfig{
		Timeout: 5 * time.Second,
	}
	client := newNativeBizClient("http://localhost:9999", cfg)

	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", client.httpClient.Timeout)
	}
}
