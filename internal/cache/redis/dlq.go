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
	"strconv"
	"strings"
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
	pool      *Pool
	wg        sync.WaitGroup
	mu        sync.Mutex
	stopCh    chan struct{}
	doneCh    chan struct{}
	cancelCtx context.CancelFunc // cancels the internal goroutine context
	stopped   bool              // prevents double-close of stopCh
}

func NewDLQ(pool *Pool) *DLQ {
	return &DLQ{
		pool:   pool,
		stopCh: make(chan struct{}),
	}
}

// updateDepth increments the DLQ depth gauge.
func (d *DLQ) updateDepth() {
	metrics.DLQDepth.Inc()
}

// updateDepthDecr decrements the DLQ depth gauge.
func (d *DLQ) updateDepthDecr() {
	metrics.DLQDepth.Dec()
}

func (d *DLQ) Enqueue(ctx context.Context, item interface{}) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("dlq: marshal: %w", err)
	}
	if err := d.pool.Client().LPush(ctx, amfDLQKey, data).Err(); err != nil {
		return err
	}
	d.updateDepth()
	return nil
}

// Dequeue blocks on BRPOP until an item is available or the timeout expires.
// Returns (nil, nil) when the timeout expires (queue empty) or when the
// context is cancelled in miniredis. Callers must check ctx.Err() to
// distinguish "queue empty" from "context cancelled".
func (d *DLQ) Dequeue(ctx context.Context, timeout time.Duration) (*AMFDLQItem, error) {
	result, err := d.pool.Client().BRPop(ctx, timeout, amfDLQKey).Result()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	if len(result) < 2 {
		return nil, nil
	}
	var item AMFDLQItem
	if err := json.Unmarshal([]byte(result[1]), &item); err != nil {
		return nil, fmt.Errorf("dlq: unmarshal: %w", err)
	}
	d.updateDepthDecr()
	return &item, nil
}

func (d *DLQ) Len(ctx context.Context) (int64, error) {
	return d.pool.Client().LLen(ctx, amfDLQKey).Result()
}

func (d *DLQ) deliverToAMF(ctx context.Context, hc *http.Client, item *AMFDLQItem) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, item.URI, bytes.NewReader(item.Payload))
	if err != nil {
		return false, fmt.Errorf("dlq: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DLQ-Attempt", strconv.Itoa(item.Attempt))
	req.Header.Set("X-DLQ-MaxAttempts", strconv.Itoa(item.MaxAttempts))
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

// classifyError categorizes delivery errors for metrics labeling.
func classifyError(err error) string {
	if err == nil {
		return "unknown"
	}
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "connection refused"),
		strings.Contains(errStr, "timeout"),
		strings.Contains(errStr, "i/o timeout"):
		return "network"
	case strings.Contains(errStr, "non-2xx"):
		return "http"
	default:
		return "unknown"
	}
}

// Process starts a background goroutine that continuously polls the DLQ and
// attempts to deliver items to AMF. Items are re-enqueued on delivery failure
// with an incremented Attempt counter. Items exceeding MaxAttempts are discarded.
// The goroutine exits when ctx is cancelled or Stop() is called.
func (d *DLQ) Process(ctx context.Context, hc *http.Client) {
	d.mu.Lock()
	d.stopCh = make(chan struct{})
	d.doneCh = make(chan struct{})
	d.stopped = false
	innerCtx, cancel := context.WithCancel(ctx)
	d.cancelCtx = cancel
	d.mu.Unlock()
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer close(d.doneCh)
		defer cancel() // clean up the inner context on exit
		for {
			item, err := d.Dequeue(innerCtx, 1*time.Second)
			if err != nil || item == nil {
				d.mu.Lock()
				stop := d.stopCh
				d.mu.Unlock()
				select {
				case <-stop:
					return
				case <-innerCtx.Done():
					return
				default:
					time.Sleep(250 * time.Millisecond)
				}
				continue
			}

			if item.MaxAttempts > 0 && item.Attempt >= item.MaxAttempts {
				slog.Error("dlq: max attempts exhausted, discarding item",
					"id", item.ID, "type", item.Type, "auth_ctx_id", item.AuthCtxID,
					"attempt", item.Attempt, "max_attempts", item.MaxAttempts)
				metrics.DLQProcessed.WithLabelValues("exhausted").Inc()
				continue
			}

			ok, retryErr := d.deliverToAMF(innerCtx, hc, item)
			if ok {
				slog.Info("dlq: delivered", "id", item.ID, "type", item.Type, "auth_ctx_id", item.AuthCtxID)
				metrics.DLQProcessed.WithLabelValues("success").Inc()
				continue
			}

			item.Attempt++
			if retryErr != nil {
				item.LastError = retryErr.Error()
				metrics.DLQRetry.WithLabelValues(classifyError(retryErr)).Inc()
			}
			if err := d.Enqueue(innerCtx, item); err != nil {
				const enqueueRetryMax = 5
				var enqueueErr error
				for attempt := 0; attempt < enqueueRetryMax; attempt++ {
					if err := d.Enqueue(innerCtx, item); err == nil {
						enqueueErr = nil
						break
					}
					enqueueErr = err
					if attempt < enqueueRetryMax-1 {
						delay := time.Duration(1<<attempt) * 200 * time.Millisecond
						time.Sleep(delay)
					}
				}
				if enqueueErr != nil {
					slog.Error("dlq: re-enqueue failed after all retries",
						"id", item.ID, "error", enqueueErr)
					metrics.DLQProcessed.WithLabelValues("error").Inc()
					metrics.DLQReenqueueFailures.Inc()
				} else {
					slog.Info("dlq: re-enqueued after transient failure", "id", item.ID, "attempt", item.Attempt, "error", retryErr)
					time.Sleep(500 * time.Millisecond)
				}
			}
		}
	}()
}

// Stop signals the Process goroutine to exit and blocks until it finishes.
// Safe to call multiple times.
func (d *DLQ) Stop() {
	d.mu.Lock()
	if d.cancelCtx == nil || d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	cancel := d.cancelCtx
	d.cancelCtx = nil
	d.mu.Unlock()

	close(d.stopCh)
	cancel() // cancel innerCtx so BRPOP returns immediately in the goroutine
	d.wg.Wait()
}

// Done returns a channel that is closed when the Process goroutine has exited.
func (d *DLQ) Done() <-chan struct{} {
	d.mu.Lock()
	ch := d.doneCh
	d.mu.Unlock()
	return ch
}
