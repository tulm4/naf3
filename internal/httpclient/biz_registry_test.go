package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
)

func TestBizRegistry_ForwardsToLivePod(t *testing.T) {
	pod1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer pod1.Close()

	registry := NewBizRegistry("localhost:9999", pod1.URL, config.NativeCommConfig{
		Retry:   config.RetryConfig{MaxAttempts: 1},
		Timeout: 5 * time.Second,
	}, nil)

	ctx := context.Background()
	body, status, err := registry.ForwardRequest(ctx, "/test", "GET", nil, "req-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("expected status 200, got %d", status)
	}
	if len(body) != 0 {
		t.Errorf("expected empty body for GET, got %d bytes", len(body))
	}
}

func TestBizRegistry_PropagatesRequestID(t *testing.T) {
	var receivedReqID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedReqID = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	registry := NewBizRegistry("localhost:9999", server.URL, config.NativeCommConfig{
		Retry: config.RetryConfig{MaxAttempts: 1},
	}, nil)

	ctx := context.Background()
	_, _, err := registry.ForwardRequest(ctx, "/test", "GET", nil, "my-request-id")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedReqID != "my-request-id" {
		t.Errorf("expected X-Request-ID 'my-request-id', got '%s'", receivedReqID)
	}
}
