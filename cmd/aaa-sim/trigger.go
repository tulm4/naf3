// Server-initiated RADIUS trigger subcommands for aaa-sim.
//
// trigger-rar and trigger-asr send a single CoA-Request / Disconnect-Request
// (RFC 5176 §3) to a target RADIUS peer and exit. They reuse the
// RadiusServer machinery from test/aaa_sim to construct a packet with a
// random Request Authenticator, the session ID in the State attribute,
// and a valid Message-Authenticator (RFC 5176 §3 mandates MA on
// server-initiated packets).
//
// 3GPP TS 29.561 §16 maps the AAA-S → AAA-Client RAR onto RADIUS CoA-Request
// (code 43) and ASR onto Disconnect-Request (code 44).

package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/operator/nssAAF/test/aaa_sim"
)

const (
	codeRAR = 43 // RFC 5176 §3.1 CoA-Request; 3GPP TS 29.561 §16 RAR
	codeASR = 44 // RFC 5176 §3.2 Disconnect-Request; 3GPP TS 29.561 §16 ASR

	defaultTriggerTarget = "172.0.3.15:1812"
	triggerSendDelay     = 500 * time.Millisecond
)

func runTrigger(cmd string, args []string) {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	target := fs.String("target", defaultTriggerTarget, "aaa-gateway RADIUS address (host:port)")
	sessionID := fs.String("session-id", "", "session ID to re-auth (RAR) or abort (ASR)")
	secret := fs.String("secret", "testing123", "shared secret matching aaa-gateway")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *sessionID == "" {
		fmt.Fprintln(os.Stderr, "--session-id is required")
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	conn, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	defer conn.Close()

	addr, err := net.ResolveUDPAddr("udp", *target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve:", err)
		os.Exit(1)
	}

	srv := aaa_sim.NewRadiusServer(conn, aaa_sim.ModeEAP_TLS_SUCCESS, []byte(*secret), logger)

	var code uint8
	switch cmd {
	case "trigger-rar":
		code = codeRAR
	case "trigger-asr":
		code = codeASR
	default:
		fmt.Fprintln(os.Stderr, "unknown trigger subcommand:", cmd)
		os.Exit(2)
	}

	if err := srv.SendServerInitiated(addr, code, *sessionID); err != nil {
		fmt.Fprintln(os.Stderr, "send:", err)
		os.Exit(1)
	}
	logger.Info("aaa-sim trigger sent",
		"subcommand", cmd,
		"code", code,
		"target", *target,
		"session_id", *sessionID)

	// Hold the socket open briefly so the kernel doesn't recycle the port
	// before the receiver sees the packet (UDP has no delivery guarantee).
	time.Sleep(triggerSendDelay)
	fmt.Println("ok")
}