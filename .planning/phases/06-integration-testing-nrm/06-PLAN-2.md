---
phase: 06
plan: 06-PLAN-2
wave: 2
depends_on: []
requirements: [REQ-32, REQ-33, REQ-34]
files_modified:
  - internal/nrm/model.go
  - internal/nrm/alarm.go
  - internal/nrm/alarm_manager.go
  - internal/nrm/config.go
  - internal/nrm/server.go
  - internal/nrm/restconf/router.go
  - internal/nrm/restconf/handlers.go
  - internal/nrm/restconf/json.go
  - internal/config/config.go
  - cmd/nrm/main.go
  - compose/configs/nrm.yaml
  - Dockerfile.nrm
---

<objective>

Build the NRM (Network Resource Model) package and standalone `cmd/nrm/` binary. The NRM implements FCAPS fault management via YANG model structs, an AlarmManager with deduplication, and a RESTCONF server (RFC 8040 JSON) that exposes NSSAAFFunction IOC and alarm data. This is Wave 2 — the NRM does not depend on test infrastructure, so it can execute in parallel with Wave 1.

</objective>

<tasks>

## Task 1 — YANG Model Structs (`internal/nrm/model.go`)

<read_first>
- `docs/design/18_nrm_fcaps.md` §2 — YANG model, `NSSAAFFunction`, `Alarm` structs
- `docs/design/18_nrm_fcaps.md` §3 — alarm types and severity constants
- `internal/nrm/` — does not exist yet (new package)
</read_first>

<action>
Create `internal/nrm/model.go` — Go structs matching the YANG model with RFC 8040 JSON tags:
- `NssaaFunction` — root container
- `NssaaFunctionEntry` — list item with ManagedElementID as key
- `NssaaInfo` — NSSAAF-specific info (supi-ranges, supported-security-algo)
- `EndpointN58`, `EndpointN59` — interface endpoints
- `Alarm` — alarm type with AlarmID, AlarmType, ProbableCause, SpecificProblem, Severity, PerceivedSeverity, BackupObject, CorrelatedAlarms, ProposedRepairActions, EventTime
- Alarm severity constants: `SeverityCritical`, `SeverityMajor`, `SeverityMinor`, `SeverityWarning`, `SeverityIndeterminate`
- All JSON field names use YANG hyphens (e.g., `json:"managed-element-id"`, NOT camelCase)
- Package: `internal/nrm`
</action>

<acceptance_criteria>
- `grep "type NssaaFunction" internal/nrm/model.go` returns the struct
- `grep "type Alarm struct" internal/nrm/model.go` returns the alarm struct
- All JSON field names use lowercase-hyphen format
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 2 — Alarm Store (`internal/nrm/alarm.go`)

<read_first>
- `docs/design/18_nrm_fcaps.md` §3 — alarm types, deduplication policy
- `docs/design/18_nrm_fcaps.md` §10 — land mines (deduplication window)
</read_first>

<action>
Create `internal/nrm/alarm.go` — in-memory alarm store with deduplication:
- `AlarmStore` struct — holds map of `(alarmType, backupObject)` → alarm
- `AlarmStore.Save(alarm *Alarm) error` — deduplicate: skip if same `(AlarmType, BackupObject)` exists within 5-minute window
- `AlarmStore.List()` — returns all active alarms
- `AlarmStore.Get(id string)` — returns alarm by ID
- `AlarmStore.Clear(id string)` — removes alarm by ID
- `AlarmStore.Count()` — returns active alarm count
- Alarm deduplication key: `(alarm.AlarmType, alarm.BackupObject)` per ITU-T X.733
- 5-minute deduplication window: `time.Now().Before(alarm.EventTime.Add(5 * time.Minute))`
- Package: `internal/nrm`
</action>

<acceptance_criteria>
- `grep "func.*AlarmStore" internal/nrm/alarm.go` returns Save, List, Get, Clear, Count
- Two `Save()` calls with same `(AlarmType, BackupObject)` within 5 min → second is deduplicated
- `Save()` with different `(AlarmType, BackupObject)` → both stored
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 3 — Alarm Manager (`internal/nrm/alarm.go`)

<read_first>
- `docs/design/18_nrm_fcaps.md` §3.1 — AlarmManager, alarm types
- `docs/design/18_nrm_fcaps.md` §9.1 — Biz Pod → NRM event flow (Option A push)
</read_first>

