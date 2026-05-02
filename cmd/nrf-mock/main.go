// cmd/nrf-mock is a standalone NRF mock server for fullchain E2E tests.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/operator/nssAAF/internal/nrfserver"
)

func main() {
	addr := flag.String("addr", ":8081", "address to listen on")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	srv := nrfserver.NewServer()

	// Configure NF statuses from environment variable.
	// Format: NRF_NF_STATUS=udm-001:REGISTERED,ausf-001:REGISTERED
	if statusEnv := os.Getenv("NRF_NF_STATUS"); statusEnv != "" {
		for _, entry := range strings.Split(statusEnv, ",") {
			parts := strings.Split(entry, ":")
			if len(parts) == 2 {
				srv.SetNFStatus(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}

	// Configure service endpoints from environment variable.
	// Format: NRF_SERVICE_ENDPOINTS=UDM:nudm-uem:udm-mock:8081,AUSF:nausf-auth:ausf-mock:8081
	if endpointEnv := os.Getenv("NRF_SERVICE_ENDPOINTS"); endpointEnv != "" {
		for _, entry := range strings.Split(endpointEnv, ",") {
			parts := strings.Split(entry, ":")
			if len(parts) == 4 {
				port, _ := strconv.Atoi(strings.TrimSpace(parts[3]))
				srv.SetServiceEndpoint(
					strings.TrimSpace(parts[0]), // nfType
					strings.TrimSpace(parts[1]), // serviceName
					strings.TrimSpace(parts[2]), // host
					port,
				)
			}
		}
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		logger.Info("NRF mock server starting", "addr", *addr)
		errCh <- srv.ListenAndServe(*addr)
	}()

	// Wait for SIGINT or SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down", "signal", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "err", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}
	logger.Info("NRF mock server stopped")
}
