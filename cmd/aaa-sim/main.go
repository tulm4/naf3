// main is the entry point for the aaa-sim binary.
package main

import (
	"log/slog"
	"os"

	"github.com/operator/nssAAF/test/aaa_sim"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	modeStr := os.Getenv("AAA_SIM_MODE")
	if modeStr == "" {
		modeStr = "EAP_TLS_SUCCESS"
	}
	mode := aaa_sim.ParseMode(modeStr)
	aaa_sim.Run(mode, logger)
}
