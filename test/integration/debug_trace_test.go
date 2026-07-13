//go:build integration
// +build integration

// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/logging"
)

// TestDebugTrace_CrossComponentCorrelation verifies that events emitted from
// multiple layers into Redis Streams all carry the same trace_id.
func TestDebugTrace_CrossComponentCorrelation(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	dbg, err := debug.New(context.Background(), debug.Config{
		Enabled:   true,
		RedisAddr: mr.Addr(),
		Service:   "biz",
		PodID:     "biz-1",
		TTL:       time.Hour,
		MaxLen:    100,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer dbg.Close()

	ctx, span := tp.Tracer("test").Start(context.Background(), "inbound")
	defer span.End()

	dbg.Emit(ctx, debug.Event{Op: "http.request", Kind: debug.KindHTTP, Status: "ok", GPSI: "msisdn-1"})
	dbg.Emit(ctx, debug.Event{Op: "biz.handler", Kind: debug.KindInternal, Status: "ok", GPSI: "msisdn-1"})
	dbg.Emit(ctx, debug.Event{Op: "aaa.radius.forward", Kind: debug.KindProtocol, Status: "ok", GPSI: "msisdn-1"})

	hash := logging.HashGPSI("msisdn-1")
	key := "nssaa:debug:stream:" + hash
	msgs, err := rdb.XRange(context.Background(), key, "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 events, got %d", len(msgs))
	}
	expected := span.SpanContext().TraceID().String()
	for i, m := range msgs {
		if m.Values["trace"] != expected {
			t.Fatalf("event %d trace_id mismatch: got %s want %s", i, m.Values["trace"], expected)
		}
	}
}
