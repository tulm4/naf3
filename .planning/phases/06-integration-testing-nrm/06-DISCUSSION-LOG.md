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

**Date:** 2026-04-28 (supplemental)
**Phase:** 06-integration-testing-nrm
**Areas discussed:** 3 (directory structure, NRM deployment model, RESTCONF encoding) + 2 (AIW E2E scope, conformance naming) = 5 total

---

## Area 4: AIW E2E Test Scope

||| Option | Description | Selected |
||--------|-------------|----------|
|| Full 3-component flow | Verify HTTP GW routing, AAA GW transport, MSK forwarding end-to-end | |
|| Biz Pod only (mock AAA client) | Faster tests, validate Biz Pod logic in isolation | |
|| Both | Biz Pod unit tests + separate 3-component E2E tests | ✓ |

**User's choice:** Both
**Notes:** AIW E2E at two layers: (1) Biz Pod unit tests with mock AAA client for fast feedback; (2) 3-component E2E via `StartAUSFMock()` httptest server + `mock-aaa-s` container. Both layers use the AUSF mock from `test/mocks/ausf.go` (httptest server, matching D-02). Covers all AIW cases from `docs/design/24_test_strategy.md` §5.3: MSK extraction, TTLS inner method, EAP failure, invalid SUPI, AAA not configured.

---

## Area 5: Conformance Test Naming Convention

||| Option | Description | Selected |
||--------|-------------|----------|
|| Prefixed (TC-NSSAA-001, TC-RADIUS-001) | Spec reference in function name; matches research doc | |
|| Table-driven (one function per spec, subtests) | Compact; matches `engine_test.go` pattern | ✓ |
|| Hybrid — prefixed for conformance, table for unit | Mix approaches | |

**User's choice:** Table-driven
**Notes:** One function per spec (`TestTS29526`, `TestRFC3579`, `TestRFC5216`) with subtests named by case type (`valid_request`, `missing_gpsi`, etc.). This matches the existing `engine_test.go` pattern and keeps the test file compact. Spec section references added in comments per test case.

---

## Area 6: NSSAA E2E 2-Layer Coverage

||| Option | Description | Selected |
||--------|-------------|----------|
|| Already covered (both layers) | PLAN-3 Task 8 (Biz Pod unit) + PLAN-5 Tasks 2-4 (3-component E2E) | ✓ |

**User's choice:** Already covered
**Notes:** NSSAA has both layers. PLAN-3 Task 8: `test/unit/e2e_amf/amf_notification_test.go` (AMF notification unit tests, 5 cases). PLAN-5 Tasks 2-4: `test/e2e/nssaa_flow_test.go` (8 cases) + `test/e2e/reauth_test.go` (4 cases) + `test/e2e/revocation_test.go` (3 cases).

---

## Area 7: AIW E2E 2-Layer Coverage

||| Option | Description | Selected |
||--------|-------------|----------|
|| Add PLAN-6: AIW E2E (3-component) + AIW conformance | `test/e2e/aiw_flow_test.go` (6 cases) + `test/conformance/aiw_conformance_test.go` (13 cases) | ✓ |
|| Skip — conformance tests cover critical cases | PLAN-5 conformance tests sufficient | |
|| Skip — unit tests sufficient | PLAN-3 Task 6 Biz Pod unit tests sufficient | |

**User's choice:** Add PLAN-6: both AIW E2E and conformance
**Notes:** Two gaps found: (1) AIW 3-component E2E not covered in any plan — PLAN-5 Tasks 2-4 cover NSSAA E2E only. (2) AIW conformance (TS 29.526 §7.3) not covered in PLAN-5. PLAN-6 created with both: `test/e2e/aiw_flow_test.go` (6 cases: BasicFlow, MSKExtraction, EAPFailure, InvalidSupi, AAA_NotConfigured, TTLS) + `test/conformance/aiw_conformance_test.go` (13 cases TC-AIW-01 through TC-AIW-13). D-08 honored: both layers now covered for AIW.

---

## Claude's Discretion

The following remain open for the planner to decide:
- Alarm severity thresholds and deduplication policy
- Exact compose file structure for test isolation

## Deferred Ideas

None — all discussion stayed within phase scope.
