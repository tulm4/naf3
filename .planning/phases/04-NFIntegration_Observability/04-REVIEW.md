---
phase: 04-NFIntegration_Observability
reviewed: 2026-04-26T13:54:00Z
depth: standard
files_reviewed: 40
files_reviewed_list:
  - cmd/aaa-gateway/main.go
  - cmd/aaa-gateway/main_test.go
  - cmd/biz/http_aaa_client.go
  - cmd/biz/http_aaa_client_test.go
  - cmd/biz/main.go
  - cmd/biz/main_test.go
  - cmd/http-gateway/main_test.go
  - compose/mock_aaa_s.go
  - internal/aaa/gateway/diameter_forward.go
  - internal/aaa/gateway/diameter_handler.go
  - internal/aaa/gateway/gateway.go
  - internal/aaa/gateway/radius_forward.go
  - internal/aaa/gateway/radius_handler.go
  - internal/aaa/gateway/radius_handler_test.go
  - internal/aaa/router.go
  - internal/amf/amf.go
  - internal/amf/notifier_test.go
  - internal/api/aiw/handler.go
  - internal/api/nssaa/handler.go
  - internal/ausf/client.go
  - internal/ausf/client_test.go
  - internal/biz/router_test.go
  - internal/cache/redis/dlq.go
  - internal/cache/redis/dlq_test.go
  - internal/config/component_test.go
  - internal/config/config.go
  - internal/logging/gpsi.go
  - internal/logging/gpsi_test.go
  - internal/metrics/metrics.go
  - internal/nrf/client.go
  - internal/nrf/client_test.go
  - internal/resilience/circuit_breaker.go
  - internal/resilience/circuit_breaker_test.go
  - internal/resilience/retry.go
  - internal/resilience/retry_test.go
  - internal/storage/postgres/pool.go
  - internal/storage/postgres/session_store.go
  - internal/storage/postgres/session_store_test.go
  - internal/tracing/tracing.go
  - internal/udm/udm.go
findings:
  critical: 1
  warning: 4
  info: 5
  total: 10
status: issues_found
---

# Phase 4: Code Review Report

**Reviewed:** 2026-04-26T13:54:00Z
**Depth:** standard
**Files Reviewed:** 40
**Status:** issues_found

## Summary

Phase 4 introduces a substantial new surface area: a 3-component architecture with the AAA Gateway as a separate binary, NF integrations (NRF, UDM, AUSF, AMF), PostgreSQL session persistence, Redis DLQ, circuit breakers, and OpenTelemetry tracing. The implementation is generally well-structured with good separation of concerns. However, several issues were identified that require attention before production use.

The most critical finding is the use of `context.Background()` in the Diameter server-initiated path, which silently discards tracing context on ASR/RAR messages from AAA-S. The DLQ goroutine leak is a minor but real resource management issue. The UDM marshal-skip is a correctness bug that would silently send an empty body on the rare JSON-encoding failure path.

## Critical Issues

### CR-01: `context.Background()` used in Diameter server-initiated handler — tracing context lost

**File:** `internal/aaa/gateway/diameter_handler.go:220,248`

The `handleASR()` and `handleRAR()` handlers pass `context.Background()` to `forwardToBiz()` instead of the request's context:

```220:    h.forwardToBiz(context.Background(), sessionID, "DIAMETER", "ASR", raw)
...
248:    h.forwardToBiz(context.Background(), sessionID, "DIAMETER", "RAR", raw)
```

**Issue:** Server-initiated messages (ASR, RAR) from AAA-S are forwarded to the Biz Pod with no tracing context. All downstream spans (HTTP call to Biz Pod, Biz Pod processing, DB operations) will appear as root spans disconnected from the AAA-S initiation. This makes distributed trace analysis impossible for the server-initiated path, which is a key NSSAAF feature (TS 23.502 §4.2.9.3).

**Fix:**
```go
// Extract context from the diam.Conn if available, otherwise use a background
// context with a trace span from the incoming message.
h.forwardToBiz(r.Context(), sessionID, "DIAMETER", "ASR", raw)
// where r is extracted from the handler's function signature
```

The `diam.HandlerFunc` receives `conn diam.Conn` which embeds `context.Context`. Use `conn.Context()` as the parent span context for the forward operation.

---

## Warnings

### WR-01: Goroutine leak in `DLQ.Process()` — WaitGroup never decremented

**File:** `internal/cache/redis/dlq.go:78-107`

`DLQ.Process()` calls `go func() { ... }()` but there is no `defer g.wg.Done()` to balance the `g.wg.Add(1)` in `gateway.go:158`. The `gateway.go` calls `g.wg.Wait()` in `Stop()`, but the goroutine started by `Process()` is never tracked:

