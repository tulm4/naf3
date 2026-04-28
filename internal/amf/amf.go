// Package amf provides AMF (Access and Mobility Management Function)
// client utilities for N58 interface communication.
package amf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/resilience"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NotificationType identifies the type of AMF notification.
type NotificationType string

const (
	// NotificationTypeReAuth indicates a slice re-authentication notification.
	// NotificationTypeReAuth indicates a slice re-authentication notification.
	NotificationTypeReAuth NotificationType = "reauth"
	// NotificationTypeRevocation indicates a slice authorization revocation notification.
	NotificationTypeRevocation NotificationType = "revocation"
)

// DLQItem represents an item in the AMF notification DLQ.
// D-02: Redis LPUSH/BRPOP, key `nssAAF:dlq:amf-notifications`.
type DLQItem struct {
	ID          string           `json:"id"`
	Type        NotificationType `json:"type"`
	URI         string           `json:"uri"`
	Payload     json.RawMessage  `json:"payload"`
	AuthCtxID   string           `json:"authCtxId"`
	Attempt     int              `json:"attempt"`
	MaxAttempts int              `json:"maxAttempts"`
	CreatedAt   time.Time        `json:"createdAt"`
	LastError   string           `json:"lastError"`
}

// redisAMFDLQItem is the DLQItem variant stored in Redis.
// Field types match redis.AMFDLQItem for serialization compatibility.
type redisAMFDLQItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	URI       string          `json:"uri"`
	Payload   json.RawMessage `json:"payload"`
	AuthCtxID string          `json:"authCtxId"`
	Attempt   int             `json:"attempt"`
	CreatedAt time.Time       `json:"createdAt"`
	LastError string          `json:"lastError"`
}

// Client sends notifications to the AMF.
// REQ-06: Re-Auth notification POST to reauthNotifUri.
// REQ-07: Revocation notification POST to revocNotifUri.
// REQ-10: DLQ on retry exhaustion.
type Client struct {
	httpClient *http.Client
	cbRegistry *resilience.Registry
	dlq        interface {
		Enqueue(ctx context.Context, item interface{}) error
	}
	notifyTimeout time.Duration
	maxRetries    int
}

// NewClient creates a new AMF notifier.
func NewClient(timeout time.Duration, cbRegistry *resilience.Registry, dlq interface {
	Enqueue(ctx context.Context, item interface{}) error
}) *Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		cbRegistry:    cbRegistry,
		dlq:           dlq,
		notifyTimeout: timeout,
		maxRetries:    3,
	}
}

// SendReAuthNotification sends a slice re-authentication notification to the AMF.
// REQ-06: POST to reauthNotifUri with retry and DLQ on exhaustion.
// Spec: TS 23.502 §4.2.9.3.
func (c *Client) SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
	return c.sendNotification(ctx, NotificationTypeReAuth, uri, authCtxID, payload)
}

// SendRevocationNotification sends a slice revocation notification to the AMF.
// REQ-07: POST to revocNotifUri with retry and DLQ on exhaustion.
// Spec: TS 23.502 §4.2.9.4.
func (c *Client) SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
	return c.sendNotification(ctx, NotificationTypeRevocation, uri, authCtxID, payload)
}

// sendNotification sends a notification with retry and DLQ fallback.
// D-02: On retry exhaustion, enqueue to DLQ instead of dropping.
func (c *Client) sendNotification(ctx context.Context, typ NotificationType, uri, authCtxID string, payload []byte) error {
	cbKey := extractHostPort(uri)
	cb := c.cbRegistry.Get(cbKey)

	item := &DLQItem{
		ID:          fmt.Sprintf("%s-%d", authCtxID, time.Now().UnixNano()),
		Type:        typ,
		URI:         uri,
		Payload:     payload,
		AuthCtxID:   authCtxID,
		Attempt:     0,
		MaxAttempts: c.maxRetries,
		CreatedAt:   time.Now(),
	}

	err := resilience.Do(ctx, resilience.RetryConfig{
		MaxAttempts: c.maxRetries,
		BaseDelay:   1 * time.Second,
		MaxDelay:    4 * time.Second,
	}, func() error {
		item.Attempt++

		if !cb.Allow() {
			cb.RecordFailure()
			return fmt.Errorf("circuit breaker open for %s", cbKey)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("amf: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			cb.RecordFailure()
			return fmt.Errorf("amf: send %s: %w", typ, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 500 {
			cb.RecordFailure()
			return fmt.Errorf("amf: server error %d", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			cb.RecordSuccess()
			return fmt.Errorf("amf: client error %d (not retryable)", resp.StatusCode)
		}

		cb.RecordSuccess()
		return nil
	})

	if err != nil {
		item.LastError = err.Error()
		// Convert to redis.AMFDLQItem for storage
		dlqItem := &redisAMFDLQItem{
			ID:        item.ID,
			Type:      string(item.Type),
			URI:       item.URI,
			Payload:   item.Payload,
			AuthCtxID: item.AuthCtxID,
			Attempt:   item.Attempt,
			CreatedAt: item.CreatedAt,
			LastError: item.LastError,
		}
		if dlqErr := c.dlq.Enqueue(ctx, dlqItem); dlqErr != nil {
			slog.Error("amf notification: dlq enqueue failed",
				"auth_ctx_id", authCtxID,
				"type", typ,
				"notify_error", err,
				"dlq_error", dlqErr,
			)
			return fmt.Errorf("notification failed and dlq enqueue failed: %w (dlq: %w)", err, dlqErr)
		}
		slog.Warn("amf notification: sent to DLQ after retries exhausted",
			"auth_ctx_id", authCtxID,
			"type", typ,
			"uri", uri,
			"error", err,
		)
		return nil // DLQ accepted, consider it handled
	}

	return nil
}

// extractHostPort extracts host:port from a URI.
// "http://host:port/path" → "host:port"
func extractHostPort(uri string) string {
	if len(uri) > 7 && uri[:7] == "http://" {
		rest := uri[7:]
		// Find the colon after the host
		for i := 0; i < len(rest); i++ {
			if rest[i] == ':' {
				// Found colon — port starts at i+1
				end := i + 1
				for end < len(rest) && rest[end] != '/' {
					end++
				}
				return rest[:end] // host:port
			}
			if rest[i] == '/' {
				// No port found — return host only
				return rest[:i]
			}
		}
		// No colon or slash found — return the rest as host
		return rest
	}
	// No http:// prefix — parse as-is
	for i, ch := range uri {
		if ch == ':' {
			return uri[:i+1] + uri[i+1:min(i+6, len(uri))]
		}
		if ch == '/' {
			return uri[:i]
		}
	}
	return uri
}
