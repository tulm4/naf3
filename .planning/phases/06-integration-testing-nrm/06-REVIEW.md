---
phase: "06"
phase_name: "integration-testing-nrm"
status: "critical_issues_found"
files_reviewed: 66
depth: "deep"
critical: 7
warning: 21
info: 19
total: 47
---

# Phase 06 — Code Review Report (Deep)

**Phase:** 06 — Integration Testing & NRM
**Reviewed:** 2026-04-29
**Depth:** deep
**Files Reviewed:** 66 (across 3 batches: A=test infra+mocks, B=NRM+RESTCONF, C=handlers+conformance)
**Status:** critical_issues_found

---

## Executive Summary

47 findings across 66 files. 7 CRITICAL issues require immediate attention, 21 WARNINGs should be addressed before production, and 19 INFO items are minor improvements.

**Most urgent:** Fix GPSI hashing in RADIUS protocol path, RADIUS response authenticator computation, AMF mock race condition, and alarm EventTime corruption.

---

## CRITICAL Issues (7)

### CR-01 — RADIUS Response Authenticator Uses Wrong Attributes (RFC 2865 Violation)

**File(s):** `test/aaa_sim/radius.go:171-209`

**Spec:** RFC 2865 §4

**Description:** `buildRadiusPacket` computes the Response Authenticator as `MD5(code+id+length+requestAuth+response_attrs+secret)`, but RFC 2865 §4 specifies it must use the **original request's attributes** (`req[20:]`), not the response attributes. Real RADIUS clients that validate Response Authenticators will reject all Access-Accept and Access-Challenge packets.

**Evidence:**
```go
// Line 64: passes `attrs` (NEW response attrs) — WRONG
respAuth := md5Authenticator(packet[:20], req[4:20], attrs, s.sharedSecret)
// Should be:
reqAttrs := req[20:]  // original request attributes
respAuth := md5Authenticator(packet[:20], req[4:20], reqAttrs, s.sharedSecret)
```

**Recommendation:** Extract original request attributes (`req[20:]`) and pass those to `md5Authenticator`.

---

### CR-02 — RADIUS Server Never Validates Request Authenticator

**File(s):** `test/aaa_sim/radius.go:89-126`

**Security:** Replay Attack Vector

**Description:** `handlePacket` never validates the Request Authenticator field (bytes 4-20). Any actor with the shared secret can replay old Access-Request packets. While this is a test simulator, it creates test coverage gaps for replay scenarios.

**Evidence:**
```go
// Validates Message-Authenticator (RFC 3579) but NOT Request Authenticator:
if hasMessageAuth(raw) {
    if !verifyMessageAuth(raw, s.sharedSecret) { return }
}
// raw[4:20] (Request Authenticator) is never checked
```

**Recommendation:** Add `verifyRequestAuth(raw, s.sharedSecret)` validation in `handlePacket`.

---

### CR-03 — AMF Mock TOCTOU Race on `failNext` / `errorCode`

**File(s):** `test/mocks/amf.go:91-102`

**Concurrency:** Race Condition

**Description:** Mutex is unlocked between reading `failNext`/`errorCode` and using them. Two concurrent requests can both see `fail=true` and both return errors, when the contract is "exactly one failure."

**Evidence:**
```go
m.mu.Lock()
fail := m.failNext        // read
errCode := m.errorCode    // read
if fail { m.failNext = false }
m.mu.Unlock()             // unlock BEFORE using values
if fail {                  // use after unlock — TOCTOU
    http.Error(w, ..., errCode)
    return
}
```

**Recommendation:** Move the `if fail` check and response inside the locked section.

---

### CR-04 — Alarm `EventTime` Unconditionally Overwritten, Corrupting Deduplication

**File(s):** `internal/nrm/alarm.go:74`

**Data Integrity:** ITU-T X.733 Violation

**Description:** `alarm.EventTime = time.Now()` unconditionally overwrites the EventTime **after** the dedup window was computed from the original value. The stored alarm always gets `EventTime = time.Now()` regardless of when the event actually occurred, breaking audit trails and compliance.

