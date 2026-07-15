// Package main is the entry point for the NSSAAF AAA Gateway.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/operator/nssAAF/internal/aaa/gateway"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/radius"
	"github.com/operator/nssAAF/internal/tracing"
)

var configPath = flag.String("config", "configs/aaa-gateway.yaml", "path to YAML configuration file")

func main() {
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Enable RADIUS protocol debug logging for troubleshooting.
	radius.SetDebugLogger(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.Component != config.ComponentAAAGateway {
		slog.Error("config.component must be 'aaa-gateway'", "got", cfg.Component)
		os.Exit(1)
	}

	podID, _ := os.Hostname()
	slog.Info("starting NSSAAF AAA Gateway",
		"version", cfg.Version,
		"listen_radius", cfg.AAAgw.ListenRADIUS, // server-initiated inbound
	)

	// Per-UE debug subsystem (optional; off by default).
	// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §6
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize OTel tracing so otelhttp.NewHandler creates valid spans.
	// This must happen before the debug subsystem and HTTP server are started.
	shutdownTracing := tracing.Init("nssAAF-aaa-gw", cfg.Version, podID)
	defer shutdownTracing()

	var dbg *debug.Debug
	if cfg.Debug.Enabled {
		var err error
		dbg, err = debug.New(ctx, debug.Config{
			Enabled:   cfg.Debug.Enabled,
			RedisAddr: cfg.Debug.RedisAddr,
			Service:   "aaa-gw",
			PodID:     podID,
			TTL:       cfg.Debug.TTL,
			MaxLen:    cfg.Debug.MaxLen,
		})
		if err != nil {
			slog.Warn("debug subsystem init failed; continuing without debug", "error", err)
			dbg = nil
		}
	}

	gw := gateway.New(gateway.Config{
		BizServiceURL:         cfg.AAAgw.BizServiceURL,
		RedisAddr:             cfg.Redis.Addr,
		ListenRADIUS:          cfg.AAAgw.ListenRADIUS,
		AAAGatewayURL:         "http://" + cfg.Server.Addr,
		Logger:                logger,
		Version:               cfg.Version,
		DiameterServerAddress: cfg.AAAgw.DiameterServerAddress,
		DiameterRealm:         cfg.AAAgw.DiameterRealm,
		DiameterHost:          cfg.AAAgw.DiameterHost,
		DiameterTransport:     cfg.AAAgw.DiameterTransport,
		RadiusServerAddress:   cfg.AAAgw.RadiusServerAddress,
		RadiusSharedSecret:    cfg.AAAgw.RadiusSharedSecret,
		RedisMode:             cfg.AAAgw.RedisMode,
		VIPAddress:            cfg.AAAgw.VIPAddress,
		DLQ:                   cfg.AAAgw.DLQ,
		Debug:                 dbg,
	})

	// Expose HTTP endpoints for Biz Pod communication.
	// Wrap the mux with otelhttp so spans are emitted for every inbound request,
	// matching the Biz Pod pattern.
	mux := http.NewServeMux()
	mux.HandleFunc("/aaa/forward", gw.HandleForward)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/health/vip", gw.VIPHealthHandler)
	handler := otelhttp.NewHandler(mux, "aaa-gw")

	// Start HTTP server in background
	errCh := make(chan error, 1)
	go func() {
		slog.Info("aaa-gateway HTTP listening", "addr", cfg.Server.Addr)
		if err := http.ListenAndServe(cfg.Server.Addr, handler); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// VIP-aware startup: start listeners only when this replica owns the VIP
	if !gw.StartVIPAware(ctx, cfg.AAAgw.VIPAddress) {
		slog.Error("gateway failed to acquire VIP or start listeners")
		os.Exit(1)
	}

	<-signalReceived()
	slog.Info("shutting down AAA Gateway")
	cancel()
	gw.Stop()
	if dbg != nil {
		_ = dbg.Close()
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"status":"ok","service":"aaa-gateway"}`)
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
