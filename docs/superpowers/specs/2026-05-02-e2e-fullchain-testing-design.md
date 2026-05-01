# E2E Full Chain Testing Design

**Date:** 2026-05-02
**Project:** NSSAAF
**Status:** Approved

---

## 1. Overview

This design specifies the implementation of E2E full chain testing for NSSAAF, achieving telecom-grade validation by testing the complete flow from AMF/AUSF through HTTP Gateway, Biz Pod, AAA Gateway, to AAA-S (RADIUS/Diameter).

**Goals:**
- Verify end-to-end correctness of N58 (NSSAA) and N60 (AIW) interfaces
- Validate resilience scenarios (AAA timeout, circuit breaker, pod kill)
- Ensure spec compliance (TS 23.502, TS 29.526, RFC 3579, RFC 5216)
- Support AMF/AUSF mock initiation (not just notification reception)

**Non-Goals:**
- Load testing (Phase 8)
- Chaos engineering with real K8s (Phase 8)
- Performance benchmarking

---

## 2. Architecture

### 2.1 Component Topology

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           E2E Test Suite                                    │
│                         (test/e2e/fullchain/)                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌──────────────┐     ┌───────────────┐     ┌─────────────┐             │
│  │  AMF Mock   │────▶│  HTTP Gateway │────▶│  Biz Pod    │             │
│  │ (initiator) │     │   (real)      │     │   (real)    │             │
│  └──────────────┘     └───────────────┘     └──────┬──────┘             │
│                                                       │                    │
│  ┌──────────────┐                                    ▼                    │
│  │ AUSF Mock    │─────────────────────────────▶┌─────────────┐           │
│  │ (initiator)  │                              │ AAA Gateway │           │
│  └──────────────┘                              │   (real)   │           │
│                                                  └──────┬──────┘          │
│                                                         │                   │
│                                                         ▼                   │
│                                               ┌─────────────────┐           │
│                                               │  mock-aaa-s    │           │
│                                               │  (RADIUS/Dia)  │           │
│                                               └─────────────────┘           │
│                                                                             │
│  Supporting Infrastructure:                                                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                    │
│  │  PostgreSQL  │  │    Redis     │  │    NRF Mock  │                    │
│  └──────────────┘  └──────────────┘  └──────────────┘                    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Responsibilities

| Component | Role | Implementation |
|-----------|------|----------------|
| AMF Mock (initiator) | Initiates N58 calls, receives notifications | `test/e2e/fullchain/mocks/amf_initiator.go` |
| AUSF Mock (initiator) | Initiates N60 calls | `test/e2e/fullchain/mocks/ausf_initiator.go` |
| HTTP Gateway | TLS terminator, request routing | Real component (docker-compose) |
| Biz Pod | EAP engine, session management, NF integration | Real component with full wiring |
| AAA Gateway | RADIUS/Diameter encoding, active-standby | Real component |
| mock-aaa-s | EAP-TLS, EAP-TTLS simulation | Container-based (existing) |
| PostgreSQL | Session storage, monthly partitions | Real infrastructure |
| Redis | Session cache, rate limiting | Real infrastructure |

---

## 3. Directory Structure

```
test/e2e/
├── harness.go              # Existing - shared harness
├── harness.yaml           # Existing - config
├── e2e.go                 # Existing - TestMain
├── nssaa_flow_test.go     # Existing
├── aiw_flow_test.go       # Existing
├── reauth_test.go         # Existing
├── revocation_test.go      # Existing
│
└── fullchain/             # NEW
    ├── fullchain_test.go   # TestMain + scenarios
    ├── mocks/
    │   ├── amf_initiator.go   # AMF mock that initiates N58 calls
    │   └── ausf_initiator.go  # AUSF mock that initiates N60 calls
    ├── scenarios/
    │   ├── n58_scenarios.go   # NSSAA full chain scenarios
    │   ├── n60_scenarios.go   # AIW full chain scenarios
    │   └── resilience.go      # Resilience/failure scenarios
    └── harness_fullchain.go   # Extended harness for full chain
```

---

## 4. Test Scenarios

### 4.1 NSSAA Full Chain (N58 Interface)

| ID | Scenario | Spec | Expected Result |
|----|----------|------|----------------|
| TC-N58-01 | Happy Path | TS 23.502 §4.2.9 | 201, authCtxId, EAP message |
| TC-N58-02 | Multi-Round EAP | TS 23.502 §4.2.9.2 | authResult: EAP_SUCCESS |
| TC-N58-03 | EAP Failure | TS 29.526 §7.2.2 | 200, authResult: EAP_FAILURE |
| TC-N58-04 | AAA Timeout | TS 29.526 §7.2.2 | 504 Gateway Timeout |
| TC-N58-05 | Circuit Breaker | TS 29.526 | 502 after retries exhausted |
| TC-N58-06 | Re-Auth Trigger | TS 23.502 §4.2.9.3 | AMF receives notification |
| TC-N58-07 | Revocation | TS 23.502 §4.2.9.4 | AMF receives notification |
| TC-N58-08 | Invalid GPSI | TS 29.571 §5.2.2 | 400 Bad Request |
| TC-N58-09 | Invalid Snssai | TS 29.571 | 400 Bad Request (SST > 255) |
| TC-N58-10 | AAA Not Configured | TS 29.526 | 404 Not Found |