**Evidence:**
```go
// Line 74: unconditional overwrite
alarm.EventTime = time.Now()
// ... dedup check at line 78 uses the ORIGINAL EventTime:
// deadline: alarm.EventTime.Add(5 * time.Minute)
// But the stored alarm.EventTime is already time.Now()
```

**Recommendation:** Remove the unconditional assignment. Only set EventTime if zero at the start of `Save()`:
```go
if alarm.EventTime.IsZero() {
    alarm.EventTime = time.Now()
}
```

---

### CR-05 — GPSI Transmitted in Raw Form to RADIUS/Diameter AAA Servers

**File(s):** `internal/radius/client.go:182-189`

**PII Violation:** TS 33.501 §16, REQ-11/REQ-12

**Description:** `SendEAP` puts raw GPSI into `User-Name` and `Calling-Station-Id` RADIUS attributes. Per 3GPP PII handling requirements, GPSI must be pseudonymized. `HashGPSI` exists in `internal/cache/redis/session_cache.go:104` but is never wired into the RADIUS send path.

**Evidence:**
```go
attrs := []Attribute{
    MakeStringAttribute(AttrUserName, gpsi),           // raw GPSI
    MakeStringAttribute(AttrCallingStationID, gpsi),    // raw GPSI
}
```

**Recommendation:**
```go
hashedGpsi := redis.HashGPSI(gpsi)
attrs := []Attribute{
    MakeStringAttribute(AttrUserName, hashedGpsi),
    MakeStringAttribute(AttrCallingStationID, hashedGpsi),
}
```

---

### CR-06 — GPSI Field Name Mismatch: Compile Error

**File(s):** `internal/api/nssaa/handler.go:33,347`

**Description:** `AuthCtx` struct defines field `GPSI` (all uppercase, line 33) but the handler accesses `authCtx.GPSI` (mixed case, line 347). This is a Go compile error — `GPSI != GPSI` due to case sensitivity.

**Fix:** Change `authCtx.GPSI` to `authCtx.GPSI` (match struct field exactly).

---

### CR-07 — E2E Harness: Docker-Compose Process Orphaned on Test Timeout

**File(s):** `test/e2e/harness.go:300-330`

**Resource Leak:** The `exec.CommandContext(ctx, "sh", "-c", ...)` pattern for docker-compose means SIGKILL from test timeout only kills the shell, not the docker-compose child process. Containers may remain running after test timeout.

**Recommendation:** Kill docker-compose process group directly, or use `exec.Command` without `sh -c`.

---

## WARNING Issues (21)

### WR-01 — Diameter EAP-Payload AVP Encoded Inside Wrong Parent AVP

**File(s):** `test/aaa_sim/diameter.go:118-127` | **Spec:** RFC 6733, TS 29.561 §17.3

EAP-Payload AVP (1265) is nested inside `VendorSpecificApplicationID` instead of being a top-level AVP. Correct:
```go
a.NewAVP(1265, avp.Mbit, vendor3GPP, datatype.OctetString(eapPayload))
```

### WR-02 — AMF Mock Accepts Wrong `NotificationType` Values

**File(s):** `test/mocks/amf.go:111-117` | **Spec:** TS 23.502 §4.2.9.3

Accepts lowercase `"reauth"`/`"revocation"` but rejects the spec-required `"SLICE_RE_AUTH"`/`"SLICE_REVOCATION"`. Fix: match the 3GPP enum values.

### WR-03 — RADIUS `seenChallenge` Map Has No Concurrency Protection

**File(s):** `test/aaa_sim/radius.go:116-124`

`seenChallenge[sessionID]` read and write from multiple goroutines without RWMutex. Fatal in Go 1.21+.

### WR-04 — `checkComposeHealth` Silently Swallows JSON Decode Errors

**File(s):** `test/mocks/compose.go:168-169`

Malformed service JSON entries are skipped. If ALL entries are malformed, function returns `true, nil` (all healthy) — false positive.

