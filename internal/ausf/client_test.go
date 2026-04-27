package ausf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	cfg := config.AUSFConfig{
		BaseURL: "http://ausf.operator.com:8080",
		Timeout: 10 * time.Second,
	}

	client := NewClient(cfg)

	assert.NotNil(t, client)
	assert.Equal(t, "http://ausf.operator.com:8080", client.baseURL)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, 10*time.Second, client.httpClient.Timeout)
}

func TestNewClient_Defaults(t *testing.T) {
	cfg := config.AUSFConfig{}

	client := NewClient(cfg)

	assert.NotNil(t, client)
	assert.Empty(t, client.baseURL)
	assert.NotNil(t, client.httpClient)
}

func TestForwardMSK_Success(t *testing.T) {
	var callCount atomic.Int32
	var receivedBody struct {
		AuthCtxID string `json:"authCtxId"`
		MSK       []byte `json:"msk"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/nnssaaaf-aiw/v1/msk", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		err := json.NewDecoder(r.Body).Decode(&receivedBody)
		assert.NoError(t, err)
		assert.Equal(t, "auth-123", receivedBody.AuthCtxID)
		assert.Equal(t, []byte("test-msk-data"), receivedBody.MSK)

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewClient(config.AUSFConfig{BaseURL: server.URL})
	err := client.ForwardMSK(context.Background(), "auth-123", []byte("test-msk-data"))

	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
}

func TestForwardMSK_Error_Non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewClient(config.AUSFConfig{BaseURL: server.URL})
	err := client.ForwardMSK(context.Background(), "auth-123", []byte("test-msk"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestForwardMSK_Error_NotConfigured(t *testing.T) {
	client := NewClient(config.AUSFConfig{})
	err := client.ForwardMSK(context.Background(), "auth-123", []byte("test-msk"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "baseURL not configured")
}

func TestForwardMSK_Error_ConnectionRefused(t *testing.T) {
	// Use an address that will fail to connect
	client := NewClient(config.AUSFConfig{BaseURL: "http://localhost:19999"})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.ForwardMSK(ctx, "auth-123", []byte("test-msk"))

	assert.Error(t, err)
}

func TestForwardMSK_ServerReturns404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client := NewClient(config.AUSFConfig{BaseURL: server.URL})
	err := client.ForwardMSK(context.Background(), "auth-nonexistent", []byte("test-msk"))

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestForwardMSK_MultipleCalls(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewClient(config.AUSFConfig{BaseURL: server.URL})

	for i := 0; i < 5; i++ {
		err := client.ForwardMSK(context.Background(), "auth-"+string(rune('0'+i)), []byte("msk-data"))
		assert.NoError(t, err)
	}

	assert.Equal(t, int32(5), callCount.Load())
}