### 4.2 AIW Full Chain (N60 Interface)

| ID | Scenario | Spec | Expected Result |
|----|----------|------|----------------|
| TC-N60-01 | Happy Path | TS 29.526 §7.3 | 201, authCtxId, MSK returned |
| TC-N60-02 | MSK Verification | RFC 5216 §2.1.4 | MSK = 64 octets |
| TC-N60-03 | EAP Failure | TS 29.526 §7.3 | 200, authResult: EAP_FAILURE |
| TC-N60-04 | Invalid SUPI | TS 29.571 §5.4.4.61 | 400 Bad Request |
| TC-N60-05 | TTLS Flow | TS 33.501 §I.2.2.2 | MSK returned, pvsInfo present |
| TC-N60-06 | No Re-Auth | TS 29.526 AC8 | Re-Auth returns error |
| TC-N60-07 | No Revocation | TS 29.526 AC8 | Revocation returns error |

### 4.3 Resilience Scenarios

| ID | Scenario | Spec | Expected Result |
|----|----------|------|----------------|
| TC-RES-01 | Pod Kill | - | Session recoverable |
| TC-RES-02 | DB Failover | - | Operations continue |
| TC-RES-03 | Redis Down | - | Fallback to DB |
| TC-RES-04 | DLQ Processing | - | Notification eventually delivered |
| TC-RES-05 | Circuit Recovery | - | Circuit closes after timeout |

---

## 5. Data Flow

### 5.1 NSSAA Flow (TS 23.502 §4.2.9)

```
Step 1: AMF Mock initiates N58 call
┌─────────────┐    POST /slice-authentications    ┌─────────────┐
│  AMF Mock  │ ────────────────────────────────▶ │ HTTP Gateway│
│ (initiator)│    {gpsi, snssai, eapIdRsp}     │             │
└─────────────┘                                   └──────┬──────┘
                                                          │
Step 2: HTTP GW routes to Biz Pod                         ▼
                                                 ┌─────────────┐
                                                 │   Biz Pod   │
                                                 │  (validate, │
                                                 │  session,   │
                                                 │  forward)   │
                                                 └──────┬──────┘
                                                          │
Step 3: Biz Pod forwards to AAA Gateway                   ▼
                                                 ┌─────────────┐
                                                 │ AAA Gateway │
                                                 │  (RADIUS/  │
                                                 │  Diameter)  │
                                                 └──────┬──────┘
                                                          │
Step 4: AAA Gateway sends to AAA-S                       ▼
                                                 ┌─────────────┐
                                                 │ mock-aaa-s │
                                                 │  (EAP-TLS) │
                                                 └──────┬──────┘
                                                          │
Step 5: AAA-S responds with Access-Challenge
                              Access-Challenge ◀────────────
                       (eapMessage: TLS handshake)
                                                          │
Step 6-8: Multi-round EAP-TLS handshake
       (repeat steps 2-5 until handshake complete)

Step 9: Final response to AMF
┌─────────────┐    200 OK                      ┌─────────────┐
│  AMF Mock  │ ◀────────────────────────────── │ HTTP Gateway│
│             │    {authCtxId, authResult:     │             │
└─────────────┘     EAP_SUCCESS}               └─────────────┘
```

### 5.2 AIW Flow (TS 29.526 §7.3)

```
AUSF Mock  ──▶  HTTP GW  ──▶  Biz Pod  ──▶  AAA GW  ──▶  mock-aaa-s
                  ▲                                         │
                  │                                         │
              201 Created                              Access-
              {authCtxId, eapMessage}                 Challenge
                  │                                         │
                  └─────────────────────────────────────────┘
                           PUT /authentications/{id}
                           {eapMessage: response}
```

---

## 6. Error Handling

### 6.1 Error Scenarios

| Scenario | Trigger | Expected Behavior |
|----------|---------|-------------------|
| AAA Timeout | mock-aaa-s doesn't respond for 5s | 504 after retry exhaustion |
| AAA Unreachable | mock-aaa-s is down | 502 after circuit breaker |
| Invalid GPSI | GPSI doesn't match pattern | 400 with ProblemDetails |
| Session Not Found | PUT with unknown authCtxId | 404 Not Found |
| DB Down | PostgreSQL unavailable | 503 Service Unavailable |
| Redis Down | Redis unavailable | Fallback to PostgreSQL |

