// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/eap"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/redis/go-redis/v9"
)

// httpAAAClient satisfies eap.AAAClient by forwarding EAP messages to the AAA Gateway via HTTP.
// It also subscribes to the nssaa:aaa-response Redis channel for response routing.
type httpAAAClient struct {
	aaaGatewayURL string
	httpClient    *http.Client
	version       string
	redis         *redis.Client
	podID         string

	// pending maps SessionID → response channel.
	// This is used by subscribeResponses to dispatch Redis pub/sub events.
	// The gateway stores pending[SessionID] and publishes AaaResponseEvent{AuthCtxID} on Redis.
	pending   map[string]chan []byte
	pendingMu sync.RWMutex
}

// newHTTPAAAClient creates a new HTTP AAA client.
// The httpClient parameter must be configured by the caller (cmd/biz/main.go) based on
// biz.useMTLS config — either a plain http.Client or one with TLS configured.
func newHTTPAAAClient(aaaGatewayURL, redisAddr, podID, version string, httpClient *http.Client) *httpAAAClient {
	c := &httpAAAClient{
		aaaGatewayURL: aaaGatewayURL,
		httpClient:    httpClient,
		version:       version,
		redis: redis.NewClient(&redis.Options{
			Addr: redisAddr,
		}),
		podID:   podID,
		pending: make(map[string]chan []byte),
	}

	// Start Redis subscription in background
	go c.subscribeResponses(context.Background())
	return c
}

// newHTTPAAAClientForTest creates a new HTTP AAA client with a provided Redis client.
// This is for unit tests that need to inject a mock Redis client.
//
//nolint:unparam // podID parameter is always "test-pod" in test calls
func newHTTPAAAClientForTest(aaaGatewayURL, podID, version string, httpClient *http.Client, redisClient *redis.Client) *httpAAAClient {
	return &httpAAAClient{
		aaaGatewayURL: aaaGatewayURL,
		httpClient:    httpClient,
		version:       version,
		redis:         redisClient,
		podID:         podID,
		pending:       make(map[string]chan []byte),
	}
}

// SendEAP satisfies eap.AAARouter.
// Spec: PHASE §1.1 pattern
func (c *httpAAAClient) SendEAP(ctx context.Context, session *eap.Session, eapPayload []byte) ([]byte, error) {
	// 1. Build forward request
	req := &proto.AaaForwardRequest{
		Version:       c.version,
		SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), session.AuthCtxID),
		AuthCtxID:     session.AuthCtxID,
		TransportType: proto.TransportRADIUS, // Default to RADIUS; Biz Router determines actual type
		Direction:     proto.DirectionClientInitiated,
		Payload:       eapPayload,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal forward request: %w", err)
	}

	// 2. POST to AAA Gateway
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.aaaGatewayURL+"/aaa/forward", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(proto.HeaderName, c.version)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("aaa gateway unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aaa gateway returned %d", resp.StatusCode)
	}

	var fwdResp proto.AaaForwardResponse
	if err := json.NewDecoder(resp.Body).Decode(&fwdResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return fwdResp.Payload, nil
}

// subscribeResponses listens to nssaa:aaa-response and dispatches to pending channels.
func (c *httpAAAClient) subscribeResponses(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("subscribeResponses recovered from panic", "panic", r)
		}
	}()
	if c.redis == nil {
		return
	}
	ch := c.redis.PSubscribe(ctx, proto.AaaResponseChannel)
	if ch == nil {
		slog.Warn("subscribeResponses: PSubscribe returned nil")
		return
	}
	defer func() { _ = ch.Close() }()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch.Channel():
			if msg == nil {
				return // Channel closed
			}
			var event proto.AaaResponseEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				slog.Debug("subscribeResponses: failed to unmarshal event", "error", err)
				continue
			}

			// Fix: lookup by SessionID (same key used by AAA Gateway pending map).
			// Previously this looked up by event.AuthCtxID which never matched because
			// the gateway stored pending[SessionID] but the event had AuthCtxID="".
			c.pendingMu.RLock()
			pendingCh, ok := c.pending[event.SessionID]
			c.pendingMu.RUnlock()

			if !ok {
				continue // Not for this Biz Pod
			}

			select {
			case pendingCh <- event.Payload:
			default:
			}
		}
	}
}

// Close shuts down the HTTP AAA client.
func (c *httpAAAClient) Close() error {
	if c.redis != nil {
		return c.redis.Close()
	}
	return nil
}

var _ eap.AAARouter = (*httpAAAClient)(nil)
