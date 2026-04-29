---
batch: "B"
phase: "06"
files_reviewed: 22
depth: "deep"
findings:
  critical: 1
  warning: 8
  info: 10
  total: 19
status: issues_found
files_reviewed_list:
  - internal/nrm/alarm.go
  - internal/nrm/alarm_manager.go
  - internal/nrm/alarm_test.go
  - internal/nrm/client.go
  - internal/nrm/config.go
  - internal/nrm/model.go
  - internal/nrm/server.go
  - internal/restconf/handlers.go
  - internal/restconf/json.go
  - internal/restconf/router.go
  - cmd/nrm/main.go
  - compose/configs/nrm.yaml
  - internal/config/config.go
---

# Phase 6 — Batch B: Code Review Report (Deep)

**Reviewed:** 2026-04-29T10:00:00Z
**Depth:** deep
**Files Reviewed:** 22
**Status:** issues_found

---

## Summary

Reviewed all 22 files across `internal/nrm/`, `internal/restconf/`, `cmd/nrm/`, `compose/configs/`, and `internal/config/`. Found **1 critical** data corruption bug in the alarm deduplication path, **8 warnings** (2 RFC 8040 violations, 2 nil-pointer risks, 1 logic bug, 3 implementation gaps), and **10 info-level items** (API encoding inconsistencies, dead fields, test gaps).

---

## Critical Issues

### CR-B-01 — EventTime Overwritten in AlarmStore.Save, Corrupting Deduplication Semantics

**File(s):** `internal/nrm/alarm.go:74`

**Severity:** Critical

**Description:** In `AlarmStore.Save`, the line `alarm.EventTime = time.Now()` unconditionally overwrites the `EventTime` field **after** the dedup window was already computed using the original `alarm.EventTime` (line 78). This means:

1. If a caller passes a non-zero `EventTime` (e.g., from a Biz Pod event timestamp), the dedup window is computed from the correct timestamp.
2. But the stored alarm's `EventTime` is then silently replaced with `time.Now()`, discarding the original event timestamp.

This corrupts the alarm data: the `EventTime` in the stored alarm no longer reflects when the event actually occurred. Over time, all alarms will report `EventTime = time.Now()` regardless of when the underlying event happened — breaking audit trails, correlation analysis, and compliance with ITU-T X.733 §8.2 which requires accurate event timestamps.

**Evidence:**
```78:79:internal/nrm/alarm.go
    deadline: alarm.EventTime.Add(5 * time.Minute),
}
return alarm.AlarmID, nil
```
The deadline is computed from the pre-overwrite `EventTime`, but the stored `alarm.EventTime` is set to `time.Now()` at line 74, two statements earlier.

**Recommendation:** Remove line 74 entirely. Only set `EventTime` if it is zero at the start of `Save`, before computing the dedup key:

```go
if alarm.EventTime.IsZero() {
    alarm.EventTime = time.Now()
}
```
Keep this at lines 54–56 (before the dedup check), and remove the unconditional assignment at line 74.

---

## Warnings

### WR-B-01 — `handleEvents` Missing Nil Guard, Runtime Panic Risk

**File(s):** `internal/nrm/server.go:101–125`

**Severity:** Warning

**Description:** `handleEvents` calls `alarmMgr.Evaluate(&event)` at line 119 without checking if `alarmMgr` is nil. If `handleEvents` is ever called with a nil `AlarmManager` (e.g., due to a programming error during initialization or a race during shutdown), this will panic at runtime. While the current call chain in `NewServer` always passes a non-nil `alarmMgr`, defensive nil guards are appropriate for public/internal API boundaries.

**Evidence:**
```119:119:internal/nrm/server.go
    alarmMgr.Evaluate(&event)
```

**Recommendation:** Add a nil guard:
```go
if alarmMgr == nil {
    http.Error(w, "alarm manager not available", http.StatusServiceUnavailable)
    return
}
alarmMgr.Evaluate(&event)
```

### WR-B-02 — `Evaluate` Calls `store.List()` Outside `mu` Lock, Goroutine Safety Gap

