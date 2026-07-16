// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
	goredis "github.com/redis/go-redis/v9"
)

var configPath = flag.String("config", "configs/biz.yaml", "path to YAML configuration file")

// Health check closure variables (set during initialization)
var (
	pgHealth                func(ctx context.Context) error
	redisHealth             func(ctx context.Context) error
	serverInitiatedHandler  func(context.Context, *proto.AaaServerInitiatedRequest) (*proto.AaaServerInitiatedResponse, error)
)

func main() {
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.Component != config.ComponentBiz {
		slog.Error("config.component must be 'biz'", "got", cfg.Component)
		os.Exit(1)
	}

	podID, _ := os.Hostname()
	slog.Info("starting NSSAAF Biz Pod",
		"pod_id", podID,
		"version", cfg.Version,
		"use_mtls", cfg.Biz.UseMTLS,
	)

	// Context for initialization (long-running operations)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build BizPod using factory
	factory := NewBizPodFactory(cfg,
		WithLogger(logger),
		WithPodID(podID),
	)
	pod, podCleanup, err := factory.Build(ctx)
	if err != nil {
		slog.Error("failed to build BizPod", "error", err)
		os.Exit(1)
	}
	defer podCleanup()

	// Wire health check closures
	pgHealth = pod.Pool.Ping
	redisHealth = func(ctx context.Context) error {
		return pod.RedisPool.Client().Ping(ctx).Err()
	}

	// Start HTTP server
	errCh := make(chan error, 1)
	go func() {
		slog.Info("biz HTTP server listening", "addr", pod.Server.Addr)
		if err := pod.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Biz Pod heartbeat: register pod in Redis per-pod key with TTL
	podCtx, podCancel := context.WithCancel(context.Background())
	podURL := fmt.Sprintf("http://%s%s", podID, cfg.Server.Addr)
	go podHeartbeat(podCtx, cfg.Redis.Addr, podID, podURL)
	pod.HeartbeatCancel = podCancel

	select {
	case err := <-errCh:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case <-signalReceived():
		slog.Info("shutdown signal received")
		pod.Close()
	}
}

// handleAaaForward forwards a request from the AAA Gateway to the Biz Pod.
// This endpoint is reserved for future AAA-initiated callbacks.
func handleAaaForward(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "aaa-forward is not implemented; use /aaa/server-initiated", http.StatusNotImplemented)
}

func handleServerInitiated(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	var req proto.AaaServerInitiatedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if serverInitiatedHandler == nil {
		http.Error(w, "server initiated handler not configured", http.StatusServiceUnavailable)
		return
	}

	resp, err := serverInitiatedHandler(r.Context(), &req)
	if err != nil {
		slog.Warn("handle_server_initiated: handler failed",
			"message_type", req.MessageType,
			"session_id", req.SessionID,
			"auth_ctx_id", req.AuthCtxID,
			"error", err,
		)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func handleReAuth(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_re_auth",
		"auth_ctx_id", req.AuthCtxID,
		"session_id", req.SessionID,
		"payload_len", len(req.Payload))
	// TODO (Wave 3 continuation): Load EAP session from Redis, process re-auth, notify AMF
	return []byte{2, 0, 0, 12}
}

func handleRevocation(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_revoc",
		"auth_ctx_id", req.AuthCtxID,
		"session_id", req.SessionID)
	// TODO (Wave 3 continuation): Load EAP session, mark revoked, notify AMF
	return []byte{}
}

func handleCoA(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_coa",
		"auth_ctx_id", req.AuthCtxID,
		"session_id", req.SessionID)
	// TODO (Wave 3 continuation): Load session, apply attribute changes, persist
	return []byte{2, 0, 0, 12}
}

// loadEAPSessionFromRedis loads an EAP session by authCtxID from Redis.
// Returns nil if not found.
// TODO: Wire to actual EAP session store (redis-based, see internal/eap/session_redis.go)
func loadEAPSessionFromRedis(ctx context.Context, redisAddr, authCtxID string) ([]byte, error) {
	rdb := goredis.NewClient(&goredis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()
	key := "nssaa:eap:session:" + authCtxID
	data, err := rdb.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// podHeartbeat registers the Biz Pod in the Redis HASH and refreshes every 30 seconds.
// On shutdown, deletes the pod entry.
func podHeartbeat(ctx context.Context, redisAddr, podID, podURL string) {
	rdb := goredis.NewClient(&goredis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()

	key := proto.BizPodsKey(podID)
	entry := proto.BizPodEntry{
		URL:      podURL,
		LastSeen: time.Now().Unix(),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		slog.Warn("failed to marshal BizPodEntry", "error", err)
		return
	}

	if err := rdb.Set(ctx, key, data, proto.BizPodEntryTTL).Err(); err != nil {
		slog.Warn("failed to register pod in Redis HASH", "error", err, "pod_id", podID)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := rdb.Del(ctx, key).Err(); err != nil {
				slog.Warn("failed to unregister pod on shutdown", "error", err, "pod_id", podID)
			}
			return
		case <-ticker.C:
			entry.LastSeen = time.Now().Unix()
			data, _ := json.Marshal(entry)
			if err := rdb.Set(ctx, key, data, proto.BizPodEntryTTL).Err(); err != nil {
				slog.Warn("failed to refresh pod heartbeat", "error", err, "pod_id", podID)
			}
		}
	}
}

// handleLiveness implements /healthz/live — always 200, no dependency checks.
func handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}

// handleReadiness implements /healthz/ready — checks PostgreSQL, Redis.
// NRF registration is owned by HTTP Gateway (Phase 4 migration), so the
// Biz Pod no longer reports NRF health here.
func handleReadiness(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}

	if pgHealth != nil {
		if err := pgHealth(r.Context()); err != nil {
			checks["postgres"] = "unhealthy: " + err.Error()
		} else {
			checks["postgres"] = "ok"
		}
	} else {
		checks["postgres"] = "degraded (not initialized)"
	}

	if redisHealth != nil {
		if err := redisHealth(r.Context()); err != nil {
			checks["redis"] = "unhealthy: " + err.Error()
		} else {
			checks["redis"] = "ok"
		}
	} else {
		checks["redis"] = "degraded (not initialized)"
	}

	allOk := true
	for _, v := range checks {
		if v != "ok" && v != "degraded (not initialized)" {
			allOk = false
			break
		}
	}

	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	if allOk {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(checks)
}

func signalReceived() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		close(ch)
	}()
	return ch
}
