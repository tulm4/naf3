// Package e2e_amf provides AMF notification unit tests.
// Spec: TS 23.502 §4.2.9.3, REQ-06, REQ-07, REQ-10
package e2e_amf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/amf"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDLQ is a test double for the AMF notification DLQ.
type mockDLQ struct {
	EnqueueCount atomic.Int32
	LastItem    atomic.Value
}

func (m *mockDLQ) Enqueue(ctx context.Context, item interface{}) error {
	m.EnqueueCount.Add(1)
	if data, err := json.Marshal(item); err == nil {
		m.LastItem.Store(data)
	}
	return nil
}

// notifServer is a minimal httptest.Server implementing the AMF callback receiver.
type notifServer struct {
	*httptest.Server
	notifications []json.RawMessage
	mu            sync.Mutex
	failNext     bool
	errorCode    int
}

func newNotifServer() *notifServer {
	s := &notifServer{notifications: make([]json.RawMessage, 0), errorCode: http.StatusServiceUnavailable}
	mux := http.NewServeMux()
	mux.HandleFunc("/namf-callback/v1/", s.handle)
	s.Server = httptest.NewServer(mux)
	return s
}

func (s *notifServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"cause":"METHOD_NOT_ALLOWED"}`, http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	fail := s.failNext
	errCode := s.errorCode
	if fail {
		s.failNext = false
	}
	s.mu.Unlock()

	if fail {
		http.Error(w, `{"cause":"SERVICE_UNAVAILABLE"}`, errCode)
		return
	}

	var payload json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"cause":"INVALID_PAYLOAD"}`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.notifications = append(s.notifications, payload)
	s.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (s *notifServer) getNotifications() []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]json.RawMessage, len(s.notifications))
	copy(out, s.notifications)
	return out
}

// TestNotifier_ReAuthNotification verifies that SendReAuthNotification sends a
// reauth notification to the AMF callback URL.
func TestNotifier_ReAuthNotification(t *testing.T) {
	srv := newNotifServer()
	defer srv.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	dlq := &mockDLQ{}
	client := amf.NewClient(5*time.Second, cbRegistry, dlq)

	payload := []byte(`{"notificationType":"SLICE_RE_AUTH","reason":"expired"}`)
	err := client.SendReAuthNotification(context.Background(), srv.URL+"/namf-callback/v1/", "auth-ctx-reauth-001", payload)
	require.NoError(t, err)

	notifications := srv.getNotifications()
	require.Len(t, notifications, 1)
	assert.Equal(t, int32(0), dlq.EnqueueCount.Load(), "DLQ must not be invoked on success")
}

// TestNotifier_RevocationNotification verifies that SendRevocationNotification sends a
// revocation notification to the AMF callback URL.
func TestNotifier_RevocationNotification(t *testing.T) {
	srv := newNotifServer()
	defer srv.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	dlq := &mockDLQ{}
	client := amf.NewClient(5*time.Second, cbRegistry, dlq)

	payload := []byte(`{"notificationType":"SLICE_REVOCATION","reason":"policy_change"}`)
	err := client.SendRevocationNotification(context.Background(), srv.URL+"/namf-callback/v1/", "auth-ctx-revoc-001", payload)
	require.NoError(t, err)

	notifications := srv.getNotifications()
	require.Len(t, notifications, 1)
	assert.Equal(t, int32(0), dlq.EnqueueCount.Load())
}

// TestNotifier_RetryOnFailure verifies that the notifier retries on HTTP 503 and
// succeeds after one retry.
func TestNotifier_RetryOnFailure(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)
		if count == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	dlq := &mockDLQ{}
	client := amf.NewClient(1*time.Second, cbRegistry, dlq)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.SendReAuthNotification(ctx, srv.URL, "auth-ctx-retry", []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, int32(2), callCount.Load(), "one retry expected after first 503")
	assert.Equal(t, int32(0), dlq.EnqueueCount.Load(), "DLQ must not be used when retry succeeds")
}

// TestNotifier_RetryExhausted verifies that after max retries are exhausted,
// the notification is enqueued to the DLQ.
func TestNotifier_RetryExhausted(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	dlq := &mockDLQ{}
	client := amf.NewClient(1*time.Second, cbRegistry, dlq)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := client.SendReAuthNotification(ctx, srv.URL, "auth-ctx-dlq", []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, int32(3), callCount.Load(), "max 3 retry attempts expected")
	assert.Equal(t, int32(1), dlq.EnqueueCount.Load(), "DLQ must be invoked after retries exhausted")
}
