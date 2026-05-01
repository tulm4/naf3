---
phase: 04-NFIntegration_Observability
reviewed: 2026-04-26T14:19:00Z
depth: standard
files_reviewed: 7
files_reviewed_list:
  - internal/aaa/gateway/radius_handler.go
  - internal/aaa/gateway/gateway.go
  - internal/aaa/gateway/diameter_handler.go
  - internal/cache/redis/dlq.go
  - internal/udm/udm.go
  - internal/metrics/metrics.go
  - cmd/biz/main.go
findings:
  critical: 0
  warning: 1
  info: 0
  total: 1
status: issues_found
---

# Phase 4: Code Review Report

**Reviewed:** 2026-04-26T14:19:00Z
**Depth:** standard
**Files Reviewed:** 7
**Status:** issues_found

## Summary

RADIUS `context.Background()` fix verified as correct. All context chains now properly trace from listener through handlers. One bounds-check bug found in `udm.go` that causes a panic on certain SUPI lengths.

## RADIUS context.Background() Fix Verification: PASS

### Fix Location 1: `radius_handler.go`

**`Listen` (line 33-61):** Uses the passed `ctx` for graceful shutdown via `ctx.Done()`. Correct.

```95:127:internal/aaa/gateway/radius_handler.go
func (h *RadiusHandler) handleServerInitiated(ctx context.Context, raw []byte, transport string) {
	// ...
	ctx, span := h.tracer.Start(ctx, msgType,    // span created from incoming ctx ✓
		trace.WithAttributes(
			attribute.String("session_id", sessionID),
			attribute.String("transport", transport),
			attribute.String("message_type", msgType),
		))
	defer span.End()

	h.forwardToBiz(ctx, sessionID, "RADIUS", msgType, raw)  // ctx threaded ✓
}
```

**`handlePacket` (line 64-84):** Receives `ctx` from `Listen` and passes it to `handleServerInitiated`. No `context.Background()` anywhere in the file.

**Fix is correct.** No regression.

### Fix Location 2: `gateway.go`

**Tracer injection (line 94):**
```92:97:internal/aaa/gateway/gateway.go
g.radiusHandler = &RadiusHandler{
	logger:          cfg.Logger,
	tracer:          otel.Tracer("aaa-gateway/radius"),  // tracer injected ✓
	publishResponse: g.publishResponseBytes,
	forwardToBiz:    g.forwardToBiz,
}
```

**`forwardToBiz` (line 346-397):** Uses passed `ctx` for Redis session lookup and HTTP call to Biz Pod.

**Fix is correct.** Tracer properly injected and context threaded through to downstream HTTP call.

## Warnings

### WR-01: SUPI bounds check in `extractPLMNFromSupi` causes panic on short SUPI

**File:** `internal/udm/udm.go:136-141`
**Issue:** The bounds check `if len(supi) >= 10` is insufficient. If `len(supi)` is between 4 and 9 (inclusive), `supi[4:10]` will panic with an index-out-of-range error. The comment says SUPI format is `imsi-{mcc}{mnc}{rest}`, which requires at least 10 characters, but no validation exists upstream to enforce this before calling `extractPLMNFromSupi`.

```134:141:internal/udm/udm.go
// extractPLMNFromSupi extracts PLMN from SUPI format: imsi-{mcc}{mnc}{rest}.
// e.g. imsi-208001000000000 → "208001"
func extractPLMNFromSupi(supi string) string {
	if len(supi) >= 10 {
		return supi[4:10] // "imsi-" = 4 chars, next 6 = MCC+MNC
	}
	return "208001" // default PLMN
}
```

**Fix:** Add lower-bound check to avoid slice panic, and consider returning an error or an explicit default when SUPI is too short:

```go
func extractPLMNFromSupi(supi string) string {
	if len(supi) < 10 {
		return "208001" // default PLMN — SUPI too short
	}
	return supi[4:10]
}
```

Alternatively, validate SUPI format upstream (e.g., via regex `^imsi-[0-9]{5,15}$`) before calling this function, and document the assumption.

---

_Reviewed: 2026-04-26T14:19:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
