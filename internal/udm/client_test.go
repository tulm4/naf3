package udm

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
	cfg := config.UDMConfig{
		BaseURL: "http://udm.operator.com:8080",
		Timeout: 10 * time.Second,
	}

	client := NewClient(cfg, nil)

	assert.NotNil(t, client)
	assert.Equal(t, "http://udm.operator.com:8080", client.baseURL)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, 10*time.Second, client.httpClient.Timeout)
}

func TestNewClient_Defaults(t *testing.T) {
	cfg := config.UDMConfig{}

	client := NewClient(cfg, nil)

	assert.NotNil(t, client)
	assert.Equal(t, "", client.baseURL)
	assert.NotNil(t, client.httpClient)
}

func TestGetAuthContext_Success(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		assert.Equal(t, "/nudm-uem/v1/subscribers/imu-208001000000000/auth-contexts", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		resp := map[string]interface{}{
			"authContexts": []map[string]string{
				{"authType": "EAP_TLS", "aaaServer": "radius://aaa.operator.com:1812"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imu-208001000000000")

	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
	sub, ok := result.(*AuthSubscription)
	assert.True(t, ok)
	assert.Equal(t, "EAP_TLS", sub.AuthType)
	assert.Equal(t, "radius://aaa.operator.com:1812", sub.AAAServer)
}

func TestGetAuthContext_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imu-999999999999999")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetAuthContext_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imu-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestGetAuthContext_EmptyAuthContexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"authContexts": []map[string]string{},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imu-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no auth contexts found")
}

func TestGetAuthContext_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imu-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "decode response")
}

func TestGetAuthContext_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := client.GetAuthContext(ctx, "imu-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestGetAuthContext_NRFDiscovery(t *testing.T) {
	// When BaseURL is empty, client should fall back to NRF discovery
	var callCount atomic.Int32

	nrfServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		// NRF discovery returns UDM endpoint
		resp := map[string]interface{}{
			"nfInstances": []map[string]interface{}{
				{
					"nfInstanceId": "udm-1",
					"nfServices": []map[string]interface{}{
						{
							"serviceName": "nudm-uem",
							"ipEndPoints": []map[string]string{
								{"ipv4Address": "10.60.0.5", "port": "8080"},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer nrfServer.Close()

	// We can't easily mock NRF here since the client calls nrfClient.DiscoverUDM.
	// Instead, test that with an empty baseURL and nil nrfClient, we get a clear error.
	cfg := config.UDMConfig{BaseURL: "", Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	result, err := client.GetAuthContext(ctx, "imu-208001000000000")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unsupported protocol scheme")
}

func TestUpdateAuthContext_Success(t *testing.T) {
	var callCount atomic.Int32
	var reqBody atomic.Value

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		assert.Equal(t, "/nudm-uem/v1/subscribers/imu-208001000000000/auth-contexts/auth-123", r.URL.Path)
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			reqBody.Store(body)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	err := client.UpdateAuthContext(ctx, "imu-208001000000000", "auth-123", "EAP_SUCCESS")

	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())

	body, ok := reqBody.Load().(map[string]string)
	assert.True(t, ok)
	assert.Equal(t, "EAP_SUCCESS", body["authResult"])
}

func TestUpdateAuthContext_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	err := client.UpdateAuthContext(ctx, "imu-208001000000000", "auth-456", "EAP_FAILURE")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update status 400")
}

func TestUpdateAuthContext_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.UDMConfig{BaseURL: server.URL, Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.UpdateAuthContext(ctx, "imu-208001000000000", "auth-789", "EAP_SUCCESS")

	assert.Error(t, err)
}

func TestUpdateAuthContext_NRFDiscovery(t *testing.T) {
	// When BaseURL is empty and nrfClient is nil, should get a clear error
	cfg := config.UDMConfig{BaseURL: "", Timeout: 5 * time.Second}
	client := NewClient(cfg, nil)

	ctx := context.Background()
	err := client.UpdateAuthContext(ctx, "imu-208001000000000", "auth-xyz", "EAP_SUCCESS")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported protocol scheme")
}

func TestExtractPLMNFromSupi(t *testing.T) {
	tests := []struct {
		supi     string
		expected string
	}{
		{"imu-208001000000000", "208001"},
		{"imu-440010123456789", "440010"},
		{"imu-310410999999999", "310410"},
		{"imu-12345", "208001"},  // too short → default
		{"", "208001"},           // empty → default
		{"imu-208", "208001"},    // just enough for "imu-" + MCC = 7 chars → "208001"
		{"imu-208001", "208001"}, // "imu-"(4) + "208001"(6) = 10 → matches
	}

	for _, tt := range tests {
		t.Run(tt.supi, func(t *testing.T) {
			result := extractPLMNFromSupi(tt.supi)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAuthSubscription_JSON(t *testing.T) {
	// Verify AuthSubscription marshals/unmarshals correctly
	sub := AuthSubscription{
		AuthType:  "EAP_AKA_PRIME",
		AAAServer: "radius://aaa.operator.com:1813",
	}

	data, err := json.Marshal(sub)
	assert.NoError(t, err)

	var decoded AuthSubscription
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, sub.AuthType, decoded.AuthType)
	assert.Equal(t, sub.AAAServer, decoded.AAAServer)
}

// Verify interface compatibility with nssaa.WithUDMClient.
// nssaa.WithUDMClient requires GetAuthContext(context.Context, string) (interface{}, error).
var _ interface {
	GetAuthContext(context.Context, string) (interface{}, error)
} = (*Client)(nil)

// Verify interface compatibility for UpdateAuthContext.
var _ interface {
	UpdateAuthContext(context.Context, string, string, string) error
} = (*Client)(nil)
