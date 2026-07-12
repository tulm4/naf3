# NSSAAF Per-UE Debug Tracing

**Date:** 2026-07-12
**Status:** Draft (awaiting review)
**Author:** Brainstorming session
**Target phase:** Insert before Phase 7 (Kubernetes Deployment) or as part of Phase 4 retro
**Related design:** `docs/design/19_observability.md` (existing OTel/Prometheus/Logs design)

---

## 1. Problem statement

Operators need to debug a single UE's slice-authentication flow end-to-end across three processes (HTTP Gateway → Biz Pod → AAA Gateway) including its database (PostgreSQL) and cache (Redis) interactions. The existing observability stack (Prometheus metrics, OTel tracing with stdout exporter, structured logs) provides aggregate signal and per-trace data in stdout, but has three gaps:

1. **No per-UE query path.** When a customer reports a problem for `gpsi=msisdn-208046000000001`, the operator must grep logs across 3 services. GPSI never appears raw in logs (REQ-16), so even a `gpsi_hash` grep across pods is not enough because trace context is not propagated end-to-end.
2. **No DB / Redis call visibility per UE.** The Prometheus histograms `nssAAF_db_query_duration_seconds` and `nssAAF_redis_operations_total` show aggregate latency, not which calls were made for a specific auth_ctx_id or GPSI.
3. **No enable/disable lever for fine-grained capture.** Increasing log verbosity floods the entire pod, not just the flow of interest.

This spec introduces a per-UE debug subsystem that:

- Captures a chronological timeline of one UE's flow across all three processes.
- Captures every storage and cache call made in service of that flow.
- Is **off by default** with near-zero overhead when disabled.
- Is queryable by a CLI tool that prints the timeline.

---

## 2. Goals and non-goals

### Goals

1. An operator can run `nssAAF debug trace --gpsi <gpsi>` (N58 / NSSAA) or `nssAAF debug trace --supi <supi>` (N60 / AIW) and get a single chronological timeline of a UE's flow across HTTP Gateway → Biz Pod → AAA Gateway, including every storage/cache call and any error.
2. Cross-component correlation uses the **W3C `traceparent` header**, so the same `trace_id` flows through all three processes.
3. Debug mode is **off by default**. When off, overhead is one atomic load per event-emit point (<10ns).
4. Debug events are persisted to a **Redis Stream** keyed by subscriber hash (GPSI or SUPI, hashed with the same SHA-256-based function), with a 24-hour TTL and a per-stream cap of 10,000 events.
5. Debug emission never breaks the request path. Emit failures are silently dropped; emit never returns an error.

### Non-goals

- No new public HTTP debug endpoint. Operator access is CLI only.
- No full SQL capture. Operation-level only (`store.Save`, `pool.Exec`, etc.).
- No production tracing backend (Jaeger/Tempo) — the existing `tracing.Init` stdout exporter stays as-is. This spec only **propagates** the trace context further so the per-UE timeline can be assembled.
- No UI / dashboard. CLI prints a timeline to stdout.
- No changes to 3GPP spec compliance or behavior.

---

## 3. Architecture

### 3.1 New package: `internal/debug/`

A single source of truth for the debug subsystem, used by all three binaries and the CLI.

```
internal/debug/
  debug.go         ← Debug struct, atomic-enabled flag, New(), Enabled(), Set(), Emit()
  hooks.go         ← WrapDB / WrapRedis / WrapProtocol helpers
  sanitize.go      ← PII sanitizer for Detail maps

cmd/nssAAF-debug/
  main.go          ← CLI binary (operator runs on demand)
```

The `Emit` method lives on `*Debug` in `debug.go` (it is the central emission path; there is no separate `emitter.go` because emitting requires the `*Debug` state and there is no benefit to a second file).

### 3.2 Data flow

