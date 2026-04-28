# Phase 6: Integration Testing & NRM - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-28
**Phase:** 06-integration-testing-nrm
**Areas discussed:** Test infrastructure, NF mock strategy

---

## Test Infrastructure

### Question
How should integration and E2E tests get PostgreSQL and Redis?

### Options Presented

| Option | Description | Selected |
|--------|-------------|----------|
| Pure mocks | sqlmock for DB, miniredis for Redis, gock for HTTP. Zero infra dependency, simplest CI. | |
| Testcontainers | Real PostgreSQL/Redis spun up in-process. Most realistic, but heavy and requires Docker in CI. | |
| Docker-compose sidecar | Infrastructure started separately (`docker-compose up -d`). Real databases. Familiar pattern. | ✓ |

### User's choice
C — Docker-compose for real PostgreSQL and Redis (infrastructure started separately, not managed in-process).

### Notes
- `miniredis` is already in go.mod — covers Redis mocking for unit tests
- Docker-compose sidecar: tests run against the same stack as local dev
- CI runs `docker-compose up -d` before integration/E2E test suite

---

## NF Mock Strategy

### Question
How should NRF, UDM, AMF, AUSF, and AAA-S be mocked for integration/E2E tests?

### Options Presented

| Option | Description | Selected |
|--------|-------------|----------|
| In-process (httptest) for everything | All NFs as httptest servers in test binary. Simplest CI. | |
| AAA-S simulator in dedicated container | Rest as httptest, AAA-S gets its own container with real EAP-TLS stack. More realistic for EAP path. | ✓ |

### User's choice
AAA-S simulator in dedicated container.

### Notes
- NRF, UDM, AMF, AUSF → in-process httptest servers in test binary
- AAA-S → dedicated container with Go-based EAP-TLS simulator (`test/mocks/aaasim/`)
- AMF mock receives re-auth/revocation POST callbacks — httptest for unit/integration, container for E2E
- AAA-S container can test actual TLS certificate chain validation end-to-end

---

## Claude's Discretion

The following areas were deferred to planning/research:

- Exact test directory structure (co-located `*_test.go` vs separate `test/` subdirectories)
- Naming conventions for conformance test suites
- RESTCONF/NRM server deployment model
- Alarm severity thresholds and deduplication policy
- RESTCONF encoding (YAML vs JSON)

## Deferred Ideas

- k6 load testing — belongs in Phase 8
- Chaos testing — belongs in Phase 8
- NRM RESTCONF Kubernetes manifests — belongs in Phase 7
