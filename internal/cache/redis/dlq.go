// Package redis provides Redis caching and queue layer for NSSAAF.
// REQ-10: DLQ for AMF notification failures after retries exhausted.
// D-02: Redis list LPUSH/BRPOP, key `nssAAF:dlq:amf-notifications`.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// DLQ key prefix per D-02.
const amfDLQKey = "nssAAF:dlq:amf-notifications"

// AMFDLQItem represents an item in the AMF notification DLQ.
type AMFDLQItem struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"` // "reauth" | "revocation"
	URI         string          `json:"uri"`
	Payload     json.RawMessage `json:"payload"`
	AuthCtxID   string          `json:"authCtxId"`
	Attempt     int             `json:"attempt"`
	MaxAttempts int             `json:"maxAttempts"`
	CreatedAt   time.Time       `json:"createdAt"`
	LastError   string          `json:"lastError"`
}

// DLQ provides a dead-letter queue for failed AMF notifications.
type DLQ struct {
	pool *Pool
}

// NewDLQ creates a new AMF notification DLQ.
func NewDLQ(pool *Pool) *DLQ {
	return &DLQ{pool: pool}
}

// Enqueue adds an AMF notification DLQ item to the queue using LPUSH.
// D-02: Redis LPUSH for queue insertion.
func (d *DLQ) Enqueue(ctx context.Context, item interface{}) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("dlq: marshal: %w", err)
	}
	return d.pool.Client().LPush(ctx, amfDLQKey, data).Err()
}

// Dequeue removes and returns an item from the DLQ using BRPOP.
// D-02: Redis BRPOP with timeout for queue consumption.
// Returns nil, nil if timeout expires.
func (d *DLQ) Dequeue(ctx context.Context, timeout time.Duration) (*AMFDLQItem, error) {
	result, err := d.pool.Client().BRPop(ctx, timeout, amfDLQKey).Result()
	if err != nil {
		// context deadline exceeded or cancelled — not an error
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

// Len returns the current DLQ depth for metrics.
func (d *DLQ) Len(ctx context.Context) (int64, error) {
	return d.pool.Client().LLen(ctx, amfDLQKey).Result()
}

// Process starts a background goroutine that polls and logs DLQ items.
// REQ-10: DLQ stores items for later inspection/reprocessing.
// Actual retry delivery is handled by the AMF notifier on its own schedule.
// The DLQ items can be inspected via Redis directly or via a separate DLQ worker.
func (d *DLQ) Process(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}

			item, err := d.Dequeue(ctx, 1*time.Second)
			if err != nil || item == nil {
				continue
			}

			slog.Info("dlq: processing item",
				"id", item.ID,
				"type", item.Type,
				"auth_ctx_id", item.AuthCtxID,
				"attempt", item.Attempt,
			)

			// Re-enqueue for next cycle (DLQ items are inspected, not auto-retried here).
			if reErr := d.Enqueue(ctx, item); reErr != nil {
				slog.Warn("dlq: re-enqueue failed",
					"id", item.ID,
					"error", reErr,
				)
			}
		}
	}()
}
