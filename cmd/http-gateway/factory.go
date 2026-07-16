// Package main is the entry point for the NSSAAF HTTP Gateway.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/nrf"
	"github.com/operator/nssAAF/internal/resilience"
)

// HTTPGatewayPod holds all dependencies for the HTTP Gateway Pod.
type HTTPGatewayPod struct {
	NRFClient *nrf.Client
	Logger    *slog.Logger
}

// httpGatewayFactory creates HTTPGatewayPod instances with dependency injection.
type httpGatewayFactory struct {
	cfg    *config.Config
	logger *slog.Logger
}

// NewHTTPGatewayFactory creates a new factory.
func NewHTTPGatewayFactory(cfg *config.Config, opts ...HTTPGatewayOption) *httpGatewayFactory {
	f := &httpGatewayFactory{cfg: cfg, logger: slog.Default()}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// HTTPGatewayOption configures an httpGatewayFactory.
type HTTPGatewayOption func(*httpGatewayFactory)

// WithHTTPGatewayLogger sets the logger on the factory.
func WithHTTPGatewayLogger(logger *slog.Logger) HTTPGatewayOption {
	return func(f *httpGatewayFactory) { f.logger = logger }
}

// newNFRegistry creates the circuit breaker registry for NRF communication.
func (f *httpGatewayFactory) newNFRegistry() *resilience.Registry {
	cfg := f.cfg.InternalComm.Native.CB
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.RecoveryTimeout == 0 {
		cfg.RecoveryTimeout = 10 * time.Second
	}
	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = 2
	}
	return resilience.NewRegistry(cfg.FailureThreshold, cfg.RecoveryTimeout, cfg.SuccessThreshold)
}

// Build creates a fully initialized HTTPGatewayPod with all dependencies wired.
func (f *httpGatewayFactory) Build(ctx context.Context) (*HTTPGatewayPod, func(), error) {
	cleanup := func() {}

	// Circuit breaker registry for NRF (CB-G1)
	internalNFRegistry := f.newNFRegistry()
	nrfFactory := nfclient.NewFactory(internalNFRegistry)
	nrfClient := nrf.NewClient(f.cfg.NRF, nrfFactory)

	// Load the NF profile YAML (if configured) before starting the heartbeat.
	// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 1
	if f.cfg.NRF.ProfilePath != "" {
		if err := nrfClient.SetProfilePath(f.cfg.NRF.ProfilePath, f.cfg.NRF.Heartbeat); err != nil {
			f.logger.Warn("failed to load NFProfile; continuing without profile-based registration",
				"path", f.cfg.NRF.ProfilePath,
				"error", err,
			)
		}
	}

	// StartHeartbeat performs the initial PUT registration synchronously and
	// then runs the PATCH heartbeat loop.
	// Failures are non-fatal: StartHeartbeat handles background retries.
	if err := nrfClient.StartHeartbeat(ctx); err != nil {
		f.logger.Warn("nrf heartbeat start failed; NRF registration will retry in background",
			"error", err,
		)
	}

	f.logger.Info("HTTP Gateway NRF client initialized",
		"base_url", f.cfg.NRF.BaseURL,
		"profile_path", f.cfg.NRF.ProfilePath,
	)

	return &HTTPGatewayPod{
		NRFClient: nrfClient,
		Logger:    f.logger,
	}, cleanup, nil
}
