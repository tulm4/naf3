# Phase 4 Plan Verification Report

**Plan:** `/home/tulm/naf3/.planning/PHASE_4_IMPLEMENTATION_PLAN.md`
**Verification date:** 2026-04-25
**Verified by:** gsd-plan-checker

---

## Assumption Audit

| # | Assumption | Status | Evidence |
|---|-----------|--------|----------|
| 1 | `cmd/biz/main.go` has in-memory stores at lines 59-60 | **CORRECT** | Lines 59-60: `nssaaStore := nssaa.NewInMemoryStore()`, `aiwStore := aiw.NewInMemoryStore()` |
| 2 | Stub handlers exist at lines 188-201 | **CORRECT** | Lines 188-201: `handleReAuth`, `handleRevocation`, `handleCoA` — all return hardcoded bytes |
| 3 | `internal/nrf/`, `internal/udm/`, `internal/amf/` are stubs | **CORRECT** | All are 4-5 line package declarations only |
| 4 | `internal/ausf/`, `internal/metrics/`, `internal/logging/`, `internal/tracing/` do NOT exist | **CORRECT** | Glob confirms zero matches for all four directories |
| 5 | `internal/storage/postgres/` is READY (6 files) | **CORRECT** | `pool.go`, `session.go`, `migrate.go`, `aaa_config.go`, `audit.go`, `session_test.go` all exist |
| 6 | `internal/cache/redis/` is READY (6 files) | **CORRECT** | `pool.go`, `session_cache.go`, `ratelimit.go`, `lock.go`, `idempotency.go`, `cache_test.go` all exist |
| 7 | `go.mod` missing Prometheus + OpenTelemetry | **CORRECT** | go.mod shows only `pgx`, `redis`, `uuid`, `testify`, `chi`, `diameter` — no Prometheus, no OpenTelemetry |
| 8 | Health endpoints at `/health` and `/ready` | **DISCREPANCY** | See §Gap Analysis item #2 |
| 9 | `nssaa.NewHandler()` accepts `WithNRFClient()` + `WithUDMClient()` | **MISSING** | Only `WithAAA()` and `WithAPIRoot()` exist in handler.go |
| 10 | `aiw.NewHandler()` accepts `aiw.WithAUSFClient()` | **MISSING** | Only `WithAAA()` and `WithAPIRoot()` exist in handler.go |
| 11 | `deployments/k8s/` directory exists | **MISSING** | Glob returns zero matches — directory must be created |
| 12 | `postgres.NewSessionStore()` exists | **MISSING** | `pool.go` only has `NewPool()` — no `NewSessionStore()` function |
| 13 | Config has `AMFConfig` and `AUSFConfig` | **MISSING** | config.go has `NRFConfig` + `UDMConfig` only — `AMFConfig`/`AUSFConfig` absent |

---

## Gap Analysis

### 1. Missing Option Functions for Handler Injection (BLOCKER)

**Problem:** P4-TASK-302 (NRF wiring) and P4-TASK-402 (UDM wiring) assume `nssaa.NewHandler()` accepts `WithNRFClient()` and `WithUDMClient()`. P4-TASK-602 (AUSF wiring) assumes `aiw.NewHandler()` accepts `aiw.WithAUSFClient()`.

**Current state (`internal/api/nssaa/handler.go` lines 97-117):**
```go
// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithAAA sets the AAA router.
func WithAAA(aaa AAARouter) HandlerOption
// WithAPIRoot sets the API root URL.
func WithAPIRoot(apiRoot string) HandlerOption

// NewHandler creates a new NSSAA handler.
func NewHandler(store AuthCtxStore, opts ...HandlerOption) *Handler
```

Only `WithAAA` and `WithAPIRoot` exist. The plan tasks MUST include creating these option functions:

- `nssaa.WithNRFClient(nrfClient *nrf.Client) HandlerOption`
- `nssaa.WithUDMClient(udmClient *udm.Client) HandlerOption`
- `aiw.WithAUSFClient(ausfClient *ausf.Client) HandlerOption`

**Impact:** Without these, the wiring tasks cannot be completed. These are not listed as file creation items anywhere in the plan.

