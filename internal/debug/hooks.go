package debug

import (
	"context"
	"time"
)

// WrapDB runs fn and emits a db-kind debug event with timing. Returns
// the original error unchanged. No-op (only the atomic check + fn call) when
// debug is disabled.
func (d *Debug) WrapDB(ctx context.Context, op, table string, fn func() error) error {
	if !d.Enabled() {
		return fn()
	}
	start := time.Now()
	err := fn()
	d.emitTiming(ctx, op, KindDB, table, "", start, err)
	return err
}

// WrapRedis runs fn and emits a cache-kind debug event with timing. Returns
// the original error unchanged. No-op when debug is disabled.
func (d *Debug) WrapRedis(ctx context.Context, op, key string, fn func() error) error {
	if !d.Enabled() {
		return fn()
	}
	start := time.Now()
	err := fn()
	d.emitTiming(ctx, op, KindCache, "", key, start, err)
	return err
}

// WrapProtocol runs fn and emits a protocol-kind debug event with timing.
// Returns the original error unchanged. No-op when debug is disabled.
func (d *Debug) WrapProtocol(ctx context.Context, op string, fn func() error) error {
	if !d.Enabled() {
		return fn()
	}
	start := time.Now()
	err := fn()
	d.emitTiming(ctx, op, KindProtocol, "", "", start, err)
	return err
}

func (d *Debug) emitTiming(ctx context.Context, op string, kind Kind, table, key string, start time.Time, err error) {
	ev := Event{
		Op:     op,
		Kind:   kind,
		Status: "ok",
		Detail: map[string]any{"duration_ms": time.Since(start).Milliseconds()},
	}
	if table != "" {
		ev.Detail["table"] = table
	}
	if key != "" {
		ev.Detail["key"] = key
	}
	if err != nil {
		ev.Status = "error"
		ev.Error = err
	}
	d.Emit(ctx, ev)
}