### WR-05 — E2E Tests Use Non-Deterministic Sleep Instead of Synchronization

**File(s):** `test/e2e/nssaa_flow_test.go`, `test/aaa_sim/aaa_sim_test.go`

`time.Sleep` for flow control instead of `sync.WaitGroup` or channels.

### WR-06 — E2E Happy Path Tests Lack Response Body Assertions

**File(s):** `test/e2e/nssaa_flow_test.go:69-71`

Only checks HTTP status codes. Never verifies `authResult`, `pvsInfo`, or that AMF mock received the notification.

### WR-07 — AUSF Mock Has Same TOCTOU Pattern as AMF Mock

**File(s):** `test/mocks/ausf.go:71-88`

`errorCodes[gpsi]` and `authData[gpsi]` read outside lock. Lower severity than CR-03 (atomic reads) but should still be fixed.

### WR-08 — `TestRadiusServerChallengeMode` Has No Real Assertions

**File(s):** `test/aaa_sim/aaa_sim_test.go:51-86`

Only verifies server doesn't crash, not correct RADIUS protocol output (response code, State attribute, Message-Authenticator).

### WR-09 — `TestE2E_NSSAA_AuthChallenge` Hardcodes Loop Count

**File(s):** `test/e2e/nssaa_flow_test.go:146-192`

Loop of 3 is arbitrary. No assertion on final `authResult`.

### WR-10 — `handleEvents` Missing Nil Guard

**File(s):** `internal/nrm/server.go:119`

Calls `alarmMgr.Evaluate(&event)` without nil check. Runtime panic if called during initialization or shutdown race.

### WR-11 — `Evaluate` Calls `store.List()` While Holding `AlarmManager.mu`

**File(s):** `internal/nrm/alarm_manager.go:227`

Lock-in-lock pattern (safe with Go reentrant mutex, but fragile). See report for refactoring recommendation.

### WR-12 — `EvaluationWindowSec` Defined but Never Used

**File(s):** `internal/nrm/alarm_manager.go:46,283-288`

`ResetAuthMetrics()` is never called. Failure rate is truly lifetime-based, not sliding window. Either implement the sliding window or remove the dead field.

### WR-13 — RFC 8040 OPTIONS Pre-flight Only at `/data`, Not Subpaths

**File(s):** `internal/restconf/router.go:46`

OPTIONS to `/restconf/data/3gpp-nssaaf-nrm:nssaa-function` hits the GET handler (405), not the OPTIONS handler.

### WR-14 — `NRMURL` Field in `nrm.NRMConfig` Never Populated

**File(s):** `internal/nrm/config.go:15`, `cmd/nrm/main.go:39`

`NRMURL` has `yaml:"-"` (never deserialized) with comment "Set automatically" but is never set.

### WR-15 — YAML Config Has Unused `server` Top-level Block

**File(s):** `compose/configs/nrm.yaml:8-12`

`server.addr`, `server.readTimeout`, etc. are defined but never read. Conflicts with `nrm.listenAddr`.

### WR-16 — No Global Panic Recovery Middleware

**File(s):** `internal/nrm/server.go:41`, `internal/restconf/router.go:25`

Handler panic closes HTTP connection without RFC 8040 error response.

### WR-17 — `handleAckAlarm` Hardcodes `"operator"` as Acknowledging Principal

**File(s):** `internal/restconf/handlers.go:159`

All acknowledgments attributed to fictional "operator" user. Break audit trails.

### WR-18 — Conformance Tests Document Gaps That Handler Already Fixed

**File(s):** `test/conformance/ts29526_test.go:196-288,485-502`

Tests assert `_ = rec` (nothing) for TC-NSSAA-009 and TC-NSSAA-023, but the NSSAA handler now validates base64 and Snssai. Stale gap documentation.

### WR-19 — AIW Handler Base64 Comment Is Stale

**File(s):** `internal/api/aiw/handler.go:164-166`

Comment claims "no explicit base64 validation needed" but NSSAA handler DOES validate base64 explicitly. AIW handler lacks the same explicit validation.

