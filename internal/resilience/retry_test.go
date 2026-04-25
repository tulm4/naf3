package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetry_Do_FirstAttemptSucceeds(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
	}
	callCount := 0

	err := Do(ctx, cfg, func() error {
		callCount++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount, "should succeed on first attempt without retry")
}

func TestRetry_Do_AllAttemptsFail(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}
	callCount := 0

	err := Do(ctx, cfg, func() error {
		callCount++
		return errors.New("test error")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max retries exceeded")
	assert.Equal(t, 3, callCount, "should attempt 3 times")
}

func TestRetry_Do_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   50 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
	}
	callCount := 0

	// Cancel context after 1 attempt
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, cfg, func() error {
		callCount++
		return errors.New("test error")
	})

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 1, callCount, "should stop after context cancellation")
}

func TestRetry_Do_ExponentialBackoff(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   50 * time.Millisecond,
		MaxDelay:    200 * time.Millisecond,
	}
	callTimes := make([]time.Duration, 0, 3)
	startTime := time.Now()

	err := Do(ctx, cfg, func() error {
		callTimes = append(callTimes, time.Since(startTime))
		return errors.New("test error")
	})

	assert.Error(t, err)
	assert.Equal(t, 3, len(callTimes), "should have 3 call times")

	// First attempt should be near 0
	assert.True(t, callTimes[0] < 10*time.Millisecond, "first attempt should be immediate")

	// Second attempt should be ~50ms after first
	interval1 := callTimes[1] - callTimes[0]
	assert.True(t, interval1 >= 45*time.Millisecond && interval1 <= 100*time.Millisecond,
		"first delay should be ~50ms, got %v", interval1)

	// Third attempt should be ~100ms after second (50ms * 2^1)
	interval2 := callTimes[2] - callTimes[1]
	assert.True(t, interval2 >= 90*time.Millisecond && interval2 <= 150*time.Millisecond,
		"second delay should be ~100ms, got %v", interval2)
}

func TestRetry_Do_MaxDelayCap(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{
		MaxAttempts: 4,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    150 * time.Millisecond, // Cap at 150ms
	}
	callTimes := make([]time.Duration, 0, 4)
	startTime := time.Now()

	err := Do(ctx, cfg, func() error {
		callTimes = append(callTimes, time.Since(startTime))
		return errors.New("test error")
	})

	assert.Error(t, err)
	assert.Equal(t, 4, len(callTimes))

	// Third delay should be capped at MaxDelay (150ms), not 200ms
	interval2 := callTimes[3] - callTimes[2]
	assert.True(t, interval2 <= 160*time.Millisecond,
		"delay should be capped at MaxDelay, got %v", interval2)
}

func TestRetry_DefaultRetryConfig(t *testing.T) {
	assert.Equal(t, 3, DefaultRetryConfig.MaxAttempts)
	assert.Equal(t, 1*time.Second, DefaultRetryConfig.BaseDelay)
	assert.Equal(t, 4*time.Second, DefaultRetryConfig.MaxDelay)
}

func TestRetry_Do_UsesDefaults(t *testing.T) {
	ctx := context.Background()
	cfg := RetryConfig{} // All zeros, should use defaults

	err := Do(ctx, cfg, func() error {
		return nil
	})

	assert.NoError(t, err)
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expected   bool
	}{
		{"5xx is retryable", 500, true},
		{"502 is retryable", 502, true},
		{"503 is retryable", 503, true},
		{"504 is retryable", 504, true},
		{"429 is retryable", 429, true},
		{"400 is not retryable", 400, false},
		{"401 is not retryable", 401, false},
		{"403 is not retryable", 403, false},
		{"404 is not retryable", 404, false},
		{"200 is not retryable", 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryable(tt.statusCode)
			assert.Equal(t, tt.expected, result)
		})
	}
}