**Fix required:** Add to the `handler.go` modification tasks (P4-TASK-302, P4-TASK-402, P4-TASK-602):
- Create `WithNRFClient` option function
- Create `WithUDMClient` option function
- Create `WithAUSFClient` option function
- Add `nrfClient`, `udmClient`, `ausfClient` fields to the respective `Handler` structs

---

### 2. Health Endpoint Paths Inconsistent (WARNING)

**Plan says:** `/healthz/live` and `/healthz/ready` (P4-TASK-103 acceptance criteria, PHASE_4 roadmap §4.4)

**Code has:** `/health` and `/ready` (main.go lines 103-104)

**Current implementation (main.go lines 257-267):**
```go
func handleHealth(w http.ResponseWriter, r *http.Request) {
    w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
    w.WriteHeader(http.StatusOK)
    io.WriteString(w, `{"status":"ok","service":"nssAAF-biz"}`)
}

func handleReady(w http.ResponseWriter, r *http.Request) {
    w.Header().Set(common.HeaderContentType, common.MediaTypeJSONVersion)
    w.WriteHeader(http.StatusOK)
    io.WriteString(w, `{"status":"ready","service":"nssAAF-biz"}`)
}
```

**Plan's P4-TASK-103 expects:**
- `/healthz/live` returns 200 always (liveness)
- `/healthz/ready` checks DB/Redis/AAA (readiness)

The current endpoints don't distinguish liveness from readiness. `handleHealth` always returns OK with no checks. `handleReady` always returns OK with no checks.

**Fix required:** The plan should add task to rename/replace these handlers:
- `/healthz/live` → always 200 (no dependency checks)
- `/healthz/ready` → check PostgreSQL, Redis, and AAA connectivity first
- Deprecate `/health` and `/ready` or keep as aliases

---

### 3. `NewSessionStore()` Function Missing from PostgreSQL Package (BLOCKER)

**Plan assumption:** `internal/storage/postgres/` provides `NewSessionStore(db *Pool)` that implements `nssaa.AuthCtxStore` and `aiw.AuthCtxStore`.

**Current state (`internal/storage/postgres/session.go`):**
The file exists (confirmed by Glob), but there is no exported `NewSessionStore()` function that wraps `Pool` into a store compatible with the handler interfaces.

**Impact:** P4-TASK-201 cannot replace `nssaa.NewInMemoryStore()` without this function. The plan assumes this function exists but it doesn't.

**Fix required:** P4-TASK-201 must include:
1. Implement `NewSessionStore(*Pool) *PostgresSessionStore` that implements `nssaa.AuthCtxStore` interface
2. Implement `NewAIWSessionStore(*Pool) *PostgresAIWSessionStore` that implements `aiw.AuthCtxStore` interface
3. Or refactor: add `AuthCtxStore() nssaa.AuthCtxStore` method to `*Pool`

The plan currently lists no file creation for these store wrappers. They are implicitly assumed but not explicitly planned.

---

### 4. `deployments/k8s/` Directory Must Be Created (BLOCKER)

P4-TASK-701 and P4-TASK-702 create files inside `deployments/k8s/`:
- `deployments/k8s/nssaa-alerts.yaml`
- `deployments/k8s/servicemonitor.yaml`

The `deployments/k8s/` directory does NOT exist (Glob confirms zero matches). Creating files in a non-existent directory requires creating the directory first.