### WR-20 — `Calling-Station-Id` Used for GPSI Is Non-Standard

**File(s):** `internal/radius/client.go:184`

Conventionally carries layer-2 ID (MAC), not subscriber identity. Per TS 29.561, GPSI should be in `User-Name`.

### WR-21 — `seenChallenge` Concurrent Map Access in RADIUS Challenge Mode

**File(s):** `test/aaa_sim/radius.go:53,63,116-124`

Same root cause as WR-03. `seenChallenge[sessionID]` read+write from concurrent goroutines without synchronization.

---

## INFO Issues (19)

### IN-01 — Hardcoded `"testing123"` Shared Secret (test infra only)

**File(s):** `test/aaa_sim/mode.go:71` — Acceptable for test; add `// Test-only` comment.

### IN-02 — GPSI Format Not Validated in AUSF Mock

**File(s):** `test/mocks/ausf.go:78` — Accepts any non-empty string as GPSI.

### IN-03 — UDM Mock Accepts `5g-` Prefix for SUPI

**File(s):** `test/mocks/udm.go:106` — TS 29.571 only allows `imu-` prefix.

### IN-04 — `exec.CommandContext` with `sh -c` Ignores Context Cancellation

**File(s):** `test/e2e/harness.go:259` — Shell receives SIGKILL, not the child build process.

### IN-05 — `TestE2E_AIW_MSKExtraction` Always Skipped

**File(s):** `test/e2e/aiw_flow_test.go:84-90` — No implementation.

### IN-06 — `NssaaFunction` Struct in `nrm/model.go` Never Used

**File(s):** `internal/nrm/model.go:25-27` — Dead code; RESTCONF handlers build response manually.

### IN-07 — Types Duplicated Between `nrm/model.go` and `restconf/json.go`

**File(s):** `internal/nrm/model.go:33-91`, `internal/restconf/json.go:43-75` — `NssaaFunctionEntry`, `AlarmInfo` defined in both packages.

### IN-08 — `AlarmStore.List()` Returns Unsorted Alarms

**File(s):** `internal/nrm/alarm.go:83-93` — Docstring promises "ordered by EventTime descending" but Go map iteration order is not sorted.

### IN-09 — `Evaluate` Not Directly Unit-Tested

**File(s):** `internal/nrm/alarm_manager.go:205-279` — Only indirect coverage via `TestAlarmManager_FailureRateAlarm`.

### IN-10 — `handleHealthz` Returns Unconditional `{"status":"ok"}`

**File(s):** `internal/nrm/server.go:127-132` — Acceptable as liveness probe, but not suitable as readiness check.

### IN-11 — `NewAlarmData` Wrapping Structure Inconsistent with `NewNssaaFunctionData`

**File(s):** `internal/restconf/json.go:91-101` — Extra nesting under `alarms` key vs consistent YANG encoding.

### IN-12 — `handleGetNssaaFunctionByID` Returns Extra Nesting

**File(s):** `internal/restconf/handlers.go:91-93` — Single-entry response differs structure from list response.

### IN-13 — `handleModules` Hardcodes Revision `"2025-01-01"`

**File(s):** `internal/restconf/handlers.go:198` — Static value; should be a named constant.

### IN-14 — `ResetAuthMetrics` Never Called

**File(s):** `internal/nrm/alarm_manager.go:283-288` — Combined with WR-12: cumulative counter, not sliding window.

### IN-15 — `NewServer` Receives Unused `alarmStore` Parameter

**File(s):** `internal/nrm/server.go:25` — `alarmStore` never stored or used.

### IN-16 — GPSI Hashing Function Exists but Unused in Protocol Path

**File(s):** `internal/cache/redis/session_cache.go:104` — `HashGPSI` exported but not wired into RADIUS/Diameter send path.

### IN-17 — Redis Key Prefix Hardcoded in Integration Tests

**File(s):** `test/integration/nssaa_api_test.go:352` — Should use `sessionKey()` helper from `session_cache.go`.

