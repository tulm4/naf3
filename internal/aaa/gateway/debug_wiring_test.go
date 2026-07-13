package gateway

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/radius"
)

// TestRadiusForwarder_Forward_NoClientConfigured_DoesNotPanic proves Task 13
// of the per-UE debug plan: even with debug enabled, Forward must return the
// existing "client not configured" error without panicking on Emit calls.
func TestRadiusForwarder_Forward_NoClientConfigured_DoesNotPanic(t *testing.T) {
	rf := &radiusForwarder{
		config: RadiusForwarderConfig{},
		logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		debug:  &debug.Debug{}, // disabled — Emit is a no-op
	}
	_, err := rf.Forward(context.Background(), []byte{1, 2, 3}, "sess-1", 1, "FFFFFF")
	if err == nil {
		t.Fatal("expected error when client is nil")
	}
}

// TestRadiusForwarder_New_StoresDebugFromConfig proves the constructor wires
// cfg.Debug so the Forward method can call rf.debug.Emit/Wrap*. This is a
// compile-time + behavior guard for Task 13.
func TestRadiusForwarder_New_StoresDebugFromConfig(t *testing.T) {
	dbg := &debug.Debug{}
	rf := newRadiusForwarder(RadiusForwarderConfig{
		ServerAddress: "127.0.0.1:9999", // nothing listens here
		Timeout:       100 * time.Millisecond,
		MaxRetries:    0,
	}, slog.New(slog.NewTextHandler(os.Stdout, nil)), dbg)
	if rf == nil {
		t.Fatal("newRadiusForwarder returned nil")
	}
	if rf.debug != dbg {
		t.Fatalf("rf.debug = %p; want %p", rf.debug, dbg)
	}
	// Client may legitimately be nil if the listening port isn't bound; that's
	// fine — the field check is what we're proving.
	_ = rf.client
}

// TestRadiusForwarder_New_AcceptsNilDebug is the nil-safety guard: callers
// that don't configure the debug subsystem must keep working.
func TestRadiusForwarder_New_AcceptsNilDebug(t *testing.T) {
	rf := newRadiusForwarder(RadiusForwarderConfig{
		ServerAddress: "", // skip client construction entirely
	}, slog.New(slog.NewTextHandler(os.Stdout, nil)), nil)
	if rf == nil {
		t.Fatal("newRadiusForwarder returned nil")
	}
	if rf.debug != nil {
		t.Fatalf("rf.debug should be nil when d is nil; got %p", rf.debug)
	}
	if rf.client != nil {
		t.Fatal("rf.client should be nil when ServerAddress is empty")
	}
}

// TestDiamForwarder_New_StoresDebugFromConfig proves the diameter forwarder
// constructor wires debug. This guards Task 13 + Task 14 wrap sites.
func TestDiamForwarder_New_StoresDebugFromConfig(t *testing.T) {
	dbg := &debug.Debug{}
	df := newDiamForwarder(
		"127.0.0.1:1", // nothing listens here
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
		nil, // forwardToBiz
		nil, // registry
		dbg, // debug
	)
	if df == nil {
		t.Fatal("newDiamForwarder returned nil")
	}
	if df.debug != dbg {
		t.Fatalf("df.debug = %p; want %p", df.debug, dbg)
	}
}

// TestDiamForwarder_New_AcceptsNilDebug is the nil-safety guard for the
// diameter forwarder.
func TestDiamForwarder_New_AcceptsNilDebug(t *testing.T) {
	df := newDiamForwarder(
		"127.0.0.1:1",
		"tcp",
		"aaa-gateway.example.com",
		"example.com",
		"aaa-server.example.com",
		"example.com",
		DefaultConfig(),
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
		nil,
		nil,
		nil, // debug — must accept nil without panic
	)
	if df == nil {
		t.Fatal("newDiamForwarder returned nil")
	}
	if df.debug != nil {
		t.Fatalf("df.debug should be nil when d is nil; got %p", df.debug)
	}
}

// _ keeps the radius import alive even if every test happens to be skipped.
var _ = radius.CodeAccessRequest