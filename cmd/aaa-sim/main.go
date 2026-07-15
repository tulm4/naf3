// main is the entry point for the aaa-sim binary.
//
// Modes of operation:
//
//	default — run the long-running RADIUS (1812) + Diameter (3868) servers.
//	trigger-rar — one-shot: send a RADIUS Re-Auth-Request (RFC 5176 CoA-Request,
//	  code 43; 3GPP TS 29.561 RAR) to the configured --target and exit.
//	trigger-asr — one-shot: send a RADIUS Abort-Session-Request (RFC 5176
//	  Disconnect-Request, code 44; 3GPP TS 29.561 ASR) to --target and exit.
//
// The one-shot trigger subcommands are used by E2E tests to drive the
// server-initiated flow (AAA-S → AAA-Client → biz → AMF) without modifying
// the long-running server loop.
package main

import (
	"log/slog"
	"os"

	"github.com/operator/nssAAF/test/aaa_sim"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "trigger-rar", "trigger-asr":
			runTrigger(os.Args[1], os.Args[2:])
			return
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	modeStr := os.Getenv("AAA_SIM_MODE")
	if modeStr == "" {
		modeStr = "EAP_TLS_SUCCESS"
	}
	mode := aaa_sim.ParseMode(modeStr)
	aaa_sim.Run(mode, logger)
}
