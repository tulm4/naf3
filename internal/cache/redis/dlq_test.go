package redis

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDLQ(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)
	assert.NotNil(t, dlq)
}

func TestDLQ_Enqueue(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	item := &AMFDLQItem{
		ID:        "test-123",
		Type:      "SLICE_RE_AUTH",
		URI:       "http://amf:8080/reauth",
		AuthCtxID: "auth-456",
		Attempt:   1,
		CreatedAt: time.Now(),
		LastError: "connection refused",
	}

	err = dlq.Enqueue(context.Background(), item)
	assert.NoError(t, err)
}

func TestDLQ_Len(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)

	for i := 0; i < 3; i++ {
		item := &AMFDLQItem{ID: "item-" + string(rune('a'+i))}
		enqErr := dlq.Enqueue(context.Background(), item)
		require.NoError(t, enqErr)
	}

	length, err = dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), length)
}

func TestDLQ_Dequeue(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	item := &AMFDLQItem{ID: "test-item", Type: "SLICE_RE_AUTH", AuthCtxID: "auth-1", Attempt: 1}
	err = dlq.Enqueue(context.Background(), item)
	require.NoError(t, err)

	got, err := dlq.Dequeue(context.Background(), 1*time.Second)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "test-item", got.ID)
	assert.Equal(t, 1, got.Attempt)

	got, err = dlq.Dequeue(context.Background(), 1*time.Second)
	require.NoError(t, err)
	assert.Nil(t, got, "should return nil when queue is empty and timeout expires")
}

func TestDLQ_DeliverToAMF_2xx(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	item := &AMFDLQItem{
		ID:      "deliver-1",
		Type:    "SLICE_RE_AUTH",
		URI:     server.URL,
		Payload: []byte(`{"event":"test"}`),
	}

	ok, err := dlq.deliverToAMF(context.Background(), &http.Client{Timeout: 5 * time.Second}, item)
	require.NoError(t, err)
	assert.True(t, ok, "2xx response should return ok=true")
}

func TestDLQ_DeliverToAMF_5xx(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	item := &AMFDLQItem{ID: "fail-1", Type: "SLICE_REVOCATION", URI: server.URL}

	ok, err := dlq.deliverToAMF(context.Background(), &http.Client{Timeout: 5 * time.Second}, item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-2xx status: 503")
	assert.False(t, ok, "5xx response should return ok=false")
}

func TestDLQ_DeliverToAMF_ConnectionRefused(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	item := &AMFDLQItem{ID: "fail-2", Type: "SLICE_REVOCATION", URI: "http://localhost:1/notify"}

	ok, err := dlq.deliverToAMF(context.Background(), &http.Client{Timeout: 1 * time.Second}, item)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "do request")
	assert.False(t, ok, "connection refused should return ok=false")
}

func TestDLQ_Process_Exhaustion(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	item := &AMFDLQItem{
		ID:          "exhaust-1",
		Type:        "SLICE_RE_AUTH",
		URI:         "http://amf:8080/notify",
		AuthCtxID:   "auth-123",
		Attempt:     3,
		MaxAttempts: 3,
		CreatedAt:   time.Now(),
	}
	err = dlq.Enqueue(context.Background(), item)
	require.NoError(t, err)

	// Start goroutine; it dequeues and discards immediately (attempt == max).
	// Give the goroutine time to process before calling Stop().
	hc := &http.Client{Timeout: 1 * time.Second}
	go dlq.Process(context.Background(), hc)
	time.Sleep(200 * time.Millisecond) // wait for goroutine to dequeue and discard item
	dlq.Stop()

	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length, "exhausted item should be discarded, not re-enqueued")
}

func TestDLQ_Process_DeliverySuccess(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	var callCount int
	var mu sync.Mutex
	var deliveredCh = make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		count := callCount
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		if count == 1 {
			close(deliveredCh) // signal that delivery occurred
		}
	}))
	defer server.Close()

	item := &AMFDLQItem{
		ID:          "deliver-1",
		Type:        "SLICE_RE_AUTH",
		URI:         server.URL,
		AuthCtxID:   "auth-456",
		Attempt:     0,
		MaxAttempts: 5,
		Payload:     []byte(`{}`),
		CreatedAt:   time.Now(),
	}
	err = dlq.Enqueue(context.Background(), item)
	require.NoError(t, err)

	// Start goroutine; it dequeues and delivers to AMF.
	// Wait for delivery to complete before calling Stop().
	hc := &http.Client{Timeout: 5 * time.Second}
	go dlq.Process(context.Background(), hc)
	select {
	case <-deliveredCh:
		// delivery confirmed
	case <-time.After(5 * time.Second):
		t.Fatal("AMF delivery timed out")
	}
	dlq.Stop()

	mu.Lock()
	count := callCount
	mu.Unlock()
	assert.Equal(t, 1, count, "AMF should receive exactly one delivery attempt")

	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length, "delivered item should be removed from queue")
}