### IN-18 — AUSF Mock Test Has No Field-Level Assertions

**File(s):** `test/integration/ausf_mock_test.go:33-36` — Unmarshal succeeds but fields are never verified.

### IN-19 — Test Naming Inconsistency: Unit vs Integration Packages

**File(s):** `test/unit/api/`, `test/integration/` — `TestConfirmSliceAuth_*` in both packages with different scopes.

---

## Priority Fix Order

| Priority | Finding | File | Type |
|---|---|---|---|
| P0 | CR-06: GPSI field compile error | `handler.go:347` | Bug |
| P0 | CR-04: EventTime overwrite | `alarm.go:74` | Data integrity |
| P0 | CR-05: Raw GPSI in RADIUS attrs | `client.go:182-189` | PII leak |
| P0 | CR-03: AMF mock TOCTOU race | `amf.go:91-102` | Concurrency |
| P0 | CR-01: RADIUS Response Auth | `radius.go:64` | Protocol |
| P0 | CR-02: No Request Auth validation | `radius.go:99-105` | Security |
| P0 | CR-07: Compose process orphan | `harness.go:300` | Resource leak |
| P1 | WR-01: Diameter EAP AVP nesting | `diameter.go:118-127` | Protocol |
| P1 | WR-02: Wrong NotificationType values | `amf.go:111-117` | Spec |
| P1 | WR-03/WR-21: `seenChallenge` no lock | `radius.go:53,116` | Concurrency |
| P1 | WR-10: Nil guard missing | `server.go:119` | Panic |
| P1 | WR-18: Conformance test staleness | `ts29526_test.go` | Test quality |
| P1 | WR-19: AIW base64 validation | `aiw/handler.go:164` | Gap |
| P2 | All remaining WR and IN | Various | Improvement |

---

## Cross-Batch Findings (Issues Spanning Multiple Batches)

| Issue | Batch A | Batch C | Notes |
|---|---|---|---|
| GPSI not hashed in RADIUS | `test/aaa_sim/` | `internal/radius/` | Simulator + production both affected |
| RADIUS protocol compliance | `test/aaa_sim/radius.go` | — | Simulator-only, doesn't affect production |
| TOCTOU races | `test/mocks/amf.go` | — | Test mock only |

---

## Protocol Compliance Summary

| Protocol | Spec | Status | Critical Issues |
|---|---|---|---|
| RADIUS Access-Request validation | RFC 2865 §4 | PARTIAL | CR-01 (response attrs), CR-02 (no request auth check) |
| RADIUS Message-Authenticator | RFC 3579 §3.2 | PASS | `verifyMessageAuth` correct |
| RADIUS Access-Challenge | RFC 2865 §4.3 | PASS | State attribute correct |
| Diameter DER/DEA | RFC 6733, TS 29.561 | PARTIAL | WR-01 (wrong EAP AVP nesting) |
| RESTCONF | RFC 8040 | PARTIAL | WR-13 (OPTIONS subpaths), WR-16 (no panic recovery) |
| 3GPP NssaaNotification | TS 29.518 §5.2.2.27 | PARTIAL | WR-02 (wrong NotificationType values) |
| GPSI PII Handling | TS 33.501 §16 | FAIL | CR-05 (raw GPSI in protocol) |
| ITU-T X.733 Alarm Timestamps | X.733 §8.2 | FAIL | CR-04 (EventTime overwritten) |

---

## Batch Files Reference

| Batch | Files | CR | WR | IN | Total |
|-------|-------|----|----|----|-------|
| A — Test Infra & Mocks | 17 | 4 | 9 | 5 | 18 |
| B — NRM & RESTCONF | 22 | 1 | 8 | 10 | 19 |
| C — Handlers, Config, Conformance | 36 | 2 | 4 | 4 | 10 |
| **Total (deduplicated)** | **66** | **7** | **21** | **19** | **47** |

---

_Reviewed: 2026-04-29T10:00:00Z_
_Reviewers: Claude (gsd-code-reviewer, 3 parallel agents)_
_Depth: deep_
