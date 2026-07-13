package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/operator/nssAAF/internal/config"
	dbg "github.com/operator/nssAAF/internal/debug"
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
	dbg     *dbg.Debug
}

// newHTTPAAAClient creates a new HTTP AAA client.
// It embeds httpclient.NativeAAAClient for ForwardEAP with retry + circuit breaker.
// The d parameter is nil-safe: Emit short-circuits when nil or disabled.
func newHTTPAAAClient(aaaGatewayURL, podID, version string, cfg config.InternalCommConfig, logger *slog.Logger, d *dbg.Debug) *httpAAAClient {
	native := httpclient.NewNativeAAAClient(aaaGatewayURL, cfg.Native, logger)
	return &httpAAAClient{
		NativeAAAClient: native,
		podID:           podID,
		version:         version,
		dbg:             d,
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
func (c *httpAAAClient) SendEAP(ctx context.Context, session *eap.Session, routing eap.RoutingContext, eapPayload []byte) ([]byte, error) {
	req := &proto.AaaForwardRequest{
		Version:       c.version,
		SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), session.AuthCtxID),
		AuthCtxID:     session.AuthCtxID,
		TransportType: proto.TransportRADIUS, // Default to RADIUS; Biz Router determines actual type
		Direction:     proto.DirectionClientInitiated,
		Payload:       eapPayload,
	}

	// Spec: debug tracing verification spec §3, hop "biz" — emit http.request.out
	// before the outbound call to aaa-gateway and http.request.exit after the
	// call returns (with status + duration). Nil-safe via Emit.
	c.emitHTTPRequestOut(ctx, session.AuthCtxID)
	start := time.Now()
	resp, err := c.NativeAAAClient.ForwardEAP(ctx, req)
	c.emitHTTPRequestExit(ctx, session.AuthCtxID, start, err)

	if err != nil {
		return nil, err
	}

	return resp.Payload, nil
}

// RoutingContext satisfies eap.AAARouter.
func (c *httpAAAClient) RoutingContext(session *eap.Session) eap.RoutingContext {
	sst, sd := session.DecodeSnssaiKey()
	return eap.RoutingContext{
		GPSI:      session.Gpsi,
		Sst:       sst,
		Sd:        sd,
		AuthCtxID: session.AuthCtxID,
	}
}

// Close shuts down the HTTP AAA client.
func (c *httpAAAClient) Close() error {
	return nil
}

// emitHTTPRequestOut fires the http.request.out event before the outbound
// call to aaa-gateway. Nil-safe.
func (c *httpAAAClient) emitHTTPRequestOut(ctx context.Context, authCtxID string) {
	if c.dbg == nil || !c.dbg.Enabled() {
		return
	}
	c.dbg.Emit(ctx, dbg.Event{
		Op:     "http.request.out",
		Kind:   dbg.KindHTTP,
		AuthID: authCtxID,
		Detail: map[string]any{
			"method": "POST",
			"target": "aaa-gw",
			"path":   "/aaa/forward",
		},
	})
}

// emitHTTPRequestExit fires the http.request.exit event after the outbound
// call to aaa-gateway returns. Nil-safe.
func (c *httpAAAClient) emitHTTPRequestExit(ctx context.Context, authCtxID string, start time.Time, err error) {
	if c.dbg == nil || !c.dbg.Enabled() {
		return
	}
	status := "ok"
	if err != nil {
		status = "error"
	}
	c.dbg.Emit(ctx, dbg.Event{
		Op:     "http.request.exit",
		Kind:   dbg.KindHTTP,
		AuthID: authCtxID,
		Detail: map[string]any{
			"target":      "aaa-gw",
			"path":        "/aaa/forward",
			"duration_ms": time.Since(start).Milliseconds(),
		},
		Status: status,
		Error:  err,
	})
}

var _ eap.AAARouter = (*httpAAAClient)(nil)