func TestDLQ_Process_MultipleRetriesThenExhaustion(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	// One item in the queue with MaxAttempts=3. Process repeatedly dequeues,
	// fails delivery, and re-enqueues with incremented Attempt.
	// Delivery sequence: attempt 0 (fail, re-enqueue as 1), 1 (fail, re-enqueue as 2),
	// 2 (fail, re-enqueue as 3), then 3 >= 3 triggers discard. Total: 3 deliveries.
	// Total time: ~1s for the retry cycle + 1s for final BRPOP wait < 5s deadline.
	item := &AMFDLQItem{
		ID:          "fail-1",
		Type:        "SLICE_REVOCATION",
		URI:         server.URL,
		AuthCtxID:   "auth-789",
		Attempt:     0,
		MaxAttempts: 3,
		Payload:     []byte(`{}`),
		CreatedAt:   time.Now(),
	}
	err = dlq.Enqueue(context.Background(), item)
	require.NoError(t, err)

	hc := &http.Client{Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go dlq.Process(ctx, hc)
	<-ctx.Done()
	dlq.Done()

	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length, "all attempts exhausted, item should be discarded")
}

func TestDLQ_Process_MetadataPreservation(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	pool, err := NewPool(context.Background(), Config{
		Addrs:        []string{mr.Addr()},
		PoolSize:     10,
		MinIdleConns: 1,
		DialTimeout:  100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()

	dlq := NewDLQ(pool)

	var mu sync.Mutex
	var receivedAttempts []int
	var receivedMaxAttempts []int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		attemptStr := r.Header.Get("X-DLQ-Attempt")
		maxAttemptsStr := r.Header.Get("X-DLQ-MaxAttempts")
		attempt, _ := strconv.Atoi(attemptStr)
		maxAttempts, _ := strconv.Atoi(maxAttemptsStr)
		receivedAttempts = append(receivedAttempts, attempt)
		receivedMaxAttempts = append(receivedMaxAttempts, maxAttempts)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	item := &AMFDLQItem{
		ID:          "metadata-1",
		Type:        "SLICE_RE_AUTH",
		URI:         server.URL,
		AuthCtxID:   "auth-meta",
		Attempt:     0, // start at 0 so all 3 attempts (0,1,2) are delivered before exhaustion
		MaxAttempts: 3, // exhaustion check: 2 >= 3? NO → deliver; 3 >= 3? YES → discard
		Payload:     []byte(`{}`),
		CreatedAt:   time.Now(),
		LastError:   "previous error",
	}
	err = dlq.Enqueue(context.Background(), item)
	require.NoError(t, err)

	hc := &http.Client{Timeout: 5 * time.Second}

	go dlq.Process(context.Background(), hc)

	// Poll until 3 attempts are captured (attempts 0, 1, 2), with a generous safety timeout.
	// With Attempt=0, MaxAttempts=3: attempts 0,1 delivered; attempt 2 (3>=3) discarded.
	deadline := time.Now().Add(30 * time.Second)
	for {
		mu.Lock()
		done := len(receivedAttempts) >= 3
		mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			t.Logf("timeout: only %d attempts captured: %v", len(receivedAttempts), receivedAttempts)
			mu.Unlock()
			t.Fatal("DLQ processing did not complete in time")
		}
		time.Sleep(100 * time.Millisecond)
	}
	dlq.Stop()

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, receivedAttempts, 3, "should receive 3 delivery attempts")
	require.Len(t, receivedMaxAttempts, 3, "maxAttempts should be preserved across all retries")

	for i := 0; i < 3; i++ {
		assert.Equal(t, 3, receivedMaxAttempts[i], "MaxAttempts must be preserved on attempt %d", i+1)
	}
	// With Attempt=0, MaxAttempts=3 and >= exhaustion:
	// attempt 0 (0>=3? no) → deliver → re-enqueue as 1
	// attempt 1 (1>=3? no) → deliver → re-enqueue as 2
	// attempt 2 (2>=3? no) → deliver → re-enqueue as 3
	// attempt 3 (3>=3? yes) → discard (no delivery)
	assert.Equal(t, 0, receivedAttempts[0], "first attempt should be 0")
	assert.Equal(t, 1, receivedAttempts[1], "second attempt should be 1")
	assert.Equal(t, 2, receivedAttempts[2], "third attempt should be 2")

	// Verify item was eventually exhausted and removed from queue
	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length, "item should be exhausted and removed from queue after 3 retries")
}