<action>
Create `internal/nrm/alarm_manager.go` (separate file from `alarm.go` created in Task 2):
- `AlarmManager` struct — holds AlarmStore, thresholds, logger
- `AlarmManager.RaiseAlarm(eventType, backupObject, specificProblem string, severity string)` — creates and stores alarm, generates alarm ID
- `AlarmManager.ClearAlarm(alarmID)` — removes alarm
- `AlarmManager.ListAlarms()` — returns all active alarms
- Predefined alarm type constants:
  - `AlarmAAAUnreachable = "NSSAA_AAA_SERVER_UNREACHABLE"`
  - `AlarmSessionTableFull = "NSSAA_SESSION_TABLE_FULL"`
  - `AlarmDBUnreachable = "NSSAA_DB_UNREACHABLE"`
  - `AlarmRedisUnreachable = "NSSAA_REDIS_UNREACHABLE"`
  - `AlarmNRFUnreachable = "NSSAA_NRF_UNREACHABLE"`
  - `AlarmHighAuthFailureRate = "NSSAA_HIGH_AUTH_FAILURE_RATE"` — REQ-33
  - `AlarmCircuitBreakerOpen = "NSSAA_CIRCUIT_BREAKER_OPEN"` — REQ-34
- `AlarmManager.Evaluate(eventType, metrics)` — evaluates alarm conditions (called by Biz Pod NRMClient)
- Package: `internal/nrm`
</action>

<acceptance_criteria>
- All 7 alarm type constants defined in `alarm.go`
- `AlarmManager` defined in `alarm_manager.go`
- `RaiseAlarm` generates unique alarm ID
- `ClearAlarm` removes alarm by ID
- `ListAlarms` returns all active alarms
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 4 — NRM Config (`internal/nrm/config.go`)

<read_first>
- `internal/config/config.go` — existing config structure, Load/Validate pattern
- `compose/configs/biz.yaml` — existing config file format
</read_first>

<action>
Create `internal/nrm/config.go`:
- `NRMConfig` struct — holds RESTCONF listen address, alarm thresholds
- `NRMConfig.ListenAddr` (default: `:8081`)
- `AlarmThreshold` struct — `FailureRatePercent float64`, `EvaluationWindowSec int`
- `DefaultAlarmThresholds()` — returns default thresholds (failure rate >10%, window 5 min)
- Package: `internal/nrm`
</action>

<acceptance_criteria>
- `grep "type NRMConfig struct" internal/nrm/config.go` returns the struct
- Default listen addr is `:8081`
- Default failure rate threshold is 10.0%
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 5 — Biz Pod NRM Client (`internal/nrm/client.go`)

<read_first>
- `cmd/biz/main.go` — existing Biz Pod startup, health check pattern
- `docs/design/18_nrm_fcaps.md` §9.1 — Biz Pod → NRM push model (Option A)
</read_first>

<action>
Create `internal/nrm/client.go` — `BizPodNRMClient` that pushes alarm-relevant events to the NRM:
- `BizPodNRMClient` struct — holds NRM server URL, HTTP client
- `NewBizPodNRMClient(nrmURL string) *BizPodNRMClient`
- `BizPodNRMClient.PushAuthSuccess()` — pushes AUTH_SUCCESS event
- `BizPodNRMClient.PushAuthFailure(failureRate float64)` — pushes AUTH_FAILURE event with failure rate (triggers alarm evaluation)
- `BizPodNRMClient.PushCircuitBreakerOpen(aaaServer string)` — pushes CIRCUIT_BREAKER_OPEN event (triggers REQ-34 alarm)
- `BizPodNRMClient.PushCircuitBreakerClosed(aaaServer string)` — pushes CIRCUIT_BREAKER_CLOSED event (triggers alarm clear)
- Uses `http.Post` to `http://nrm:8081/internal/events` (configurable URL)
- Package: `internal/nrm`
</action>

<acceptance_criteria>
- `grep "func.*BizPodNRMClient" internal/nrm/client.go` returns all 4 push methods
- All push methods send HTTP POST to the configured NRM URL
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 6 — RESTCONF Router (`internal/nrm/restconf/router.go`)

<read_first>
- `docs/design/18_nrm_fcaps.md` §4 — RESTCONF routes per RFC 8040
- `docs/design/18_nrm_fcaps.md` §2 — YANG JSON encoding rules
</read_first>

<action>
Create `internal/nrm/restconf/router.go`:
- `NewRouter(alarmMgr *AlarmManager, alarmStore *AlarmStore) *http.ServeMux`
- Route definitions:
  - `GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function` — list all NSSAAF function entries
  - `GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function={id}` — get single entry
  - `GET /restconf/data/3gpp-nssaaf-nrm:alarms` — list all active alarms
  - `GET /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId}` — get single alarm
  - `POST /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId}/ack` — acknowledge alarm (empty body)
  - `GET /restconf/data` — RFC 8040 §3.1: OPTIONS pre-flight
  - `GET /restconf/modules` — RFC 8040 §3.8: YANG module capability (static JSON)
- RFC 8040 compliance:
  - `Accept: application/yang.data+json` header required
  - `Content-Type: application/yang.data+json` on responses
  - RFC 8040 error response format for invalid requests
