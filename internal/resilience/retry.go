package resilience

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetryConfig holds retry configuration.
// REQ-12: MaxAttempts=3, BaseDelay=1s, MaxDelay=4s (1s, 2s, 4s).
var DefaultRetryConfig = RetryConfig{
	MaxAttempts: 3,
	BaseDelay:   1 * time.Second,
	MaxDelay:    4 * time.Second,
}

// RetryConfig holds retry parameters.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// ErrMaxRetriesExceeded is returned when all retry attempts fail.
var ErrMaxRetriesExceeded = errors.New("max retries exceeded")

// Do executes fn up to MaxAttempts times with exponential backoff.
// It respects context cancellation during sleep intervals.
// It does NOT sleep before the first attempt.
// It sleeps between attempts (1s, 2s, 4s for default config).
// It returns ErrMaxRetriesExceeded if all attempts fail.
func Do(ctx context.Context, cfg RetryConfig, fn func() error) error {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = DefaultRetryConfig.MaxAttempts
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = DefaultRetryConfig.BaseDelay
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = DefaultRetryConfig.MaxDelay
	}

	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt < cfg.MaxAttempts-1 {
			delay := cfg.BaseDelay * time.Duration(1<<attempt)
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, lastErr)
}

// IsRetryable returns true if an error should trigger a retry.
// Retryable: 5xx status codes, 429 Too Many Requests.
func IsRetryable(statusCode int) bool {
	return statusCode >= 500 || statusCode == 429
}
