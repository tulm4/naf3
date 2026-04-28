---
phase: 06-integration-testing-nrm
plan: "06-PLAN-2"
subsystem: nrm
tags: [nrm, restconf, fcaps, alarm, yang, rfc8040, fault-management]
note: |
  Wave 2 of Phase 6: NRM RESTCONF server + AlarmManager
  The restconf package was moved from internal/nrm/restconf/ to
  internal/restconf/ to resolve the import cycle between nrm and restconf.

# Dependency graph
requires:
  - 06-PLAN-1 (test mocks not needed for NRM — independent Wave 2)
provides:
  - internal/nrm/model.go
  - internal/nrm/alarm.go
  - internal/nrm/alarm_manager.go
  - internal/nrm/config.go
  - internal/nrm/client.go
  - internal/nrm/server.go
  - internal/restconf/router.go
  - internal/restconf/handlers.go
  - internal/restconf/json.go
  - cmd/nrm/main.go
  - internal/config/config.go (ComponentNRM, NRMConfig)
  - compose/configs/nrm.yaml
  - Dockerfile.nrm
affects:
  - 06-PLAN-3 (NRM alarm evaluation used by integration tests)
  - 06-PLAN-5 (E2E tests verify alarm raising)

# Tech tracking
tech-stack:
  added:
    - github.com/google/uuid v1.6.0
  patterns:
    - AlarmManagerProvider interface to break import cycle
    - RESTCONF JSON encoding per RFC 8040 §5.3.1
    - ITU-T X.733 alarm deduplication (5-minute window)
    - Separate internal/restconf/ package (not subpackage of nrm)

key-files:
  created:
    - internal/nrm/model.go — YANG model structs (NssaaFunctionEntry, Alarm, AlarmEvent)
    - internal/nrm/alarm.go — AlarmStore with deduplication
    - internal/nrm/alarm_manager.go — AlarmManager with 7 alarm types
    - internal/nrm/config.go — NRMConfig struct
    - internal/nrm/client.go — BizPodNRMClient
    - internal/nrm/server.go — Server struct
    - internal/restconf/router.go — RESTCONF router
    - internal/restconf/handlers.go — RESTCONF handler functions
    - internal/restconf/json.go — JSON helpers, AlarmInfo type
    - cmd/nrm/main.go — Binary entry point
    - compose/configs/nrm.yaml — NRM config file
    - Dockerfile.nrm — Docker image
  modified:
    - internal/config/config.go — ComponentNRM, NRMConfig, NRMAlarmThreshold

key-decisions:
  - "restconf/ moved from internal/nrm/restconf/ to internal/restconf/ to break import cycle"
  - "AlarmManagerProvider interface used to decouple restconf from nrm package"
  - "restconf.AlarmInfo owned by restconf package; AlarmManager converts to it"
  - "NRM as standalone cmd/nrm/ binary per D-05 (separate lifecycle from Biz Pod)"
  - "RESTCONF uses JSON encoding (RFC 8040) per D-06"

patterns-established:
  - "YANG JSON encoding: module-prefix:container wrapping"
  - "Alarm deduplication: (AlarmType, BackupObject) key, 5-minute TTL"
  - "RFC 8040 error response format: {ietf-restconf:errors: {error: [...]}}"
  - "Biz Pod → NRM event push via POST /internal/events"

requirements-completed: [REQ-32, REQ-33, REQ-34]

# Metrics
duration: ~15 min
completed: 2026-04-29
---

# Phase 06 — PLAN-2 Summary: NRM RESTCONF Server + AlarmManager

**Wave 2: NRM (Network Resource Model) RESTCONF server with FCAPS fault management**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-04-29T00:00:00Z
- **Completed:** 2026-04-29T00:15:00Z
- **Tasks:** 13/13
- **Commits:** 1 (13 files)

## Accomplishments

- `internal/nrm/` package with YANG model structs, AlarmStore, AlarmManager, BizPodNRMClient, Server
- `internal/restconf/` package with RFC 8040 RESTCONF router and handlers
- `cmd/nrm/` standalone binary with graceful shutdown and /healthz
- `internal/config/` updated with ComponentNRM and NRMConfig
- REQ-32: NSSAAFFunction IOC readable via RESTCONF GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function
- REQ-33: Alarm raised when failure rate >10% (AlarmHighAuthFailureRate)
- REQ-34: Alarm raised when circuit breaker opens (AlarmCircuitBreakerOpen)

## Task Commits

| # | Task | Commit | Type |
|---|------|--------|------|
| 1-13 | NRM RESTCONF Server + AlarmManager | `8799c72` | feat |

## Files Created/Modified

- `internal/nrm/model.go` — YANG model structs (NssaaFunctionEntry, Alarm, AlarmEvent, Severity constants)
- `internal/nrm/alarm.go` — AlarmStore with (AlarmType, BackupObject) deduplication, 5-minute window
- `internal/nrm/alarm_manager.go` — AlarmManager with RaiseAlarm, ClearAlarm, ListAlarms, AckAlarm, Evaluate
- `internal/nrm/config.go` — NRMConfig struct (ListenAddr default :8081, AlarmThresholds)
- `internal/nrm/client.go` — BizPodNRMClient with PushAuthSuccess, PushAuthFailure, PushCircuitBreakerOpen/Closed
- `internal/nrm/server.go` — Server struct with Start, Shutdown, Addr methods
- `internal/restconf/router.go` — RESTCONF router with 7 routes (RFC 8040)
- `internal/restconf/handlers.go` — Handler functions (GET nssaa-function, alarms, ack, OPTIONS, modules)
- `internal/restconf/json.go` — JSON helpers (WrapWithModule, NewAlarmData, ErrorResponse) and AlarmInfo
- `cmd/nrm/main.go` — Binary entry point with --config flag, SIGINT/SIGTERM shutdown, /healthz
- `internal/config/config.go` — Added ComponentNRM, NRMConfig, NRMAlarmThreshold
- `compose/configs/nrm.yaml` — NRM config file
- `Dockerfile.nrm` — Multi-stage Docker image

