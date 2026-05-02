// Package main runs the UDM mock server for fullchain E2E testing.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/operator/nssAAF/internal/udmserver"
)

func main() {
	addr := flag.String("addr", ":8081", "address to listen on")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	srv := udmserver.NewServer()

	// Pre-load test subscribers from UDM_AUTH_SUBSCRIPTIONS env var.
	// Format: UDM_AUTH_SUBSCRIPTIONS=imsi-001:EAP-AKA:aaa-sim:1812,imsi-002:EAP-AKA:aaa-sim:1812
	if authEnv := os.Getenv("UDM_AUTH_SUBSCRIPTIONS"); authEnv != "" {
		for _, entry := range strings.Split(authEnv, ",") {
			parts := strings.Split(entry, ":")
			if len(parts) >= 4 {
				supi := strings.TrimSpace(parts[0])
				authType := strings.TrimSpace(parts[1])
				aaaServer := strings.TrimSpace(parts[2]) + ":" + strings.TrimSpace(parts[3])
				srv.SetAuthSubscription(supi, authType, aaaServer)
			}
		}
	} else {
		// Default test subscribers if no env var is set.
		testSubscribers := []struct {
			supi      string
			authType  string
			aaaServer string
		}{
			{"imsi-001234567890123", "EAP-AKA", "aaa-sim:1812"},
			{"imsi-001234567890124", "EAP-AKA", "aaa-sim:1812"},
			{"imsi-001234567890125", "EAP-AKA", "aaa-sim:1812"},
		}
		for _, sub := range testSubscribers {
			srv.SetAuthSubscription(sub.supi, sub.authType, sub.aaaServer)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("UDM mock server starting", "addr", *addr)
		errCh <- srv.ListenAndServe(*addr)
	}()

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
	logger.Info("UDM mock server stopped")
}
