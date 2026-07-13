// Package main — non-test wiring helpers for cmd/http-gateway.
//
// buildHandlerDeps is declared here (rather than in a _test.go file) because
// main.go's buildHandler function consumes it. Keeping it in a non-test
// source keeps the production wiring reachable from the test binary via
// package main linkage.
package main

import (
	"github.com/operator/nssAAF/internal/auth"
	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/proto"
)

// buildHandlerDeps collects the dependencies buildHandler needs so the
// handler chain construction is testable without booting the full process.
type buildHandlerDeps struct {
	BizClient proto.BizServiceClient
	AuthCfg   auth.Config
	Debug     *debug.Debug
}
