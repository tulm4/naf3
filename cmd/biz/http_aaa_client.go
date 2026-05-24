package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/eap"
	"github.com/operator/nssAAF/internal/httpclient"
	"github.com/operator/nssAAF/internal/proto"
)

// httpAAAClient satisfies eap.AAAClient by forwarding EAP messages to the AAA Gateway.
// It embeds httpclient.NativeAAAClient for ForwardEAP with built-in retry + circuit breaker.
type httpAAAClient struct {
	*httpclient.NativeAAAClient // Embed for ForwardEAP (retry + circuit breaker)

	podID   string
	version string
}

// newHTTPAAAClient creates a new HTTP AAA client.
// It embeds httpclient.NativeAAAClient for ForwardEAP with retry + circuit breaker.
func newHTTPAAAClient(aaaGatewayURL, podID, version string, cfg config.InternalCommConfig, logger *slog.Logger) *httpAAAClient {
	native := httpclient.NewNativeAAAClient(aaaGatewayURL, cfg.Native, logger)
	return &httpAAAClient{
		NativeAAAClient: native,
		podID:           podID,
		version:         version,
	}
}

// newHTTPAAAClientForTest creates a new HTTP AAA client for unit tests.
func newHTTPAAAClientForTest(aaaGatewayURL, podID, version string, cfg config.InternalCommConfig) *httpAAAClient {
	native := httpclient.NewNativeAAAClient(aaaGatewayURL, cfg.Native, slog.Default())
	return &httpAAAClient{
		NativeAAAClient: native,
		podID:           podID,
		version:         version,
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

// Close shuts down the HTTP AAA client.
func (c *httpAAAClient) Close() error {
	return nil
}

var _ eap.AAARouter = (*httpAAAClient)(nil)
