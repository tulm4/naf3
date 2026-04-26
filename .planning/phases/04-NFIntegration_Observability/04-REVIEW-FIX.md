---
phase: 04-NFIntegration_Observability
fixed_at: 2026-04-26T14:35:00Z
review_path: .planning/phases/04-NFIntegration_Observability/04-REVIEW.md
iteration: 3
findings_in_scope: 10
fixed: 9
skipped: 1
status: partial
---

# Phase 4: Code Review Fix Report

**Fixed at:** 2026-04-26T14:35:00Z
**Source review:** `.planning/phases/04-NFIntegration_Observability/04-REVIEW.md`
**Iterations:** 3 (cap reached)
**Fix scope:** critical_warning + auto-discovery of RADIUS equivalent

## Summary

- Findings in scope: 10 (5 from initial review + 5 discovered via auto re-review)
- Fixed: 9
- Skipped: 1 (WR-02 doc-only, no code change needed)
- Status: `partial` — 1 warning remaining (see Unfixed Issues below)

## Fixed Issues

### CR-01: `context.Background()` in Diameter server-initiated handler
**Files:** `internal/aaa/gateway/diameter_handler.go`
**Commit:** `57a1cfe`
**Applied fix:** Replaced `context.Background()` in `handleASR()` and `handleRAR()` with `conn.Context()` from the diam.Conn, preserving OpenTelemetry trace continuity.

### CR-01 (RADIUS equivalent): `context.Background()` in RADIUS server-initiated handler
**Files:** `internal/aaa/gateway/radius_handler.go`, `internal/aaa/gateway/gateway.go`
**Commit:** `1836bf9`
**Applied fix:** Threaded a fresh traced context from the gateway lifecycle (`g.ctx`) through the RADIUS handler chain (`Listen` → `handlePacket` → `handleServerInitiated`). Created an OTel span with message type as span name, session_id/transport/message_type attributes, and propagated span context through `forwardToBiz`. Discovered during iteration 2 re-review — the initial review only covered Diameter handlers.

### WR-01: Goroutine leak in DLQ.Process()
**Files:** `internal/cache/redis/dlq.go`
**Commit:** `61856aa`
**Applied fix:** Added `defer d.wg.Done()` at goroutine start. DLQ.Process() goroutine is now properly tracked by the WaitGroup, and gateway.Stop() can cleanly wait for it.

### WR-02: handleServerInitiated message type validation
**Files:** `cmd/biz/main.go`
**Commit:** `8b17691`
**Applied fix:** Added `slog.Warn` with `session_id`, `message_type`, and `handler_type` attributes in the default case, making unknown type fallthrough observable instead of silent.

### WR-03: JSON marshal error silently skipped in UpdateAuthContext
**Files:** `internal/udm/udm.go`
**Commit:** `48fe111`
**Applied fix:** Checked `json.Marshal` error and returned wrapped error. PUT requests with nil body on marshal failure are prevented.

### WR-04: Prometheus promauto init() panic risk
**Files:** `internal/metrics/metrics.go`
**Commit:** `aaa75c4`
**Applied fix:** Replaced all `promauto` metric registrations with `prometheus.New*` + `MustRegister()`. Custom `Registry = prometheus.NewRegistry()` prevents duplicate registration panics across multiple imports.

### WR-05: hasScheme doesn't handle https URLs
**Files:** `cmd/biz/main.go`
**Commit:** `ad2df1f`
**Applied fix:** Used `strings.HasPrefix(strings.ToLower(s), "http")` to recognize both `http://` and `https://` URLs, preventing double-scheme prepend for TLS endpoints.

### WR-06: Response body not drained on bizHTTPClient error
**Files:** `internal/aaa/gateway/gateway.go`
**Commit:** `9f3c72e`
**Applied fix:** Added `io.Copy(io.Discard, resp.Body)` before `resp.Body.Close()` in all error paths to ensure connection reuse.

## Unfixed Issues

### WR-07: extractPLMNFromSupi slice panic on short input
**File:** `internal/udm/udm.go:136`
**Status:** `not_fixed` (found in iteration 3 re-review, beyond 3-iteration cap)
**Issue:** Function checks `len(supi) >= 10` but `supi[4:10]` will panic on inputs of length 4-9.
**Recommended fix:** Validate SUPI format upstream before calling `extractPLMNFromSupi`, or add a bounds check in the function itself.

---

_Report: 2026-04-26T14:35:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iterations: 3 (cap reached)_
