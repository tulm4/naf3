// Package amf provides AMF (Access and Mobility Management Function)
// client utilities for N58 interface communication.
package amf

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/resilience"
	redisclient "github.com/operator/nssAAF/internal/cache/redis"
)

// NotificationType identifies the type of AMF notification.
type NotificationType string

const (
	// NotificationTypeSliceReAuth indicates a slice re-authentication notification.
	// Spec: TS 23.502 §4.2.9.3, TS 29.518 §5.2.2.27
	NotificationTypeSliceReAuth NotificationType = "SLICE_RE_AUTH"
	// NotificationTypeSliceRevoc indicates a slice authorization revocation notification.
	// Spec: TS 23.502 §4.2.9.4, TS 29.518 §5.2.2.27
	NotificationTypeSliceRevoc NotificationType = "SLICE_REVOCATION"
)

// Client sends notifications to the AMF.
// REQ-06: Re-Auth notification POST to reauthNotifUri.
// REQ-07: Revocation notification POST to revocNotifUri.
// REQ-10: DLQ on retry exhaustion.
type Client struct {
	factory      *nfclient.Factory
	cbRegistry   *resilience.Registry
	dlq          interface {
		Enqueue(ctx context.Context, item interface{}) error
	}
	notifyTimeout time.Duration
	cbCfg        config.CircuitBreakerConfig
	retryCfg     resilience.RetryConfig
}

// NewClient creates a new AMF notifier.
func NewClient(factory *nfclient.Factory, cbRegistry *resilience.Registry, dlq interface {
	Enqueue(ctx context.Context, item interface{}) error
}, cbCfg config.CircuitBreakerConfig, retryCfg resilience.RetryConfig) *Client {
	return &Client{
		factory:      factory,
		cbRegistry:   cbRegistry,
		dlq:          dlq,
		notifyTimeout: 30 * time.Second,
		cbCfg:        cbCfg,
		retryCfg:     retryCfg,
	}
}

// SendReAuthNotification sends a slice re-authentication notification to the AMF.
// REQ-06: POST to reauthNotifUri with retry and DLQ on exhaustion.
// Spec: TS 23.502 §4.2.9.3.
func (c *Client) SendReAuthNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
	return c.sendNotification(ctx, NotificationTypeSliceReAuth, uri, authCtxID, payload)
}

// SendRevocationNotification sends a slice revocation notification to the AMF.
// REQ-07: POST to revocNotifUri with retry and DLQ on exhaustion.
// Spec: TS 23.502 §4.2.9.4.
func (c *Client) SendRevocationNotification(ctx context.Context, uri, authCtxID string, payload []byte) error {
	return c.sendNotification(ctx, NotificationTypeSliceRevoc, uri, authCtxID, payload)
}

// sendNotification sends a notification with retry and DLQ fallback.
// D-02: On retry exhaustion, enqueue to DLQ instead of dropping.
func (c *Client) sendNotification(ctx context.Context, typ NotificationType, uri, authCtxID string, payload []byte) error {
	item := &redisclient.AMFDLQItem{
		ID:          fmt.Sprintf("%s-%d", authCtxID, time.Now().UnixNano()),
		Type:        string(typ),
		URI:         uri,
		Payload:     payload,
		AuthCtxID:   authCtxID,
		Attempt:     0,
		MaxAttempts: c.retryCfg.MaxAttempts,
		CreatedAt:   time.Now(),
	}

	err := resilience.Do(ctx, c.retryCfg, func() error {
		item.Attempt++

		// Extract baseURL and path from full URI.
		// uri like "http://amf:8080/nsmf-callback/..." → baseURL="http://amf:8080", path="/nsmf-callback/..."
		baseURL, path, err := extractBaseURLAndPath(uri)
		if err != nil {
			return fmt.Errorf("amf: parse uri: %w", err)
		}

		status, _, err := c.factory.Do(ctx, baseURL, http.MethodPost, path, payload)
		if err != nil {
			return fmt.Errorf("amf: send %s: %w", typ, err)
		}

		// Factory already recorded CB failure/success internally based on status.
		if status >= 500 {
			return fmt.Errorf("amf: server error %d", status)
		}
		if status >= 400 {
			return fmt.Errorf("amf: client error %d (not retryable)", status)
		}

		return nil
	})

	if err != nil {
		item.LastError = err.Error()
		if dlqErr := c.dlq.Enqueue(ctx, item); dlqErr != nil {
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

// extractBaseURLAndPath splits a full URI into baseURL and path.
// "http://host:port/path" → ("http://host:port", "/path")
func extractBaseURLAndPath(uri string) (string, string, error) {
	if len(uri) == 0 {
		return "", "", fmt.Errorf("empty uri")
	}

	schemeEnd := strings.Index(uri, "://")
	if schemeEnd == -1 {
		return "", "", fmt.Errorf("no scheme in uri: %s", uri)
	}
	rest := uri[schemeEnd+3:] // after "://"
	slashIdx := strings.Index(rest, "/")
	if slashIdx == -1 {
		return uri, "/", nil
	}
	hostEnd := schemeEnd + 3 + slashIdx
	return uri[:hostEnd], uri[hostEnd:], nil
}
