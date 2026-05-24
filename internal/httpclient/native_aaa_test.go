package httpclient

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
)

func TestNativeAAAClient_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/aaa/forward" {
			t.Errorf("expected /aaa/forward, got: %s", r.URL.Path)
		}

		resp := &proto.AaaForwardResponse{
			Version:   "1.0",
			SessionID: "test-session",
			AuthCtxID: "test-auth",
			Payload:   []byte(`eap-response`),
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	logger := slog.Default()
	client := NewNativeAAAClient(server.URL, cfg, logger)

	req := &proto.AaaForwardRequest{
		Version:   "1.0",
		SessionID: "test-session",
		AuthCtxID: "test-auth",
		Payload:   []byte(`eap-payload`),
	}

	resp, err := client.ForwardEAP(context.Background(), req)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if string(resp.Payload) != "eap-response" {
		t.Fatalf("expected 'eap-response', got: %s", string(resp.Payload))
	}
}

func TestNativeAAAClient_StrictRetry(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.NativeCommConfig{}
	logger := slog.Default()
	client := NewNativeAAAClient(server.URL, cfg, logger)

	req := &proto.AaaForwardRequest{
		Version:   "1.0",
		SessionID: "test-session",
		AuthCtxID: "test-auth",
	}

	_, err := client.ForwardEAP(context.Background(), req)

	if err == nil {
		t.Fatal("expected error after max retries")
	}
	// AAA client retries 2 times max (not 3 like Biz client)
	if attempt != 2 {
		t.Fatalf("expected 2 attempts for AAA client (MaxAttempts=2), got: %d", attempt)
	}
}