**Fix required:** Either:
- Add `mkdir -p deployments/k8s` to the task actions for P4-TASK-701/P4-TASK-702, OR
- Treat directory creation as implicit (Go doesn't need it, but `kubectl apply` does)

---

### 5. Missing Config Types for AMF and AUSF (BLOCKER)

The plan assumes `amf.Config` and `ausf.Config` structs exist in `cmd/biz/main.go` wiring code:

```go
amfNotifier := amf.NewNotifier(amf.Config{
    HTTPClient: &http.Client{Timeout: 5 * time.Second},
    MaxRetries: 3,
    NRFClient: nrfClient,
})

ausfClient := ausf.NewClient(ausf.Config{
    BaseURL: cfg.AUSF.BaseURL,
    HTTPClient: &http.Client{Timeout: 30 * time.Second},
    NRFClient: nrfClient,
})
```

**Current config.go state:** Only `NRFConfig` and `UDMConfig` exist (lines 152-162). `AMFConfig` and `AUSFConfig` are absent.

**Impact:** P4-TASK-501 and P4-TASK-601 cannot use `cfg.AUSF` or `cfg.AMF` — these config keys don't exist in the Config struct.

**Fix required:** Either:
1. Add `AMFConfig` and `AUSFConfig` to `internal/config/config.go` as part of P4-TASK-501/P4-TASK-601, OR
2. Use `cfg.NRF` for discovery-based clients (AMF/AUSF don't need explicit base URL — discovered via NRF)

Option 2 is cleaner: AMF and AUSF clients should be discovered via NRF, not configured with static URLs. The NRF discovery cache would return the service endpoint.

---

## Dependency Corrections

### 1. P4-TASK-201: Database Wiring Dependency on P4-TASK-104 Is Spurious

The plan says:
```
P4-TASK-201: Dependencies = P4-TASK-104 (logging) for startup errors
```

**Assessment:** This is overly conservative. `postgres.NewPool()` does not require structured logging — it can use `fmt.Printf` or `log.Printf` for startup errors. The dependency should be removed, making P4-TASK-201 parallelizable with Wave 1.

**Fix:** Change dependency to `None` or leave implicit.

---

### 2. P4-TASK-301: NRF Client Dependency Is Correct

NRF client depends on logging, metrics, and tracing (P4-TASK-104/105/106). This is correct — NRF client needs structured logging for errors, metrics for registration/heartbeat monitoring, and tracing for NRF API calls.

**However:** The plan does not address circular dependency risk explicitly. The plan mentions in Risks (line 648):

> "NRF uses resilience, resilience needs metrics" — resolved by using Prometheus static registration.

This mitigation is valid: `promauto` registers metrics at init time, so `internal/metrics` can be imported by `internal/resilience` without circular import.

---

### 3. P4-TASK-401 and P4-TASK-501: UDM and AMF Depend on NRF (Correct)

UDM and AMF clients both need NRF for service discovery. The dependency chain is:
```
P4-TASK-301 (NRF client) → P4-TASK-401 (UDM) → P4-TASK-402 (UDM wiring)
                           P4-TASK-501 (AMF) → P4-TASK-502 (AMF wiring)
```

This is correct. However, the plan doesn't clarify whether UDM and AMF can run in parallel after NRF client is complete (Wave 4 shows them as parallel). This is valid — UDM client and AMF notifier are independent.

---

## Wave Corrections

### Current Wave Structure (from plan)

| Wave | Tasks | Parallelizable |
|------|-------|----------------|
| 1 | P4-TASK-101, P4-TASK-102, P4-TASK-103, P4-TASK-104, P4-TASK-105, P4-TASK-106 | Yes |
| 2 | P4-TASK-201 (DB wiring) | No (depends on Wave 1 logging) |
| 3 | P4-TASK-301 (NRF client) | No (depends on Wave 1) |
| 4 | P4-TASK-401, P4-TASK-501 | Yes (depend on P4-TASK-301) |
| 5 | P4-TASK-402, P4-TASK-502, P4-TASK-601 | Yes |
| 6 | P4-TASK-602 (AUSF wiring) | No (depends on P4-TASK-601) |
| 7 | P4-TASK-701, P4-TASK-702 | No (depend on Wave 1 metrics) |

### Recommended Corrections

**Wave 1:** Correct. All foundation packages are independent.

**Wave 2:** Remove spurious dependency on P4-TASK-104. P4-TASK-201 can start immediately in Wave 1 or as Wave 1.5 (after pool.go confirmed READY).

**Wave 3:** Correct.

**Wave 4:** Correct. UDM and AMF are independent.

**Wave 5:** **WARNING — P4-TASK-402 and P4-TASK-502 conflict on `cmd/biz/main.go`.** Both modify `cmd/biz/main.go`:
- P4-TASK-402: "In `main.go`: Create UDM client; inject via `nssaa.WithUDMClient()`"
- P4-TASK-502: "In `main.go`: Replace stub handlers, create notifier"

These cannot run in parallel on the same file without merge conflicts. Wave 5 should be split:
- Wave 5a: P4-TASK-402 (UDM wiring) — no conflicts
- Wave 5b: P4-TASK-501 (AMF notifier implementation) — no conflicts
- Wave 5c: P4-TASK-602 (AUSF wiring) — no conflicts
- **Wave 5d: P4-TASK-502 (AMF handler replacement) — must be AFTER 5b (same file as 5b)**

Actually, the real conflict is between **P4-TASK-402** and **P4-TASK-602** for the `main.go` wiring section, AND **P4-TASK-402** and **P4-TASK-502** for the `handler.go` modifications.

Better wave structure:
```
Wave 5a: P4-TASK-402 (UDM handler wiring — modifies nssaa/handler.go + main.go)
Wave 5b: P4-TASK-501 + P4-TASK-601 (AMF + AUSF implementations — no file conflicts)
Wave 6:  P4-TASK-502 + P4-TASK-602 (AMF handler replacement + AUSF wiring — modifies main.go)
```

**Wave 7:** Correct.

---

## Missing Files

### A. Not Listed in File Creation Map (but implied by tasks)

| File | Implied By | Status |
|------|-----------|--------|
| `internal/nssaa/handler.go` | P4-TASK-402 (add UDM client field + calls) | EXISTS — needs MODIFY |
| `internal/aiw/handler.go` | P4-TASK-602 (add AUSF client field) | EXISTS — needs MODIFY |
| `internal/storage/postgres/session_store.go` | P4-TASK-201 (wrappers for AuthCtxStore) | MISSING — needs CREATE |
| `internal/config/config.go` | P4-TASK-501/601 (AMF/AUSF config) | EXISTS — needs MODIFY |
| `internal/nrf/client.go` | P4-TASK-301 | MISSING — needs CREATE |
| `internal/udm/client.go` | P4-TASK-401 | MISSING — needs CREATE |
| `internal/amf/notifier.go` | P4-TASK-501 | MISSING — needs CREATE |
| `internal/ausf/client.go` | P4-TASK-601 | MISSING — needs CREATE |
| `internal/nrf/handler.go` | P4-TASK-301 (HandleStatusChange callback) | MISSING — needs CREATE |
| `internal/resilience/registry.go` | P4-TASK-101 (CircuitBreakerRegistry) | MISSING — needs CREATE |

### B. Handler Option Functions Not Listed

The plan assumes these option functions exist without creating them:

| File to Create | Provides |
|---------------|----------|
| `internal/nssaa/nrf_option.go` | `WithNRFClient(*nrf.Client) HandlerOption` |
| `internal/nssaa/udm_option.go` | `WithUDMClient(*udm.Client) HandlerOption` |
| `internal/aiw/ausf_option.go` | `WithAUSFClient(*ausf.Client) HandlerOption` |

These can be added to existing handler files, but they're not listed.

### C. Missing Handler Fields

| Handler | New Field | Provided By |
|---------|-----------|-------------|
| `nssaa.Handler` | `nrfClient *nrf.Client` | P4-TASK-301 |
| `nssaa.Handler` | `udmClient *udm.Client` | P4-TASK-401 |
| `aiw.Handler` | `ausfClient *ausf.Client` | P4-TASK-601 |

---

## Goal-Backward Verification

### Success Criteria from PHASE_4_NFIntegration_Observability.md (14 criteria + 2 from plan)

| # | Criterion | Covered By | Gap |
|---|-----------|-----------|-----|
| 1 | NSSAAF registers with NRF on startup | P4-TASK-302 | ✅ Covered |
| 2 | Nnrf_NFHeartBeat every 5 minutes | P4-TASK-302 | ✅ Covered |
| 3 | AMF discovered via Nnrf_NFDiscovery | P4-TASK-301 + P4-TASK-501 | ✅ Covered |
| 4 | UDM Nudm_UECM_Get wired to N58 handler | P4-TASK-402 | ✅ Covered (but missing option fn) |
| 5 | AMF Re-Auth/Revocation POSTed correctly | P4-TASK-502 | ✅ Covered (but missing handler option) |
| 6 | AUSF N60 handler with MSK forwarding | P4-TASK-601 + P4-TASK-602 | ✅ Covered (but missing option fn) |
| 7 | PostgreSQL replaces in-memory store | P4-TASK-201 | ⚠️ Partial — `NewSessionStore()` missing |
| 8 | Circuit breaker CLOSED→OPEN→HALF_OPEN | P4-TASK-101 | ✅ Covered |
| 9 | Retry exponential backoff | P4-TASK-102 | ✅ Covered |
| 10 | Health endpoints functional | P4-TASK-103 | ⚠️ Discrepancy — paths don't match current impl |
| 11 | Prometheus metrics at `/metrics` | P4-TASK-105 | ✅ Covered (endpoint not in plan though) |
| 12 | Structured JSON logs with trace context | P4-TASK-104 | ✅ Covered |
| 13 | OpenTelemetry traces | P4-TASK-106 | ✅ Covered |
| 14 | Alert rules defined | P4-TASK-701 + P4-TASK-702 | ✅ Covered |
| 15 | `go build ./...` compiles | Integration phase | ✅ Covered |
| 16 | `go test ./...` passes | Test phase | ✅ Covered |

---

## go.mod Dependency Verification

| Dependency | Plan Says | Current go.mod | Status |
|------------|-----------|----------------|--------|
| `github.com/prometheus/client_golang` | Add v1.20.x | Not present | ✅ Need to add |
| `go.opentelemetry.io/otel` | Add v1.30.x | Not present | ✅ Need to add |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` | Add v1.30.x | Not present | ✅ Need to add |
| `go.opentelemetry.io/otel/sdk/trace` | Add v1.30.x | Not present | ✅ Need to add |
| `go.opentelemetry.io/otel/trace` | Add v1.30.x | Not present | ✅ Need to add |
| `go.opentelemetry.io/otel/propagation` | Add v1.30.x | Not present | ✅ Need to add |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | Add v0.53.x | Not present | ✅ Need to add |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | Add v0.53.x | Not present | ✅ Need to add |

**Note:** The plan lists these as "External Dependency Additions" (section starting line 626), which is correct. The plan does NOT attempt to create tasks for these — they should be added via `go get` during execution.

---

## Structured Issues

```yaml
issues:
  # BLOCKERS

  - dimension: task_completeness
    severity: blocker
    description: "Handler option functions WithNRFClient, WithUDMClient, WithAUSFClient are assumed but not created in any task"
    plan: "PHASE_4"
    tasks: "P4-TASK-302, P4-TASK-402, P4-TASK-602"
    fix_hint: "Add file creation tasks or extend existing handler.go tasks to create WithNRFClient, WithUDMClient, WithAUSFClient option functions and add corresponding fields to Handler structs"

  - dimension: task_completeness
    severity: blocker
    description: "PostgresSessionStore wrapper (NewSessionStore function) is assumed by P4-TASK-201 but not listed as a file to create"
    plan: "PHASE_4"
    task: "P4-TASK-201"
    fix_hint: "Either add internal/storage/postgres/session_store.go to File Creation Map, or clarify that session.go already provides NewSessionStore()"

  - dimension: dependency_correctness
    severity: blocker
    description: "P4-TASK-402 (UDM wiring) and P4-TASK-502 (AMF handler replacement) both modify cmd/biz/main.go in Wave 5 with no depends_on relationship"
    plan: "PHASE_4"
    tasks: "5"
    fix_hint: "Split Wave 5: run P4-TASK-402 first (nssaa handler + main.go UDM section), then P4-TASK-502 (main.go AMF section) as Wave 6"

  - dimension: key_links_planned
    severity: blocker
    description: "AMFConfig and AUSFConfig are used in task actions but don't exist in internal/config/config.go"
    plan: "PHASE_4"
    tasks: "P4-TASK-501, P4-TASK-601"
    fix_hint: "Add AMFConfig and AUSFConfig to internal/config/config.go, OR refactor AMF/AUSF clients to use cfg.NRF for discovery-based URL resolution (recommended)"

  # WARNINGS

  - dimension: task_completeness
    severity: warning
    description: "Health endpoint paths in P4-TASK-103 (/healthz/live, /healthz/ready) don't match current main.go implementation (/health, /ready)"
    plan: "PHASE_4"
    task: "P4-TASK-103"
    fix_hint: "Add task to replace handleHealth/handleReady with /healthz/live and /healthz/ready, or clarify that current /health and /ready are sufficient"

  - dimension: scope_sanity
    severity: warning
    description: "P4-TASK-201 lists cmd/biz/main.go as only modified file but implicitly requires session_store.go creation"
    plan: "PHASE_4"
    task: "P4-TASK-201"
    fix_hint: "Add internal/storage/postgres/session_store.go to files list for P4-TASK-201"

  - dimension: dependency_correctness
    severity: warning
    description: "P4-TASK-201 has spurious dependency on P4-TASK-104 (logging). Database wiring can proceed without structured logging."
    plan: "PHASE_4"
    task: "P4-TASK-201"
    fix_hint: "Remove depends_on: P4-TASK-104. Database wiring has no functional dependency on structured logging."

  - dimension: key_links_planned
    severity: warning
    description: "deployments/k8s/ directory does not exist — P4-TASK-701 and P4-TASK-702 create files inside non-existent directory"
    plan: "PHASE_4"
    tasks: "P4-TASK-701, P4-TASK-702"
    fix_hint: "Either add 'mkdir -p deployments/k8s' to task actions, or clarify directory creation is implicit via kubectl apply"

  - dimension: scope_sanity
    severity: warning
    description: "CircuitBreakerRegistry (mentioned in PHASE_4 roadmap §5.1) is not listed as a file to create in P4-TASK-101"
    plan: "PHASE_4"
    task: "P4-TASK-101"
    fix_hint: "Add internal/resilience/registry.go to P4-TASK-101 files, or clarify that CircuitBreakerRegistry is defined within circuit_breaker.go"

  # INFO

  - dimension: scope_sanity
    severity: info
    description: "NRF client depends on all 5 foundation packages (P4-TASK-104/105/106) plus P4-TASK-101/102/103. This makes Wave 3 wait for the entire Wave 1. Consider whether tracing (P4-TASK-106) can be deferred."
    plan: "PHASE_4"
    task: "P4-TASK-301"
    fix_hint: "NRF client can use structured logging and metrics without tracing. Consider making P4-TASK-106 (tracing) Wave 2 instead of Wave 1, so NRF client can start in Wave 2."

  - dimension: requirement_coverage
    severity: info
    description: "P4-TASK-103 acceptance criteria mentions '/metrics endpoint handler' but no task explicitly creates the /metrics HTTP endpoint in main.go"
    plan: "PHASE_4"
    task: "P4-TASK-105"
    fix_hint: "Add 'mux.Handle(\"/metrics\", promhttp.Handler())' to main.go as part of P4-TASK-201 or a new P4-TASK-108 task"
```

---

## Final Verdict

### NEEDS REVISION

The plan is **structurally sound** in its task breakdown and dependency graph, but has **4 blocking issues** that will prevent successful execution:

1. **Handler option functions are missing from all task files** — Without `WithNRFClient`, `WithUDMClient`, and `WithAUSFClient`, the wiring tasks (P4-TASK-302, P4-TASK-402, P4-TASK-602) cannot complete.

2. **`NewSessionStore()` is missing** — The PostgreSQL package doesn't expose the store wrappers needed to replace in-memory stores.

3. **File conflict in Wave 5** — P4-TASK-402 and P4-TASK-502 both modify `cmd/biz/main.go` in parallel without dependency.

4. **AMF/AUSF config types missing** — The wiring tasks reference `cfg.AMF` and `cfg.AUSF` which don't exist in `config.go`.

### Fix Priority

**Must fix before execution:**
1. Add handler option function creation to P4-TASK-302/402/602 (or create new sub-tasks)
2. Add `NewSessionStore()` to PostgreSQL package in P4-TASK-201
3. Fix Wave 5 dependency: split P4-TASK-402 from P4-TASK-502
4. Add AMFConfig/AUSFConfig to config.go, or refactor to use NRF discovery

**Should fix before execution:**
5. Resolve health endpoint path discrepancy
6. Remove spurious P4-TASK-201 → P4-TASK-104 dependency
7. Add `mkdir -p deployments/k8s` to P4-TASK-701/702

### Estimated Revision Effort

Minor — the plan structure is correct. The issues are all additive (missing items, not structural problems).预计修复工作量：约2-3小时重新规划。

---

*Generated by gsd-plan-checker — 2026-04-25*
