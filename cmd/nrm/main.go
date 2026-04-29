// Package main is the entry point for the NSSAAF NRM RESTCONF server.
// Spec: TS 28.541 §5.3.145-148 (NRM), RFC 8040 (RESTCONF).
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nrm"
)

var configPath = flag.String("config", "configs/nrm.yaml", "path to YAML configuration file")

func main() {
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.Component != config.ComponentNRM {
		slog.Error("config.component must be 'nrm'", "got", cfg.Component)
		os.Exit(1)
	}

	slog.Info("starting NSSAAF NRM",
		"version", cfg.Version,
		"listen_addr", cfg.NRM.ListenAddr,
	)

	// Populate NRMURL from ListenAddr so that Biz Pod NRMClient
	// can push events to this NRM server.
	nrmURL := cfg.NRM.ListenAddr
	if !strings.HasPrefix(nrmURL, "http") {
		nrmURL = "http://localhost" + nrmURL
	}
	cfg.NRM.NRMURL = nrmURL

	// Initialize NRM components.
	store := nrm.NewAlarmStore()

	// Apply alarm thresholds from config or use defaults.
	thresholds := nrm.DefaultAlarmThresholds()
	if cfg.NRM != nil && cfg.NRM.AlarmThresholds != nil {
		thresholds = &nrm.AlarmThresholds{
			FailureRatePercent:   cfg.NRM.AlarmThresholds.FailureRatePercent,
			EvaluationWindowSec:  cfg.NRM.AlarmThresholds.EvaluationWindowSec,
		}
	}

	alarmMgr := nrm.NewAlarmManager(store, thresholds, logger)

	// Start metrics window for sliding window evaluation (WR-12).
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	alarmMgr.StartMetricsWindow(appCtx)

	// Initialize RESTCONF server.
	server := nrm.NewServer(
		&nrm.NRMConfig{
			ListenAddr:     cfg.NRM.ListenAddr,
			NRMURL:         cfg.NRM.NRMURL,
			ReadTimeout:    cfg.NRM.ReadTimeout,
			WriteTimeout:   cfg.NRM.WriteTimeout,
			IdleTimeout:    cfg.NRM.IdleTimeout,
			AlarmThresholds: thresholds,
		},
		alarmMgr,
		store,
		logger,
	)

	// Start server in background.
	errCh := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			errCh <- err
		}
	}()

	// Wait for shutdown signal.
	select {
	case err := <-errCh:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case <-signalReceived():
		slog.Info("shutdown signal received")
		appCancel() // Stop background goroutines
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
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
