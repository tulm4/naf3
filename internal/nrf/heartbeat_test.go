package nrf

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
)

// testableClient implements HeartbeatClient for testing.
type testableClient struct {
	baseURL             string
	registerCalls       atomic.Int32
	heartbeatCalls      atomic.Int32
	heartbeatInterval   time.Duration
	shouldFailRegister  bool
	shouldFailHeartbeat bool

	// function-field overrides let tests intercept behavior without
	// redefining the methods themselves.
	registerFn  func(ctx context.Context, profile *NFProfile) (time.Duration, string, error)
	heartbeatFn func(ctx context.Context, instanceID, etag string) (string, error)
}

func (c *testableClient) Register(ctx context.Context, profile *NFProfile) (time.Duration, string, error) {
	c.registerCalls.Add(1)
	if c.registerFn != nil {
		return c.registerFn(ctx, profile)
	}
	if c.shouldFailRegister {
		return 0, "", context.DeadlineExceeded
	}
	return c.heartbeatInterval, `"etag-1"`, nil
}

func (c *testableClient) Heartbeat(ctx context.Context, instanceID, etag string) (string, error) {
	c.heartbeatCalls.Add(1)
	if c.heartbeatFn != nil {
		return c.heartbeatFn(ctx, instanceID, etag)
	}
	if c.shouldFailHeartbeat {
		return "", context.DeadlineExceeded
	}
	return `"etag-2"`, nil
}

func (c *testableClient) Deregister(ctx context.Context, instanceID string) error {
	return nil
}

func TestHeartbeatManagerStart(t *testing.T) {
	client := &testableClient{
		baseURL:           "http://test",
		heartbeatInterval: 50 * time.Millisecond,
	}

	cfg := config.HeartbeatConfig{
		InitialInterval:         50 * time.Millisecond,
		MaxConsecutiveFailures:  3,
	}

	mgr := NewHeartbeatManager(client, "test-id", cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	mgr.Stop()

	if client.registerCalls.Load() < 1 {
		t.Errorf("expected at least 1 registration call, got %d", client.registerCalls.Load())
	}

	if client.heartbeatCalls.Load() < 1 {
		t.Errorf("expected at least 1 heartbeat call, got %d", client.heartbeatCalls.Load())
	}
}

func TestHeartbeatManagerReRegistration(t *testing.T) {
	shouldFail := atomic.Bool{}
	shouldFail.Store(true)

	client := &testableClient{
		baseURL:           "http://test",
		heartbeatInterval: 50 * time.Millisecond,
	}
	client.registerFn = func(ctx context.Context, profile *NFProfile) (time.Duration, string, error) {
		if shouldFail.Load() {
			return 0, "", context.DeadlineExceeded
		}
		return client.heartbeatInterval, `"etag-1"`, nil
	}

	cfg := config.HeartbeatConfig{
		InitialInterval:        50 * time.Millisecond,
		MaxConsecutiveFailures: 3,
	}

	mgr := NewHeartbeatManager(client, "test-id", cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	shouldFail.Store(false)

	time.Sleep(100 * time.Millisecond)
	mgr.Stop()

	if client.registerCalls.Load() < 2 {
		t.Errorf("expected at least 2 registration calls (initial + re-registration), got %d", client.registerCalls.Load())
	}
}
