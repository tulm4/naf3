// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/operator/nssAAF/internal/proto"
	"github.com/redis/go-redis/v9"
)

// Config holds AAA Gateway configuration.
type Config struct {
	BizServiceURL       string // http://svc-nssaa-biz:8080
	RedisAddr          string // Redis address for pub/sub and session correlation
	ListenRADIUS      string // ":1812" — UDP listen address for RADIUS
	ListenDIAMETER    string // ":3868" — listen address for Diameter (TCP or SCTP)
	AAAGatewayURL     string // self-referential for health checks
	Logger            *slog.Logger
	Version           string // Injected at build time
	DiameterProtocol  string // "tcp" or "sctp"

	// Diameter client-initiated config (PLAN §2.3.5):
	// Required for DER/DEA forwarding to AAA-S.
	DiameterServerAddress string // e.g. "nss-aaa-server:3868"
	DiameterRealm        string // e.g. "operator.com"
	DiameterHost         string // Origin-Host for CER (AAA Gateway identity)

	// RADIUS client-initiated config:
	// Required for Access-Request forwarding to AAA-S.
	RadiusServerAddress string // e.g. "nss-aaa-server:1812"
	RadiusSharedSecret string // Shared secret for Message-Authenticator

	RedisMode          string // "standalone" or "sentinel"
	KeepalivedStatePath string // path to keepalived state file
}

// Gateway is the AAA Gateway component. It runs in a separate process from Biz Pods.
type Gateway struct {
	cfg Config

	redis         *redis.Client
	bizHTTPClient *http.Client
	version       string
	logger        *slog.Logger

	radiusHandler    *RadiusHandler
	diameterHandler  *DiameterHandler
	radiusForwarder *radiusForwarder // RADIUS client (client-initiated path)
	diamForwarder   *diamForwarder   // Diameter client (client-initiated path)

	// pending maps SessionID → pendingEntry (for client-initiated response routing).
	// Fix: store both SessionID and AuthCtxID so AaaResponseEvent.AuthCtxID is populated.
	pending   map[string]*pendingEntry
	pendingMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// pendingEntry holds the response channel and auth context metadata for a pending request.
type pendingEntry struct {
	authCtxID string
	sessionID string
	ch       chan []byte
}

// New creates a new AAA Gateway.
func New(cfg Config) *Gateway {
	g := &Gateway{
		cfg:     cfg,
		version: cfg.Version,
		logger:  cfg.Logger,
		pending: make(map[string]*pendingEntry),
	}

	g.redis = newRedisClient(cfg.RedisAddr, cfg.RedisMode)
	g.bizHTTPClient = &http.Client{Timeout: 30 * time.Second}

	g.radiusHandler = &RadiusHandler{
		logger:          cfg.Logger,
		tracer:          otel.Tracer("aaa-gateway/radius"),
		publishResponse: g.publishResponseBytes,
		forwardToBiz:    g.forwardToBiz,
	}

	// Create the RADIUS forwarder for client-initiated path.
	// It wraps EAP payload in Access-Request with EAP-Message and Message-Authenticator.
	if cfg.RadiusServerAddress != "" {
		g.radiusForwarder = newRadiusForwarder(
			cfg.RadiusServerAddress,
			1812, // Default RADIUS port
			cfg.RadiusSharedSecret,
			cfg.Logger,
		)
	}

	// Create the persistent Diameter forwarder for client-initiated path.
	// This maintains CER/CEA handshake and DWR/DWA watchdog to AAA-S.
	g.diamForwarder = newDiamForwarder(
		cfg.DiameterServerAddress,
		cfg.DiameterProtocol,
		cfg.DiameterHost,
		cfg.DiameterRealm,
		cfg.DiameterServerAddress, // destHost: use server address as host identifier
		cfg.DiameterRealm,        // destRealm
		cfg.Logger,
	)

	g.diameterHandler = NewDiameterHandler(
		cfg.Logger,
		g.publishResponseBytes,
		g.forwardToBiz,
		cfg.Version,
		cfg.BizServiceURL,
		g.bizHTTPClient,
		g.diamForwarder,
		cfg.DiameterHost,
		cfg.DiameterRealm,
	)

	return g
}

// Start starts the AAA Gateway listeners.
func (g *Gateway) Start(ctx context.Context) error {
	g.ctx, g.cancel = context.WithCancel(ctx)

	// Start RADIUS UDP listener
	if g.cfg.ListenRADIUS != "" {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			g.radiusHandler.Listen(g.ctx, g.cfg.ListenRADIUS)
		}()
	}

	// Start Diameter listener (TCP or SCTP)
	if g.cfg.ListenDIAMETER != "" {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			if err := g.diameterHandler.Listen(g.ctx, g.cfg.ListenDIAMETER, g.cfg.DiameterProtocol); err != nil {
				g.logger.Error("diameter listener failed", "error", err)
			}
		}()
	}

	// Start Redis subscription for dispatching responses
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		g.subscribeResponses(g.ctx)
	}()

	// Connect Diameter forwarder to AAA-S (client-initiated path).
	// This performs CER/CEA handshake and starts DWR/DWA watchdog.
	if g.diamForwarder != nil && g.cfg.DiameterServerAddress != "" {
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			if err := g.diamForwarder.Connect(g.ctx); err != nil {
				g.logger.Error("diameter_forward_connect_failed",
					"addr", g.cfg.DiameterServerAddress,
					"error", err)
			}
		}()
	}

	return nil
}

