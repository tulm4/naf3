package main

import (
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

// TestSendEAP_Success verifies that SendEAP returns the EAP payload
// when the AAA Gateway responds with 200 OK.
func TestSendEAP_Success(t *testing.T) {
	expectedPayload := []byte{2, 0, 0, 4} // EAP Success

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/aaa/forward", r.URL.Path)
		assert.NotEmpty(t, r.Header.Get("Content-Type"))
		assert.NotEmpty(t, r.Header.Get(proto.HeaderName))

		var req proto.AaaForwardRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "auth-ctx-test", req.AuthCtxID)
		assert.Equal(t, proto.TransportRADIUS, req.TransportType)
		assert.Equal(t, proto.DirectionClientInitiated, req.Direction)

		resp := proto.AaaForwardResponse{
			Version:   proto.CurrentVersion,
			SessionID: req.SessionID,
			AuthCtxID: req.AuthCtxID,
			Payload:   expectedPayload,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newHTTPAAAClientForTest(
		server.URL,
		"test-pod",
		proto.CurrentVersion,
		&http.Client{Timeout: 5 * time.Second},
		nil, // no Redis in unit tests
	)
	defer func() { _ = c.Close() }()

	payload, err := c.SendEAP(context.Background(), "auth-ctx-test", []byte{1, 2, 3})

	require.NoError(t, err)
	assert.Equal(t, expectedPayload, payload)
}

// TestSendEAP_Non200Error verifies that SendEAP returns an error
// when the AAA Gateway responds with a non-200 status code.
func TestSendEAP_Non200Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "AAA server unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	c := newHTTPAAAClientForTest(
		server.URL,
		"test-pod",
		proto.CurrentVersion,
		&http.Client{Timeout: 5 * time.Second},
		nil,
	)
	defer func() { _ = c.Close() }()

	_, err := c.SendEAP(context.Background(), "auth-ctx-fail", []byte{1, 2, 3})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "aaa gateway returned 502")
}

// TestSendEAP_InvalidJSONResponse verifies that SendEAP returns an error
// when the response body is not valid JSON.
func TestSendEAP_InvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	c := newHTTPAAAClientForTest(
		server.URL,
		"test-pod",
		proto.CurrentVersion,
		&http.Client{Timeout: 5 * time.Second},
		nil,
	)
	defer func() { _ = c.Close() }()

	_, err := c.SendEAP(context.Background(), "auth-ctx-badjson", []byte{1, 2, 3})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
}

// TestSendEAP_BuildsSessionID verifies that SendEAP includes a session ID
// that starts with the "nssAAF;" prefix.
func TestSendEAP_BuildsSessionID(t *testing.T) {
	var receivedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req proto.AaaForwardRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedPayload = req.Payload

		resp := proto.AaaForwardResponse{
			Version:   proto.CurrentVersion,
			SessionID: req.SessionID,
			AuthCtxID: req.AuthCtxID,
			Payload:   []byte{2, 0, 0, 4},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newHTTPAAAClientForTest(
		server.URL,
		"test-pod",
		proto.CurrentVersion,
		&http.Client{Timeout: 5 * time.Second},
		nil,
	)
	defer func() { _ = c.Close() }()

	_, err := c.SendEAP(context.Background(), "auth-ctx-session", []byte{9, 8, 7})

	require.NoError(t, err)
	assert.Equal(t, []byte{9, 8, 7}, receivedPayload)
}

// TestSendEAP_PassesXVersionHeader verifies that the X-NSSAAF-Version header
// is set on the outgoing request.
func TestSendEAP_PassesXVersionHeader(t *testing.T) {
	var receivedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get(proto.HeaderName)

		resp := proto.AaaForwardResponse{
			Version:   proto.CurrentVersion,
			SessionID: "sess",
			AuthCtxID: "auth",
			Payload:   []byte{2, 0, 0, 4},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newHTTPAAAClientForTest(
		server.URL,
		"test-pod",
		"1.2.3",
		&http.Client{Timeout: 5 * time.Second},
		nil,
	)
	defer func() { _ = c.Close() }()

	_, err := c.SendEAP(context.Background(), "auth-ctx", []byte{1})

	require.NoError(t, err)
	assert.Equal(t, "1.2.3", receivedHeader)
}