```78:func (d *DLQ) Process(ctx context.Context) {
79:    go func() {
80:        for {
81:            select {
82:            case <-ctx.Done():
83:                return
84:            case <-time.After(5 * time.Second):
85:            }
...
```

**Fix:** Either (a) have `Process()` return the `wg.Done` channel for the caller to track, or (b) track the goroutine inside `Process` itself with a local `wg` and wait inside the method. The simplest fix is to document that `Process` must not outlive the DLQ pool and that callers should treat it as fire-and-forget, since the goroutine exits when `ctx` is cancelled.

### WR-02: `handleServerInitiated` silently ignores message type validation

**File:** `cmd/biz/main.go:282-292`

```282:    switch req.MessageType {
283:    case proto.MessageTypeRAR:
284:        respPayload = handleReAuth(r.Context(), &req)
285:    case proto.MessageTypeASR:
286:        respPayload = handleRevocation(r.Context(), &req)
287:    case proto.MessageTypeCoA:
288:        respPayload = handleCoA(r.Context(), &req)
289:    default:
290:        http.Error(w, "unknown message type", http.StatusBadRequest)
291:        return
292:    }
```

The `default` case returns 400, which is correct. However, none of the placeholder handlers (`handleReAuth`, `handleRevocation`, `handleCoA`) perform any meaningful work — all return hardcoded byte slices. The `proto.MessageType` values accepted here are not validated against the protocol spec. Specifically, the switch only handles `RAR`, `ASR`, and `CoA`, but the proto definition may include other types. If an unexpected type is added to the proto in the future, the `default` branch will catch it, but the current narrow case list is fragile.

**Fix:** Add `unknown` types to the switch (using `default` as a catch-all is fine) and consider validating the proto enum exhaustively at compile time with a linter rule or a reflection-based check.

### WR-03: JSON marshal error silently skipped in `UpdateAuthContext`

**File:** `internal/udm/udm.go:111`

```111:    payload, _ := json.Marshal(payload)
```

The error from `json.Marshal` is discarded. While JSON-encoding a `map[string]string` should almost never fail, if it does, the PUT request will be sent with a nil body, producing an incorrect or rejected request to UDM.

**Fix:**
```go
payload, err := json.Marshal(payload)
if err != nil {
    return fmt.Errorf("udm: marshal update payload: %w", err)
}
```

### WR-04: Prometheus `promauto` metrics registered at `init()` time — no duplicate detection at startup

**File:** `internal/metrics/metrics.go:10-108`

All metrics are created with `promauto.NewCounterVec`, `promauto.NewHistogramVec`, etc. These register metrics with the default `prometheus.Registry` at package initialization time. If the metrics package is imported by multiple binaries, or if two different code paths both register the same metric name, `prometheus.Register()` will return `ErrAlreadyRegistered` and cause a panic.

**Fix:** The standard mitigation is to use `prometheus.NewCounterVec` + `prometheus.MustRegister()` at startup, or to check `prometheus.Register()` return value. Alternatively, use `prometheus.NewRegistry()` per-binary and pass it to the handler. The current `promauto` pattern is acceptable for single-binary deployments with no duplicate imports, but the risk increases as the codebase grows.

---

## Info

### IN-01: RADIUS shared secret hardcoded in mock AAA server

**File:** `compose/mock_aaa_s.go:37`

```37:var radiusSecret = []byte("testing123")
```

This is explicitly a development-only mock (`compose/mock_aaa_s.go` comment: "mock only"). No action required, but the file should not be compiled into production binaries. Confirm that `compose/` is excluded from production builds.

### IN-02: `HandleConnection` goroutine never exits cleanly — blocks on channel until timeout

**File:** `internal/aaa/gateway/diameter_handler.go:190`

```190:    <-make(chan struct{}) // Blocks until the connection is closed
```

The connection handler goroutine blocks on an empty `chan struct{}` that never closes. It only exits after the 60-second timeout fires. After a successful handshake, the connection remains open indefinitely and the goroutine leaks (in goroutine-count terms, not memory) until either the peer disconnects or the 60-second timeout fires. For long-lived connections, this is acceptable, but the goroutine is not tracked by the gateway's `wg`.

**Fix:** The goroutine should either (a) track itself with a waitgroup, or (b) the comment should clarify that this is the expected behavior (block until connection closes, not tracked because the connection is managed externally by `sm.StateMachine`).

### IN-03: `sessionToAuthCtx` silently uses zero values for missing fields

**File:** `internal/storage/postgres/session_store.go:96-107`

