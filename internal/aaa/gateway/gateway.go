// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/operator/nssAAF/internal/proto"
	"github.com/redis/go-redis/v9"
)

// Config holds AAA Gateway configuration.
type Config struct {
	BizServiceURL    string // http://svc-nssaa-biz:8080
	RedisAddr        string // Redis address for pub/sub and session correlation
	ListenRADIUS     string // ":1812" — UDP listen address for RADIUS
	ListenDIAMETER   string // ":3868" — listen address for Diameter (TCP or SCTP)
	AAAGatewayURL    string // self-referential for health checks
	Logger           *slog.Logger
	Version          string // Injected at build time
	DiameterProtocol string // "tcp" or "sctp"

	// Diameter client-initiated config (PLAN §2.3.5):
	// Required for DER/DEA forwarding to AAA-S.
	DiameterServerAddress string // e.g. "nss-aaa-server:3868"
	DiameterRealm         string // e.g. "operator.com"
	DiameterHost          string // Origin-Host for CER (AAA Gateway identity)

	// RADIUS client-initiated config:
	// Required for Access-Request forwarding to AAA-S.
	RadiusServerAddress string // e.g. "nss-aaa-server:1812"
	RadiusSharedSecret  string // Shared secret for Message-Authenticator

	RedisMode           string // "standalone" or "sentinel"
	KeepalivedStatePath string // path to keepalived state file
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
	radiusForwarder *radiusForwarder // RADIUS client (client-initiated path)
	diamForwarder   *diamForwarder   // Diameter client (client-initiated path)

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
	}

	g.redis = newRedisClient(cfg.RedisAddr, cfg.RedisMode)
	g.bizHTTPClient = &http.Client{Timeout: 30 * time.Second}

	g.radiusHandler = &RadiusHandler{
		logger:       cfg.Logger,
		tracer:      otel.Tracer("aaa-gateway/radius"),
		forwardToBiz: g.forwardToBiz,
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
		cfg.DiameterRealm,         // destRealm
		cfg.Logger,
	)

	g.diameterHandler = NewDiameterHandler(
		cfg.Logger,
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
		_ = g.redis.Close()
	}
	if g.diamForwarder != nil {
		_ = g.diamForwarder.Close()
	}
	if g.radiusForwarder != nil {
		_ = g.radiusForwarder.Close()
	}
}

// ForwardEAP satisfies proto.BizAAAClient.
// It receives AaaForwardRequest from Biz Pod, writes session correlation to Redis,
// forwards to AAA-S, and returns the response directly to the caller.
func (g *Gateway) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
	// 1. Write session correlation entry to Redis (before forwarding)
	// Wire os.Hostname() now so direct pod lookup works immediately.
	hostname, _ := os.Hostname()
	entry := proto.SessionCorrEntry{
		AuthCtxID: req.AuthCtxID,
		PodID:     hostname, // Written once here; read on server-initiated routing
		Sst:       req.Sst,
		Sd:        req.Sd,
		CreatedAt: time.Now().Unix(),
	}
	if err := g.writeSessionCorr(ctx, req.SessionID, &entry); err != nil {
		return nil, fmt.Errorf("aaa-gateway: failed to write session corr: %w", err)
	}

	// 2. Forward to AAA-S based on transport type
	var response []byte
	var err error
	switch req.TransportType {
	case proto.TransportRADIUS:
		response, err = g.radiusForwarder.Forward(ctx, req.Payload, req.SessionID, req.Sst, req.Sd)
	case proto.TransportDIAMETER:
		response, err = g.diamForwarder.Forward(ctx, req.Payload, req.SessionID, req.Sst, req.Sd)
	default:
		return nil, fmt.Errorf("aaa-gateway: unknown transport type: %s", req.TransportType)
	}
	if err != nil {
		return nil, fmt.Errorf("aaa-gateway: forward failed: %w", err)
	}

	// 3. Return response directly to caller (no Redis pub/sub needed)
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
	_ = json.NewEncoder(w).Encode(resp)
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
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

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
		if errors.Is(err, redis.Nil) {
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

// VIPHealthHandler returns 200 if this AAA Gateway replica is the VIP owner, 503 otherwise.
func (g *Gateway) VIPHealthHandler(w http.ResponseWriter, r *http.Request) {
	statePath := g.cfg.KeepalivedStatePath
	data, err := readKeepalivedState(statePath)
	if err != nil {
		g.logger.Warn("keepalived state file not readable", "path", statePath, "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, `{"vip_owner":false,"error":"state file not readable"}`)
		return
	}
	if data == "MASTER" {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"vip_owner":true}`)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, `{"vip_owner":false,"state":"%s"}`, data)
	}
}