// Stop gracefully stops the AAA Gateway.
func (g *Gateway) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
	g.wg.Wait()
	if g.redis != nil {
		g.redis.Close()
	}
	if g.diamForwarder != nil {
		g.diamForwarder.Close()
	}
	if g.radiusForwarder != nil {
		g.radiusForwarder.Close()
	}
}

// ForwardEAP satisfies proto.BizAAAClient.
// It receives AaaForwardRequest from Biz Pod, writes session correlation to Redis,
// forwards to AAA-S, waits for response, publishes to Redis, and returns response bytes.
func (g *Gateway) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
	// 1. Write session correlation entry to Redis (before forwarding)
	entry := proto.SessionCorrEntry{
		AuthCtxID: req.AuthCtxID,
		PodID:     "", // Populated by Biz Pod via heartbeat; AAA GW writes read-only
		Sst:       req.Sst,
		Sd:        req.Sd,
		CreatedAt:  time.Now().Unix(),
	}
	if err := g.writeSessionCorr(ctx, req.SessionID, &entry); err != nil {
		return nil, fmt.Errorf("aaa-gateway: failed to write session corr: %w", err)
	}

	// 2. Set up response channel for this session.
	// pendingEntry stores both SessionID and AuthCtxID for correct response routing.
	pendingEntry := &pendingEntry{
		authCtxID: req.AuthCtxID,
		sessionID: req.SessionID,
		ch:        make(chan []byte, 1),
	}
	g.pendingMu.Lock()
	g.pending[req.SessionID] = pendingEntry
	g.pendingMu.Unlock()

	defer func() {
		g.pendingMu.Lock()
		delete(g.pending, req.SessionID)
		g.pendingMu.Unlock()
	}()

	// 3. Forward to AAA-S based on transport type
	var response []byte
	var err error
	switch req.TransportType {
	case proto.TransportRADIUS:
		response, err = g.radiusForwarder.Forward(ctx, req.Payload, req.SessionID, req.Sst, req.Sd)
	case proto.TransportDIAMETER:
		// Use diamForwarder directly for the client-initiated path.
		// It handles CER/CEA handshake, DER encoding, DWR watchdog, and DEA correlation.
		response, err = g.diamForwarder.Forward(ctx, req.Payload, req.SessionID, req.Sst, req.Sd)
	default:
		return nil, fmt.Errorf("aaa-gateway: unknown transport type: %s", req.TransportType)
	}
	if err != nil {
		return nil, fmt.Errorf("aaa-gateway: forward failed: %w", err)
	}

	// 4. Publish response to Redis channel for Biz Pods to receive
	event := proto.AaaResponseEvent{
		Version:   g.version,
		SessionID: req.SessionID,
		AuthCtxID: req.AuthCtxID,
		Payload:   response,
	}
	if err := g.publishResponse(ctx, &event); err != nil {
		g.logger.Error("failed to publish response event", "error", err, "session_id", req.SessionID)
		// Continue — the response was already received, just couldn't publish
	}

	return &proto.AaaForwardResponse{
		Version:   g.version,
		SessionID: req.SessionID,
		AuthCtxID: req.AuthCtxID,
		Payload:   response,
	}, nil
}

