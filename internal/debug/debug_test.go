package debug

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestEnabled_DefaultsFalse(t *testing.T) {
	d := &Debug{}
	if d.Enabled() {
		t.Fatal("expected Enabled()=false for zero-value Debug")
	}
}

func TestEnabled_TogglesWithSet(t *testing.T) {
	d := &Debug{}
	d.Set(true)
	if !d.Enabled() {
		t.Fatal("expected Enabled()=true after Set(true)")
	}
	d.Set(false)
	if d.Enabled() {
		t.Fatal("expected Enabled()=false after Set(false)")
	}
}

// newFaultClient returns a redis client that always errors on every command.
func newFaultClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("test: no redis")
		},
		MaxRetries: -1,
	})
}

func TestEmit_NoOpWhenDisabled(t *testing.T) {
	d := &Debug{client: newFaultClient()}
	d.enabled.Store(false)
	// Should not panic, even though client is broken.
	d.Emit(context.Background(), Event{Op: "test", Kind: KindInternal, Status: "ok"})
}

func TestEmit_NilReceiverIsSafe(t *testing.T) {
	var d *Debug
	d.Emit(context.Background(), Event{Op: "test", Kind: KindInternal})
}

func TestEmit_SwallowsRedisErrors(t *testing.T) {
	d := &Debug{client: newFaultClient(), podID: "p1", service: "biz", maxLen: 100, ttl: time.Hour}
	d.enabled.Store(true)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()
	d.Emit(ctx, Event{Op: "x", Kind: KindInternal, Status: "ok", Error: errors.New("boom")})
	// No panic, no return value to assert; reach here = pass.
}

func TestEmit_RequiresSpan(t *testing.T) {
	d := &Debug{client: newFaultClient(), podID: "p1", service: "biz", maxLen: 100, ttl: time.Hour}
	d.enabled.Store(true)
	atomic.StoreInt32(new(int32), 0) // keep import
	// No span in ctx → must skip silently. The "fault client" is never touched.
	d.Emit(context.Background(), Event{Op: "x", Kind: KindInternal, Status: "ok"})
}

func BenchmarkEmit_Disabled(b *testing.B) {
	d := &Debug{}
	d.enabled.Store(false)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Emit(ctx, Event{Op: "x", Kind: KindInternal})
	}
}
