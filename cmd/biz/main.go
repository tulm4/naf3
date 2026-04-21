// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/operator/nssAAF/internal/api/aiw"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/redis/go-redis/v9"
)

var configPath = flag.String("config", "configs/biz.yaml", "path to YAML configuration file")

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

	// Build API root URL
	apiRoot := cfg.Server.Addr
	if !hasScheme(apiRoot) {
		apiRoot = "http://" + apiRoot
	}

	// ─── Data stores ────────────────────────────────────────────────────────
	nssaaStore := nssaa.NewInMemoryStore()
	aiwStore := aiw.NewInMemoryStore()

	// ─── HTTP AAA client (satisfies eap.AAAClient = AAARouter) ────────────────
	tlsCfg := &tls.Config{}
	if cfg.Biz.UseMTLS {
		tlsCfg.RootCAs = mustLoadCertPool(cfg.Biz.TLSCA)
		tlsCfg.Certificates = []tls.Certificate{mustLoadCert(cfg.Biz.TLSCert, cfg.Biz.TLSKey)}
		tlsCfg.ServerName = "aaa-gateway" // SNI for AAA Gateway cert verification
	}
	aaaClient := newHTTPAAAClient(
		cfg.Biz.AAAGatewayURL,
		cfg.Redis.Addr,
		podID,
		cfg.Version,
		&http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
			Timeout:   30 * time.Second,
		},
	)

	// ─── N58: Nnssaaf_NSSAA ────────────────────────────────────────────────
	nssaaHandler := nssaa.NewHandler(nssaaStore,
		nssaa.WithAPIRoot(apiRoot),
		nssaa.WithAAA(aaaClient), // aaaClient satisfies eap.AAAClient
	)
	nssaaRouter := nssaa.NewRouter(nssaaHandler, apiRoot)

	// ─── N60: Nnssaaf_AIW ─────────────────────────────────────────────────
	aiwHandler := aiw.NewHandler(aiwStore,
		aiw.WithAPIRoot(apiRoot),
	)
	aiwRouter := aiw.NewRouter(aiwHandler, apiRoot)

	// ─── Internal AAA forwarding endpoints (for AAA Gateway) ─────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/aaa/forward", handleAaaForward)
	mux.HandleFunc("/aaa/server-initiated", handleServerInitiated)

	// ─── Compose with N58/N60 handlers ────────────────────────────────────
	mux.Handle("/nnssaaf-nssaa/", http.StripPrefix("/nnssaaf-nssaa", nssaaRouter))
	mux.Handle("/nnssaaf-aiw/", http.StripPrefix("/nnssaaf-aiw", aiwRouter))

	// ─── OAM endpoints ─────────────────────────────────────────────────────
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/ready", handleReady)

	// ─── Middleware stack ─────────────────────────────────────────────────
	var handler http.Handler = mux
	handler = common.RecoveryMiddleware(handler)
	handler = common.RequestIDMiddleware(handler)
	handler = common.LoggingMiddleware(handler)
	handler = common.CORSMiddleware(handler)

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("biz HTTP server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// ─── Biz Pod heartbeat: register pod in Redis SET ──────────────────────
	go podHeartbeat(context.Background(), cfg.Redis.Addr, podID)

	select {
	case err := <-errCh:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case <-signalReceived():
		slog.Info("shutdown signal received")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		aaaClient.Close()
	}
}

func handleAaaForward(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
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
		http.Error(w, "unknown message type", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(proto.AaaServerInitiatedResponse{
		Version:   proto.CurrentVersion,
		SessionID: req.SessionID,
		AuthCtxID: req.AuthCtxID,
		Payload:   respPayload,
	})
}

func handleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_re_auth", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
	return []byte{2, 0, 0, 12}
}

func handleRevocation(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_revoc", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
	return []byte{}
}

func handleCoA(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
	slog.Info("handle_coa", "auth_ctx_id", req.AuthCtxID, "session_id", req.SessionID)
	return []byte{2, 0, 0, 12}
}

// podHeartbeat registers the Biz Pod in the Redis SET and refreshes every 30 seconds.
// On context cancellation, it removes the pod from the SET.
func podHeartbeat(ctx context.Context, redisAddr, podID string) {
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()

	// Register immediately on startup
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

// hasScheme returns true if s already contains a URL scheme prefix.
func hasScheme(s string) bool {
	return len(s) >= 4 && (s[:4] == "http" || s[:4] == "Http")
}

// mustLoadCertPool loads and parses a CA certificate file into an x509.CertPool.
// Panics on error — called during startup validation only.
func mustLoadCertPool(caPath string) *x509.CertPool {
	data, err := os.ReadFile(caPath)
	if err != nil {
		panic("failed to read TLS CA cert: " + err.Error())
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		panic("failed to parse TLS CA cert from: " + caPath)
	}
	return pool
}

// mustLoadCert loads a client certificate and key for mTLS.
// Panics on error — called during startup validation only.
func mustLoadCert(certPath, keyPath string) tls.Certificate {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		panic("failed to load TLS cert/key pair: " + err.Error())
	}
	return cert
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}

func handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, `{"status":"ready","service":"nssAAF-biz"}`)
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
