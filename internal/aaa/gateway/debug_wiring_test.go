package gateway

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

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

// newDebugWithSpan returns a debug.Debug, the underlying miniredis (so tests
// can inspect emitted events), and a context with a valid OTel span. Uses
// miniredis so XAdd + Expire actually round-trip; the debug subsystem's Emit
// then publishes to a real stream the test can read back.
func newDebugWithSpan(t *testing.T) (*debug.Debug, *miniredis.Miniredis, context.Context) {
	t.Helper()
	mr := miniredis.RunT(t)

	d, err := debug.New(context.Background(), debug.Config{
		Enabled:   true,
		RedisAddr: mr.Addr(),
		Service:   "aaa-gw-test",
		PodID:     "test-pod",
		TTL:       time.Hour,
		MaxLen:    100,
	})
	if err != nil {
		t.Fatalf("debug.New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	t.Cleanup(func() { span.End() })
	return d, mr, ctx
}

// findOpInStream reads the no-subject debug stream from miniredis and returns
// the first event whose "op" field matches wantOp. Used to assert that a
// specific Emit/WrapProtocol call fired with the expected op name.
func findOpInStream(t *testing.T, mr *miniredis.Miniredis, wantOp string) (map[string]any, bool) {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	key := "nssaa:debug:stream:_no_sub"
	msgs, err := rdb.XRange(context.Background(), key, "-", "+").Result()
	if err != nil {
		t.Fatalf("xrange %s: %v", key, err)
	}
	for _, m := range msgs {
		if m.Values["op"] == wantOp {
			return m.Values, true
		}
	}
	return nil, false
}

// TestRadiusForwarder_Forward_EmitsProtocolEventWithOp proves Task 14: the
// radius forwarder's Forward method must WrapProtocol around the underlying
// radius.Client.Send call with op="radius.eap.forward". With debug enabled,
// a miniredis-backed Debug, and a valid span in context, the test asserts the
// expected op name appears in the no-subject debug stream. Without WrapProtocol
// the op would not be emitted and the assertion fails.
func TestRadiusForwarder_Forward_EmitsProtocolEventWithOp(t *testing.T) {
	dbg, mr, ctx := newDebugWithSpan(t)
	// Use the constructor so rf.client is wired (real radius.Client). The
	// ServerAddress doesn't have to be reachable — we only need to exercise
	// the Emit/WrapProtocol call site, not the underlying UDP transport.
	rf := newRadiusForwarder(RadiusForwarderConfig{
		ServerAddress:  "127.0.0.1:9999",
		Timeout:        50 * time.Millisecond,
		MaxRetries:     0,
		ResponseWindow: 50 * time.Millisecond,
	}, slog.New(slog.NewTextHandler(os.Stdout, nil)), dbg)
	if rf.client == nil {
		t.Fatal("radius client should be wired via newRadiusForwarder")
	}

	// Use a background ctx so Emit inside WrapProtocol can flush its XAdd
	// even after the underlying send fails. The SendAccessRequest call uses
	// the same ctx; it will fail because nothing listens, but the Emit must
	// still publish to miniredis.
	_, _ = rf.Forward(ctx, []byte{1, 2, 3}, "sess-1", 1, "FFFFFF")

	// Miniredis is in-process; XAdd completes synchronously. No sleep needed.
	if _, ok := findOpInStream(t, mr, "aaa.radius.forward"); !ok {
		t.Fatal("expected aaa.radius.forward event in debug stream; Task 13 Emit not wired")
	}
	if _, ok := findOpInStream(t, mr, "radius.eap.forward"); !ok {
		t.Fatal("expected radius.eap.forward event in debug stream; Task 14 WrapProtocol not wired")
	}
}

// TestDiamForwarder_Forward_EmitsProtocolEventWithOp proves Task 14: the
// diameter forwarder's Forward method must Emit "diameter.eap.send" before
// the wire write, and WrapProtocol with op="diameter.eap.forward" around
// the actual WriteTo. To drive both call sites we wire a fake conn into
// df so getConn returns it without dialing; the fake WriteTo succeeds so
// Forward reaches the response wait. With no DEA responder the call times
// out via ctx, but the Emit and WrapProtocol events fire before the wait
// and are visible in miniredis.
func TestDiamForwarder_Forward_EmitsProtocolEventWithOp(t *testing.T) {
	dbg, mr, ctx := newDebugWithSpan(t)
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
		dbg,
	)
	df.mu.Lock()
	df.conn = newFakeNotifierConn()
	df.connected = true
	df.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	_, _ = df.Forward(callCtx, []byte{1, 2, 3}, "sess-1", 1, "FFFFFF")

	if _, ok := findOpInStream(t, mr, "aaa.diameter.forward"); !ok {
		t.Fatal("expected aaa.diameter.forward event in debug stream; Task 13 Emit not wired")
	}
	if _, ok := findOpInStream(t, mr, "diameter.eap.forward"); !ok {
		t.Fatal("expected diameter.eap.forward event in debug stream; Task 14 WrapProtocol not wired")
	}
}

// _ keeps the radius import alive even if every test happens to be skipped.
var _ = radius.CodeAccessRequest