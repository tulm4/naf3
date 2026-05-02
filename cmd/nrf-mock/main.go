// cmd/nrf-mock is a standalone NRF mock server for fullchain E2E tests.
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/operator/nssAAF/internal/nrfserver"
)

func main() {
	addr := flag.String("addr", ":8081", "address to listen on")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	srv := nrfserver.NewServer()
	logger.Info("NRF mock server starting", "addr", *addr)
	if err := srv.ListenAndServe(*addr); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}
