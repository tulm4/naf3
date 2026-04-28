# Phase 6: Integration Testing & NRM

## Overview

Phase 6 implements the test harness and conformance suites for the 3-component NSSAAF architecture, plus the NSSAAFFunction NRM (Network Resource Model) for management interface support.

**Spec Foundation:** TS 29.526 §7.2-7.3, TS 28.541 §5.3.145-148, RFC 3579, RFC 5216

---

## Modules Implemented

### Test Infrastructure

| Module | Plan | Files | Description |
|--------|------|-------|-------------|
| `test/e2e/` | PLAN-5 Wave 5 | harness.go, nssaa_flow_test.go, reauth_test.go, revocation_test.go, aiw_flow_test.go | E2E test harness and full-stack tests |
| `test/conformance/` | PLAN-5 Wave 5 | ts29526_test.go, rfc3579_test.go, rfc5216_test.go | TS 29.526, RFC 3579, RFC 5216 conformance suites |
| `internal/nrm/` | PLAN-2 Wave 2 | alarm.go, server.go, nrm.go | NRM store, alarm manager, RESTCONF server |
| `cmd/nrm/` | PLAN-2 Wave 2 | main.go | Standalone NRM binary |

### Test Case Summary

| Suite | File | Cases | Description |
|-------|------|-------|-------------|
| NSSAA E2E | test/e2e/nssaa_flow_test.go | 8 | AMF→HTTP GW→Biz→AAA GW→AAA-S full flow |
| Re-Auth E2E | test/e2e/reauth_test.go | 4 | AAA-S→RAR→BizPod→AMF notification |
| Revocation E2E | test/e2e/revocation_test.go | 3 | AAA-S→DR→BizPod→AMF notification |
| AIW E2E | test/e2e/aiw_flow_test.go | 6 | AUSF→HTTP GW→Biz→AAA GW→AAA-S full flow |
| TS 29.526 NSSAA | test/conformance/ts29526_test.go | 32 | §7.2 Create/Confirm/Get |
| TS 29.526 AIW | test/conformance/ts29526_test.go | 13 | §7.3 AuthFlow/MSK/PVSInfo |
| RFC 3579 | test/conformance/rfc3579_test.go | 10 | RADIUS EAP conformance |
| RFC 5216 | test/conformance/rfc5216_test.go | 10 | MSK derivation (64 octets) |
| **Total** | | **86** | |

---

## Validation Checklist

### Build Verification

- [ ] `go build ./test/e2e/...` compiles without error
- [ ] `go build ./test/conformance/...` compiles without error
- [ ] `go build ./cmd/nrm/...` compiles without error
- [ ] `go build ./...` compiles without error (full project)

### Test Execution

- [ ] `go test ./test/e2e/... -count=1 -short` skips E2E tests (requires full stack)
- [ ] `go test ./test/conformance/... -count=1 -short` passes (all 65 cases)
- [ ] `go test ./test/... -count=1 -short` passes (unit + conformance)

### E2E Test Cases (PLAN-5 Wave 5)

#### NSSAA E2E (test/e2e/nssaa_flow_test.go)

- [ ] TestE2E_NSSAA_HappyPath — 201, Location header, X-Request-ID echoed
- [ ] TestE2E_NSSAA_AuthFailure — EAP-Failure returns 200 OK (not 403)
- [ ] TestE2E_NSSAA_AuthChallenge — Multi-step handshake completes
- [ ] TestE2E_NSSAA_InvalidGPSI — 400 with ProblemDetails
- [ ] TestE2E_NSSAA_InvalidSnssai — 400 for SST out of range, invalid SD
- [ ] TestE2E_NSSAA_Unauthorized — 401 without Authorization header
- [ ] TestE2E_NSSAA_AaaServerDown — 502 when AAA-S unreachable (CB trips)
- [ ] TestE2E_NSSAA_CircuitBreakerAlarm — NRM alarm raised when CB opens

#### Re-Auth E2E (test/e2e/reauth_test.go)

- [ ] TestE2E_ReAuth_HappyPath — RAR → BizPod → AMF notification
- [ ] TestE2E_ReAuth_AmfUnreachable — Notification goes to DLQ
- [ ] TestE2E_ReAuth_MultipleReAuth — Concurrent re-auth handled
- [ ] TestE2E_ReAuth_CircuitBreakerOpen — Graceful failure when CB open

#### Revocation E2E (test/e2e/revocation_test.go)

- [ ] TestE2E_Revocation_HappyPath — DR → BizPod → AMF notification
- [ ] TestE2E_Revocation_AmfUnreachable — Notification goes to DLQ
- [ ] TestE2E_Revocation_ConcurrentRevocations — Multiple simultaneous revocations

