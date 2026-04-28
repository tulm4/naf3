package nrf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	cfg := config.NRFConfig{
		BaseURL:         "http://nrf:8080",
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	assert.NotNil(t, client)
	assert.Equal(t, "http://nrf:8080", client.baseURL)
	assert.NotNil(t, client.httpClient)
	assert.NotNil(t, client.cache)
	assert.False(t, client.IsRegistered())
}

func TestClient_Register_Success(t *testing.T) {
	var receivedBody NFProfile
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/nnrf-disc/v1/nf-instances", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		err := json.NewDecoder(r.Body).Decode(&receivedBody)
		require.NoError(t, err)
		assert.Equal(t, "NSSAAF", receivedBody.NFType)
		assert.Equal(t, "REGISTERED", receivedBody.NFStatus)
		assert.Equal(t, 300, receivedBody.HeartBeatTimer)

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	err := client.Register(context.Background())
	require.NoError(t, err)
	assert.True(t, client.IsRegistered())
	assert.NotEmpty(t, receivedBody.NFInstanceID)
}

func TestClient_Register_NonCreatedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	err := client.Register(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
	assert.False(t, client.IsRegistered())
}

func TestClient_Heartbeat_Success(t *testing.T) {
	var receivedPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle both Register (POST) and Heartbeat (PUT)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/nnrf-disc/v1/nf-instances/")

		err := json.NewDecoder(r.Body).Decode(&receivedPayload)
		require.NoError(t, err)
		assert.Equal(t, "REGISTERED", receivedPayload["nfStatus"])
		assert.Equal(t, float64(300), receivedPayload["heartBeatTimer"])

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	// Register first
	_ = client.Register(context.Background())

	err := client.Heartbeat(context.Background())
	require.NoError(t, err)
	assert.Contains(t, receivedPayload, "nfInstanceId")
}

func TestClient_Heartbeat_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	_ = client.Register(context.Background())

	err := client.Heartbeat(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "heartbeat status")
}

func TestClient_DiscoverUDM_CacheHit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called on cache hit")
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	// Pre-populate cache
	cachedEndpoint := "http://udm:8080"
	client.cache.Set("udm:uem:00101", cachedEndpoint)

	endpoint, err := client.DiscoverUDM(context.Background(), "00101")
	require.NoError(t, err)
	assert.Equal(t, cachedEndpoint, endpoint)
}

func TestClient_DiscoverUDM_CacheMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.RawQuery, "target-nf-type=UDM")
		assert.Contains(t, r.URL.RawQuery, "service-names=nudm-uem")

		resp := map[string]interface{}{
			"nfInstances": []map[string]interface{}{
				{
					"nfServices": map[string]interface{}{
						"nudm-uem": map[string]interface{}{
							"ipEndPoints": []map[string]interface{}{
								{
									"ipv4Address": "192.168.1.100",
									"port":        8080,
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	endpoint, err := client.DiscoverUDM(context.Background(), "00101")
	require.NoError(t, err)
	assert.Equal(t, "http://192.168.1.100:8080", endpoint)

	// Verify it's cached
	cached, ok := client.cache.Get("udm:uem:00101")
	assert.True(t, ok)
	assert.Equal(t, endpoint, cached.(string))
}

func TestClient_DiscoverUDM_NoUDMFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"nfInstances": []map[string]interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	_, err := client.DiscoverUDM(context.Background(), "00101")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no UDM found")
}

func TestClient_DiscoverAMF_CacheHit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called on cache hit")
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	cachedID := "amf-instance-123"
	client.cache.Set("amf:amf-instance-123", cachedID)

	amfID, err := client.DiscoverAMF(context.Background(), "amf-instance-123")
	require.NoError(t, err)
	assert.Equal(t, cachedID, amfID)
}

func TestClient_DiscoverAMF_CacheMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/nnrf-disc/v1/nf-instances/amf-001")

		resp := map[string]interface{}{
			"nfInstanceId": "amf-001",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	amfID, err := client.DiscoverAMF(context.Background(), "amf-001")
	require.NoError(t, err)
	assert.Equal(t, "amf-001", amfID)
}

func TestClient_DiscoverAMF_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	_, err := client.DiscoverAMF(context.Background(), "unknown-amf")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "discover amf status")
}

func TestClient_Deregister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle both Register (POST) and Deregister (DELETE)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/nnrf-disc/v1/nf-instances/")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	// Register first
	_ = client.Register(context.Background())
	assert.True(t, client.IsRegistered())

	err := client.Deregister(context.Background())
	require.NoError(t, err)
	assert.False(t, client.IsRegistered())
}

func TestNRFDiscoveryCache_GetSet(t *testing.T) {
	cache := &NRFDiscoveryCache{ttl: 5 * time.Minute}

	// Test empty cache
	_, ok := cache.Get("key1")
	assert.False(t, ok)

	// Set and get
	cache.Set("key1", "value1")
	val, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)
}

func TestNRFDiscoveryCache_TTL(t *testing.T) {
	cache := &NRFDiscoveryCache{ttl: 50 * time.Millisecond}

	cache.Set("key1", "value1")

	// Should be available immediately
	val, ok := cache.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	_, ok = cache.Get("key1")
	assert.False(t, ok)
}

func TestClient_RegisterAsync_ReturnsImmediately(t *testing.T) {
	// Server that accepts registration
	registrationDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		close(registrationDone)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// RegisterAsync should return immediately
	client.RegisterAsync(ctx)

	// Wait for async registration to complete
	select {
	case <-registrationDone:
	case <-time.After(2 * time.Second):
		t.Fatal("async registration did not complete")
	}

	// Give time for registered.Store(true) to be called
	time.Sleep(10 * time.Millisecond)
	assert.True(t, client.IsRegistered())
}

func TestClient_RegisterAsync_RetryOnFailure(t *testing.T) {
	attempt := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempt++
		currentAttempt := attempt
		mu.Unlock()
		if currentAttempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client.RegisterAsync(ctx)

	// Wait for async registration to succeed after retry (base delay = 1s)
	time.Sleep(2500 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, client.IsRegistered(), "client should be registered after retry")
	assert.GreaterOrEqual(t, attempt, 2, "should have at least 2 attempts")
}
