# Phase 6: Integration Testing & NRM - Context

**Gathered:** 2026-04-28 (initial) + 2026-04-28 (supplemental: E2E + conformance) + 2026-04-28 (supplemental: AAA-S Diameter library + SCTP) + 2026-04-30 (compose/Makefile reorganization)
**Status:** Ready for planning

<domain>
## Phase Boundary

Comprehensive testing (unit, integration, E2E, conformance) plus the NRM/FCAPS management interface for NSSAAF. Unit tests cover every package with >80% line coverage. Integration tests exercise all API endpoints against real PostgreSQL and Redis via docker-compose. E2E tests run the full AMF → HTTP GW → Biz Pod → AAA GW → AAA-S flow, plus AIW flows (AUSF → HTTP GW → Biz Pod → AAA GW → AAA-S with MSK verification). Conformance tests validate TS 29.526 §7.2, RFC 3579, and RFC 5216. NRM implements the YANG model, RESTCONF API, and alarm management per TS 28.541 §5.3.145.

Not this phase: k6 load tests (Phase 8), chaos testing (Phase 8), Kubernetes manifests (Phase 7).

</domain>

<decisions>
## Implementation Decisions

### Test infrastructure
- **D-01:** Docker-compose for real PostgreSQL and Redis — infrastructure started separately (`docker-compose up -d`) before running tests, not managed in-process
- Real databases for integration and E2E tests; in-process mocks (miniredis, sqlmock) for unit tests
- Docker-compose sidecar pattern: familiar since compose already exists for local dev; separate test entrypoint (`docker-compose -f compose.yaml -f compose.test.yaml`) with isolated networks
- No testcontainers — keeps test runs deterministic and CI simple

### NF mock strategy
- **D-02:** NRF, UDM, AMF, AUSF mocked as in-process httptest servers in the test binary — HTTP/JSON mocks, no separate containers needed
- **D-03:** AAA-S (RADIUS/Diameter EAP simulator) gets its own dedicated container — real EAP-TLS stack, configurable auth results (Success/Failure/Challenge), can test actual TLS certificate chain validation
- AAA-S simulator: lightweight Go binary in `test/mocks/aaasim/` or a test helper package; configured via environment variables or config file in the test compose
- AMF mock also needs to receive re-auth/revocation HTTP POST callbacks from NSSAAF — httptest server handles this for unit/integration; E2E uses the dedicated AMF mock container

### Directory structure (D-04)
- **D-04:** Separate `test/` subdirectories — `test/unit/`, `test/integration/`, `test/e2e/`, `test/conformance/` — rather than co-located `*_test.go` alongside source
- Existing 31 co-located `*_test.go` remain in place; new test code goes into `test/`
- `test/mocks/` for NF mock helpers (NRF, UDM, AMF, AUSF httptest servers)
- `test/mocks/aaasim/` for the AAA-S simulator (dedicated container)

### NRM RESTCONF deployment (D-05)
- **D-05:** NRM RESTCONF server as a **standalone binary** — `cmd/nrm/`
- NOT embedded in Biz Pod, NOT a K8s sidecar — separate process with its own lifecycle
- Communicates with Biz Pod via internal HTTP callback for alarm state

### RESTCONF encoding (D-06)
- **D-06:** RESTCONF uses **JSON** encoding (RFC 8040 supports both JSON and XML)

### AAA-S Diameter library (D-09)
- **D-09:** `test/aaa_sim/diameter.go` uses **`github.com/fiorix/go-diameter/v4`** — same library as production `internal/diameter/`
- Rationale: RFC 6733-compliant CER/CEA handshake, DWR/DWA watchdog, and connection state management give E2E tests fidelity matching production behavior
- go-diameter/v4 is already in `go.mod` — no new dependency introduced
- Import path: `github.com/fiorix/go-diameter/v4/sm` for CER/CEA state machine; keep AVP building and EAP handling in manual code within `test/aaa_sim/`