#### AIW E2E (test/e2e/aiw_flow_test.go)

- [ ] TestE2E_AIW_BasicFlow — 201, Location header, X-Request-ID
- [ ] TestE2E_AIW_MSKExtraction — MSK 64 octets, MSK != EMSK
- [ ] TestE2E_AIW_EAPFailure — 200 OK with authResult=EAP_FAILURE in body
- [ ] TestE2E_AIW_InvalidSupi — 400 with ProblemDetails
- [ ] TestE2E_AIW_AAA_NotConfigured — 404 when no AAA server configured
- [ ] TestE2E_AIW_TTLS — EAP-TTLS with inner PAP, PVSInfo returned

### Conformance Test Cases (PLAN-5 Wave 5)

#### TS 29.526 NSSAA §7.2 (test/conformance/ts29526_test.go)

- [ ] TC-NSSAA-001: Valid request → 201, Location, X-Request-ID
- [ ] TC-NSSAA-002: Missing GPSI → 400
- [ ] TC-NSSAA-003: Invalid GPSI format → 400
- [ ] TC-NSSAA-004: Missing snssai → 400
- [ ] TC-NSSAA-005: snssai.sst out of range → 400
- [ ] TC-NSSAA-006: snssai.sd invalid hex → 400
- [ ] TC-NSSAA-007: Missing eapIdRsp → 400
- [ ] TC-NSSAA-008: Empty eapIdRsp → 400
- [ ] TC-NSSAA-009: Invalid base64 in eapIdRsp → gap (not validated at API layer)
- [ ] TC-NSSAA-010: AAA not configured → 404
- [ ] TC-NSSAA-011: Invalid JSON → 400
- [ ] TC-NSSAA-012: Missing Authorization → 401 (gateway-level)
- [ ] TC-NSSAA-013: Invalid Authorization → 401 (gateway-level)
- [ ] TC-NSSAA-014: No AMF ID → 201 with warning
- [ ] TC-NSSAA-020: Valid confirm → 200
- [ ] TC-NSSAA-021: Session not found → 404
- [ ] TC-NSSAA-022: GPSI mismatch → 400
- [ ] TC-NSSAA-023: Snssai mismatch → gap (not validated at API layer)
- [ ] TC-NSSAA-024: Missing eapMessage → 400
- [ ] TC-NSSAA-025: Invalid base64 in eapMessage → gap (not validated at API layer)
- [ ] TC-NSSAA-026: Session already completed → 409
- [ ] TC-NSSAA-027: Invalid authCtxId format → 404
- [ ] TC-NSSAA-028: Redis unavailable → 503
- [ ] TC-NSSAA-029: AAA GW unreachable → 502
- [ ] TC-NSSAA-030: Session exists → 200
- [ ] TC-NSSAA-031: Session not found → 404
- [ ] TC-NSSAA-032: Session expired → 404

#### TS 29.526 AIW §7.3 (test/conformance/ts29526_test.go)

- [ ] TC-AIW-01: BasicAuthFlow — valid SUPI → 201
- [ ] TC-AIW-02: MSKReturnedOnSuccess — 200 with 64-octet MSK
- [ ] TC-AIW-03: PVSInfoReturned — PvsInfo array in response
- [ ] TC-AIW-04: EAPFailureInBody — 200 with authResult=EAP_FAILURE
- [ ] TC-AIW-05: InvalidSupiRejected → 400
- [ ] TC-AIW-06: AAA_NotConfigured → 404
- [ ] TC-AIW-07: MultiRoundChallenge — multi-step handshake → final authResult
- [ ] TC-AIW-08: SupportedFeaturesEcho — echoed in response
- [ ] TC-AIW-09: TTLSInnerMethodContainer — echoed in response
- [ ] TC-AIW-10: MSKLength64Octets — exactly 64 bytes
- [ ] TC-AIW-11: MSKNotEqualEMSK — MSK[:32] != MSK[32:]
- [ ] TC-AIW-12: NoReauthSupport — AIW N60 does not support SLICE_RE_AUTH
- [ ] TC-AIW-13: NoRevocationSupport — AIW N60 does not support SLICE_REVOCATION

#### RFC 3579 (test/conformance/rfc3579_test.go)

- [ ] TC-RADIUS-001: EAP-Message attribute present in Access-Request
- [ ] TC-RADIUS-002: Message-Authenticator HMAC-MD5 over entire packet
- [ ] TC-RADIUS-003: EAP-Message fragmentation (>253 bytes split)
- [ ] TC-RADIUS-004: EAP-Message reassembly at receiver
- [ ] TC-RADIUS-005: Message-Authenticator in Access-Challenge
- [ ] TC-RADIUS-006: Message-Authenticator in Access-Accept
- [ ] TC-RADIUS-007: Message-Authenticator in Access-Reject
- [ ] TC-RADIUS-008: Invalid Message-Authenticator → packet dropped
- [ ] TC-RADIUS-009: Proxy-State attribute preserved end-to-end
- [ ] TC-RADIUS-010: User-Name attribute UTF-8 encoding

