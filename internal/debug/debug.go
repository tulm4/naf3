// Package debug provides a per-UE debug subsystem for NSSAAF.
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md
package debug

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"

	"github.com/operator/nssAAF/internal/logging"
)

// Config holds the per-binary debug subsystem configuration.
type Config struct {
	Enabled   bool
	RedisAddr string
	Service   string // "http-gw" | "biz" | "aaa-gw"
	PodID     string
	TTL       time.Duration // default 24h
	MaxLen    int64         // default 10000
}

// Debug is the per-binary debug subsystem. Pass via DI; never global.
// Zero value is unusable; always call New.
type Debug struct {
	enabled atomic.Bool
	client  *redis.Client
	podID   string
	service string
	maxLen  int64
	ttl     time.Duration
}

// Kind classifies a debug event.
type Kind string

const (
	KindHTTP     Kind = "http"
	KindDB       Kind = "db"
	KindCache    Kind = "cache"
	KindProtocol Kind = "protocol"
	KindInternal Kind = "internal"
)

// Event is the in-memory representation of a debug event before XADD.
type Event struct {
	Op     string
	Kind   Kind
	GPSI   string         // raw GPSI (N58 flow); hashed internally; "" if unknown
	SUPI   string         // raw SUPI (N60 AIW flow); hashed internally; "" if unknown
	AuthID string         // auth_ctx_id, when known
	Detail map[string]any // op-specific, JSON-encoded, sanitized
	Status string         // "ok" | "error"
	Error  error
}

// New creates a Debug. If Redis is unreachable, returns an error; the caller
// (main.go) MUST log a warning and continue with d == nil. All Emit paths
// check d == nil and become no-ops, so the request flow is unaffected.
func New(ctx context.Context, cfg Config) (*Debug, error) {
	if cfg.TTL == 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.MaxLen == 0 {
		cfg.MaxLen = 10000
	}
	client := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	// Best-effort ping; do not fail startup if Redis is down.
	pingCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	d := &Debug{
		client:  client,
		podID:   cfg.PodID,
		service: cfg.Service,
		maxLen:  cfg.MaxLen,
		ttl:     cfg.TTL,
	}
	d.enabled.Store(cfg.Enabled)
	return d, nil
}

// Enabled reports whether debug is on. Hot-path check: ~1ns.
func (d *Debug) Enabled() bool {
	if d == nil {
		return false
	}
	return d.enabled.Load()
}

// Set toggles debug at runtime. v1 reads once at startup; SIGHUP is a future enhancement.
func (d *Debug) Set(on bool) {
	if d == nil {
		return
	}
	d.enabled.Store(on)
}

// Close shuts down the underlying Redis client.
func (d *Debug) Close() error {
	if d == nil || d.client == nil {
		return nil
	}
	return d.client.Close()
}

// Emit records one debug event. Best-effort: errors are NOT returned.
// Skips immediately if disabled or no span in context. ~1µs per emit when enabled.
func (d *Debug) Emit(ctx context.Context, ev Event) {
	if d == nil || !d.enabled.Load() {
		return
	}
	span := trace.SpanFromContext(ctx).SpanContext()
	if !span.IsValid() {
		slog.Info("DEBUG_EMIT skip no span", "svc", d.service)
		return
	}
	slog.Info("DEBUG_EMIT proceeding", "svc", d.service, "gpsi", ev.GPSI, "op", ev.Op)
	subHash, subKind := "", ""
	gpsiHash := ""
	switch {
	case ev.GPSI != "":
		subHash = logging.HashGPSI(ev.GPSI)
		subKind = "gpsi"
		gpsiHash = subHash
	case ev.SUPI != "":
		subHash = logging.HashGPSI(ev.SUPI)
		subKind = "supi"
	}
	fields := map[string]any{
		"ts":       time.Now().UnixMilli(),
		"pod":      d.podID,
		"svc":      d.service,
		"trace":    span.TraceID().String(),
		"span":     span.SpanID().String(),
		"sub_h":    subHash,
		"sub_kind": subKind,
		"gpsi_h":   gpsiHash,
		"auth":     ev.AuthID,
		"op":       ev.Op,
		"kind":     string(ev.Kind),
		"status":   ev.Status,
	}
	if ev.Error != nil {
		fields["err"] = ev.Error.Error()
	}
	if len(ev.Detail) > 0 {
		b, _ := json.Marshal(sanitize(ev.Detail))
		if len(b) > 512 {
			b = b[:512]
		}
		fields["detail"] = string(b)
	}
	key := "nssaa:debug:stream:" + subHash
	if subHash == "" {
		key = "nssaa:debug:stream:_no_sub"
	}
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	err := d.client.XAdd(ctx2, &redis.XAddArgs{
		Stream: key,
		MaxLen: d.maxLen,
		Approx: true,
		Values: fields,
	}).Err()
	if err != nil {
		slog.Warn("DEBUG_EMIT XAdd failed", "svc", d.service, "key", key, "error", err)
	}
	_ = d.client.Expire(ctx2, key, d.ttl).Err()
}
