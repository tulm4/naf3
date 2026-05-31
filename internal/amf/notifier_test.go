package amf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/resilience"
	redisclient "github.com/operator/nssAAF/internal/cache/redis"
	"github.com/stretchr/testify/assert"
)

func testCBCfg() config.CircuitBreakerConfig {
	return config.CircuitBreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  30 * time.Second,
		SuccessThreshold: 3,
	}
}

func testRetryCfg() resilience.RetryConfig {
	return resilience.RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    2 * time.Second,
	}
}

func TestSendReAuthNotification_Success(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		assert.Equal(t, "/notify/reauth", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	dlq := &mockDLQ{}

	client := NewClient(factory, cbRegistry, dlq, testCBCfg(), testRetryCfg())

	err := client.SendReAuthNotification(context.Background(), server.URL+"/notify/reauth", "auth-123", []byte(`{"reason":"expired"}`))
	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
	assert.Equal(t, int32(0), dlq.EnqueueCount.Load())
}

func TestSendReAuthNotification_RetryThenSuccess(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count < 2 {
			// First call fails, second succeeds
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	dlq := &mockDLQ{}

	client := NewClient(factory, cbRegistry, dlq, testCBCfg(), testRetryCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.SendReAuthNotification(ctx, server.URL, "auth-123", []byte(`{}`))
	assert.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load())
	assert.Equal(t, int32(0), dlq.EnqueueCount.Load()) // Succeeded after retry
}

func TestSendReAuthNotification_RetryExhausted_DLQEnqueued(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	dlq := &mockDLQ{}

	client := NewClient(factory, cbRegistry, dlq, testCBCfg(), testRetryCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := client.SendReAuthNotification(ctx, server.URL, "auth-456", []byte(`{}`))
	assert.NoError(t, err)                      // DLQ accepted, so no error returned
	assert.Equal(t, int32(3), callCount.Load()) // 3 retry attempts
	assert.Equal(t, int32(1), dlq.EnqueueCount.Load())
}

func TestSendRevocationNotification_Success(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	dlq := &mockDLQ{}

	client := NewClient(factory, cbRegistry, dlq, testCBCfg(), testRetryCfg())

	err := client.SendRevocationNotification(context.Background(), server.URL, "auth-789", []byte(`{"reason":"policy_change"}`))
	assert.NoError(t, err)
	assert.Equal(t, int32(1), callCount.Load())
	assert.Equal(t, int32(0), dlq.EnqueueCount.Load())
}

func TestSendRevocationNotification_RetryExhausted_DLQEnqueued(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	dlq := &mockDLQ{}

	client := NewClient(factory, cbRegistry, dlq, testCBCfg(), testRetryCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := client.SendRevocationNotification(ctx, server.URL, "auth-999", []byte(`{}`))
	assert.NoError(t, err) // DLQ accepted
	assert.Equal(t, int32(3), callCount.Load())
	assert.Equal(t, int32(1), dlq.EnqueueCount.Load())
}

func TestSendNotification_ClientError_StillRetried(t *testing.T) {
	// Note: 4xx responses are returned as errors from the HTTP layer but are still
	// retried by resilience.Do. The retry logic only distinguishes by checking
	// resp.StatusCode >= 500 for failure recording. 4xx is treated as a regular
	// error, so resilience.Do will retry it MaxAttempts times.
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := nfclient.NewFactory(cbRegistry)
	dlq := &mockDLQ{}

	client := NewClient(factory, cbRegistry, dlq, testCBCfg(), testRetryCfg())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := client.SendReAuthNotification(ctx, server.URL, "auth-bad", []byte(`{}`))
	assert.NoError(t, err)                             // DLQ accepted, no error returned
	assert.Equal(t, int32(3), callCount.Load())        // 3 retry attempts
	assert.Equal(t, int32(1), dlq.EnqueueCount.Load()) // DLQ enqueued after retries
}

func TestExtractBaseURLAndPath(t *testing.T) {
	tests := []struct {
		uri      string
		baseURL  string
		path     string
		hasError bool
	}{
		{"http://amf:8080/notify", "http://amf:8080", "/notify", false},
		{"http://10.0.0.1:9090/path", "http://10.0.0.1:9090", "/path", false},
		{"http://host:80/", "http://host:80", "/", false},
		{"http://host/", "http://host", "/", false},
		{"http://192.168.1.1:8080/n62/notify", "http://192.168.1.1:8080", "/n62/notify", false},
		{"", "", "", true},
		{"no-scheme", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			baseURL, path, err := extractBaseURLAndPath(tt.uri)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.baseURL, baseURL)
				assert.Equal(t, tt.path, path)
			}
		})
	}
}

// mockDLQ is a test double for the DLQ interface.
type mockDLQ struct {
	EnqueueCount atomic.Int32
	LastItem     atomic.Value // stores *redisclient.AMFDLQItem
}

func (m *mockDLQ) Enqueue(ctx context.Context, item interface{}) error {
	m.EnqueueCount.Add(1)
	if data, err := json.Marshal(item); err == nil {
		var dlqItem redisclient.AMFDLQItem
		if json.Unmarshal(data, &dlqItem) == nil {
			m.LastItem.Store(&dlqItem)
		}
	}
	return nil
}
