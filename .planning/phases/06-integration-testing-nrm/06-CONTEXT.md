# Phase 6: Integration Testing & NRM - Context

**Gathered:** 2026-04-28 (initial) + 2026-04-28 (supplemental: E2E + conformance)
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

### AIW E2E test scope (D-08)
- **D-08:** AIW E2E tests at **two layers**:
  - **Biz Pod unit tests** with mock AAA client — fast feedback, validate Biz Pod logic in isolation
  - **3-component E2E tests** — verify HTTP GW routing, AAA GW transport, and MSK forwarding end-to-end via `StartAUSFMock()` httptest server + `mock-aaa-s` container
- Both layers use the AUSF mock from `test/mocks/ausf.go` (httptest server, matching D-02)
- Covers all AIW test cases from `docs/design/24_test_strategy.md` §5.3: MSK extraction, TTLS inner method, EAP failure, invalid SUPI, AAA not configured

### Claude's Discretion
- Alarm severity thresholds and deduplication policy
- Exact compose file structure for test isolation

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
- `internal/eap/engine_test.go` — Existing test patterns: mockAAAClient, testify assertions, in-process mocks
- `internal/api/aiw/handler_test.go` — API handler test patterns: mockStore, httptest, doRequest helper
- `cmd/biz/main_test.go` — Main test patterns: httptest server, JSON request helpers
- `compose/configs/biz.yaml` — Existing docker-compose config (reference for test compose)
- `go.mod` — Existing test dependencies: testify, miniredis/v2

### 3GPP Specifications
- TS 29.526 §7.2 — N58 API operations and error codes
- TS 28.541 §5.3.145-148 — NSSAAFFunction IOC, NRM attributes
- RFC 3579 — RADIUS EAP extension
- RFC 5216 — EAP-TLS MSK derivation

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
- `compose.yaml` + `compose/configs/` — test compose extends existing dev compose
- `internal/nrm/` does not exist yet — new package needed for RESTCONF server and alarm manager
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
*Context gathered: 2026-04-28*