- Package: `internal/nrm/restconf`
</action>

<acceptance_criteria>
- All 7 RESTCONF routes registered in the mux
- `GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function` returns JSON with module prefix
- `GET /restconf/data/3gpp-nssaaf-nrm:alarms` returns alarm list
- `POST /restconf/data/3gpp-nssaaf-nrm:alarms/{id}/ack` returns 204
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 7 — RESTCONF Handlers (`internal/nrm/restconf/handlers.go`)

<read_first>
- `internal/nrm/restconf/router.go` — route definitions
- `internal/nrm/model.go` — YANG model structs
- `internal/nrm/alarm.go` — alarm store
</read_first>

<action>
Create `internal/nrm/restconf/handlers.go`:
- `handleGetNssaaFunction(w, r)` — returns `NssaaFunction` JSON with `3gpp-nssaaf-nrm:` module prefix
- `handleGetAlarms(w, r)` — returns alarm list as `{ "3gpp-nssaaf-nrm:alarms": { "alarm": [...] } }`
- `handleGetAlarm(w, r, alarmID string)` — returns single alarm
- `handleAckAlarm(w, r, alarmID string)` — clears alarm, returns 204
- All handlers use `json.NewEncoder(w)` with `json:"..."` tags matching YANG naming
- Error responses: RFC 8040 §3.2.2 JSON format: `{ "ietf-restconf:errors": { "error": [...] } }`
- Package: `internal/nrm/restconf`
</action>

<acceptance_criteria>
- All 4 handler functions implemented
- JSON output uses YANG module prefix (e.g., `3gpp-nssaaf-nrm:nssaa-function`)
- Error responses use RFC 8040 format
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 8 — RESTCONF JSON Helpers (`internal/nrm/restconf/json.go`)

<read_first>
- `docs/design/18_nrm_fcaps.md` §3 — YANG JSON serialization examples
- RFC 8040 §5.3.1 — YANG JSON encoding rules
</read_first>

<action>
Create `internal/nrm/restconf/json.go`:
- `WrapWithModule(data interface{}, modulePrefix string)` — wraps a struct in `{"{modulePrefix}:{container}": data}`
- `NewNssaaFunctionData(entries []NssaaFunctionEntry)` — returns YANG JSON response for GET nssaa-function
- `NewAlarmData(alarms []*Alarm)` — returns YANG JSON response for GET alarms
- `NewErrorResponse(status int, reason string)` — RFC 8040 error format
- `SetJSONHeaders(w http.ResponseWriter)` — sets Content-Type to `application/yang.data+json`
- Package: `internal/nrm/restconf`
</action>

<acceptance_criteria>
- `WrapWithModule` produces `{"3gpp-nssaaf-nrm:container": {...}}` format
- `NewNssaaFunctionData` returns valid YANG JSON
- `NewAlarmData` returns valid YANG JSON
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 9 — RESTCONF Server (`internal/nrm/server.go`)

<read_first>
- `internal/nrm/restconf/router.go` — router
- `internal/nrm/alarm.go` — alarm manager
- `cmd/biz/main.go` — existing server startup pattern
</read_first>

<action>
Create `internal/nrm/server.go`:
- `NewServer(cfg *NRMConfig, alarmMgr *AlarmManager, alarmStore *AlarmStore, logger *slog.Logger) *Server`
- `Server` struct — holds `http.Server`, router, config
- `Server.Start() error` — starts HTTP server on `cfg.ListenAddr`
- `Server.Shutdown(ctx)` — graceful shutdown
- `Server.Addr()` — returns listen address
- Standard HTTP server fields: ReadTimeout, WriteTimeout, IdleTimeout
- Package: `internal/nrm`
</action>