#### RFC 5216 (test/conformance/rfc5216_test.go)

- [ ] TC-EAPTLS-001: MSK length is exactly 64 bytes
- [ ] TC-EAPTLS-002: MSK = first 32 bytes of TLS key material
- [ ] TC-EAPTLS-003: EMSK = last 32 bytes
- [ ] TC-EAPTLS-004: MSK and EMSK are different
- [ ] TC-EAPTLS-005: Empty TLS session → error
- [ ] TC-EAPTLS-006: Insufficient key material (<64 bytes) → error
- [ ] TC-EAPTLS-007: Key export label is "EAP-TLS MSK"
- [ ] TC-EAPTLS-008: Session ID included in derivation context
- [ ] TC-EAPTLS-009: Server handshake_messages included in derivation
- [ ] TC-EAPTLS-010: Peer certificate used in derivation when available

### NRM (PLAN-2 Wave 2)

- [ ] `cmd/nrm/main.go` starts and listens on configured port
- [ ] `GET /restconf/data/ietf-yang-library:modules-state` returns 200
- [ ] `GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function` returns NSSAAFFunction IOC
- [ ] `GET /restconf/data/3gpp-nssaaf-nrm:alarms` returns alarm list
- [ ] `POST /restconf/data/3gpp-nssaaf-nrm:alarms` creates alarm with dedup
- [ ] Alarm dedup: same (AlarmType, BackupObject) within 5 min → same alarm ID
- [ ] Alarm threshold: failure rate >10% → alarm raised
- [ ] Circuit breaker open → alarm raised (REQ-34)

### Requirement Traceability

| REQ | Description | Test Coverage |
|-----|-------------|--------------|
| REQ-26 | Coverage report >80% overall | Unit + conformance tests |
| REQ-27 | All API endpoints have integration tests | test/e2e/, test/conformance/ |
| REQ-28 | E2E tests verify AMF→HTTP GW→Biz→AAA GW→AAA-S (NSSAA) and AUSF→HTTP GW→Biz→AAA GW→AAA-S (AIW) | TestE2E_NSSAA_HappyPath, TestE2E_AIW_BasicFlow |
| REQ-29 | ~45 TS 29.526 conformance test cases (32 NSSAA + 13 AIW) | ts29526_test.go |
| REQ-30 | ~10 RFC 3579 test cases | rfc3579_test.go |
| REQ-31 | ~10 RFC 5216 test cases (MSK 64 octets, MSK != EMSK) | rfc5216_test.go |
| REQ-32 | NSSAAFFunction IOC readable via RESTCONF | NRM RESTCONF GET tests |
| REQ-33 | Alarm raised when auth failure rate >10% | Alarm threshold tests |
| REQ-34 | Alarm raised when circuit breaker opens | TestE2E_NSSAA_CircuitBreakerAlarm |

---

## Success Criteria

1. **All 86 test cases exist** across E2E and conformance suites
2. **`go build ./test/e2e/...`** and **`go build ./test/conformance/...`** compile without errors
3. **`go test ./test/conformance/... -short`** passes (all 65 conformance cases)
4. **`go test ./test/e2e/... -short`** skips E2E tests gracefully
5. **E2E tests** verify both NSSAA (AMF→...→AAA-S) and AIW (AUSF→...→AAA-S) flows
6. **NRM RESTCONF** returns valid NSSAAFFunction IOC and alarm list
7. **Conformance gaps documented** (base64 validation, S-NSSAI mismatch, GetSlice handler)
8. **All REQs verified** via test cases

---

## Gaps Identified

The following gaps were found during test implementation and are documented for future work:

| Gap | Description | Priority |
|-----|-------------|---------|
| G-01 | Base64 validation not enforced at NSSAA handler API layer (TC-NSSAA-009, TC-NSSAA-025) | P1 |
| G-02 | S-NSSAI mismatch not validated at Confirm API layer (TC-NSSAA-023) | P1 |
| G-03 | GetSliceAuthenticationContext not implemented in handler (TC-NSSAA-030/031/032) | P1 |
| G-04 | Authorization header validation is at HTTP Gateway level, not Biz Pod handler | P0 (by design) |
| G-05 | AAA routing/AAA-not-configured check not at handler create stage (AIW) | P2 |

---

## Next Phase

Phase 7: Kubernetes Deployment — Helm charts, Kustomize overlays, ArgoCD application, HPA/PDB configs