### AAA-S SCTP support (D-10)
- **D-10:** `test/aaa_sim/` adds **both SCTP and TCP** transport support for Diameter
- `mode.go` adds `AAA_SIM_DIAMETER_TRANSPORT` env var: `tcp` (default) or `sctp`
- SCTP uses `net.Dial("sctp", ...)` / `net.Listen("sctp", ...)` — standard Go `net` package
- `test/aaa_sim/diameter.go` accepts `net.Listener` from caller, so transport is pluggable
- go-diameter/v4/sm supports SCTP natively via `sm.Listen` with SCTP listener

### AIW E2E test scope (D-08)
- **D-08:** AIW E2E tests at **two layers**:
  - **Biz Pod unit tests** with mock AAA client — fast feedback, validate Biz Pod logic in isolation
  - **3-component E2E tests** — verify HTTP GW routing, AAA GW transport, and MSK forwarding end-to-end via `StartAUSFMock()` httptest server + `mock-aaa-s` container
- Both layers use the AUSF mock from `test/mocks/ausf.go` (httptest server, matching D-02)
- Covers all AIW test cases from `docs/design/24_test_strategy.md` §5.3: MSK extraction, TTLS inner method, EAP failure, invalid SUPI, AAA not configured
- **PLAN-6 added** to cover the AIW 3-component E2E gap (test/e2e/aiw_flow_test.go, 6 cases) and AIW conformance gap (test/conformance/aiw_conformance_test.go, 13 cases)

### Harness Docker Compose command (D-11)
- **D-11:** Use `docker compose` (V2, `go-docker/docker/compose`) throughout — not `docker-compose` (V1, standalone binary)
- `harness.go`: Change `exec.CommandContext(ctx, "docker-compose", args...)` to `exec.CommandContext(ctx, "docker", "compose", args...)`
- `Makefile` already uses `docker compose` correctly (lines 234, 238, 242) — no change needed
- Verify `docker compose version` works; fallback skip if unavailable (existing pattern)

### Compose file layout (D-12)
- **D-12:** Single `compose/dev.yaml` for all test types — no separate `test.yaml`, no `biz-e2e.yaml`, no `http-gateway-e2e.yaml`, no `aaa-gateway-e2e.yaml`
- Rationale: Single source of truth for infrastructure topology. Test-specific overrides handled via env vars passed to binary processes, not via compose overlays.
- Remove: `compose/test.yaml`, `compose/configs/biz-e2e.yaml`, `compose/configs/http-gateway-e2e.yaml`, `compose/configs/aaa-gateway-e2e.yaml`
- Keep: `compose/dev.yaml` (infra + component containers), `compose/configs/` (dev config files used at runtime by binary processes via env)

### Test infrastructure ports (D-13)
- **D-13:** Integration and E2E tests use the same infrastructure ports as dev (Redis: 6379, PostgreSQL: 5432)
- No separate test ports (5433, 6380) — test DB/Redis are the same as dev DB/Redis
- Rationale: Single compose file means single port scheme. Test isolation is at the process level (binaries started by harness), not at the network level.

### Harness config externalization (D-14)
- **D-14:** All harness hardcoded values externalized as env vars — `getEnv(key, default)` pattern in `harness.go`
- Key env vars (all with defaults matching dev.yaml):
  - `TEST_BIZ_LISTEN` (default: `:8080`)
  - `TEST_HTTPGW_LISTEN` (default: `:8443`)
  - `TEST_AAAGW_LISTEN` (default: `:9090`)
  - `TEST_BIZ_PG_URL` (default: `postgres://nssaa:nssaa@localhost:5432/nssaa?sslmode=disable`)
  - `TEST_BIZ_REDIS_URL` (default: `redis://localhost:6379`)
  - `TEST_BIZ_AAA_GW_URL` (default: `http://localhost:9090`)
  - `TEST_BIZ_NRM_URL` (default: `http://localhost:8081`)
  - `TEST_HTTP_GW_BIZ_URL` (default: `http://localhost:8080`)
  - `TEST_AAAGW_RADIUS_PORT` (default: `1812`)
  - `TEST_AAAGW_DIAMETER_PORT` (default: `3868`)
  - `TEST_DOCKER_COMPOSE` (default: `-f compose/dev.yaml`)
  - `TEST_BINARY_DIR` (default: `.`)
