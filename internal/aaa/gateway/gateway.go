// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/proto"
	"github.com/redis/go-redis/v9"
)

// Config holds AAA Gateway configuration.
type Config struct {
	BizServiceURL       string        // http://svc-nssaa-biz:8080
	RedisAddr           string        // Redis address for pub/sub and session correlation
	ListenRADIUS       string        // ":1812" — UDP listen address for RADIUS
	ListenDIAMETER     string        // ":3868" — listen address for Diameter (TCP or SCTP)
	AAAGatewayURL      string        // self-referential for health checks
	Logger             *slog.Logger
	Version            string        // Injected at build time
	DiameterProtocol   string        // "tcp" or "sctp"
	RedisMode          string        // "standalone" or "sentinel"
	KeepalivedStatePath string      // path to keepalived state file
}

// Gateway is the AAA Gateway component. It runs in a separate process from Biz Pods.
type Gateway struct {
	cfg Config

	redis         *redis.Client
	bizHTTPClient *http.Client
	version       string
	logger        *slog.Logger

	radiusHandler   *RadiusHandler
	diameterHandler *DiameterHandler

	// pending maps SessionID → response channel (used for client-initiated flow)
	pending   map[string]chan []byte
	pendingMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new AAA Gateway.
func New(cfg Config) *Gateway {
	g := &Gateway{
		cfg:     cfg,
		version: cfg.Version,
		logger:  cfg.Logger,
		pending: make(map[string]chan []byte),
	}

	g.redis = newRedisClient(cfg.RedisAddr, cfg.RedisMode)

	g.radiusHandler = &RadiusHandler{
		logger:          cfg.Logger,
		publishResponse: g.publishResponseBytes,
	}

	g.diameterHandler = &DiameterHandler{
		logger:          cfg.Logger,
		publishResponse: g.publishResponseBytes,
	}

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

	// 2. Set up response channel for this session
	ch := make(chan []byte, 1)
	g.pendingMu.Lock()
	g.pending[req.SessionID] = ch
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
		response, err = g.radiusHandler.Forward(ctx, req.Payload, req.SessionID)
	case proto.TransportDIAMETER:
		response, err = g.diameterHandler.Forward(ctx, req.Payload, req.SessionID)
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
func (g *Gateway) dispatchResponse(event *proto.AaaResponseEvent) {
	g.pendingMu.RLock()
	ch, ok := g.pending[event.SessionID]
	g.pendingMu.RUnlock()

	if !ok {
		return // No pending request for this session
	}

	select {
	case ch <- event.Payload:
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
	event := proto.AaaResponseEvent{
		Version:   g.version,
		SessionID: sessionID,
		AuthCtxID: "",
		Payload:   raw,
	}
	if err := g.publishResponse(g.ctx, &event); err != nil {
		g.logger.Error("failed to publish response bytes", "error", err, "session_id", sessionID)
	}
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
