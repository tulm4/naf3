# Phase 6: Research — Integration Testing & NRM

**Researched:** 2026-04-28
**Status:** Validated approaches, land mines identified

---

## 1. Testing Architecture

### 1.1 Test Directory Structure

```
test/
├── unit/                    # go test ./test/unit/...  (no infra)
│   ├── eap/
│   │   └── engine_test.go   # EAP engine unit tests
│   ├── radius/
│   │   └── encoding_test.go # RADIUS encoding unit tests
│   ├── diameter/
│   │   └── encoding_test.go # Diameter encoding unit tests
│   ├── api/
│   │   ├── nssaa_handler_test.go
│   │   └── aiw_handler_test.go
│   ├── storage/
│   │   └── session_store_test.go
│   ├── auth/
│   │   └── middleware_test.go
│   └── config/
│       └── config_test.go
│
├── integration/             # go test ./test/integration/...  (real PG + Redis)
│   ├── api_test.go          # All N58/N60 endpoints against real DB
│   ├── postgres_test.go     # Partition creation, query, encryption
│   ├── redis_test.go        # Cache, DLQ, session correlation
│   ├── nrf_mock_test.go    # NRF discovery integration
│   ├── udm_mock_test.go    # UDM Nudm_UECM_Get integration
│   ├── ausf_mock_test.go   # AUSF Nnssaaf_AIW_Get integration
│   └── circuit_breaker_test.go
│
├── e2e/                     # go test ./test/e2e/...  (full stack)
│   ├── nssaa_flow_test.go  # AMF mock → HTTP GW → Biz Pod → AAA GW → AAA-S
│   ├── reauth_test.go       # AAA-S → NSSAAF → AMF mock
│   └── revocation_test.go   # AAA-S → NSSAAF → AMF mock
│
├── conformance/             # go test ./test/conformance/...  (spec compliance)
│   ├── ts29526_test.go      # TS 29.526 §7.2 — ~30 cases
│   ├── rfc3579_test.go      # RFC 3579 — RADIUS EAP-Message
│   └── rfc5216_test.go      # RFC 5216 — EAP-TLS MSK derivation
│
├── mocks/                   # Shared mock helpers
│   ├── nrf.go               # NRF httptest server
│   ├── udm.go               # UDM httptest server
│   ├── amf.go               # AMF httptest server (handles callbacks)
│   ├── ausf.go              # AUSF httptest server
│   └── compose.go           # Docker-compose lifecycle for E2E
│
└── aaa_sim/                 # AAA-S simulator (becomes a container)
    ├── main.go
    ├── radius.go            # RADIUS EAP handling
    ├── diameter.go          # Diameter EAP handling
    └── eaptls.go            # EAP-TLS with configurable outcomes
```

**Decision (D-04 already locked):** New tests go into `test/` subdirectories. Existing 36 co-located `*_test.go` files stay in place. This gives clean separation between "tests that ship with source" (unit co-located) and "tests that require infrastructure" (isolated).

### 1.2 NF Mock Structure

All NF mocks (NRF, UDM, AMF, AUSF) are in-process `httptest.Server` instances embedded in `test/mocks/`. Each mock implements the actual HTTP API contract and runs inside the test binary.

**Interface to mock per service:**

- **NRF** (`test/mocks/nrf.go`): `GET /nnrf-nfm/v1/nf_instances/{nfInstanceId}` — returns NF profile; `POST /nnrf-nfm/v1/nf_instances` — registers; `PUT /nnrf-nfm/v1/subscriptions/{subscriptionId}` — heartbeat. Must support `nfStatus=REGISTERED` filtering for service discovery.
- **UDM** (`test/mocks/udm.go`): `GET /nudm-uemm/v1/{supi}/registration` — returns SUCI or mapped SUPI + GPSI. Must handle both "GPSI known" and "GPSI unknown" paths.
- **AMF** (`test/mocks/amf.go`): `POST /namf-callback/v1/{amfId}/Nssaa-Notification` — receives re-auth/revocation from Biz Pod; returns 204. This is the callback receiver.
- **AUSF** (`test/mocks/ausf.go`): `GET /nausf-auth/v1/ue-identities/{gpsi}` — returns UE authentication data for N60.

