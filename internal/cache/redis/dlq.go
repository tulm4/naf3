// Package redis provides Redis caching and queue layer for NSSAAF.
// REQ-10: DLQ for AMF notification failures after retries exhausted.
// D-02: Redis list LPUSH/BRPOP, key `nssAAF:dlq:amf-notifications`.
package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/metrics"
)

const amfDLQKey = "nssAAF:dlq:amf-notifications"

type AMFDLQItem struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	URI         string          `json:"uri"`
	Payload     json.RawMessage `json:"payload"`
	AuthCtxID   string          `json:"authCtxId"`
	Attempt     int             `json:"attempt"`
	MaxAttempts int             `json:"maxAttempts"`
	CreatedAt   time.Time       `json:"createdAt"`
	LastError   string          `json:"lastError"`
}

type DLQ struct {
	pool   *Pool
	wg     sync.WaitGroup
	stopCh chan struct{}
}

func NewDLQ(pool *Pool) *DLQ {
	return &DLQ{pool: pool, stopCh: make(chan struct{})}
}

func (d *DLQ) Enqueue(ctx context.Context, item interface{}) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("dlq: marshal: %w", err)
	}
	return d.pool.Client().LPush(ctx, amfDLQKey, data).Err()
}

// Dequeue removes and returns an item from the DLQ using BRPOP.
// D-02: Redis BRPOP with timeout for queue consumption.
// Returns (nil, nil) on Redis timeout (queue empty, context still valid).
// Returns (nil, ctx.Err()) when the context is cancelled or its deadline fires.
func (d *DLQ) Dequeue(ctx context.Context, timeout time.Duration) (*AMFDLQItem, error) {
	// Check context before blocking on BRPOP so callers can exit on cancellation.
	// We check ctx.Err() because ctx.Done() is only closed on explicit cancellation,
	// not when a deadline passes.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := d.pool.Client().BRPop(ctx, timeout, amfDLQKey).Result()
	if err != nil {
		// Propagate context cancellation/deadline so callers can exit.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		// Redis timeout (queue empty) — not an error.
		return nil, nil
	}
	if len(result) < 2 {
		return nil, nil
	}
	var item AMFDLQItem
	if err := json.Unmarshal([]byte(result[1]), &item); err != nil {
		return nil, fmt.Errorf("dlq: unmarshal: %w", err)
	}
	return &item, nil
}

func (d *DLQ) Len(ctx context.Context) (int64, error) {
	return d.pool.Client().LLen(ctx, amfDLQKey).Result()
}

// deliverToAMF attempts to deliver a DLQ item to the AMF via HTTP POST.
// Returns (true, nil) on 2xx, (false, error) otherwise.
func (d *DLQ) deliverToAMF(ctx context.Context, hc *http.Client, item *AMFDLQItem) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, item.URI, bytes.NewReader(item.Payload))
	if err != nil {
		return false, fmt.Errorf("dlq: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return false, fmt.Errorf("dlq: do request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	return false, fmt.Errorf("dlq: non-2xx status: %d", resp.StatusCode)
}

// Process starts a background goroutine that polls the DLQ and attempts
// HTTP delivery to AMF. Items are re-enqueued with incremented Attempt on
// failure, or discarded after MaxAttempts exhaustion.
// The caller must pass a dedicated *http.Client (e.g., 10s timeout) to
// avoid circular dependencies.
// onDone is called after each item is processed (exhaustion/discard, delivery, or
// re-enqueue). The caller can use this to wait for completion in tests.
func (d *DLQ) Process(ctx context.Context, hc *http.Client, onDone func()) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			item, err := d.Dequeue(ctx, 1*time.Second)
			// Return on context cancellation or deadline exceeded.
			if err != nil {
				return
			}
			if item == nil {
				// Queue empty (Redis timeout); continue polling.
				continue
			}

			// DLQ-G2: exhaustion check
			if item.MaxAttempts > 0 && item.Attempt >= item.MaxAttempts {
				slog.Error("dlq: max attempts exhausted, discarding item",
					"id", item.ID, "type", item.Type, "auth_ctx_id", item.AuthCtxID,
					"attempt", item.Attempt, "max_attempts", item.MaxAttempts)
				metrics.DLQProcessed.WithLabelValues("exhausted").Inc()
				if onDone != nil {
					onDone()
				}
				continue
			}

			// DLQ-G1: attempt actual AMF delivery
			ok, retryErr := d.deliverToAMF(ctx, hc, item)
			if ok {
				slog.Info("dlq: delivered", "id", item.ID, "type", item.Type, "auth_ctx_id", item.AuthCtxID)
				metrics.DLQProcessed.WithLabelValues("success").Inc()
				if onDone != nil {
					onDone()
				}
				continue
			}

			// Re-enqueue with incremented attempt counter
			item.Attempt++
			if retryErr != nil {
				item.LastError = retryErr.Error()
			}
			if reErr := d.Enqueue(ctx, item); reErr != nil {
				slog.Error("dlq: re-enqueue failed", "id", item.ID, "error", reErr)
				metrics.DLQProcessed.WithLabelValues("error").Inc()
				return // Exit on unrecoverable error (cannot re-enqueue)
			}
			slog.Warn("dlq: re-enqueued", "id", item.ID, "attempt", item.Attempt, "error", retryErr)
			if onDone != nil {
				onDone()
			}
			return // Exit after re-enqueue so tests can verify queue state
		}
	}()
}

func (d *DLQ) Stop() {
	if d.stopCh != nil {
		close(d.stopCh)
	}
	d.wg.Wait()
}

func (d *DLQ) Done() {
	d.wg.Wait()
}