- Env vars set by harness itself (not sourced from shell) via `os.Setenv` before `exec.CommandContext`
- Rationale: No extra config files needed. Easy to override per-machine or per-CI-run.

### Integration test Docker Compose infrastructure (D-15)
- **D-15:** Integration tests (`test/integration/`) use Docker Compose for real PostgreSQL and Redis — not in-process mocks
- `NewHarness()` is NOT used by integration tests (no binary processes needed)
- Instead: a lightweight `infra` package in `test/` with `InfraUp(ctx)`, `InfraDown(ctx)`, `WaitReady(ctx)` functions
- `InfraUp` runs `docker compose -f compose/dev.yaml up -d postgres redis mock-aaa-s`
- `InfraDown` runs `docker compose -f compose/dev.yaml down`
- Integration tests call `infra.InfraUp(t)` in `TestMain` (or per-test setup) and `infra.InfraDown(t)` in teardown
- DB migration: `internal/storage/postgres.Migrator` (already exists in `internal/storage/postgres/migrate.go`, used via `NewMigrator(pool).Migrate(ctx)`)
- Real Redis: use `github.com/redis/go-redis/v9` client, connect to `localhost:6379`

### Makefile test targets (D-16)
- **D-16:** Separate `make` targets per test layer — each manages its own infra lifecycle:

```
make test-unit          # go test ./test/unit/... — no infra
make test-integration   # docker compose up -d → go test ./test/integration/... → docker compose down
make test-e2e          # build binaries → docker compose up -d → go test ./test/e2e/... → docker compose down
make test-conformance   # go test ./test/conformance/... — no infra (httptest only)
make test-all          # test-unit + test-integration + test-e2e + test-conformance
```

- `test-integration` and `test-e2e` both run `docker compose -f compose/dev.yaml up -d` (same infra)
- `test-integration` does NOT start component binaries — just infra (postgres, redis, mock-aaa-s)
- `test-e2e` starts component binaries via harness
- Both tear down with `docker compose -f compose/dev.yaml down`
- DB migration handled inside `test/integration` setup (via `postgres.Migrator`)
- NRM binary: built separately, started by harness on `nrmURL` (port 8081)

### Binary management in harness (D-17)
- **D-17:** Harness builds binaries on first run, reuses on subsequent runs — existing behavior preserved
- `buildBinaries()` checks `os.Stat(bin)`; only runs `go build` if binary is missing
- Users can force rebuild by deleting the binary or setting `FORCE_REBUILD=1` env var
- Harness logs: "reusing existing binary" vs "building binary"

### Claude's Discretion
- Alarm severity thresholds and deduplication policy
- NRM startup timing (wait for RESTCONF ready before running E2E tests)

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Testing
- `docs/design/24_test_strategy.md` — Test pyramid, unit/integration/E2E patterns, conformance test specs, k6 load test (read for structure; load testing is Phase 8)
  - **§5 E2E Tests** — 3-component E2E flows (NSSAA, re-auth, revocation), AIW E2E flows with AUSF mock (MSK extraction, TTLS, EAP failure, invalid SUPI, AAA not configured)
  - **§6 Conformance Tests** — TS 29.526 §7.2 (~30 cases), RFC 3579 (~10 cases), RFC 5216 MSK (~10 cases)
- `docs/design/06_eap_engine.md` — EAP engine internals for unit test coverage
- `docs/design/07_radius_client.md` — RADIUS encoding for RFC 3579 conformance tests
- `docs/design/08_diameter_client.md` — Diameter encoding for protocol conformance

