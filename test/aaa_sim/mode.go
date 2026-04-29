// Package aaa_sim provides a standalone AAA-S simulator for E2E testing.
// It implements RADIUS (UDP/1812) and Diameter (TCP/3868) EAP handling with
// configurable behavior via environment variables.
//
// Mode selection (AAA_SIM_MODE):
//
//	EAP_TLS_SUCCESS — Every Access-Request returns Access-Accept (EAP-Success).
//	EAP_TLS_FAILURE — Every Access-Request returns Access-Reject (EAP-Failure).
//	EAP_TLS_CHALLENGE — First request returns Access-Challenge, second returns Access-Accept.
//
// Shared secret (AAA_SIM_SECRET): defaults to "testing123".
//
// This package is built as a standalone binary via:
//	go build -o aaa-sim ./cmd/aaa-sim/
package aaa_sim

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// Mode defines the simulator behavior.
type Mode int

const (
	ModeEAP_TLS_SUCCESS Mode = iota
	ModeEAP_TLS_FAILURE
	ModeEAP_TLS_CHALLENGE
)

func (m Mode) String() string {
	switch m {
	case ModeEAP_TLS_SUCCESS:
		return "EAP_TLS_SUCCESS"
	case ModeEAP_TLS_FAILURE:
		return "EAP_TLS_FAILURE"
	case ModeEAP_TLS_CHALLENGE:
		return "EAP_TLS_CHALLENGE"
	default:
		return "UNKNOWN"
	}
}

// ParseMode converts a string mode name to a Mode value.
func ParseMode(s string) Mode {
	switch s {
	case "EAP_TLS_SUCCESS":
		return ModeEAP_TLS_SUCCESS
	case "EAP_TLS_FAILURE":
		return ModeEAP_TLS_FAILURE
	case "EAP_TLS_CHALLENGE":
		return ModeEAP_TLS_CHALLENGE
	default:
		return ModeEAP_TLS_SUCCESS
	}
}

// Run starts the AAA-S simulator.
func Run(mode Mode, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	radiusAddr := ":1812"
	diameterAddr := ":3868"
	diameterTransport := "tcp"
	// Test-only: default shared secret for local testing.
	// Override with AAA_SIM_SECRET environment variable.
	sharedSecret := []byte("testing123")
	if v := os.Getenv("AAA_SIM_RADIUS_ADDR"); v != "" {
		radiusAddr = v
	}
	if v := os.Getenv("AAA_SIM_DIAMETER_ADDR"); v != "" {
		diameterAddr = v
	}
	if v := os.Getenv("AAA_SIM_SECRET"); v != "" {
		sharedSecret = []byte(v)
	}
	if v := os.Getenv("AAA_SIM_DIAMETER_TRANSPORT"); v != "" {
		diameterTransport = v
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	radLn, err := net.ListenPacket("udp", radiusAddr)
	if err != nil {
		logger.Error("failed to listen RADIUS", "addr", radiusAddr, "error", err)
		return
	}
	radServer := NewRadiusServer(radLn, mode, sharedSecret, logger)
	go radServer.Run(ctx)

	diaServer := NewDiameterServer(diameterTransport, diameterAddr, mode, logger)
	go func() {
		if err := diaServer.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("diameter_server_error", "error", err)
		}
	}()

	logger.Info("aaa-sim started",
		"mode", mode.String(),
		"radius", radiusAddr,
		"diameter", diameterAddr,
		"diameter_transport", diameterTransport,
		"secret", "***")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()

	_ = radLn.Close()
	logger.Info("aaa-sim stopped")
}
