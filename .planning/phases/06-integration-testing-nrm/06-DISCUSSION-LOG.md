# Phase 6: Integration Testing & NRM - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-28
**Phase:** 06-integration-testing-nrm
**Areas discussed:** 3 (directory structure, NRM deployment model, RESTCONF encoding)

---

## Area 1: Test Directory Structure

|| Option | Description | Selected |
|--------|-------------|----------|
| Co-located `*_test.go` alongside source | Existing 31 test files use this pattern; consistent with current codebase | |
| Separate `test/{unit,integration,e2e,conformance}/` | Design doc proposes this; clearer separation of test types | ✓ |

**User's choice:** Separate `test/` subdirectories
**Notes:** E2E, integration, conformance, and unit test code all go into `test/`. NF mock helpers go into `test/mocks/`. Existing co-located tests remain in place.

---

## Area 2: NRM RESTCONF Server Deployment

|| Option | Description | Selected |
|--------|-------------|----------|
| Embedded in Biz Pod binary | Single binary, simpler ops | |
| Standalone binary (`cmd/nrm/`) | Separate process with own lifecycle; cleaner separation of concerns | ✓ |
| Separate K8s sidecar | Additional K8s complexity | |

**User's choice:** Standalone binary
**Notes:** NRM RESTCONF server as `cmd/nrm/`, separate from Biz Pod. Communicates with Biz Pod via internal HTTP callback for alarm state.

---

## Area 3: RESTCONF Encoding

|| Option | Description | Selected |
|--------|-------------|----------|
| YAML | RFC 8040 supports it; closer to YANG source | |
| JSON | RFC 8040 supports both; more natural for Go/HTTP ecosystem | ✓ |

**User's choice:** JSON
**Notes:** RESTCONF uses JSON encoding per RFC 8040.

---

## Claude's Discretion

The following remain open for the planner to decide:
- Naming conventions for conformance test suites
- Alarm severity thresholds and deduplication policy
- Exact compose file structure for test isolation

## Deferred Ideas

None — all discussion stayed within phase scope.
