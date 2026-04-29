# Phase 6: Integration Testing & NRM - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-30
**Phase:** 06-integration-testing-nrm
**Areas discussed:** compose_layout, config_approach, integration_infra, makefile_targets, binary_mgmt

---

## compose_layout

| Option | Description | Selected |
|--------|-------------|----------|
| Single `dev.yaml` + env overrides | Use only `compose/dev.yaml`. Integration tests use standard ports (5432, 6379). E2E tests use env vars to point binaries to test services. No extra compose files. | ✓ |
| `dev.yaml` + `test.yaml` overlay | Keep current overlay pattern — base infra in dev, test-specific overrides in test.yaml. Explicit isolation. | |
| `dev.yaml` (infra) + `e2e.yaml` (full stack) | Split: dev.yaml has redis/pg/mock-aaa-s only, e2e.yaml extends with binary containers. Integration uses dev.yaml, E2E uses e2e.yaml. | |

**User's choice:** Single `dev.yaml` + env overrides
**Notes:** Simpler, no redundant files. Test isolation at the process level (binaries started by harness), not at the network level.

---

## config_approach

| Option | Description | Selected |
|--------|-------------|----------|
| Env vars only | `getEnv()` reads from environment. Default values hardcoded in Go. Simple, no extra files. | ✓ |
| YAML config files | Keep `compose/configs/biz-e2e.yaml`, harness reads it and translates to env vars. Good for complex nested config. | |
| Env vars with YAML fallback | Env vars take priority; if missing, fallback to values from a test config file. Most flexible. | |

**User's choice:** Env vars only
**Notes:** Simple, no extra files to manage. Harness uses `getEnv(key, default)` pattern. Env vars set via `os.Setenv` before `exec.CommandContext`.

---

## integration_infra

| Option | Description | Selected |
|--------|-------------|----------|
| Docker Compose infra for integration | `make test-integration` starts redis/pg via `docker compose up -d`, tests connect to real services, `docker compose down` after. Slower but tests real behavior. | ✓ |
| Keep in-process mocks (sqlmock + miniredis) | Integration tests stay as they are — in-process mocks. Fast feedback. Real DB/Redis tested in E2E only. | |

**User's choice:** Docker Compose infra for integration
**Notes:** User explicitly requested "dùng chung real infrastructure (redis, postgres)" — real Redis and PostgreSQL for integration tests. This is a change from prior context (D-01 said integration uses in-process mocks). New direction: integration tests use Docker Compose infra, E2E tests use Docker Compose + binary processes.

---

## makefile_targets

| Option | Description | Selected |
|--------|-------------|----------|
| `test-all` — single command | One `make test-all` that starts infra, runs migrate, runs all tests, tears down. Clean for CI. | |
| Separate targets | `test-unit`, `test-integration`, `test-e2e`, `test-conformance` — each manages own lifecycle. More granular. | ✓ |
| Separate lifecycle from test | `infra-up`, `infra-down`, then `test-integration`, `test-e2e` against running infra. Most control but most steps. | |

**User's choice:** Separate targets
**Notes:** Each test layer (unit, integration, e2e, conformance) gets its own `make` target that manages its own infra lifecycle. `test-integration` and `test-e2e` both use `compose/dev.yaml` but with different harness (infra-only vs full stack). `test-all` runs all four in sequence.

---

## binary_mgmt

| Option | Description | Selected |
|--------|-------------|----------|
| Build on first run, reuse later | Binary built once, reused on subsequent runs. Fast. User runs `make build` or harness auto-builds if missing. | ✓ |
| Always rebuild | Harness always calls `go build` before starting. Tests latest code every time. Slower. | |
| External build | `make build` builds all binaries first, harness just runs them. No build logic in harness. | |

**User's choice:** Build on first run, reuse later
**Notes:** Existing behavior preserved. `FORCE_REBUILD=1` env var for forced rebuild. Harness logs whether it reused or built.

---

## Claude's Discretion

- NRM startup timing — harness waits for NRM RESTCONF ready before E2E tests start (already in existing `waitHealthy` function)
- Alarm severity thresholds and deduplication policy (from prior context)

## Deferred Ideas

None — discussion stayed within phase scope.
