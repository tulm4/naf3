// cmd/nrf-mock is a standalone NRF mock server for fullchain E2E tests.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