**File(s):** `internal/nrm/alarm_manager.go:227`

**Severity:** Warning

**Description:** In `Evaluate`, `m.mu` is held during the entire switch body (lines 212–278). However, inside the `AUTH_FAILURE` case, `m.store.List()` is called at line 227. The `AlarmStore.List()` method acquires its own `sync.RWMutex`, so there is no data race. However, the logic checks `store.List()` to detect if an alarm is already raised, then calls `m.RaiseAlarm()` (which calls `store.Save()`) — both under `m.mu`. This is safe but relies on a non-obvious contract: the store's internal dedup must be consulted, not a stale in-memory snapshot. The current approach is fragile: if the store's dedup window expires but the alarm record remains, the duplicate check will fail and a duplicate alarm may be created (though the store's dedup will prevent double-storage, the log message will be confusing).

More importantly, `Evaluate` calls `m.RaiseAlarm()` while already holding `m.mu`, and `RaiseAlarm` calls `m.store.Save()` which acquires the store's lock — a lock-in-lock pattern that is safe here (Go RWMutex is reentrant on the same goroutine) but should be documented.

**Evidence:**
```226:240:internal/nrm/alarm_manager.go
    if !raised {
        fr := fmt.Sprintf("[…]", rate, m.authTotal)
        m.RaiseAlarm(AlarmHighAuthFailureRate, "global", fr, SeverityMajor)
    }
```

**Recommendation:** Extract the "alarm already raised" check into a helper that reads the store under the store's own lock, rather than calling `store.List()` from within the `AlarmManager.mu` critical section. This also avoids holding `m.mu` while performing I/O-equivalent store operations.

### WR-B-03 — `EvaluationWindowSec` Defined but Never Used in Alarm Evaluation

**File(s):** `internal/nrm/alarm_manager.go:46`, `internal/nrm/alarm_manager.go:283–288`

**Severity:** Warning

**Description:** `AlarmThresholds.EvaluationWindowSec` (e.g., 300 for 5 minutes) is documented as "the evaluation window" but is never consulted in `Evaluate()`. The failure rate is calculated over the **lifetime** of the `AlarmManager` instance, not over a sliding window. After `ResetAuthMetrics()` is called (if ever), the window resets to zero — but no goroutine or timer ever calls `ResetAuthMetrics()`. The field is effectively dead code.

**Evidence:**
```46:46:internal/nrm/alarm_manager.go
    EvaluationWindowSec int    // e.g. 300 for 5 minutes
```

**Recommendation:** Either:
1. Implement the sliding window: use `EvaluationWindowSec` in `Evaluate` to age out old events (e.g., track events with timestamps in a ring buffer), OR
2. Remove `EvaluationWindowSec` from `AlarmThresholds` if the cumulative counter approach is the intended design (document the lifetime semantics clearly), OR
3. Add a background goroutine in `NewAlarmManager` that calls `ResetAuthMetrics()` on a ticker interval matching `EvaluationWindowSec`.

### WR-B-04 — RFC 8040 §3.1 OPTIONS Pre-flight Only Registered at `/data`, Not at `/data/…` Subpaths

**File(s):** `internal/restconf/router.go:46`

**Severity:** Warning

**Description:** The OPTIONS handler is registered only at `r.Options("/data", handleOptionsData)`. RFC 8040 §3.1 states that a client MAY send an OPTIONS request to any RESTCONF resource path to discover supported methods. A pre-flight OPTIONS to `/restconf/data/3gpp-nssaaf-nrm:nssaa-function` will be handled by the `handleGetNssaaFunction` GET handler (which will return 405 Method Not Allowed), not by `handleOptionsData`.

**Evidence:**
```46:46:internal/restconf/router.go
    r.Options("/data", handleOptionsData)
```

**Recommendation:** Register OPTIONS for the specific data subpaths, or use a chi Group:
```go
r.Group(func(r chi.Router) {
    r.Options("/", handleOptionsData)
    r.Get("/data/3gpp-nssaaf-nrm:nssaa-function", handleGetNssaaFunction)
    r.Get("/data/3gpp-nssaaf-nrm:nssaa-function/{id}", handleGetNssaaFunctionByID)
    // ...
})
```
Alternatively, implement a middleware that handles OPTIONS globally before route matching.

### WR-B-05 — `NRMURL` Field in `nrm.NRMConfig` Never Populated

**File(s):** `internal/nrm/config.go:15`, `cmd/nrm/main.go:39`

**Severity:** Warning

**Description:** `NRMConfig.NRMURL` is defined as a `yaml:"-"` field (never deserialized) with the comment "Set automatically from ListenAddr." However, it is never set. `cmd/nrm/main.go` logs `cfg.NRM.ListenAddr` but never populates or uses `cfg.NRM.NRMURL`. If the Biz Pod NRM client (or any other component) relies on `NRMURL` to construct the push URL, it will get an empty value.

**Evidence:**
```15:15:internal/nrm/config.go
    NRMURL string `yaml:"-"`
```

**Recommendation:** Populate `NRMURL` in `NewServer` or in `main.go` after loading config:
```go
if cfg.NRM.NRMURL == "" {
    cfg.NRM.NRMURL = "http://" + cfg.NRM.ListenAddr
}
```

### WR-B-06 — YAML Config Has Unused `server` Top-level Block and Unused `addr` Field

**File(s):** `compose/configs/nrm.yaml:8–12`

**Severity:** Warning

**Description:** The `server` block at lines 8–12 of `compose/configs/nrm.yaml` defines `addr`, `readTimeout`, `writeTimeout`, `idleTimeout` but these fields are never read by `cmd/nrm/main.go`. The NRM component reads only from `nrm:`. The `server.addr: ":8081"` conflicts with `nrm.listenAddr: ":8081"` (line 15), creating confusion about which takes precedence (answer: `nrm.listenAddr`).

**Evidence:**
```8:12:compose/configs/nrm.yaml
server:
  addr: ":8081"
  readTimeout: 10s
  writeTimeout: 30s
  idleTimeout: 120s
```

**Recommendation:** Remove the `server` block from `nrm.yaml` to eliminate the misleading duplicate configuration. The `nrm:` block alone is sufficient.

### WR-B-07 — No Global Panic Recovery or 500 Handler; Unhandled Routes Return Default chi 404

**File(s):** `internal/nrm/server.go:41`, `internal/restconf/router.go:25`

**Severity:** Warning

**Description:** Neither `server.go` nor `restconf/router.go` installs a panic recovery middleware. If any handler panics, the HTTP connection will be closed abruptly without returning a valid HTTP response. Additionally, invalid RESTCONF paths (e.g., `/restconf/invalid`) fall through to chi's default behavior rather than returning an RFC 8040 §3.2.2 error response with `ietf-restconf:errors` format and proper `Content-Type`.

**Evidence:**
```41:45:internal/nrm/server.go
    mux := http.NewServeMux()
    restconfHandler := restconf.NewRouter(restconf.RouterConfig{AlarmMgr: alarmMgr})
    mux.Handle("/restconf/", restconfHandler)
```

**Recommendation:** Add a RecoverHandler middleware and a catch-all `mux.HandleFunc` for unmatched paths:
```go
func recoverHandler(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if r := recover(); r != nil {
                http.Error(w, "internal server error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}

mux.Handle("/restconf/", recoverHandler(restconfHandler))
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    restconf.SetJSONHeaders(w)
    body := restconf.NewErrorResponse(http.StatusNotFound, "Requested RESTCONF path does not exist")
    w.WriteHeader(http.StatusNotFound)
    json.NewEncoder(w).Encode(body)
})
```

### WR-B-08 — `handleAckAlarm` Hardcodes `"operator"` as Acknowledging Principal

**File(s):** `internal/restconf/handlers.go:159`

**Severity:** Warning

**Description:** The acknowledgment handler hardcodes `ackedBy = "operator"` instead of extracting the operator identity from the HTTP request (e.g., from a client certificate, JWT token, or `X-Operator-ID` header). In production, this means all acknowledgments are attributed to a fictional "operator" user, breaking audit trails and accountability.

**Evidence:**
```159:159:internal/restconf/handlers.go
    acked := alarmMgr.AckAlarmInfo(alarmID, "operator")
```

**Recommendation:** Extract operator identity from the request. At minimum, log a warning that this is a placeholder:
```go
// TODO: Extract operator identity from client certificate or JWT.
// For now, use a placeholder until mTLS/JWT integration is complete.
operator := r.Header.Get("X-Operator-ID")
if operator == "" {
    operator = "operator"
}
acked := alarmMgr.AckAlarmInfo(alarmID, operator)
```

---

## Info

### IN-B-01 — `NssaaFunction` in `nrm/model.go` Never Used by RESTCONF Handlers

**File(s):** `internal/nrm/model.go:25–27`

**Description:** `nrm/model.go` defines `type NssaaFunction struct { NssaaFunction []NssaaFunctionEntry ... }` with the correct YANG JSON encoding (`json:"nssaa-function"`). However, `handleGetNssaaFunction` and `handleGetNssaaFunctionByID` in `restconf/handlers.go` build the response manually using `NssaaFunctionEntry` from `restconf/json.go`, never using the `NssaaFunction` struct from `nrm/model.go`. The struct exists but is dead code in the current implementation.

**Recommendation:** Either use `nrm.NssaaFunction` in the handlers, or remove it if it is not needed. If kept, document that it is intended for future use.

### IN-B-02 — `NssaaFunctionEntry` and Related Types Duplicated Between `nrm/model.go` and `restconf/json.go`

**File(s):** `internal/nrm/model.go:33–91`, `internal/restconf/json.go:43–75`

**Description:** `NssaaFunctionEntry`, `NssaaInfo`, `EndpointN58`, `EndpointN59` are defined in both `nrm/model.go` and `restconf/json.go`. `AlarmInfo` is also defined in both files with different field orderings. This duplication requires careful synchronization on every change and increases maintenance burden. The duplication exists to break an import cycle (`nrm` imports `restconf`, `restconf` imports types from `nrm`).

**Recommendation:** Consider using a shared types package (e.g., `internal/nrmtypes/`) that both `nrm` and `restconf` import, containing only the shared types (`NssaaFunctionEntry`, `EndpointN58`, `EndpointN59`, `AlarmInfo`). The import cycle can also be broken by having `restconf` define only interfaces and having `nrm` implement them (as is done with `AlarmManagerProvider`).

### IN-B-03 — `List` Method in `AlarmStore` Returns Unsorted Alarms, Not Ordered by EventTime Descending

**File(s):** `internal/nrm/alarm.go:83–93`

**Description:** The docstring on `List` says "ordered by EventTime descending," but the implementation iterates over a Go map (insertion order not guaranteed, Go 1.12+) and returns alarms in map iteration order. The alarms are not sorted.

**Recommendation:** Sort the results before returning:
```go
func (s *AlarmStore) List() []*Alarm {
    s.mu.RLock()
    defer s.mu.RUnlock()
    alarms := make([]*Alarm, 0, len(s.alarms))
    for _, a := range s.alarms {
        alarms = append(alarms, a)
    }
    sort.Slice(alarms, func(i, j int) bool {
        return alarms[i].EventTime.After(alarms[j].EventTime)
    })
    return alarms
}
```
Add `"sort"` to the imports.

### IN-B-04 — `Evaluate` Function Not Covered by Unit Tests

**File(s):** `internal/nrm/alarm_manager.go:205–279`, `internal/nrm/alarm_test.go`

**Description:** `Evaluate` is the core alarm evaluation logic but is not directly unit-tested. `TestAlarmManager_FailureRateAlarm` indirectly calls `Evaluate` via `mgr.Evaluate()`. No tests cover the `AUTH_SUCCESS`, `CIRCUIT_BREAKER_CLOSED`, `AAA_UNREACHABLE`, `DB_UNREACHABLE`, `REDIS_UNREACHABLE`, `NRF_UNREACHABLE` event paths, nor the nil-event guard, nor the circuit breaker dedup across multiple servers.

**Recommendation:** Add tests for all `EventType` branches in `Evaluate`, including:
- `AUTH_SUCCESS` (increments counter without raising alarm)
- `AUTH_FAILURE` with rate exactly at threshold (10.0%) — should NOT raise
- `CIRCUIT_BREAKER_OPEN` on two different servers (both alarms stored)
- `CIRCUIT_BREAKER_CLOSED` when not previously open (no-op)
- `AAA_UNREACHABLE`, `DB_UNREACHABLE`, `REDIS_UNREACHABLE`, `NRF_UNREACHABLE`
- Nil event (should return without panic)

### IN-B-05 — `handleHealthz` Does Not Check AlarmManager or Store Health

**File(s):** `internal/nrm/server.go:127–132`

**Description:** `handleHealthz` returns `{"status":"ok"}` unconditionally. For a production health endpoint, this should verify that critical components (alarm store, alarm manager) are healthy. The NRM server has no dependency on external services (no DB, no Redis), so a basic liveness check is acceptable, but a proper readiness check should confirm internal state.

**Recommendation:** If this is a liveness probe (Kubernetes `livenessProbe`), the current implementation is acceptable. If it is a readiness probe, add a basic sanity check:
```go
func handleHealthz(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    if r.Method != http.MethodGet && r.Method != http.MethodHead {
        w.WriteHeader(http.StatusMethodNotAllowed)
        return
    }
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}
```

### IN-B-06 — `NewAlarmData` Wraps Under `alarms` but Route Expects `alarms` as Top-level Key

**File(s):** `internal/restconf/json.go:91–101`, `internal/restconf/router.go:37`

**Description:** `NewAlarmData` creates `{"3gpp-nssaaf-nrm:alarms": {"alarm": [...]}}`. The route is `/data/3gpp-nssaaf-nrm:alarms`. The YANG list path `alarms` should produce `{alarm: [...]}` (list items under the list name), not an extra `alarms` container wrapping the list. Compare with `NewNssaaFunctionData` which correctly produces `{"3gpp-nssaaf-nrm:nssaa-function": {"nssaa-function": [...]}}` for the route `/data/3gpp-nssaaf-nrm:nssaa-function`.

**Recommendation:** Change `NewAlarmData` to match the pattern of `NewNssaaFunctionData`:
```go
func NewAlarmData(alarms []*AlarmInfo) map[string]interface{} {
    list := make([]interface{}, len(alarms))
    for i := range alarms {
        list[i] = alarms[i]
    }
    return WrapWithModule(
        map[string]interface{}{"alarm": list},
        "3gpp-nssaaf-nrm",
        "alarms",
    )
}
```

### IN-B-07 — `handleGetNssaaFunction` Returns `{nssaa-function: [...]}` but `handleGetNssaaFunctionByID` Returns Extra Nesting

**File(s):** `internal/restconf/handlers.go:48`, `internal/restconf/handlers.go:91–93`, `internal/restconf/json.go:78–88`

**Description:** `handleGetNssaaFunction` uses `NewNssaaFunctionData` which wraps under `{3gpp-nssaaf-nrm:nssaa-function: {"nssaa-function": [...]}}`. `handleGetNssaaFunctionByID` uses `WrapWithModule(entry, ...)` which produces `{3gpp-nssaaf-nrm:nssaa-function: <entry>}` — a different structure for the same YANG container. The by-ID response should include the full YANG hierarchy: `{3gpp-nssaaf-nrm:nssaa-function: {"nssaa-function": <entry>}}`.

**Evidence:**
```48:48:internal/restconf/handlers.go
    _ = json.NewEncoder(w).Encode(data)
```
```91:93:internal/restconf/handlers.go
    data := WrapWithModule(entry, "3gpp-nssaaf-nrm", "nssaa-function")
    w.WriteHeader(http.StatusOK)
    _ = json.NewEncoder(w).Encode(data)
```

**Recommendation:** Use `NewNssaaFunctionData([]NssaaFunctionEntry{entry})` for consistency with the list handler, or create a `WrapWithModuleList` helper that produces the same structure for single entries.

### IN-B-08 — `handleModules` Hardcodes Revision `"2025-01-01"`

**File(s):** `internal/restconf/handlers.go:198`

**Description:** The `handleModules` function returns a hardcoded module revision `"2025-01-01"` instead of reading the actual revision from a defined constant or version file. If the YANG module revision changes, this static value will be stale.

**Recommendation:** Define the module revision as a constant:
```go
const yangModuleRevision = "2025-01-01"
```
And use it in `handleModules` and in the `NssaaFunction` struct's JSON field tags.

### IN-B-09 — `ResetAuthMetrics` Exists but Is Never Called

**File(s):** `internal/nrm/alarm_manager.go:283–288`

**Description:** `ResetAuthMetrics()` zeros `authTotal` and `authFailures` but is never invoked from any code path. It exists for the evaluation window but no goroutine or timer calls it. Combined with WR-B-03, this means the failure rate is truly lifetime-based.

**Recommendation:** Either wire it up to a timer (see WR-B-03 recommendation) or remove it if the cumulative counter approach is the intended design (document this in the function comment).

### IN-B-10 — `NewServer` Receives `alarmStore` Parameter but Never Uses It

**File(s):** `internal/nrm/server.go:22–77`

**Description:** `NewServer` accepts `alarmStore *AlarmStore` as a parameter (line 25) but does not store or use it. The `alarmMgr` alone is sufficient for all handler operations. The `alarmStore` parameter creates unnecessary coupling and potential confusion.

**Evidence:**
```25:25:internal/nrm/server.go
    alarmStore *AlarmStore,
```

**Recommendation:** Remove the `alarmStore` parameter from `NewServer` and from the call site in `cmd/nrm/main.go`.

---

## Cross-File Analysis Notes

### Import Cycle Resolution
The `nrm` → `restconf` → `nrm` cycle is resolved via:
- `nrm/alarm_manager.go` imports `restconf` for `restconf.AlarmInfo` (used in `ListAlarmInfos`)
- `restconf/json.go` defines its own `AlarmInfo`, `NssaaFunctionEntry`, `EndpointN58`, `EndpointN59` (duplicated from `nrm/model.go`)
- `restconf/router.go` uses `AlarmManagerProvider` interface (defined in `restconf/router.go`) to avoid importing `nrm`

This pattern is sound but requires keeping the duplicated types in sync.

### Config Flow Verified
`compose/configs/nrm.yaml` → `config.Load()` → `Config.NRM` (`*NRMConfig`) → `main.go` extracts `cfg.NRM.ListenAddr` → `nrm.NewServer(&nrm.NRMConfig{...})` → `nrm.NRMConfig` (separate from `config.NRMConfig`). The two `NRMConfig` structs in different packages (`config` and `nrm`) have different field names (`listenAddr` vs `ListenAddr`), creating a mapping responsibility in `main.go`. This is intentional but worth documenting.

### RFC 8040 Compliance Summary
- ✅ `Content-Type: application/yang.data+json` set via `SetJSONHeaders`
- ✅ RFC 8040 §3.2.2 error format (`ietf-restconf:errors`) used in `NewErrorResponse`
- ✅ Accept header validated (`application/yang.data+json` or `*/*`)
- ✅ OPTIONS handler registered (but only at `/data`, not subpaths — see WR-B-04)
- ✅ `/restconf/modules` endpoint returns module capability
- ✅ `X-Request-ID` correlation — not implemented (missing from handlers.go)
- ⚠️ `Allow` header on 405 responses — correctly set
- ⚠️ OPTIONS response should include `Content-Type: application/yang.data+json` — missing

---

_Reviewed: 2026-04-29T10:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