// HandleForward handles POST /aaa/forward from Biz Pod.
func (g *Gateway) HandleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req proto.AaaForwardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := g.ForwardEAP(r.Context(), &req)
	if err != nil {
		g.logger.Error("ForwardEAP failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// dispatchResponse dispatches a response event to the appropriate pending channel.
// This is called from subscribeResponses (Redis pub/sub) for server-initiated responses.
func (g *Gateway) dispatchResponse(event *proto.AaaResponseEvent) {
	g.pendingMu.RLock()
	p, ok := g.pending[event.SessionID]
	g.pendingMu.RUnlock()

	if !ok {
		return // No pending request for this session
	}

	select {
	case p.ch <- event.Payload:
	default:
	}
}

// writeSessionCorr writes SessionCorrEntry to Redis with TTL = DefaultPayloadTTL.
func (g *Gateway) writeSessionCorr(ctx context.Context, sessionID string, entry *proto.SessionCorrEntry) error {
	key := proto.SessionCorrKey(sessionID)
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return g.redis.Set(ctx, key, data, proto.DefaultPayloadTTL).Err()
}

// publishResponseBytes publishes raw response bytes to Redis pub/sub.
// This is the low-level publish used by RadiusHandler and DiameterHandler.
func (g *Gateway) publishResponseBytes(sessionID string, raw []byte) {
	var authCtxID string
	g.pendingMu.RLock()
	if p, ok := g.pending[sessionID]; ok {
		authCtxID = p.authCtxID
	}
	g.pendingMu.RUnlock()

	event := proto.AaaResponseEvent{
		Version:   g.version,
		SessionID: sessionID,
		AuthCtxID: authCtxID, // Populated from pending entry (fixes routing bug)
		Payload:   raw,
	}
	if err := g.publishResponse(g.ctx, &event); err != nil {
		g.logger.Error("failed to publish response bytes", "error", err, "session_id", sessionID)
	}
}

// forwardToBiz sends a server-initiated message to the Biz Pod via HTTP POST.
// It also writes the session correlation entry to Redis first.
func (g *Gateway) forwardToBiz(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte) {
	// 1. Look up session correlation from Redis
	entry, err := g.getSessionCorr(ctx, sessionID)
	if err != nil || entry == nil {
		g.logger.Warn("server_initiated_session_not_found",
			"session_id", sessionID,
			"transport", transportType,
			"message_type", messageType)
		return
	}

	// 2. Build and send the request to Biz Pod
	req := &proto.AaaServerInitiatedRequest{
		Version:       g.version,
		SessionID:     sessionID,
		AuthCtxID:     entry.AuthCtxID,
		TransportType: proto.TransportType(transportType),
		MessageType:   proto.MessageType(messageType),
		Payload:       raw,
	}

	body, err := json.Marshal(req)
	if err != nil {
		g.logger.Error("failed to marshal server-initiated request", "error", err)
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		g.cfg.BizServiceURL+"/aaa/server-initiated", bytes.NewReader(body))
	if err != nil {
		g.logger.Error("failed to create request to biz", "error", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(proto.HeaderName, g.version)

	resp, err := g.bizHTTPClient.Do(httpReq)
	if err != nil {
		g.logger.Error("biz service unavailable for server-initiated",
			"error", err, "session_id", sessionID)
		return
	}
	// Drain and close the body to allow connection reuse.
	// io.Copy is idempotent and safe even if the body is already empty.
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.logger.Warn("biz returned non-OK for server-initiated",
			"status", resp.StatusCode, "session_id", sessionID)
	}
}

// getSessionCorr reads the SessionCorrEntry from Redis for a given sessionID.
func (g *Gateway) getSessionCorr(ctx context.Context, sessionID string) (*proto.SessionCorrEntry, error) {
	key := proto.SessionCorrKey(sessionID)
	data, err := g.redis.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var entry proto.SessionCorrEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// publishResponse publishes AaaResponseEvent to the nssaa:aaa-response channel.
func (g *Gateway) publishResponse(ctx context.Context, event *proto.AaaResponseEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return g.redis.Publish(ctx, proto.AaaResponseChannel, data).Err()
}

// subscribeResponses subscribes to nssaa:aaa-response and dispatches to pending handlers.
func (g *Gateway) subscribeResponses(ctx context.Context) {
	ch := g.redis.PSubscribe(ctx, proto.AaaResponseChannel)
	defer ch.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch.Channel():
			var event proto.AaaResponseEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				g.logger.Error("failed to unmarshal response event", "error", err)
				continue
			}
			g.dispatchResponse(&event)
		}
	}
}

// VIPHealthHandler returns 200 if this AAA Gateway replica is the VIP owner, 503 otherwise.
func (g *Gateway) VIPHealthHandler(w http.ResponseWriter, r *http.Request) {
	statePath := g.cfg.KeepalivedStatePath
	data, err := readKeepalivedState(statePath)
	if err != nil {
		g.logger.Warn("keepalived state file not readable", "path", statePath, "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"vip_owner":false,"error":"state file not readable"}`)
		return
	}
	if data == "MASTER" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"vip_owner":true}`)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"vip_owner":false,"state":"%s"}`, data)
	}
}
