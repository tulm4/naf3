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

func (d *DLQ) Dequeue(ctx context.Context, timeout time.Duration) (*AMFDLQItem, error) {
	result, err := d.pool.Client().BRPop(ctx, timeout, amfDLQKey).Result()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
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

func (d *DLQ) Process(ctx context.Context, hc *http.Client) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			type deqResult struct {
				item *AMFDLQItem
				err  error
			}
			resCh := make(chan deqResult, 1)
			go func() {
				it, e := d.Dequeue(ctx, 1*time.Second)
				resCh <- deqResult{item: it, err: e}
			}()

			select {
			case <-d.stopCh:
				<-resCh
				return
			case <-ctx.Done():
				<-resCh
				return
			case res := <-resCh:
				item, err := res.item, res.err
				if err != nil {
					return
				}
				if item == nil {
					continue
				}

				if item.MaxAttempts > 0 && item.Attempt >= item.MaxAttempts {
					slog.Error("dlq: max attempts exhausted, discarding item",
						"id", item.ID, "type", item.Type, "auth_ctx_id", item.AuthCtxID,
						"attempt", item.Attempt, "max_attempts", item.MaxAttempts)
					metrics.DLQProcessed.WithLabelValues("exhausted").Inc()
					select {
					case <-d.stopCh:
						return
					default:
					}
					continue
				}

				ok, retryErr := d.deliverToAMF(ctx, hc, item)
				if ok {
					slog.Info("dlq: delivered", "id", item.ID, "type", item.Type, "auth_ctx_id", item.AuthCtxID)
					metrics.DLQProcessed.WithLabelValues("success").Inc()
					select {
					case <-d.stopCh:
						return
					default:
					}
					continue
				}

				item.Attempt++
				if retryErr != nil {
					item.LastError = retryErr.Error()
				}
				if reErr := d.Enqueue(ctx, item); reErr != nil {
					slog.Error("dlq: re-enqueue failed", "id", item.ID, "error", reErr)
					metrics.DLQProcessed.WithLabelValues("error").Inc()
				} else {
					slog.Warn("dlq: re-enqueued", "id", item.ID, "attempt", item.Attempt, "error", retryErr)
				}
				select {
				case <-d.stopCh:
					return
				default:
				}
			}
		}
	}()
}

func (d *DLQ) Stop() {
	close(d.stopCh)
	d.wg.Wait()
}

func (d *DLQ) Done() {
	d.wg.Wait()
}