```go
func sessionToAuthCtx(s *Session) *nssaa.AuthCtx {
    return &nssaa.AuthCtx{
        GPSI:        s.GPSI,         // if s.GPSI is "", this is silently stored
        SnssaiSST:   s.SnssaiSST,   // if 0, is this valid or missing?
        SnssaiSD:    s.SnssaiSD,    // empty string — unspecified SD
        ...
    }
}
```

The handler validates GPSI and Snssai before storing, so missing GPSI won't pass validation. However, SnssaiSST=0 is technically valid (TS 29.571 allows SST=0 for "standardized" slice), and an empty SnssaiSD is also valid (means "unspecified SD"). The conversion is correct but relies on the caller's validation. If `session_store.go` is used directly without going through the handler validation chain, these zero/empty values could propagate silently.

**Fix:** Add a `Validate()` method to `AuthCtx` and call it before returning from `Load()`, or document the assumption that callers must validate before `Save()`.

### IN-04: `hasScheme` does not handle `https://` URLs (only `http://`)

**File:** `cmd/biz/main.go:344-347`

```go
func hasScheme(s string) bool {
    return len(s) >= 4 && (s[:4] == "http" || s[:4] == "Http")
}
```

The comparison is case-sensitive (`"Http"` but not `"HTTPS"`), and `s[:4]` will panic if `len(s) < 4`. The `len(s) >= 4` guard prevents panic but is redundant with the `http` prefix check since a URL with `len < 4` cannot start with `http`. More importantly, `https` URLs are not recognized:

```go
hasScheme("https://example.com") // returns false because "http"[:4] != "http"
```

This means `https://` URLs would get `http://` prepended, producing invalid double-scheme URLs.

**Fix:** Use `strings.HasPrefix(strings.ToLower(s), "http")` or check both `http://` and `https://` explicitly:

```go
func hasScheme(s string) bool {
    return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
```

### IN-05: No response body consumed in `forwardToBiz` error path

**File:** `internal/aaa/gateway/gateway.go:378-389`

```go
resp, err := g.bizHTTPClient.Do(httpReq)
if err != nil {
    g.logger.Error("biz service unavailable for server-initiated", ...)
    return
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
    g.logger.Warn("biz returned non-OK for server-initiated", ...)
}
```

When `bizHTTPClient.Do` returns an error, the response body is never read. The `http.Client` should close the body automatically via its `CheckRedirect` or transport, but consuming the body (even with `io.Copy(io.Discard, resp.Body)`) is the safest way to allow connection reuse and avoid potential resource leaks.

**Fix:** In the error path (or always), drain and close the response body:

```go
io.Copy(io.Discard, resp.Body)
resp.Body.Close()
```

---

## Positive Observations

1. **GPSI hashing in logs**: `internal/logging/gpsi.go` correctly implements SHA256-based GPSI hashing (REQ-16), preventing raw GPSI exposure in observability output.

2. **GPSI/SUPI validation**: Both `nssaa/handler.go` and `aiw/handler.go` validate GPSI and SUPI against the 3GPP regex patterns (`^5[0-9]{8,14}$` and `^imu-[0-9]{15}$`) before storage — correctly following the anti-pattern list.

3. **NssaaStatus state machine preserved**: The implementation follows the state machine (NOT_EXECUTED → PENDING → EAP_SUCCESS/EAP_FAILURE) as documented in the anti-pattern list, even though actual AAA forwarding is a placeholder.

4. **Circuit breaker implementation**: `internal/resilience/circuit_breaker.go` correctly implements the CLOSED → OPEN → HALF_OPEN state machine with thread-safe `sync.Mutex` protection and proper threshold handling.

5. **Error wrapping**: Throughout the codebase, errors are wrapped with `fmt.Errorf("...: %w", err)` for chained error inspection, following Go idiom and the golang-patterns skill guidance.

6. **Context propagation in HTTP clients**: Most HTTP clients correctly use `http.NewRequestWithContext(ctx, ...)` and pass `r.Context()` from incoming requests, preserving cancellation and tracing.

7. **Test quality**: Test files (e.g., `radius_handler_test.go`, `notifier_test.go`, `nrf/client_test.go`) use proper table-driven patterns, mock interfaces, and `require.NoError` / `assert` semantics consistent with the golang-testing skill.

8. **Interface compliance via compile-time checks**: Both `nssaa.AuthCtxStore` and `aiw.AuthCtxStore` have compile-time `var _ ... = (*Store)(nil)` checks, ensuring concrete implementations match interfaces.

---

_Reviewed: 2026-04-26T13:54:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