```
[HTTP GW: handler]
    │  debug.Emit(ctx, "http.request", {method, path, status, duration_ms, ...})
    ▼
[OTel context] — trace_id derived from inbound traceparent
    │
    │  HTTP forward with traceparent header (otelhttp.NewTransport)
    ▼
[Biz Pod: handler]
    │  debug.Emit(ctx, "biz.handler", {gpsi, snssai, ...})
    │  debug.WrapDB(ctx, "pg.session.save", "sessions", func() error { ... })
    │  debug.WrapRedis(ctx, "redis.rate_limit.allow", key, func() error { ... })
    │
    │  HTTP forward /aaa/forward with traceparent
    ▼
[AAA GW: HandleForward]
    │  debug.WrapProtocol(ctx, "aaa.radius.forward", func() error { ... })
    │  debug.WrapRedis(ctx, "redis.session_corr.write", key, func() error { ... })
    │
    ▼
[AAA-S: external, no trace]
    │
    │  RADIUS/Diameter response
    ▼
[AAA GW: forward to Biz via /aaa/server-initiated]
    │  traceparent carried via otelhttp.NewTransport
    ▼
[Biz Pod: handles]  (reverse path)
    │
    ▼
[HTTP GW response: AMF]
```

Every `debug.Emit` call:

1. Skips immediately if `!d.enabled.Load()` (atomic check, ~1ns).
2. Skips if there is no OTel span in context (nothing to correlate).
3. Builds an `Event{Timestamp, PodID, Service, TraceID, SpanID, GPSI_hash, AuthCtxID, Op, Kind, Detail, Status, Error}`.
4. Sanitizes `Detail` to replace any PII keys (`gpsi|supi|imsi|msisdn|user_name|calling_station_id`) with their `logging.HashGPSI` form.
5. Encodes to a flat field map and `XADD`s to `nssaa:debug:stream:<gpsi_hash>` with `MAXLEN ~ 10000`.
6. Re-sets `EXPIRE nssaa:debug:stream:<gpsi_hash> 86400` (24h TTL).
7. All operations are best-effort: a 5ms `context.WithTimeout` caps any wait, errors are dropped silently.

### 3.3 Design points

- **No new dependencies in the hot path.** Debug is a `*Debug` struct passed via DI, never global. Test code can disable easily by passing a `*Debug` whose `Set(false)` was called.
- **Single hook function `Emit`** — every layer uses the same path. ~200 LoC for the whole subsystem.
- **GPSI is always hashed** via `logging.HashGPSI` (existing utility at `internal/logging/gpsi.go`) — never logged raw. The `sanitize` step is a defense-in-depth measure; call sites should already pass only hashed values.
- **Best-effort write.** If Redis is slow or down, the request still proceeds.

---

## 4. Data model

### 4.1 Redis Stream

**Key:** `nssaa:debug:stream:<sub_hash>` where `<sub_hash>` is the SHA-256 first-8-bytes base64url of either the GPSI (N58 flow) or the SUPI (N60 flow). The operator queries with whichever identifier the customer report quotes. GPSI-keyed and SUPI-keyed streams are distinct (they hash to different values) but the same CLI and same timeline format work for both.