### NRM/FCAPS
- `docs/design/18_nrm_fcaps.md` — YANG model (3gpp-nssaaf-nrm), alarm types, RESTCONF API, FCAPS implementation patterns

### Existing Code
- `test/e2e/harness.go` — E2E harness (CURRENT: uses `docker-compose` V1, hardcoded env vars, `compose/dev.yaml` + `compose/test.yaml`)
- `compose/dev.yaml` — Infrastructure compose (postgres, redis, mock-aaa-s, biz, http-gw, aaa-gw containers)
- `compose/test.yaml` — **REMOVE** — test port overrides (postgres_test:5433, redis_test:6380)
- `compose/configs/biz-e2e.yaml` — **REMOVE** — hardcoded test ports (to be replaced by env vars)
- `compose/configs/http-gateway-e2e.yaml` — **REMOVE**
- `compose/configs/aaa-gateway-e2e.yaml` — **REMOVE**
- `cmd/nrm/main.go` — NRM standalone binary (to be started by harness)
- `internal/storage/postgres/migrate.go` — `Migrator` struct with `Migrate(ctx)` method; used by integration test setup
- `internal/eap/engine_test.go` — Existing test patterns: mockAAAClient, testify assertions, in-process mocks
- `internal/api/aiw/handler_test.go` — API handler test patterns: mockStore, httptest, doRequest helper
- `cmd/biz/main_test.go` — Main test patterns: httptest server, JSON request helpers
- `compose/configs/biz.yaml` — Dev config for Biz Pod binary
- `Makefile` — Build targets (lines 232-243: `compose-up`, `compose-down`, `compose-logs` using `docker compose` V2)
- `go.mod` — Existing test dependencies: testify, miniredis/v2

### 3GPP Specifications
- TS 29.526 §7.2 — N58 API operations and error codes
- TS 28.541 §5.3.145-148 — NSSAAFFunction IOC, NRM attributes
- TS 29.561 Ch.17 — Diameter transport (TCP/SCTP)
- RFC 3579 — RADIUS EAP extension
- RFC 5216 — EAP-TLS MSK derivation
- RFC 6733 — Diameter Base Protocol (CER/CEA, DWR/DWA)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/eap/engine_test.go` — mockAAAClient with configurable delay, failure injection, response codes — reuse pattern for other mock clients
- `internal/api/aiw/handler_test.go` — mockStore implementing interface, doRequest helper, makeRouter helper — reuse for nssaa handler tests
- `go.mod` has `testify` and `miniredis/v2` already — no new deps needed for unit and Redis tests
- `test/` directory already has `e2e/` and `integration/` subdirectories — populate these

### Established Patterns
- `testify/assert` + `testify/require` for all existing tests
- In-process mocks embedded in test files (not separate _mock.go files)
- `httptest.NewRecorder` + `httptest.NewRequest` for HTTP handler tests
- `testing.Short()` skip pattern for long-running tests in `internal/eap/engine_test.go`

### Integration Points
- `test/` directory: organize into `test/unit/`, `test/integration/`, `test/e2e/`, `test/conformance/` (or use `*_test.go` co-located with source)
- `compose/dev.yaml` — Single compose file for both integration and E2E infra (updated per D-12)
- `internal/nrm/` — RESTCONF server and alarm manager (already exists in `cmd/nrm/`)
- All 21 existing `*_test.go` files already cover core paths — Phase 6 fills gaps and adds NRM
- `test/mocks/` directory: `test/mocks/aaasim/` for the AAA-S simulator (dedicated container); `test/mocks/` for NRF/UDM/AMF/AUSF httptest helpers

</code_context>

<deferred>
## Deferred Ideas

- Load testing (k6) — Phase 8
- Chaos testing — Phase 8
- Kubernetes manifests for NRM RESTCONF server — Phase 7

### Reviewed Todos (not folded)
None.

</deferred>

---

*Phase: 06-integration-testing-nrm*
*Context gathered: 2026-04-28 (initial + 2 supplemental)*
