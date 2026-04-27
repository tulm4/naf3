package amf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/resilience"
	"github.com/stretchr/testify/assert"
)

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
	dlq := &mockDLQ{}

	client := NewClient(5*time.Second, cbRegistry, dlq)

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
	dlq := &mockDLQ{}

	client := NewClient(2*time.Second, cbRegistry, dlq)

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
	dlq := &mockDLQ{}

	client := NewClient(1*time.Second, cbRegistry, dlq)

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
	dlq := &mockDLQ{}

	client := NewClient(5*time.Second, cbRegistry, dlq)

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
	dlq := &mockDLQ{}

	client := NewClient(1*time.Second, cbRegistry, dlq)

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
	dlq := &mockDLQ{}

	client := NewClient(1*time.Second, cbRegistry, dlq)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := client.SendReAuthNotification(ctx, server.URL, "auth-bad", []byte(`{}`))
	assert.NoError(t, err)                             // DLQ accepted, no error returned
	assert.Equal(t, int32(3), callCount.Load())        // 3 retry attempts
	assert.Equal(t, int32(1), dlq.EnqueueCount.Load()) // DLQ enqueued after retries
}

func TestExtractHostPort(t *testing.T) {
	tests := []struct {
		uri      string
		expected string
	}{
		{"http://amf:8080/notify", "amf:8080"},
		{"http://10.0.0.1:9090/path", "10.0.0.1:9090"},
		{"http://host:80/", "host:80"},
		{"http://host/", "host"},
		{"http://192.168.1.1:8080/n62/notify", "192.168.1.1:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			result := extractHostPort(tt.uri)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockDLQ is a test double for the DLQ interface.
type mockDLQ struct {
	EnqueueCount atomic.Int32
	LastItem     atomic.Value // stores *DLQItem
}

func (m *mockDLQ) Enqueue(ctx context.Context, item interface{}) error {
	m.EnqueueCount.Add(1)
	if data, err := json.Marshal(item); err == nil {
		var dlqItem DLQItem
		if json.Unmarshal(data, &dlqItem) == nil {
			m.LastItem.Store(&dlqItem)
		}
	}
	return nil
}