**Key (no subscriber context):** `nssaa:debug:stream:_no_sub` — for events emitted before a subscriber identifier is known (e.g., the very first inbound hop at HTTP Gateway when the request body hasn't been parsed yet).
**TTL:** 24h via `EXPIRE` on every XADD.
**Cap:** `MAXLEN ~ 10000` to bound memory per subscriber.

### 4.2 Stream entry fields

Redis Stream values are flat key/value pairs:

| Field | Type | Source |
|---|---|---|
| `ts` | int64 (unix ms) | `time.Now().UnixMilli()` |
| `pod` | string | `os.Hostname()` |
| `svc` | string | `"http-gw" \| "biz" \| "aaa-gw"` |
| `trace` | string | OTel trace_id (32 hex chars) |
| `span` | string | OTel span_id (16 hex chars) |
| `sub_h` | string | `logging.HashGPSI(gpsi_or_supi)` (always when known) |
| `sub_kind` | string | `"gpsi" \| "supi" \| ""` (which identifier populated `sub_h`) |
| `gpsi_h` | string | `logging.HashGPSI(gpsi)` (only when GPSI is known; AIW events leave this empty) |
| `auth` | string | auth_ctx_id (when known) |
| `op` | string | e.g., `http.request`, `pg.session.save`, `redis.rate_limit.allow` |
| `kind` | string | `http` \| `db` \| `cache` \| `protocol` \| `internal` |
| `dur` | int64 | duration in ms (for db/cache/protocol ops) |
| `status` | string | `ok` \| `error` |
| `err` | string | error message (only when status=error) |
| `detail` | string | JSON-encoded op-specific details (compact, ≤512 bytes; sanitized) |

### 4.3 Why subscriber hash as the key (not auth_ctx_id)

- A subscriber's GPSI (or SUPI) is the stable identifier across multiple sessions (re-auth, revocation, AIW re-bootstrap).
- auth_ctx_id changes per session, so it's a field, not a key.
- Hashing avoids PII in Redis keys.

---

## 5. Component design

### 5.1 `internal/debug/debug.go` (core)

```go
package debug

// Debug is the per-binary debug subsystem. Pass via DI; never global.
type Debug struct {
    enabled atomic.Bool
    client  *redis.Client
    podID   string
    service string   // "http-gw" | "biz" | "aaa-gw"
    maxLen  int64    // unexported; defaults to 10000
    ttl     time.Duration  // unexported; defaults to 24h
}

type Config struct {
    Enabled   bool
    RedisAddr string
    Service   string
    PodID     string
    TTL       time.Duration  // default 24h
    MaxLen    int64         // default 10000
}

// New creates a Debug. If Redis is unreachable, returns an error; the caller
// (main.go) MUST log a warning and continue with d == nil. All Emit paths
// check d == nil and become no-ops, so the request flow is unaffected.
func New(ctx context.Context, cfg Config) (*Debug, error)

// Enabled reports whether debug is on. Hot-path check: ~1ns.
func (d *Debug) Enabled() bool

// Set toggles debug at runtime. v1 reads once at startup; SIGHUP is a future enhancement.
func (d *Debug) Set(on bool)

// Emit records one debug event. Best-effort: errors are NOT returned.
// Skips immediately if disabled or no span in context. ~1µs per emit when enabled.
func (d *Debug) Emit(ctx context.Context, ev Event)

type Event struct {
    Op     string
    Kind   string  // "http" | "db" | "cache" | "protocol" | "internal"
    GPSI   string  // raw GPSI (N58 flow); hashed internally; "" if unknown
    SUPI   string  // raw SUPI (N60 AIW flow); hashed internally; "" if unknown
    AuthID string
    Detail map[string]any  // op-specific, JSON-encoded, sanitized
    Status string          // "ok" | "error"
    Error  error
}
```

At most one of `GPSI` / `SUPI` is set per event. The Biz Pod's NSSAA handler sets `GPSI`; the AIW handler sets `SUPI`. The emitter derives the Redis Stream key from whichever is set (preferring `GPSI` when both are present, which is the common case). The `gpsi_h` field in the stream entry is set only when `GPSI` was the source; the `sub_h` field always carries the hash regardless of source.

**Emit implementation outline:**

```go
func (d *Debug) Emit(ctx context.Context, ev Event) {
    if d == nil || !d.enabled.Load() { return }
    span := trace.SpanFromContext(ctx).SpanContext()
    if !span.IsValid() { return }
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
        "kind":     ev.Kind,
        "status":   ev.Status,
    }
    if ev.Error != nil {
        fields["err"] = ev.Error.Error()
    }
    if len(ev.Detail) > 0 {
        b, _ := json.Marshal(sanitize(ev.Detail))
        if len(b) > 512 { b = b[:512] }
        fields["detail"] = string(b)
    }
    key := "nssaa:debug:stream:" + subHash
    if subHash == "" {
        key = "nssaa:debug:stream:_no_sub"
    }
    ctx2, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
    defer cancel()
    _ = d.client.XAdd(ctx2, &redis.XAddArgs{
        Stream: key, MaxLen: d.maxLen, Approx: true, Values: fields,
    }).Err()
    _ = d.client.Expire(ctx2, key, d.ttl).Err()
}
```

### 5.2 Wrap helpers

Single-file helpers that wrap a function and emit a `db` / `cache` / `protocol` event:

```go
// internal/debug/hooks.go
func (d *Debug) WrapDB(ctx context.Context, op, table string, fn func() error) error
func (d *Debug) WrapRedis(ctx context.Context, op, key string, fn func() error) error
func (d *Debug) WrapProtocol(ctx context.Context, op string, fn func() error) error
```

All take `fn func() error`, time the call, emit on both success and error, and return the original error unchanged. They guard with `if !d.Enabled() { return fn() }` so the disabled path is one atomic load + the function call.

### 5.3 HTTP middleware

A new `DebugMiddleware` in `internal/api/common/middleware.go` (gated by `*debug.Debug`, can be nil) that wraps the response writer, calls the next handler, then emits an `http.request` event with method, path, status, duration.

### 5.4 Trace context propagation across HTTP

OTel's `otelhttp` transport already does the inbound `traceparent` extraction on the server side. The **outbound** side (Biz Pod calling AAA Gateway) is what needs explicit wiring. The existing `internal/tracing/tracing.go` already exposes `HTTPTransport()` returning `otelhttp.NewTransport(http.DefaultTransport)`. We use it at these call sites:

| Location | Direction | Change |
|---|---|---|
| `cmd/http-gateway/main.go` | inbound | `otelhttp.NewHandler(...)` wraps the inner handler so a server-side span is created and inbound `traceparent` is extracted. |
| `internal/httpclient/native_biz.go` | outbound (Biz → HTTP GW) | Use `tracing.HTTPTransport()`. |
| `internal/httpclient/native_aaa.go` | outbound (Biz → AAA GW `/aaa/forward`) | Use `tracing.HTTPTransport()`. |
| `internal/aaa/gateway/gateway.go` (mux for `/aaa/forward`) | inbound | `otelhttp.NewHandler` wraps the mux. |
| `internal/aaa/gateway/gateway.go` (outbound to `/aaa/server-initiated`) | outbound (AAA GW → Biz) | Use `tracing.HTTPTransport()`. |
| `cmd/biz/main.go` (mux for `/nnssaaf-nssaa/`, `/nnssaaf-aiw/`, `/aaa/server-initiated`) | inbound | `otelhttp.NewHandler` wraps the mux (a single wrap is sufficient). |

After all six wirings, every HTTP request entering the system — including those without an inbound `traceparent` — has a span on each handler. So `Emit` always finds a valid span context. If the AMF does not send a `traceparent`, the trace starts at the HTTP Gateway; the CLI shows a trace beginning there. Per existing decision D-01, the Biz Pod is the trace correlation hub.

### 5.5 CLI tool: `cmd/nssAAF-debug/main.go`

The CLI accepts either a GPSI (for N58 / NSSAA flow) or a SUPI (for N60 / AIW flow). Internally both are hashed with the same `logging.HashGPSI` (SHA-256 first 8 bytes, base64url) into a single `subscriber_hash` that keys the Redis Stream. This works because both identifiers identify the same human subscriber; the operator can paste whichever identifier the customer report quoted.

```bash
# Query by GPSI (N58 / NSSAA flow)
$ nssAAF debug trace --gpsi msisdn-208046000000001 --since 1h
# Query by SUPI (N60 / AIW flow)
$ nssAAF debug trace --supi imsi-208046000000001 --since 1h
# Filters (work with both --gpsi and --supi)
$ nssAAF debug trace --gpsi <gpsi> --trace <trace_id>    # filter to one trace
$ nssAAF debug trace --gpsi <gpsi> --op 'pg.*'           # filter to ops
$ nssAAF debug trace --gpsi <gpsi> --pod biz-7d2a        # filter to pod
$ nssAAF debug stream-list --gpsi <gpsi>                 # list stream metadata only
$ nssAAF debug stream-clear --gpsi <gpsi>                # manual cleanup
```

**Mutual exclusion:** `--gpsi` and `--supi` cannot be used together. The CLI rejects the call if both are set.

**Storage key for the AIW path:** the Biz Pod's AIW handler (`internal/api/aiw/handler.go`) extracts the SUPI from the request body. When emitting a debug event, the Biz Pod calls `debug.Emit(ctx, Event{GPSI: "", SUPI: supi, AuthID: authCtxID, ...})`. The Redis Stream key becomes `nssaa:debug:stream:<hash(supi)>` — a separate stream from any GPSI-keyed stream for the same human subscriber (because GPSI and SUPI are different strings that hash differently). The `gpsi_h` field stays empty for AIW events; a new `sub_h` field carries the hashed subscriber identifier and `sub_kind` records which identifier produced it.

In practice, the operator queries whichever identifier the customer report quotes. Both flows land in the same Redis instance and are queryable with the same CLI; the timeline shown is for whichever identifier the operator provided.

**Output format** (aligned columns, color-coded by `svc`):

```
TIME                 POD             SVC        TRACE    OP                          STATUS  DUR   DETAIL
2026-07-12T17:32:01  http-gw-7d2a    http-gw    abc123   http.request                2xx     45    POST /nnssaaf-nssaa/...
2026-07-12T17:32:01  biz-9c1b        biz        abc123   biz.handler                 ok      2     CreateSliceAuth...
2026-07-12T17:32:01  biz-9c1b        biz        abc123   pg.session.save             ok      3     table=sessions
2026-07-12T17:32:01  biz-9c1b        biz        abc123   redis.rate_limit.allow      ok      1     key=authctx:...
2026-07-12T17:32:02  aaa-gw-3f5e     aaa-gw     abc123   aaa.radius.forward          ok      18    sst=1, sd=000001
2026-07-12T17:32:02  aaa-gw-3f5e     aaa-gw     abc123   redis.session_corr.write    ok      2     key=nssaa:session_corr:...
2026-07-12T17:32:02  aaa-gw-3f5e     aaa-gw     abc123   aaa.radius.recv             ok      3     session=...
2026-07-12T17:32:02  biz-9c1b        biz        abc123   biz.recv_aaa_response       ok      1     session=...
2026-07-12T17:32:02  biz-9c1b        biz        abc123   pg.session.update           ok      4     table=sessions
2026-07-12T17:32:02  http-gw-7d2a    http-gw    abc123   http.response               2xx     46    201 Created
```

The CLI uses `text/tabwriter` for alignment and `fatih/color` (or stdlib ANSI codes) for color.

---

## 6. Configuration

Add a top-level `debug` section to each component's YAML config:

```yaml
# In biz.yaml, http-gateway.yaml, aaa-gateway.yaml
debug:
  enabled: false           # default off; set true per environment
  redisAddr: ${REDIS_ADDR} # same Redis as main data; v1 does not support a separate instance
  ttl: 24h                 # stream TTL
  maxLen: 10000            # MAXLEN ~ bound
```

Loaded by `internal/config/config.go` with a `DebugConfig` struct mirroring the existing `LoggingConfig` / `MetricsConfig` pattern. The CLI tool reads its own small config (Redis address only).

**Runtime toggle (future, optional):** `SIGHUP` handler that re-reads the `debug.enabled` field and calls `d.Set(...)`. v1 reads once at startup; this is documented as a future enhancement.

---

## 7. Error handling

The "must not break the request path" principle:

| Failure mode | Behavior |
|---|---|
| `debug.Enabled() == false` | First line of `Emit` returns. ~1ns. |
| No OTel span in context | `Emit` returns. |
| Redis ping fails at startup | Log warning; **do not fail startup**. |
| Redis XADD times out (5ms) | Drop event silently. Never return error from `Emit`. |
| `Emit` panics | Recovery in `Emit` logs the panic and returns. |
| CLI can't connect to Redis | Print clear error; suggest `redis-cli PING`. |
| Stream key doesn't exist | CLI prints "no events for this GPSI in the last 24h". |

**Invariant:** `Emit` never panics, never blocks longer than 5ms, never returns an error. Verified by `internal/debug/debug_test.go` with fault injection (a redis client that always errors).

---

## 8. Performance budget

| Path | Cost when disabled | Cost when enabled |
|---|---|---|
| HTTP request | 1 atomic load (~1ns) | 1 XADD (~50µs) |
| DB query | 1 atomic load in `WrapDB` (~1ns) | 1 XADD (~50µs) |
| Redis op | 1 atomic load (~1ns) | 1 XADD (~50µs) |
| AAA protocol | 1 atomic load (~1ns) | 1 XADD (~50µs) |
| Hot path total per request | ~10ns | ~250µs (5 events × 50µs) |

50µs is for Redis on localhost. Over a network it can be 1–5ms. The 5ms timeout in `Emit` caps it. The aggregate overhead per request when debug is on is bounded by `N_events × 5ms`, which for a typical 5-event request is 25ms worst case (still below the 500ms P99 latency target).

When disabled, the per-request overhead is ~10ns — well below the noise floor of a typical HTTP handler. There is no allocation, no map lookup, no goroutine creation.

---

## 9. Testing strategy

| Test type | What it covers | File |
|---|---|---|
| Unit | `Emit` is no-op when disabled | `internal/debug/debug_test.go` |
| Unit | `Emit` produces correct fields | same |
| Unit | `Emit` swallows redis errors | fault-injection redis client |
| Unit | GPSI is hashed, not raw | same |
| Unit | Stream key uses `gpsi_hash` | same |
| Unit | `MAXLEN ~ 10000` cap is applied | use miniredis |
| Unit | `sanitize` replaces PII keys | `internal/debug/sanitize_test.go` |
| Unit | CLI prints aligned output | `cmd/nssAAF-debug/main_test.go` |
| Unit | CLI filters by trace/op/pod/since | same |
| Unit | CLI handles missing stream | same |
| Integration | HTTP GW → Biz → AAA GW round-trip emits correlated events | `test/integration/debug_trace_test.go` |
| E2E | `make test-debug` — full AMF → AAA-S round trip with debug on; CLI shows all events | `test/e2e/debug_e2e_test.go` |

---

## 10. Security and privacy

- GPSI / SUPI / MSISDN are always passed through `logging.HashGPSI` before any `XADD`. The hash function is the same SHA-256-first-8-bytes-base64url used elsewhere; the field is named `sub_h` (subscriber hash) in the stream entry.
- The `sanitize(detail)` function in `internal/debug/sanitize.go` recurses through the detail map and replaces any value whose key matches `gpsi|supi|imsi|msisdn|user_name|calling_station_id` with its `logging.HashGPSI` form. This is defense-in-depth: call sites should already pass only hashed values, but a stray raw GPSI or SUPI in a log line must never leak to Redis.
- Debug events are stored only in Redis Streams. No local file, no PG.
- The CLI does not touch PG. Only Redis. PG credentials are not at risk.
- The CLI does not authenticate to Redis in this design. **For production**, the Redis instance should be access-controlled (firewall, ACL, or a read-replica with restricted scope). Documented in the rollout.

---

## 11. Rollout plan

This is a 3-component change, but each component ships independently:

1. **Wave 1:** `internal/debug/` core (`debug.go`, `emitter.go`, `hooks.go`, `sanitize.go`, tests). No call-site wiring yet. Land first; nothing else depends on the call sites yet.
2. **Wave 2:** HTTP middleware in Biz Pod only. Verify CLI shows HTTP events.
3. **Wave 3:** DB and Redis wrappers + wire into Biz Pod repos (`internal/storage/postgres/`, `internal/cache/redis/`). Verify CLI shows DB events.
4. **Wave 4:** AAA GW instrumentation — RADIUS and Diameter forwarders in `internal/aaa/gateway/`, `/aaa/forward` handler, `/aaa/server-initiated` outbound.
5. **Wave 5:** HTTP Gateway instrumentation (forward handler in `cmd/http-gateway/main.go`).
6. **Wave 6:** CLI tool (`cmd/nssAAF-debug/`) + E2E test (`test/e2e/debug_e2e_test.go`).

Each wave is independently shippable. The system runs fine with debug disabled at any wave.

**Backwards compatibility:** When `debug.enabled=false` (the default), no code path is taken. No behavior change, no log line change, no metric change. Safe to deploy.

---

## 12. Acceptance criteria

| # | Criterion | Verification |
|---|---|---|
| AC1 | `Emit` is no-op when disabled, with measurable <10ns overhead | `internal/debug/debug_test.go` benchmark |
| AC2 | `Emit` produces all 13 fields per the schema | unit test on the field map |
| AC3 | GPSI is hashed in stream key, in `gpsi_h` field, and in any sanitized detail | unit test on `sanitize` |
| AC4 | Cross-component trace propagation: HTTP GW → Biz → AAA GW → Biz all share the same `trace_id` | integration test |
| AC5 | CLI prints aligned, color-coded timeline for a given GPSI | CLI test + E2E |
| AC6 | CLI filters by trace, op, pod, since | CLI test |
| AC7 | Debug enabled at all 3 components; full round-trip timeline appears in CLI | E2E |
| AC8 | Redis outage in debug path does not fail any request | fault-injection unit test |
| AC9 | Existing test suite (`go test ./...`) still passes | CI |
| AC10 | `golangci-lint run ./...` passes | CI |

---

## 13. Out of scope / future enhancements

- **OTLP exporter to Jaeger/Tempo.** The current `tracing.Init` uses stdout. The CLI provides an in-house trace timeline; a full distributed-tracing UI is a separate effort.
- **Runtime toggle via SIGHUP or HTTP admin endpoint.** v1 reads once at startup.
- **Per-(gpsi, snssai) debug subscription.** v1 is whole-pod on/off.
- **Compression of detail JSON for high-volume ops.** v1 truncates to 512 bytes.
- **Debug events in production.** v1 is for staging / pre-prod. The performance impact of enabled mode (~250µs/req) is acceptable for staging but should be measured before enabling in production.

---

*End of spec.*