<acceptance_criteria>
- `grep "func NewServer" internal/nrm/server.go` returns the constructor
- `Server.Start()` starts HTTP server
- `Server.Shutdown(ctx)` gracefully stops server
- `go build ./internal/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 10 — cmd/nrm Binary (`cmd/nrm/main.go`)

<read_first>
- `cmd/biz/main.go` — existing binary structure, config loading, signal handling
- `internal/config/config.go` — Config.Load, component validation
- `internal/nrm/config.go` — NRMConfig
</read_first>

<action>
Create `cmd/nrm/main.go`:
- Flag: `--config` (default: `configs/nrm.yaml`)
- Validates `cfg.Component == "nrm"` (add `ComponentNRM = "nrm"` to config.go)
- Initialize structured JSON logger (slog)
- Initialize `AlarmStore` (in-memory for Phase 6)
- Initialize `AlarmManager` with thresholds from config
- Initialize `BizPodNRMClient` (for receiving events from Biz Pod)
- Initialize RESTCONF `Server`
- Start HTTP server on `cfg.NRM.ListenAddr` (default `:8081`)
- Register `/internal/events` endpoint for Biz Pod event push (alarmMgr.Evaluate)
- Graceful shutdown on SIGINT/SIGTERM
- Health check: `GET /healthz` returns 200 with JSON `{"status":"ok"}`
- Package: `main` (cmd/nrm)
</action>

<acceptance_criteria>
- `go build ./cmd/nrm/...` compiles without error
- Binary accepts `--config` flag
- Binary starts RESTCONF server on configured port
- Binary registers `/internal/events` POST endpoint
- Binary handles graceful SIGINT/SIGTERM shutdown
</acceptance_criteria>

---

## Task 11 — Update `internal/config/config.go` — Add NRMConfig and ComponentNRM

<read_first>
- `internal/config/config.go` — existing Config struct, component types, Validate
- `internal/nrm/config.go` — NRMConfig struct definition
</read_first>

<action>
Update `internal/config/config.go`:
- Add `ComponentNRM ComponentType = "nrm"` to component constants
- Add `NRMConfig` field to `Config` struct (as `NRM *NRMConfig`)
- Add `NRMConfig` struct definition (with ListenAddr, AlarmThresholds)
- Add `applyDefaults` case for `ComponentNRM`: default listen addr `:8081`
- Validate: `ComponentNRM` requires `NRM != nil` and `NRM.ListenAddr != ""`
- Package: `internal/config`
</action>

<acceptance_criteria>
- `grep "ComponentNRM" internal/config/config.go` returns the constant
- `grep "NRMConfig struct" internal/config/config.go` returns the struct
- `go build ./internal/config/...` compiles without error
- `config.Load` correctly handles `component: nrm`
</acceptance_criteria>

---

## Task 12 — NRM Docker Compose Config (`compose/configs/nrm.yaml`)

<read_first>
- `compose/configs/biz.yaml` — existing config file format
- `compose/dev.yaml` — existing compose structure
</read_first>

<action>
Create `compose/configs/nrm.yaml`:
```yaml
component: nrm
version: "1.0.0"

server:
  addr: ":8081"
  readTimeout: 10s
  writeTimeout: 30s
  idleTimeout: 120s

nrm:
  listenAddr: ":8081"
  alarmThresholds:
    - failureRatePercent: 10.0
      evaluationWindowSec: 300
```

</action>

<acceptance_criteria>
- File exists at `compose/configs/nrm.yaml`
- File is valid YAML with `component: nrm`
- File is readable by `config.Load`
</acceptance_criteria>

---

## Task 13 — Dockerfile for NRM (`Dockerfile.nrm`)

<read_first>
- `Dockerfile.biz` (or any existing Dockerfile) — base image, build pattern
- `cmd/nrm/main.go` — binary name
</read_first>

<action>
Create `Dockerfile.nrm`:
```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /nrm ./cmd/nrm/

FROM alpine:3.19
COPY --from=builder /nrm /nrm
EXPOSE 8081
CMD ["/nrm", "--config", "/etc/nssAAF/nrm.yaml"]
```
</action>

<acceptance_criteria>
- `docker build -f Dockerfile.nrm .` succeeds
- Binary `/nrm` exists in image
- `EXPOSE 8081` present in Dockerfile
</acceptance_criteria>

</tasks>

<verification>

Overall verification for Wave 2:
- `go build ./internal/nrm/...` compiles without error
- `go build ./cmd/nrm/...` compiles without error
- `go build ./internal/config/...` compiles without error (NRMConfig added)
- `docker build -f Dockerfile.nrm .` builds successfully
- `curl http://localhost:8081/restconf/data/3gpp-nssaaf-nrm:nssaa-function` returns valid JSON
- `curl http://localhost:8081/restconf/data/3gpp-nssaaf-nrm:alarms` returns alarm list
- `curl -X POST http://localhost:8081/restconf/data/3gpp-nssaaf-nrm:alarms=test-001/ack` returns 204
- `curl http://localhost:8081/healthz` returns 200

</verification>

<success_criteria>

- `internal/nrm/` package exists with all 6 files: `model.go`, `alarm.go`, `config.go`, `client.go`, `server.go`, `restconf/router.go`, `restconf/handlers.go`, `restconf/json.go`
- `cmd/nrm/main.go` is a valid standalone binary that starts a RESTCONF server
- All 7 RESTCONF endpoints respond correctly
- All 7 alarm type constants are defined and can be raised
- NRMConfig is integrated into `internal/config/config.go`
- NRM binary Docker image builds successfully
- REQ-32: NSSAAFFunction IOC readable via RESTCONF `GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function`
- REQ-33: Alarm raised when failure rate >10% (testable via POST /internal/events)
- REQ-34: Alarm raised when circuit breaker opens (testable via POST /internal/events)

</success_criteria>