**AAA-S simulator** (`test/aaa_sim/`) is different: it is a separate Go binary compiled into a container image, used in E2E tests via `docker-compose`. It speaks real RADIUS/Diameter and implements:
- `Access-Request` → `Access-Accept` (EAP-Success)
- `Access-Request` → `Access-Reject` (EAP-Failure)
- `Access-Request` → `Access-Challenge` (EAP-Request/TLS handshake)
- Configurable via `AAA_SIM_MODE` env: `EAP_TLS_SUCCESS`, `EAP_TLS_FAILURE`, `EAP_TLS_CHALLENGE`
- Responds to DER (Diameter-EAP-Request) similarly for Diameter path

### 1.3 E2E Test Runner Architecture

The E2E tests use a **test harness** pattern (not `testcontainers`):

1. `test/mocks/compose.go` starts docker-compose as a subprocess before tests and tears it down after.
2. Tests compile and run as a Go test binary that connects to the live services.
3. AMF and AUSF mocks are httptest servers embedded in the test binary (not containers).
4. The test binary calls the HTTP Gateway on `:8443` (real TLS), which routes to Biz Pod on `:8080`, which calls AAA Gateway on `:9090`, which sends RADIUS to the `mock-aaa-s` container on `:1812`.

```bash
# CI pattern:
docker-compose -f compose/dev.yaml -f compose/test.yaml up -d
go test -v ./test/e2e/... -timeout=30m
docker-compose -f compose/dev.yaml -f compose/test.yaml down
```

### 1.4 Test Binary Layers

| Layer | Command | Infra Required | Runs in CI |
|-------|---------|---------------|------------|
| Unit | `go test ./...` | None | Yes |
| Unit (test/) | `go test ./test/unit/...` | None | Yes |
| Integration | `docker-compose up -d && go test ./test/integration/...` | PG, Redis, mocks | Yes |
| E2E | `docker-compose up -d && go test ./test/e2e/...` | Full stack | Yes |
| Conformance | `go test ./test/conformance/...` | None (mocks) | Yes |

---

## 2. NRM/FCAPS Package

### 2.1 Package Structure

```
internal/nrm/
├── model.go              # YANG model in Go structs (not code-gen)
├── alarm.go              # AlarmManager, alarm types, deduplication
├── fault.go              # FaultManager — evaluates thresholds
├── config.go             # NRMConfig (RESTCONF address, alarm thresholds)
├── server.go             # RESTCONF HTTP server (RFC 8040 JSON)
└── restconf/
    ├── router.go         # Route definitions per RFC 8040 §3
    ├── handlers.go       # GET /restconf/data/... handlers
    └── json.go           # YANG JSON encoding helpers

cmd/nrm/
└── main.go               # Standalone binary entrypoint
```

### 2.2 YANG Model in Go (No Code Generation)

The YANG module `3gpp-nssaaf-nrm` is defined in `docs/design/18_nrm_fcaps.md`. For Phase 6, implement it as Go structs with JSON tags matching the YANG naming convention. YANG uses hyphens in identifiers; Go structs use `JSON:"..."` tags.

**Key structs:**

```go
// NssaaFunction is the root container for the YANG module.
type NssaaFunction struct {
    NssaaFunction []NssaaFunctionEntry `json:"nssaa-function"`
}

type NssaaFunctionEntry struct {
    ManagedElementID   string           `json:"managed-element-id"`
    PLMNInfoList       []string         `json:"pLMNInfoList"`
    SBIFQDN            string           `json:"sBIFQDN"`
    CNSIIdList         []string         `json:"cNSIIdList,omitempty"`
    CommModelList      string           `json:"commModelList"`
    NssaaInfo          *NssaaInfo       `json:"nssaaInfo,omitempty"`
    EpN58              []EndpointN58    `json:"epN58,omitempty"`
    EpN59              []EndpointN59    `json:"epN59,omitempty"`
    PerformanceData    *PerformanceData `json:"performance-data,omitempty"`
}

type NssaaInfo struct {
    SupiRanges             []string `json:"supi-ranges,omitempty"`
    InternalGroupIDRanges  []string `json:"internal-group-id-ranges,omitempty"`
    SupportedSecurityAlgo  []string `json:"supported-security-algo,omitempty"`
}

type EndpointN58 struct {
    EndpointID   string `json:"endpoint-id"`
    LocalAddress string `json:"local-address,omitempty"`
    RemoteAddress string `json:"remote-address,omitempty"`
}

// Alarms are a separate resource per RFC 8040.
type Alarm struct {
    AlarmID              string     `json:"alarm-id"`
    AlarmType            string     `json:"alarm-type"`
    ProbableCause        string     `json:"probable-cause"`
    SpecificProblem      string     `json:"specific-problem"`
    Severity             string     `json:"severity"`
    PerceivedSeverity    string     `json:"perceived-severity"`
    BackupObject         string     `json:"backup-object,omitempty"`
    CorrelatedAlarms     []string   `json:"correlated-alarms,omitempty"`
    ProposedRepairActions string    `json:"proposed-repair-actions"`
    EventTime            time.Time  `json:"event-time"`
}
```

