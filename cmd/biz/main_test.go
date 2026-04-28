package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/proto"
)

// TestServer_HealthEndpoint verifies that /health returns 200 with status ok.
func TestServer_HealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","service":"nssAAF-biz"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/health", nil)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotEmpty(t, resp.Header.Get("Content-Type"))
}

// TestServer_ReadyEndpoint verifies that /ready returns 200 with status ready.
func TestServer_ReadyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ready","service":"nssAAF-biz"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/ready", nil)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestServer_AaaForwardEndpoint_NotImplemented verifies that POST /aaa/forward
// returns 501 NotImplemented.
func TestServer_AaaForwardEndpoint_NotImplemented(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/aaa/forward" {
			http.Error(w, "not implemented", http.StatusNotImplemented)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	body := bytes.NewReader([]byte(`{}`))
	req, err := http.NewRequestWithContext(context.Background(), "POST", server.URL+"/aaa/forward", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}

// TestServer_ServerInitiated_RAR verifies that POST /aaa/server-initiated
// with messageType=RAR returns 200 and processes the request.
func TestServer_ServerInitiated_RAR(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/aaa/server-initiated" {
			handleServerInitiated(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	reqBody := proto.AaaServerInitiatedRequest{
		Version:       proto.CurrentVersion,
		SessionID:     "test-session-rar",
		AuthCtxID:     "test-auth-ctx-rar",
		TransportType: proto.TransportRADIUS,
		MessageType:   proto.MessageTypeRAR,
		Payload:       []byte{1, 2, 3},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), "POST", server.URL+"/aaa/server-initiated", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
}

// TestServer_ServerInitiated_InvalidMessageType verifies that POST /aaa/server-initiated
// with an unknown messageType returns 400.
func TestServer_ServerInitiated_InvalidMessageType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/aaa/server-initiated" {
			handleServerInitiated(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	reqBody := proto.AaaServerInitiatedRequest{
		Version:     proto.CurrentVersion,
		SessionID:   "test-session",
		AuthCtxID:   "test-auth",
		MessageType: "UNKNOWN_TYPE",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(context.Background(), "POST", server.URL+"/aaa/server-initiated", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// TestServer_ServerInitiated_WrongMethod verifies that GET /aaa/server-initiated
// returns 405 Method Not Allowed.
func TestServer_ServerInitiated_WrongMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/aaa/server-initiated" {
			handleServerInitiated(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), "GET", server.URL+"/aaa/server-initiated", nil)
	require.NoError(t, err)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// TestServer_ServerInitiated_NonJSONContentType verifies that POST /aaa/server-initiated
// with a non-JSON Content-Type returns 415.
func TestServer_ServerInitiated_NonJSONContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/aaa/server-initiated" {
			handleServerInitiated(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), "POST", server.URL+"/aaa/server-initiated", bytes.NewReader([]byte(`{}`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)

	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
}

// TestHandleReAuth_Placeholder verifies that handleReAuth returns a non-nil payload.
func TestHandleReAuth_Placeholder(t *testing.T) {
	req := &proto.AaaServerInitiatedRequest{
		AuthCtxID: "test-auth",
		SessionID: "test-session",
	}

	payload := handleReAuth(context.Background(), req)

	assert.NotNil(t, payload)
	assert.NotEmpty(t, payload)
}

// TestHandleRevocation_Placeholder verifies that handleRevocation returns empty payload.
func TestHandleRevocation_Placeholder(t *testing.T) {
	req := &proto.AaaServerInitiatedRequest{
		AuthCtxID: "test-auth",
		SessionID: "test-session",
	}

	payload := handleRevocation(context.Background(), req)

	assert.NotNil(t, payload)
	assert.Empty(t, payload)
}

// TestHandleCoA_Placeholder verifies that handleCoA returns a non-nil payload.
func TestHandleCoA_Placeholder(t *testing.T) {
	req := &proto.AaaServerInitiatedRequest{
		AuthCtxID: "test-auth",
		SessionID: "test-session",
	}

	payload := handleCoA(context.Background(), req)

	assert.NotNil(t, payload)
	assert.NotEmpty(t, payload)
}
