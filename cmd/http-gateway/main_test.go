package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHttpGateway_ForwardsRequests verifies that the http-gateway forwards
// requests to the Biz Pod and returns the response.
func TestHttpGateway_ForwardsRequests(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer bizServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, bizServer.URL, nil)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestHttpGateway_ForwardRequest_Success verifies httpBizClient.ForwardRequest
// successfully forwards a request and returns the response body and status.
func TestHttpGateway_ForwardRequest_Success(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/test/path", r.URL.Path)
		assert.NotEmpty(t, r.Header.Get("Content-Type"))
		assert.NotEmpty(t, r.Header.Get("X-NSSAAF-Version"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer bizServer.Close()

	client := &httpBizClient{
		bizServiceURL: bizServer.URL,
		httpClient:    &http.Client{},
		version:       "1.0.0",
	}

	body, status, err := client.ForwardRequest(
		context.Background(),
		"/test/path",
		"POST",
		[]byte(`{"key":"value"}`),
	)

	assert.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Equal(t, `{"result":"success"}`, string(body))
}

// TestHttpGateway_ForwardRequest_502OnBizError verifies that ForwardRequest
// returns status 502 when the Biz Pod is unreachable.
func TestHttpGateway_ForwardRequest_502OnBizError(t *testing.T) {
	client := &httpBizClient{
		bizServiceURL: "http://localhost:1",
		httpClient:    &http.Client{},
		version:       "1.0.0",
	}

	_, status, err := client.ForwardRequest(
		context.Background(),
		"/test",
		"GET",
		nil,
	)

	assert.Error(t, err)
	assert.Equal(t, 502, status)
}

// TestHttpGateway_ForwardRequest_503OnTimeout verifies that ForwardRequest
// returns status 503 when the request times out.
func TestHttpGateway_ForwardRequest_503OnTimeout(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a slow response that exceeds client timeout
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer bizServer.Close()

	client := &httpBizClient{
		bizServiceURL: bizServer.URL,
		httpClient:    &http.Client{},
		version:       "1.0.0",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, status, err := client.ForwardRequest(
		ctx,
		"/test",
		"GET",
		nil,
	)

	assert.Error(t, err)
	assert.Equal(t, 503, status)
}

// TestHttpGateway_SetsXVersionHeader verifies that ForwardRequest sets
// the X-NSSAAF-Version header on outgoing requests.
func TestHttpGateway_SetsXVersionHeader(t *testing.T) {
	var receivedVersion string

	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedVersion = r.Header.Get("X-NSSAAF-Version")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer bizServer.Close()

	client := &httpBizClient{
		bizServiceURL: bizServer.URL,
		httpClient:    &http.Client{},
		version:       "2.0.0",
	}

	_, _, err := client.ForwardRequest(context.Background(), "/path", "GET", nil)

	assert.NoError(t, err)
	assert.Equal(t, "2.0.0", receivedVersion)
}
