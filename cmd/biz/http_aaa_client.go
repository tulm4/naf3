package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/eap"
	"github.com/operator/nssAAF/internal/httpclient"
	"github.com/operator/nssAAF/internal/proto"
	goredis "github.com/redis/go-redis/v9"
)

// httpAAAClient satisfies eap.AAAClient by forwarding EAP messages to the AAA Gateway.
// It embeds httpclient.NativeAAAClient for ForwardEAP with built-in retry + circuit breaker.
// It also subscribes to the nssaa:aaa-response Redis channel for server-initiated response routing.
type httpAAAClient struct {
	*httpclient.NativeAAAClient // Embed for ForwardEAP (retry + circuit breaker)

	redis   *goredis.Client
	podID   string
	version string

	// pending maps SessionID → response channel.
	// This is used by subscribeResponses to dispatch Redis pub/sub events.
	// The gateway stores pending[SessionID] and publishes AaaResponseEvent{AuthCtxID} on Redis.
	pending   map[string]chan []byte
	pendingMu sync.RWMutex
}

// newHTTPAAAClient creates a new HTTP AAA client.
// It embeds httpclient.NativeAAAClient for ForwardEAP with retry + circuit breaker.
// The caller must pass InternalCommConfig to configure the native HTTP client.
// Redis pub/sub subscription runs in background for server-initiated responses.
func newHTTPAAAClient(aaaGatewayURL, redisAddr, podID, version string, cfg config.InternalCommConfig) *httpAAAClient {
	native := httpclient.NewNativeAAAClient(aaaGatewayURL, cfg.Native)
	c := &httpAAAClient{
		NativeAAAClient: native,
		redis: goredis.NewClient(&goredis.Options{Addr: redisAddr}),
		podID:           podID,
		version:         version,
		pending:         make(map[string]chan []byte),
	}

	go c.subscribeResponses(context.Background())
	return c
}

// newHTTPAAAClientForTest creates a new HTTP AAA client with a provided Redis client.
// This is for unit tests that need to inject a mock Redis client.
//
//nolint:unparam // podID parameter is always "test-pod" in test calls
func newHTTPAAAClientForTest(aaaGatewayURL, podID, version string, cfg config.InternalCommConfig, redisClient *goredis.Client) *httpAAAClient {
	native := httpclient.NewNativeAAAClient(aaaGatewayURL, cfg.Native)
	return &httpAAAClient{
		NativeAAAClient: native,
		redis:           redisClient,
		podID:           podID,
		version:         version,
		pending:         make(map[string]chan []byte),
	}
}

// SendEAP satisfies eap.AAARouter.
// Spec: PHASE §1.1 pattern
func (c *httpAAAClient) SendEAP(ctx context.Context, session *eap.Session, eapPayload []byte) ([]byte, error) {
	req := &proto.AaaForwardRequest{
		Version:       c.version,
		SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), session.AuthCtxID),
		AuthCtxID:     session.AuthCtxID,
		TransportType: proto.TransportRADIUS, // Default to RADIUS; Biz Router determines actual type
		Direction:     proto.DirectionClientInitiated,
		Payload:       eapPayload,
	}

	resp, err := c.NativeAAAClient.ForwardEAP(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.Payload, nil
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