### 6.2 Error Response Format

All errors return ProblemDetails (RFC 7807):

```json
{
  "type": "https://nssAAF.operator.com/problem/invalid-gpsi",
  "title": "Bad Request",
  "status": 400,
  "detail": "GPSI 'invalid' does not match required pattern",
  "cause": "INVALID_GPSI_FORMAT"
}
```

### 6.3 Error Codes

| HTTP | Cause | Description |
|------|-------|-------------|
| 400 | INVALID_GPSI_FORMAT | GPSI doesn't match `^(msisdn-[0-9]{5,15}\|extid-[^@]+@[^@]+\|.+)$` |
| 400 | INVALID_SUPI_FORMAT | SUPI doesn't match `^imsi-[0-9]{15}$` |
| 400 | INVALID_SNSSAI | Snssai.sst > 255 or sd not 6 hex chars |
| 400 | BAD_REQUEST | Missing required fields |
| 403 | AAA_REJECTED | AAA-S rejected authentication |
| 404 | AAA_NOT_CONFIGURED | No AAA server for SUPI/GPSI range |
| 404 | SESSION_NOT_FOUND | authCtxId doesn't exist |
| 502 | BAD_GATEWAY | AAA-S unreachable |
| 503 | SERVICE_UNAVAILABLE | AAA-S temporarily unavailable |
| 504 | GATEWAY_TIMEOUT | AAA-S timeout |

---

## 7. Test Execution

### 7.1 Execution Modes

```bash
# Full E2E (requires docker-compose)
make test-e2e

# Full chain tests only (skips existing tests)
go test -tags=e2e -run TestFullChain ./test/e2e/fullchain/...

# Single scenario
go test -tags=e2e -run TestFullChain_N58_HappyPath ./test/e2e/fullchain/...

# With mock tracing
E2E_TRACE=1 go test -tags=e2e -run TestFullChain ./test/e2e/fullchain/...
```

### 7.2 CI/CD Integration

```yaml
# .github/workflows/e2e.yml
name: E2E Full Chain Tests
on:
  push:
    branches: [main]
  pull_request:

jobs:
  fullchain-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build binaries
        run: make build

      - name: Run E2E tests
        run: make test-e2e
        env:
          E2E_TLS_CA: ./certs/ca.crt

      - name: Upload logs on failure
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: e2e-logs
          path: test/e2e/logs/
```

---

## 8. Mock AAA-S Integration

### 8.1 mock-aaa-s Container

The existing `mock-aaa-s` container will be:
1. Added to `compose/dev.yaml`
2. Wired to AAA Gateway via `AAAConfig`
3. Configurable via environment variables

### 8.2 Configuration

```yaml
# compose/dev.yaml
services:
  mock-aaa-s:
    build:
      context: ./mock-aaa-s
    ports:
      - "1812:1812/udp"   # RADIUS
      - "3868:3868/tcp"   # Diameter
    environment:
      EAP_METHOD: EAP-TLS
      AUTH_RESULT: ACCEPT
      MSK_LENGTH: 64
      RESPONSE_DELAY_MS: 0
```

---

## 9. Acceptance Criteria

| ID | Criteria | Validation |
|----|----------|------------|
| AC-01 | All N58 scenarios pass | `go test -tags=e2e -run TestFullChain_N58 ./test/e2e/fullchain/...` |
| AC-02 | All N60 scenarios pass | `go test -tags=e2e -run TestFullChain_N60 ./test/e2e/fullchain/...` |
| AC-03 | Resilience scenarios pass | `go test -tags=e2e -run TestFullChain_Resilience ./test/e2e/fullchain/...` |
| AC-04 | Error codes match spec | All 4xx/5xx responses have ProblemDetails |
| AC-05 | Circuit breaker verified | TC-N58-05 passes |
| AC-06 | Re-auth flow verified | TC-N58-06 passes |
| AC-07 | Revocation flow verified | TC-N58-07 passes |

---

## 10. Implementation Plan

### Phase 1: Infrastructure
1. Create `test/e2e/fullchain/` directory structure
2. Add mock-aaa-s to docker-compose
3. Implement `amf_initiator.go` and `ausf_initiator.go`
4. Implement `harness_fullchain.go`

### Phase 2: NSSAA Scenarios
5. Implement TC-N58-01 through TC-N58-10
6. Validate against existing NSSAA flow tests

### Phase 3: AIW Scenarios
7. Implement TC-N60-01 through TC-N60-07
8. Validate against existing AIW flow tests

### Phase 4: Resilience Scenarios
9. Implement TC-RES-01 through TC-RES-05
10. Add circuit breaker and DLQ verification

---

*Design approved: 2026-05-02*
