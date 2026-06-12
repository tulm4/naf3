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
	"net"
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

	BizPodEntryTTL time.Duration // TTL for BizPodEntry keys (default 60s)
}

// Gateway is the AAA Gateway component. It runs in a separate process from Biz Pods.
type Gateway struct {
	cfg Config

	redis         *redis.Client
	bizHTTPClient *http.Client
	version       string
	logger        *slog.Logger

	registry       *ServerInitiatedRegistry // tracks pending server-initiated requests
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

	// Create registry for server-initiated request tracking.
	// Biz Pods will call Complete() to deliver responses asynchronously.
	g.registry = NewServerInitiatedRegistry(30 * time.Second)

	g.radiusHandler = &RadiusHandler{
		logger:       cfg.Logger,
		tracer:       otel.Tracer("aaa-gateway/radius"),
		forwardToBiz: g.forwardToBiz,
		registry:     g.registry,
		sharedSecret: cfg.RadiusSharedSecret,
	}

	// Create the RADIUS forwarder for client-initiated path.
	// It wraps EAP payload in Access-Request with EAP-Message and Message-Authenticator.
	if cfg.RadiusServerAddress != "" {
		g.radiusForwarder = newRadiusForwarder(RadiusForwarderConfig{
			ServerAddress:   cfg.RadiusServerAddress,
			ServerPort:     1812,
			SharedSecret:    cfg.RadiusSharedSecret,
			Timeout:        10 * time.Second,
			MaxRetries:     3,
			ResponseWindow: 10 * time.Second,
		}, cfg.Logger)
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
		g.registry,
		cfg.DiameterHost,
		cfg.DiameterRealm,
	)

	return g
}

// startListeners starts all protocol goroutines. Must only be called by VIP owner.
func (g *Gateway) startListeners(ctx context.Context) error {
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

	// DLQ consumer — processes failed server-initiated messages from the DLQ list.
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		g.runDLQConsumer(g.ctx)
	}()

	return nil
}

