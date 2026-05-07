// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"encoding/json"
	"flag"
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
	pgHealth    func(ctx context.Context) error
	redisHealth func(ctx context.Context) error
	nrfHealth   interface{ IsRegistered() bool }
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
	nrfHealth = pod.NRFClient

	// Start HTTP server
	errCh := make(chan error, 1)
	go func() {
		slog.Info("biz HTTP server listening", "addr", pod.Server.Addr)
		if err := pod.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Biz Pod heartbeat: register pod in Redis SET
	go podHeartbeat(context.Background(), cfg.Redis.Addr, podID)

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

	var respPayload []byte
	switch req.MessageType {
	case proto.MessageTypeRAR:
		respPayload = handleReAuth(r.Context(), &req)
	case proto.MessageTypeASR:
		respPayload = handleRevocation(r.Context(), &req)
	case proto.MessageTypeCoA:
		respPayload = handleCoA(r.Context(), &req)
	default:
		slog.Warn("handle_server_initiated: unknown message type",
			"message_type", req.MessageType,
			"session_id", req.SessionID,
			"auth_ctx_id", req.AuthCtxID,
		)
		http.Error(w, "unknown message type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(proto.AaaServerInitiatedResponse{
		Version:   proto.CurrentVersion,
		SessionID: req.SessionID,
		AuthCtxID: req.AuthCtxID,
		Payload:   respPayload,
	})
}

func handleReAuth(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_re_auth", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
	return []byte{2, 0, 0, 12}
}

func handleRevocation(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_revoc", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
	return []byte{}
}

func handleCoA(_ context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_coa", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
	return []byte{2, 0, 0, 12}
}

// podHeartbeat registers the Biz Pod in the Redis SET and refreshes every 30 seconds.
func podHeartbeat(ctx context.Context, redisAddr, podID string) {
	rdb := goredis.NewClient(&goredis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()

	if err := rdb.SAdd(ctx, proto.PodsKey, podID).Err(); err != nil {
		slog.Warn("failed to register pod in Redis", "error", err, "pod_id", podID)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			rdb.SRem(ctx, proto.PodsKey, podID)
			return
		case <-ticker.C:
			rdb.SAdd(ctx, proto.PodsKey, podID)
		}
	}
}

// handleLiveness implements /healthz/live — always 200, no dependency checks.
func handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}

// handleReadiness implements /healthz/ready — checks PostgreSQL, Redis, NRF.
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

	if nrfHealth != nil && nrfHealth.IsRegistered() {
		checks["nrf_registration"] = "ok"
	} else {
		checks["nrf_registration"] = "degraded (retrying)"
	}

	allOk := true
	for _, v := range checks {
		if v != "ok" && v != "degraded (retrying)" && v != "degraded (not initialized)" {
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
