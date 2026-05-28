package redis

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

	hc := &http.Client{Timeout: 1 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var done atomic.Bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		dlq.Process(ctx, hc, func() { done.Store(true) })
	}()

	wg.Wait()   // Wait for goroutine to start
	dlq.Done()  // Wait for goroutine to finish processing

	assert.True(t, done.Load(), "onDone should be called after exhaustion")

	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length, "exhausted item should be discarded")
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

	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
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

	hc := &http.Client{Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var done atomic.Bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		dlq.Process(ctx, hc, func() { done.Store(true) })
	}()

	wg.Wait()   // Wait for goroutine to start
	dlq.Done()  // Wait for goroutine to finish processing

	assert.True(t, done.Load(), "onDone should be called after delivery")
	assert.Equal(t, int32(1), callCount.Load(), "AMF should receive exactly one delivery attempt")

	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), length, "delivered item should be removed from queue")
}

func TestDLQ_Process_ReenqueueOnFailure(t *testing.T) {
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

	item := &AMFDLQItem{
		ID:          "fail-1",
		Type:        "SLICE_REVOCATION",
		URI:         server.URL,
		AuthCtxID:   "auth-789",
		Attempt:     0,
		MaxAttempts: 5,
		Payload:     []byte(`{}`),
		CreatedAt:   time.Now(),
	}
	err = dlq.Enqueue(context.Background(), item)
	require.NoError(t, err)

	hc := &http.Client{Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var done atomic.Bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		dlq.Process(ctx, hc, func() { done.Store(true) })
	}()

	wg.Wait()   // Wait for goroutine to start
	dlq.Done()  // Wait for goroutine to finish processing

	assert.True(t, done.Load(), "onDone should be called after re-enqueue")

	length, err := dlq.Len(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), length, "failed item should be re-enqueued")
}
