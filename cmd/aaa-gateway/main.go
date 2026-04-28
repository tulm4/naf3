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

	"github.com/operator/nssAAF/internal/aaa/gateway"
	"github.com/operator/nssAAF/internal/config"
)

var configPath = flag.String("config", "configs/aaa-gateway.yaml", "path to YAML configuration file")

func main() {
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.Component != config.ComponentAAAGateway {
		slog.Error("config.component must be 'aaa-gateway'", "got", cfg.Component)
		os.Exit(1)
	}

	slog.Info("starting NSSAAF AAA Gateway",
		"version", cfg.Version,
		"radius_addr", cfg.AAAgw.ListenRADIUS,
		"diameter_addr", cfg.AAAgw.ListenDIAMETER,
	)

	gw := gateway.New(gateway.Config{
		BizServiceURL:         cfg.AAAgw.BizServiceURL,
		RedisAddr:             cfg.Redis.Addr,
		ListenRADIUS:          cfg.AAAgw.ListenRADIUS,
		ListenDIAMETER:        cfg.AAAgw.ListenDIAMETER,
		AAAGatewayURL:         "http://" + cfg.Server.Addr,
		Logger:                logger,
		Version:               cfg.Version,
		DiameterProtocol:      cfg.AAAgw.DiameterProtocol,
		DiameterServerAddress: cfg.AAAgw.DiameterServerAddress,
		DiameterRealm:         cfg.AAAgw.DiameterRealm,
		DiameterHost:          cfg.AAAgw.DiameterHost,
		RadiusServerAddress:   cfg.AAAgw.RadiusServerAddress,
		RadiusSharedSecret:    cfg.AAAgw.RadiusSharedSecret,
		RedisMode:             cfg.AAAgw.RedisMode,
		KeepalivedStatePath:   cfg.AAAgw.KeepalivedStatePath,
	})

	// Expose HTTP endpoints for Biz Pod communication
	http.HandleFunc("/aaa/forward", gw.HandleForward)
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/health/vip", gw.VIPHealthHandler)

	// Start HTTP server in background
	errCh := make(chan error, 1)
	go func() {
		slog.Info("aaa-gateway HTTP listening", "addr", cfg.Server.Addr)
		if err := http.ListenAndServe(cfg.Server.Addr, nil); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Start the gateway (UDP/TCP listeners)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := gw.Start(ctx); err != nil {
		slog.Error("gateway start failed", "error", err)
		os.Exit(1)
	}

	<-signalReceived()
	slog.Info("shutting down AAA Gateway")
	cancel()
	gw.Stop()
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