// StartVIPAware blocks until this pod becomes VIP owner, then starts all listeners.
// Returns true if started successfully, false on context cancellation or error.
func (g *Gateway) StartVIPAware(ctx context.Context, statePath string) bool {
	// Dev/test mode: no state file → start immediately
	if statePath == "" || statePath == "/dev/null" {
		g.logger.Info("no keepalived state file, starting immediately (dev/test mode)")
		if err := g.startListeners(ctx); err != nil {
			g.logger.Error("startListeners failed", "error", err)
			return false
		}
		return true
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		state, err := readKeepalivedState(statePath)
		if err != nil {
			g.logger.Warn("keepalived state unreadable", "error", err)
		} else if state == "MASTER" {
			g.logger.Info("VIP acquired, starting all listeners")
			if err := g.startListeners(ctx); err != nil {
				g.logger.Error("startListeners failed", "error", err)
				return false
			}
			return true
		} else {
			g.logger.Info("not VIP owner, waiting", "state", state)
		}

		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// Start starts the AAA Gateway listeners unconditionally.
// Deprecated: use StartVIPAware for HA deployments.
func (g *Gateway) Start(ctx context.Context) error {
	return g.startListeners(ctx)
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

// HandleServerInitiatedResponse handles POST /aaa/server-initiated/response from Biz Pod.
// Biz Pod calls this after processing an ASR/RAR/CoA to deliver the result back to AAA Gateway.
func (g *Gateway) HandleServerInitiatedResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var resp proto.AaaServerInitiatedResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	g.logger.Info("HandleServerInitiatedResponse",
		"session_id", resp.SessionID,
		"auth_ctx_id", resp.AuthCtxID,
		"result_code", resp.ResultCode,
	)

	// Deliver response to the pending request via registry.
	g.registry.Complete(resp.SessionID, "ASR", &ServerInitiatedResponse{
		AuthCtxID:  resp.AuthCtxID,
		ResultCode: resp.ResultCode,
		Payload:    resp.Payload,
	})

	w.WriteHeader(http.StatusNoContent)
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

// getBizPodURL reads the BizPodEntry for a specific podID from Redis HASH.
// Returns empty string if the pod is not registered or TTL has expired.
func (g *Gateway) getBizPodURL(ctx context.Context, podID string) (string, error) {
	if podID == "" {
		return "", nil
	}
	key := proto.BizPodsKey(podID)
	data, err := g.redis.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	var entry proto.BizPodEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return "", err
	}
	return entry.URL, nil
}

// pickRandomLiveBizURL selects a random live Biz Pod from the Redis HASH.
// A pod is considered live if its LastSeen is within ttl.
func (g *Gateway) pickRandomLiveBizURL(ctx context.Context) (string, error) {
	ttl := g.cfg.BizPodEntryTTL
	if ttl == 0 {
		ttl = proto.BizPodEntryTTL
	}
	cutoff := time.Now().Add(-ttl).Unix()

	var livePods []string
	var cursor uint64
	for {
		keys, nextCursor, err := g.redis.Scan(ctx, cursor, "nssaa:biz:pod:*", 100).Result()
		if err != nil {
			return "", err
		}
		for _, key := range keys {
			data, err := g.redis.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			var entry proto.BizPodEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				continue
			}
			if entry.LastSeen >= cutoff && entry.URL != "" {
				livePods = append(livePods, entry.URL)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if len(livePods) == 0 {
		return "", nil
	}
	return livePods[time.Now().UnixNano()%int64(len(livePods))], nil
}

// selectTargetBizURL selects the target URL for a server-initiated message.
// Priority: 1) direct pod lookup via podID, 2) random live pod, 3) static BizServiceURL.
func (g *Gateway) selectTargetBizURL(ctx context.Context, podID string) (string, error) {
	// 1. Try direct lookup
	if podID != "" {
		url, err := g.getBizPodURL(ctx, podID)
		if err != nil {
			g.logger.Warn("getBizPodURL failed, falling back", "pod_id", podID, "error", err)
		} else if url != "" {
			return url, nil
		}
	}
	// 2. Fallback: random live pod
	url, err := g.pickRandomLiveBizURL(ctx)
	if err != nil {
		g.logger.Warn("pickRandomLiveBizURL failed, falling back to static", "error", err)
	} else if url != "" {
		return url, nil
	}
	// 3. Final fallback: static URL
	return g.cfg.BizServiceURL, nil
}

const (
	serverInitMaxRetries   = 3
	serverInitRetryBase    = 1 * time.Second
	serverInitRetryMax     = 3 * time.Second
)

// forwardToBiz sends a server-initiated message to the Biz Pod via HTTP POST.
// It selects the target pod dynamically, retries on connection errors, and pushes
// failed messages to the DLQ after all retries are exhausted.
func (g *Gateway) forwardToBiz(ctx context.Context, sessionID string, transportType string, messageType string, raw []byte) {
	// 1. Look up session correlation to get target PodID
	entry, err := g.getSessionCorr(ctx, sessionID)
	if err != nil || entry == nil {
		g.logger.Warn("server_initiated_session_not_found",
			"session_id", sessionID,
			"transport", transportType,
			"message_type", messageType)
		return
	}

	// 2. Build the request body once
	req := &proto.AaaServerInitiatedRequest{
		Version:       g.version,
		SessionID:    sessionID,
		AuthCtxID:    entry.AuthCtxID,
		TransportType: proto.TransportType(transportType),
		MessageType:  proto.MessageType(messageType),
		Payload:      raw,
	}
	body, err := json.Marshal(req)
	if err != nil {
		g.logger.Error("failed to marshal server-initiated request", "error", err)
		return
	}

	// 3. Retry loop
	var lastErr error
	for attempt := 0; attempt < serverInitMaxRetries; attempt++ {
		if attempt > 0 {
			sleep := time.Duration(attempt) * serverInitRetryBase
			if sleep > serverInitRetryMax {
				sleep = serverInitRetryMax
			}
			time.Sleep(sleep)
		}

		targetURL, err := g.selectTargetBizURL(ctx, entry.PodID)
		if err != nil {
			lastErr = err
			g.logger.Warn("selectTargetBizURL failed",
				"attempt", attempt+1, "error", err)
			continue
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			targetURL+"/aaa/server-initiated", bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set(proto.HeaderName, g.version)

		resp, err := g.bizHTTPClient.Do(httpReq)
		if err != nil {
			lastErr = err
			isConnErr := isConnectionError(err)
			g.logger.Warn("biz HTTP call failed",
				"attempt", attempt+1, "error", err, "target_url", targetURL,
				"retrying", isConnErr)
			if !isConnErr {
				break
			}
			continue
		}
		if resp.StatusCode == http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return // Success
		}
		g.logger.Warn("biz returned non-OK",
			"status", resp.StatusCode, "session_id", sessionID, "target_url", targetURL)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		// Non-connection errors and 4xx/5xx are not retried
		break
	}

	// 4. All retries exhausted → push to DLQ
	g.logger.Error("server_initiated_all_retries_failed",
		"session_id", sessionID, "error", lastErr, "pod_id", entry.PodID)
	g.pushDLQ(ctx, sessionID, transportType, messageType, body)
}

// isConnectionError returns true if err is a connection/dial timeout error.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "dial" || opErr.Op == "read" || opErr.Op == "write"
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// pushDLQ pushes a failed server-initiated message to the Redis DLQ list.
func (g *Gateway) pushDLQ(ctx context.Context, sessionID, transportType, messageType string, body []byte) {
	msg := map[string]interface{}{
		"sessionID":     sessionID,
		"transportType": transportType,
		"messageType":   messageType,
		"payload":       body,
		"attemptCount":  0,
		"queuedAt":      time.Now().Unix(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		g.logger.Error("failed to marshal DLQ message", "error", err)
		return
	}
	if err := g.redis.RPush(ctx, proto.DLQKey, data).Err(); err != nil {
		g.logger.Error("failed to push to DLQ", "error", err)
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