## RESTCONF Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | /restconf/data/3gpp-nssaaf-nrm:nssaa-function | List NSSAAF function entries (REQ-32) |
| GET | /restconf/data/3gpp-nssaaf-nrm:nssaa-function={id} | Get single NSSAAF function entry |
| GET | /restconf/data/3gpp-nssaaf-nrm:alarms | List all active alarms |
| GET | /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId} | Get single alarm |
| POST | /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId}/ack | Acknowledge alarm |
| GET | /restconf/data | OPTIONS pre-flight (RFC 8040 §3.1) |
| GET | /restconf/modules | YANG module capability (RFC 8040 §3.8) |
| GET | /healthz | Health check |
| POST | /internal/events | Biz Pod event push |

## Alarm Types (7)

| Constant | Description | Severity | REQ |
|----------|-------------|----------|-----|
| AlarmAAAUnreachable | AAA server unreachable | CRITICAL | — |
| AlarmSessionTableFull | Session table full | MAJOR | — |
| AlarmDBUnreachable | PostgreSQL unreachable | CRITICAL | — |
| AlarmRedisUnreachable | Redis unreachable | MAJOR | — |
| AlarmNRFUnreachable | NRF unreachable | MAJOR | — |
| AlarmHighAuthFailureRate | Auth failure rate >10% | MAJOR | REQ-33 |
| AlarmCircuitBreakerOpen | Circuit breaker open | MAJOR | REQ-34 |

## Decisions Made

### Import Cycle Resolution

The initial design placed `restconf/` as a subpackage of `nrm/`, creating an import cycle: `nrm` → `restconf` (for server setup) → `nrm` (for type references in handlers). 

**Solution:** Moved `restconf/` to `internal/restconf/` (sibling package). Used the `AlarmManagerProvider` interface to decouple `restconf` from `nrm`. The `restconf` package owns `AlarmInfo` (the API type), and `AlarmManager` converts its internal `Alarm` to `restconf.AlarmInfo` when exposing data.

### Type Separation

Three alarm-related types exist across packages:
1. `nrm.Alarm` — Internal domain model with full fields
2. `restconf.AlarmInfo` — RESTCONF API type with JSON tags
3. `nrm.AlarmEvent` — Biz Pod → NRM event push payload

This separation ensures the API boundary is clean and the internal model can evolve independently.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Import cycle between nrm and restconf**
- **Found during:** Task 6 (RESTCONF Router) — initial placement in `internal/nrm/restconf/`
- **Issue:** `nrm` imports `restconf`, `restconf` imports `nrm` for types
- **Fix:** Moved `restconf/` to `internal/restconf/` (separate package). Used `AlarmManagerProvider` interface. `restconf.AlarmInfo` is owned by the RESTCONF package; `AlarmManager` implements `ListAlarmInfos() []*restconf.AlarmInfo` and `GetAlarmInfo(id string) *restconf.AlarmInfo`.
- **Files modified:** All files moved, `router.go`, `handlers.go`, `server.go` updated
- **Verification:** `go build ./...` passes
- **Committed in:** `8799c72` (single combined commit)

**2. [Rule 1 - Bug] Duplicate mediaType constant in restconf package**
- **Found during:** Task 8 (JSON helpers)
- **Issue:** `const mediaType` defined in both `handlers.go` and `json.go`
- **Fix:** Removed from `handlers.go`, kept in `json.go`
- **Files modified:** `internal/restconf/handlers.go`
- **Verification:** `go build ./internal/restconf/...` passes
- **Committed in:** `8799c72`

**3. [Rule 3 - Blocking] Type mismatch between config.NRMAlarmThreshold and nrm.AlarmThresholds**
- **Found during:** Task 10 (cmd/nrm main.go)
- **Issue:** `NRMAlarmThreshold` defined in `config` and `AlarmThresholds` in `nrm`, both different types
- **Fix:** Added explicit conversion in `cmd/nrm/main.go`: `&nrm.AlarmThresholds{...cfg.NRM.AlarmThresholds...}`
- **Files modified:** `cmd/nrm/main.go`
- **Verification:** `go build ./cmd/nrm/...` passes
- **Committed in:** `8799c72`

## Issues Encountered

- **Import cycle architecture:** The fundamental issue was designing `restconf` as a subpackage of `nrm`. Standard Go practice for resolving such cycles is sibling packages with interface-based decoupling, which was implemented.
- **Type proliferation:** Three alarm types (`nrm.Alarm`, `restconf.AlarmInfo`, `nrm.AlarmEvent`) exist for architectural clarity. This could be reduced to two if `nrm.Alarm` gains JSON tags (currently has them) and `restconf` imports `nrm` directly, but that would re-introduce the cycle.

## Known Stubs

None — all NRM components are fully implemented with wired data paths.

## Threat Flags

None — NRM is an internal management interface with no external network exposure.

## Next Phase Readiness

- NRM RESTCONF server ready for integration test verification
- AlarmManager ready for REQ-33 and REQ-34 verification
- BizPodNRMClient ready for wiring into Biz Pod handlers (future phase)
- No blockers for PLAN-3 through PLAN-6

---

*Phase: 06-integration-testing-nrm / 06-PLAN-2*
*Completed: 2026-04-29*