**Why not use `go-yang` or `pyang` code generation?** The NSSAAF YANG model is small (~150 lines). Code generation adds a build dependency and produces verbose generated code. Hand-written Go structs with JSON tags are simpler and easier to maintain for this scope.

### 2.3 Alarm Manager

The `AlarmManager` is the core FCAPS component:

- **Alarm types** (from `docs/design/18_nrm_fcaps.md`):
  - `NSSAA_AAA_SERVER_UNREACHABLE` — AAA-S unreachable
  - `NSSAA_SESSION_TABLE_FULL` — session limit reached
  - `NSSAA_DB_UNREACHABLE` — PostgreSQL unreachable
  - `NSSAA_REDIS_UNREACHABLE` — Redis unreachable
  - `NSSAA_NRF_UNREACHABLE` — NRF unreachable
  - `NSSAA_HIGH_AUTH_FAILURE_RATE` — failure rate >10% over 5 min
  - `NSSAA_CIRCUIT_BREAKER_OPEN` — circuit breaker open for an AAA server

- **Deduplication policy**: An alarm is identified by `(alarmType, backupObject)`. If the same alarm is raised within a 5-minute window, it is not duplicated. This matches ITU-T X.733 alarm deduplication behavior.

- **Thresholds** (Claude's discretion per D-07):
  - Failure rate threshold: **>10%** over a **5-minute sliding window** (REQ-33)
  - Circuit breaker open: immediate alarm (REQ-34)
  - Session table full: configurable via `config.yaml`

### 2.4 Biz Pod → NRM Communication

Two patterns are viable. Decision needed (Claude's discretion):

**Option A — Biz Pod pushes events to NRM via HTTP callback:**
- Biz Pod calls `http://nrm:8081/internal/events` on each significant event (auth success, auth failure, circuit breaker state change).
- NRM maintains alarm state in memory (with optional Redis persistence for HA).
- Pros: NRM has real-time alarm state; no polling.
- Cons: Coupling between Biz Pod and NRM; NRM must be reachable from Biz Pod.

**Option B — NRM pulls from Biz Pod via periodic HTTP fetch:**
- NRM polls `http://biz:8080/metrics` and `http://biz:8080/healthz/ready` every 30 seconds.
- NRM computes failure rates and evaluates alarm conditions from metrics data.
- Pros: Loose coupling; NRM can be deployed independently.
- Cons: 30-second lag in alarm state; metrics must expose enough data.

**Recommendation: Option A (push)** for Phase 6, with the `/internal/events` endpoint added to Biz Pod. The NRM binary is lightweight enough that a push model is more natural and aligns with the "Biz Pod communicates via internal HTTP callback" design in D-05. The poll model can be added as a fallback in Phase 7.

The new endpoint in Biz Pod:

```go
// In cmd/biz/main.go — new mux handler:
mux.HandleFunc("/internal/events", handleNrmEvent)

// handleNrmEvent receives alarm-relevant events from the Biz Pod.
// Currently not implemented — NRM is Phase 6.
// Registration kept for future extensibility.
func handleNrmEvent(w http.ResponseWriter, r *http.Request) {
    // Parse event, forward to alarm manager
}
```

### 2.5 RESTCONF Routes (RFC 8040 JSON)

```
GET  /restconf/data/3gpp-nssaaf-nrm:nssaa-function
GET  /restconf/data/3gpp-nssaaf-nrm:nssaa-function=nssaa-1
GET  /restconf/data/3gpp-nssaaf-nrm:alarms
GET  /restconf/data/3gpp-nssaaf-nrm:alarms=alarm-1
POST /restconf/data/3gpp-nssaaf-nrm:alarms=alarm-1/ack
```

Note: Alarm acknowledgment is a `POST` with an empty body to the alarm URL per RFC 8632 (lightweight alarm mechanism).

---

## 3. RESTCONF JSON Encoding

### 3.1 YANG JSON Serialization Rules

RFC 8040 §5.3.1 defines YANG JSON encoding. Key rules for the NSSAAF model:

1. **Module prefix**: Top-level container key is `"{module-prefix}:{container-name}"`, e.g., `3gpp-nssaaf-nrm:nssaa-function`.
2. **Leaf names**: YANG hyphens map directly to JSON hyphens (`supported-security-algo`, not `supportedSecurityAlgo`).
3. **Identity values**: Written as strings prefixed with the identity module name (`nssAAF-types:EAP-TLS`).
4. **Enumerations**: Written as strings (`SEVERITY_MAJOR`, not integers).
5. **Presence containers**: Represented as `null` or `{}` (not boolean).
6. **Empty leaf-list**: Represented as `[]`.

### 3.2 JSON Response Examples

**GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function:**

```json
{
  "3gpp-nssaaf-nrm:nssaa-function": {
    "nssaa-function": [
      {
        "managed-element-id": "nssaa-1",
        "pLMNInfoList": ["208001"],
        "sBIFQDN": "nssAAF.operator.com",
        "commModelList": "HTTP2_SBI",
        "nssaaInfo": {
          "supi-ranges": ["208001*"],
          "supported-security-algo": ["EAP-TLS", "EAP-TTLS"]
        },
        "epN58": [
          {
            "endpoint-id": "n58-1",
            "local-address": "10.0.1.50"
          }
        ],
        "epN59": [
          {
            "endpoint-id": "n59-1",
            "local-address": "10.0.1.50"
          }
        ]
      }
    ]
  }
}
```

**GET /restconf/data/3gpp-nssaaf-nrm:alarms:**

```json
{
  "3gpp-nssaaf-nrm:alarms": {
    "alarm": [
      {
        "alarm-id": "alarm-001",
        "alarm-type": "NSSAA_HIGH_AUTH_FAILURE_RATE",
        "probable-cause": "threshold-crossed",
        "specific-problem": "Authentication failure rate exceeded 10% over 5 minutes",
        "severity": "MAJOR",
        "perceived-severity": "MAJOR",
        "backup-object": "nssaa-1",
        "proposed-repair-actions": "Check AAA server connectivity and configuration",
        "event-time": "2026-04-28T15:00:00Z"
      }
    ]
  }
}
```

---

## 4. cmd/nrm/ Binary

### 4.1 main.go Structure

```go
// cmd/nrm/main.go
func main() {
    flag.Parse()
    cfg, err := config.Load(*configPath)
    // ... validate cfg.Component == "nrm"

    logger := slog.Default()

    // Initialize alarm store (in-memory for Phase 6; Redis-backed in Phase 8)
    alarmStore := nrm.NewAlarmStore()

    // Initialize alarm manager with thresholds from config
    alarmMgr := nrm.NewAlarmManager(alarmStore, cfg.NRM.AlarmThresholds, logger)

    // Initialize RESTCONF server
    server := nrm.NewServer(cfg.NRM, alarmMgr, logger)

    // Start HTTP server
    srv := &http.Server{
        Addr:    cfg.NRM.ListenAddr,
        Handler: server.Mux(),
    }

    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Error("nrm server error", "error", err)
        }
    }()

    // Register with NRF (Phase 7 — for now, skip)

    <-signalReceived()
    shutdown(srv, alarmStore)
}
```

### 4.2 NRMConfig in config.go

```go
// In internal/config/config.go:

type NRMConfig struct {
    ListenAddr    string         `yaml:"listenAddr"`
    AlarmInterval time.Duration  `yaml:"alarmInterval"`  // Biz Pod poll interval (if Option B)
}

type AlarmThreshold struct {
    FailureRatePercent  float64 `yaml:"failureRatePercent"`  // default: 10.0
    EvaluationWindowSec int     `yaml:"evaluationWindowSec"`  // default: 300 (5 min)
}

type Config struct {
    // ... existing fields
    NRM     *NRMConfig    `yaml:"nrm,omitempty"`
    Alarms  *AlarmConfig  `yaml:"alarms,omitempty"`
}

type AlarmConfig struct {
    Thresholds []AlarmThreshold `yaml:"thresholds"`
}
```

---

## 5. Test Composability

### 5.1 compose.test.yaml

```yaml
# compose/test.yaml extends compose/dev.yaml
services:
  # Override: no http-gateway, aaa-gateway, biz (started by test binary)
  # Add: nrm service
  nrm:
    build:
      context: .
      dockerfile: Dockerfile.nrm
    image: nssAAF-nrm:latest
    depends_on:
      redis:
        condition: service_healthy
    volumes:
      - ./compose/configs/nrm.yaml:/etc/nssAAF/nrm.yaml:ro
    ports: ["8081:8081"]
    networks:
      - backend
```

### 5.2 CI Pipeline

```bash
#!/bin/bash
set -e

# 1. Start infrastructure
docker-compose -f compose/dev.yaml up -d redis postgres

# 2. Build test binary
go test -c -o test_bin ./test/integration/

# 3. Run integration tests
./test_bin -test.v

# 4. Start full stack for E2E
docker-compose -f compose/dev.yaml up -d
go test -c -o e2e_bin ./test/e2e/
./e2e_bin -test.v

# 5. Conformance tests (no infra needed)
go test -v ./test/conformance/...

# 6. Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 5.3 Test Flags

```bash
# Skip infrastructure tests in CI unit-only runs:
go test ./... -short

# Run specific test layer:
go test ./test/unit/... -v
go test ./test/integration/... -v -tags=integration
go test ./test/e2e/... -v -tags=e2e
go test ./test/conformance/... -v

# Parallel execution for unit tests:
go test ./... -parallel=4
```

---

## 6. Coverage Strategy

### 6.1 Current Coverage Analysis

Based on the 36 existing `*_test.go` files and `internal/` package inventory:

**Already well-covered** (>80%, no new tests needed):
- `internal/eap/` — engine_test.go (31 test cases), eap_test.go
- `internal/auth/` — auth_test.go, middleware_test.go
- `internal/api/nssaa/` — handler_test.go (16 test cases)
- `internal/api/aiw/` — handler_test.go
- `internal/config/` — config_test.go, component_test.go
- `internal/proto/` — http_gateway_test.go, biz_callback_test.go, aaa_transport_test.go
- `internal/cache/redis/` — dlq_test.go, cache_test.go
- `internal/resilience/` — retry_test.go, circuit_breaker_test.go
- `internal/types/` — types_test.go
- `internal/logging/` — gpsi_test.go
- `internal/storage/postgres/` — session_store_test.go, session_test.go

**Partially covered** (need new tests):
- `internal/api/common/` — common_test.go (middleware tests exist)
- `internal/nrf/` — client_test.go (basic; needs service discovery tests)
- `internal/udm/` — client_test.go (basic; needs Nudm_UECM_Get variants)
- `internal/ausf/` — client_test.go (basic)
- `internal/amf/` — notifier_test.go (basic)
- `internal/radius/` — radius_test.go, client_test.go (needs RFC 3579 EAP-Message tests)
- `internal/diameter/` — diameter_test.go (needs DER/DEA tests)
- `internal/crypto/` — crypto_test.go (key derivation, envelope encrypt/decrypt)
- `internal/aaa/gateway/` — gateway_test.go, radius_handler_test.go

**Not yet covered** (need new test files in test/unit/):
- `internal/biz/` — Biz Pod orchestration (no tests yet)
- `internal/metrics/` — Prometheus handler tests
- `internal/tracing/` — OTEL tracer tests
- `internal/nrm/` — **will exist after Phase 6**

### 6.2 Coverage Gap Estimates

| Package | Current | Target | Gap |
|---------|---------|--------|-----|
| `internal/eap/` | ~90% | 90% | 0 |
| `internal/api/nssaa/` | ~75% | 80% | ~5% new tests |
| `internal/api/aiw/` | ~70% | 80% | ~10% new tests |
| `internal/radius/` | ~50% | 80% | ~30% new tests (RFC 3579) |
| `internal/diameter/` | ~40% | 80% | ~40% new tests |
| `internal/crypto/` | ~60% | 80% | ~20% new tests |
| `internal/aaa/gateway/` | ~50% | 80% | ~30% new tests |
| `internal/storage/postgres/` | ~60% | 80% | ~20% new tests |
| `internal/metrics/` | 0% | 60% | New tests |
| `internal/biz/` | 0% | 60% | New tests |
| `internal/nrm/` | N/A | 70% | Build from scratch |

### 6.3 Coverage Command

```bash
# Overall coverage
go test ./... -coverprofile=coverage.out -covermode=atomic

# Per-package coverage (find gaps)
go tool cover -func=coverage.out | sort -t'%' -k1 -r | head -30

# HTML report
go tool cover -html=coverage.out -o coverage.html
```

---

## 7. 3GPP Conformance Test Specifics

### 7.1 TS 29.526 §7.2 Conformance (~30 cases)

Test cases grouped by endpoint:

**CreateSliceAuthenticationContext (POST /slice-authentications):**
- TC-NSSAA-001: Valid request → 201 Created, Location header, X-Request-ID propagated
- TC-NSSAA-002: Missing gpsi → 400 Bad Request, cause=BAD_REQUEST
- TC-NSSAA-003: Invalid gpsi format → 400 Bad Request
- TC-NSSAA-004: Missing snssai → 400 Bad Request
- TC-NSSAA-005: snssai.sst out of range (0-255) → 400 Bad Request
- TC-NSSAA-006: snssai.sd invalid hex (not 6 chars) → 400 Bad Request
- TC-NSSAA-007: Missing eapIdRsp → 400 Bad Request
- TC-NSSAA-008: Empty eapIdRsp → 400 Bad Request
- TC-NSSAA-009: Invalid base64 in eapIdRsp → 400 Bad Request
- TC-NSSAA-010: AAA server not configured for snssai → 404 Not Found, cause=NOT_FOUND
- TC-NSSAA-011: Invalid JSON body → 400 Bad Request
- TC-NSSAA-012: Missing Authorization header → 401 Unauthorized
- TC-NSSAA-013: Invalid Authorization header → 401 Unauthorized
- TC-NSSAA-014: No AMF instance ID → 201 (warning in log)

**ConfirmSliceAuthenticationContext (PUT /slice-authentications/{authCtxId}):**
- TC-NSSAA-020: Valid confirm → 200 OK, eapMessage or authResult
- TC-NSSAA-021: Session not found → 404 Not Found
- TC-NSSAA-022: GPSI mismatch in body vs stored → 400 Bad Request
- TC-NSSAA-023: Snssai mismatch → 400 Bad Request
- TC-NSSAA-024: Missing eapMessage → 400 Bad Request
- TC-NSSAA-025: Invalid base64 in eapMessage → 400 Bad Request
- TC-NSSAA-026: Session already completed → 409 Conflict
- TC-NSSAA-027: Invalid authCtxId format → 404 Not Found (route not matched)
- TC-NSSAA-028: Redis unavailable → 503 Service Unavailable
- TC-NSSAA-029: AAA GW unreachable → 502 Bad Gateway

**GetSliceAuthenticationContext (GET /slice-authentications/{authCtxId}):**
- TC-NSSAA-030: Session exists → 200 OK
- TC-NSSAA-031: Session not found → 404 Not Found
- TC-NSSAA-032: Session expired → 404 Not Found

### 7.2 RFC 3579 RADIUS EAP Conformance (~10 cases)

- TC-RADIUS-001: EAP-Message attribute present in Access-Request
- TC-RADIUS-002: Message-Authenticator computed correctly (HMAC-MD5 over entire packet)
- TC-RADIUS-003: EAP-Message fragmentation (>253 bytes split across multiple attributes)
- TC-RADIUS-004: EAP-Message reassembly at receiver
- TC-RADIUS-005: Message-Authenticator in Access-Challenge
- TC-RADIUS-006: Message-Authenticator in Access-Accept
- TC-RADIUS-007: Message-Authenticator in Access-Reject
- TC-RADIUS-008: Invalid Message-Authenticator → packet dropped
- TC-RADIUS-009: Proxy-State attribute preserved end-to-end
- TC-RADIUS-010: User-Name attribute encoding (UTF-8)

### 7.3 RFC 5216 EAP-TLS MSK Derivation (~10 cases)

**MSK derivation test strategy:** Since a live TLS handshake is needed for real MSK, the conformance tests use a **mock TLS session** that returns a pre-defined master secret. This tests the MSK derivation logic, not the TLS implementation.

- TC-EAPTLS-001: MSK length is exactly 64 bytes (RFC 5216 §3.2)
- TC-EAPTLS-002: MSK = first 32 bytes of TLS-exported key material
- TC-EAPTLS-003: EMSK = last 32 bytes of TLS-exported key material
- TC-EAPTLS-004: MSK and EMSK are different
- TC-EAPTLS-005: MSK derivation with empty TLS session data → error
- TC-EAPTLS-006: MSK derivation with insufficient key material (<64 bytes) → error
- TC-EAPTLS-007: Key export label is "EAP-TLS MSK" per RFC 5216
- TC-EAPTLS-008: Session ID included in key derivation context
- TC-EAPTLS-009: Server's final handshake handshake_messages included in derivation
- TC-EAPTLS-010: Peer certificate used in derivation when available

---

## 8. Dependencies

### 8.1 Go Libraries Needed

| Library | Purpose | Status |
|---------|---------|--------|
| `github.com/stretchr/testify` | Assertions, require | Already in go.mod |
| `github.com/alicebob/miniredis/v2` | In-process Redis mock | Already in go.mod |
| `github.com/DATA-DOG/go-sqlmock` | SQL mock for DB tests | **Needs to add** |
| `gopkg.in/yaml.v3` | Config parsing | Already in go.mod |
| `github.com/go-chi/chi/v5` | HTTP routing | Already in go.mod |
| RADIUS client | Already in `internal/radius/` | No new dep |
| Diameter client | Already in `internal/diameter/` | No new dep |

**New dependencies to add to go.mod:**
```go
github.com/DATA-DOG/go-sqlmock v1.5.2
```

No RESTCONF framework library is needed — the RESTCONF server is implemented with `net/http` directly, following RFC 8040 routes. This avoids adding a heavy dependency for a simple HTTP server.

### 8.2 Dockerfile for AAA-S Simulator

```dockerfile
# Dockerfile.aaa-sim
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY test/aaa_sim/ ./aaa_sim/
RUN CGO_ENABLED=0 go build -o /aaa-sim ./aaa_sim/
FROM alpine:latest
COPY --from=builder /aaa-sim /aaa-sim
CMD ["/aaa-sim"]
```

The `main.go` for the simulator reads `AAA_SIM_MODE` env var (default: `EAP_TLS_SUCCESS`).

---

## 9. Integration Points

### 9.1 Biz Pod Changes

1. **New endpoint**: `POST /internal/events` — receives alarm-relevant events from NRM (Option A push model).
2. **New config section**: `config.nrm` — NRM server URL for event push.
3. **Config changes**: `NRMConfig` struct added to `internal/config/config.go`.

### 9.2 NRM Binary Startup

The `cmd/nrm/` binary is standalone. It does NOT depend on Biz Pod being healthy — it can run independently. It starts:
1. RESTCONF HTTP server on configured port
2. Connects to Redis for optional alarm state persistence
3. Registers with NRF (Phase 7, not in Phase 6 scope)

### 9.3 Alarm Event Flow

```
Biz Pod auth failure → calls POST http://nrm:8081/internal/events
                                         ↓
NRM AlarmManager.Evaluate() → detects >10% failure rate
                                         ↓
Alarm raised → stored in AlarmStore
                                         ↓
RESTCONF GET /restconf/data/3gpp-nssaaf-nrm:alarms → alarm JSON
```

---

## 10. Land Mines

### 10.1 Critical Constraints

1. **YANG JSON encoding**: YANG hyphens must become JSON hyphens. `go-yang` or similar is NOT used — manual Go structs with explicit JSON tags. If the JSON field names don't match RFC 8040 conventions, NRD/OAM tools won't be able to read the data.

2. **Alarm deduplication**: Must implement per-(alarmType, backupObject) deduplication with a 5-minute window. If alarms are not deduplicated, the NMS will be flooded with duplicates.

3. **Circuit breaker state**: REQ-34 requires an alarm when the circuit breaker is OPEN. This means the AlarmManager must subscribe to or poll the circuit breaker registry in Biz Pod. Option A (push) is cleaner: Biz Pod pushes `CIRCUIT_BREAKER_OPEN` events to NRM when the circuit breaker transitions to OPEN.

4. **E2E test isolation**: The E2E tests start real processes via docker-compose. The AAA-S simulator must handle a real UDP RADIUS socket (not httptest). The test binary must wait for all services to be healthy before running assertions.

5. **Coverage on crypto package**: The `internal/crypto/` package uses real crypto. Tests must use deterministic test keys, not require a Vault or SoftHSM at test time.

6. **PostgreSQL test data**: Integration tests that insert into real PostgreSQL must use a test database (not the dev database). The docker-compose must provision a separate `nssaa_test` database.

7. **AAA-S container port conflict**: The AAA-S simulator binds UDP port 1812. If the host already has something on 1812, tests will fail. Tests should use a random port or document this requirement.

8. **Config validation**: The `Config.Validate()` function in `internal/config/config.go` must handle the new `nrm` config section without breaking existing component validation.

### 10.2 Unknowns (Need Clarification)

- **NRMConfig default port**: What port should `cmd/nrm/` listen on by default? Recommendation: `:8081` (separate from Biz `:8080` and AAA GW `:9090`).
- **Alarm persistence**: Is alarm state expected to survive NRM restarts? If yes, Redis persistence is needed in Phase 6 (not Phase 8).
- **Metrics endpoint**: Does the Biz Pod `/metrics` endpoint expose enough data for the NRM to compute auth failure rates without needing a separate event push? This affects whether Option A or Option B is viable.

---

## 11. File Creation Map

### Phase 6 Files to Create

```
test/unit/
├── eap/encoding_test.go          # RADIUS/Diameter encoding tests
├── radius/rfc3579_test.go        # RFC 3579 EAP-Message conformance
├── diameter/rfc6733_test.go      # RFC 6733 Diameter conformance
├── crypto/key_derivation_test.go # MSK derivation, envelope encrypt
└── nrm/alarm_test.go             # AlarmManager unit tests

test/integration/
├── compose.go                    # Docker-compose lifecycle helpers
├── api_test.go                   # Full N58/N60 API integration
├── postgres_test.go              # Real DB with test fixtures
├── redis_test.go                 # Real Redis cache + DLQ
└── circuit_breaker_test.go       # Real CB with AAA-S

test/e2e/
├── harness.go                    # Component startup/shutdown
├── nssaa_flow_test.go           # AMF→HTTP GW→Biz→AAA GW→AAA-S
├── reauth_test.go               # AAA-S → NSSAAF → AMF
└── revocation_test.go           # AAA-S → NSSAAF → AMF

test/conformance/
├── ts29526_test.go              # TS 29.526 §7.2 (~30 cases)
├── rfc3579_test.go              # RFC 3579 (~10 cases)
└── rfc5216_test.go              # RFC 5216 MSK (~10 cases)

test/mocks/
├── nrf.go                       # NRF httptest server
├── udm.go                       # UDM httptest server
├── amf.go                       # AMF httptest server (callbacks)
└── ausf.go                      # AUSF httptest server

test/aaa_sim/                     # AAA-S simulator (becomes container)
├── main.go
├── radius.go
└── diameter.go

internal/nrm/
├── model.go                     # YANG structs
├── alarm.go                     # AlarmManager
├── fault.go                     # FaultManager
├── config.go                    # NRMConfig
├── server.go                    # RESTCONF server
└── restconf/
    ├── router.go
    ├── handlers.go
    └── json.go

cmd/nrm/
└── main.go                      # Standalone binary

compose/
├── test.yaml                    # Test compose overlay
└── configs/nrm.yaml              # NRM config for docker

configs/
└── nrm.yaml                     # NRM config reference

Dockerfile.nrm                   # NRM binary Dockerfile
```

---

## 12. Validation Architecture

### 12.1 Test Quality Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Line coverage (overall) | >80% | `go test -cover` |
| Line coverage (biz logic) | >90% | Per-package coverage |
| Branch coverage (errors) | >85% | `go test -cover` |
| TS 29.526 API cases | ~30 | `test/conformance/ts29526_test.go` |
| RFC 3579 cases | ~10 | `test/conformance/rfc3579_test.go` |
| RFC 5216 cases | ~10 | `test/conformance/rfc5216_test.go` |
| E2E critical flows | ~10 | `test/e2e/` |
| Alarm types covered | 7/7 | `internal/nrm/alarm_test.go` |

### 12.2 Validation Checklist

Before marking Phase 6 done:

- [ ] `go test ./... -cover` shows >80% overall
- [ ] `go test ./test/conformance/...` — all ~50 cases pass
- [ ] `docker-compose -f compose/dev.yaml up` → E2E tests pass
- [ ] `curl http://localhost:8081/restconf/data/3gpp-nssaaf-nrm:nssaa-function` returns valid JSON
- [ ] `curl http://localhost:8081/restconf/data/3gpp-nssaaf-nrm:alarms` returns alarm list
- [ ] REQ-33: Alarm raised when failure rate >10% (inject failures in test)
- [ ] REQ-34: Alarm raised when circuit breaker opens (trigger CB in test)
- [ ] No new `go.sum` gaps introduced
