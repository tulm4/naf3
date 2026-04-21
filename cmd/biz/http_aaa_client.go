// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	// pending maps AuthCtxID → response channel
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

// SendEAP satisfies eap.AAAClient.
// Spec: PHASE §1.1 pattern
func (c *httpAAAClient) SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error) {
	// 1. Build forward request
	req := &proto.AaaForwardRequest{
		Version:       c.version,
		SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID),
		AuthCtxID:     authCtxID,
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
	defer resp.Body.Close()

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
	ch := c.redis.PSubscribe(ctx, proto.AaaResponseChannel)
	defer ch.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch.Channel():
			var event proto.AaaResponseEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}

			c.pendingMu.RLock()
			pendingCh, ok := c.pending[event.AuthCtxID]
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
	return c.redis.Close()
}

var _ eap.AAAClient = (*httpAAAClient)(nil)
