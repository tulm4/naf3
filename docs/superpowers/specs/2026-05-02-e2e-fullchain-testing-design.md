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
- Ensure spec compliance (TS 23.502, TS 29.526, RFC 3579, RFC 5216, RFC 6733)
- Support AMF/AUSF mock initiation (not just notification reception)
- Validate UDM integration (Nudm_UECM_Get, Nudm_UECM_UpdateAuthContext)
- Validate NRF integration (service discovery, registration)
- Support both TCP and SCTP for Diameter transport

**Non-Goals:**
- Load testing (Phase 8)
- Chaos engineering with real K8s (Phase 8)
- Performance benchmarking

---

## 2. Architecture

### 2.1 Component Topology

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│                           E2E Test Suite                                        │
│                         (test/e2e/fullchain/)                                  │
├──────────────────────────────────────────────────────────────────────────────────┤
│                                                                                │
│  ┌──────────────┐      ┌───────────────┐      ┌─────────────┐                │
│  │  AMF Mock   │─────▶│  HTTP Gateway │─────▶│  Biz Pod    │                │
│  │ (initiator) │      │   (real)      │      │   (real)    │                │
│  └──────────────┘      └───────────────┘      └──────┬──────┘                │
│                                                        │                       │
│  ┌──────────────┐                                     ▼                       │
│  │ AUSF Mock    │────────────────────────────▶ ┌─────────────┐                │
│  │ (initiator)  │                               │ AAA Gateway │                │
│  └──────────────┘                               │   (real)   │                │
│                                                  └──────┬──────┘               │
│                                                        │                       │
│  ┌──────────────┐                                     ▼                       │
│  │  UDM Mock   │ ◀── Nudm_UECM_Get              ┌─────────────────┐        │
│  │              │    Nudm_UECM_UpdateAuthContext │  test/aaa_sim   │        │
│  └──────────────┘                                 │  (RADIUS/Dia)  │        │
│                                                  └────────┬────────┘        │
│                                                             │                 │
│  ┌──────────────┐                                        ▼                 │
│  │  NRF Mock   │ ◀── Nnrf_NFDiscovery           ┌─────────────────┐        │
│  │              │    Nnrf_NFRegistration         │  test/mocks/    │        │
│  └──────────────┘                                 │  (NRF, UDM,...) │        │
│                                                  └─────────────────┘        │
│  Supporting Infrastructure:                                                   │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                      │
│  │  PostgreSQL  │  │    Redis     │  │   NRF Mock  │                      │
│  └──────────────┘  └──────────────┘  └──────────────┘                      │
│                                                                                │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Responsibilities

| Component | Role | Implementation |
|-----------|------|----------------|
| AMF Mock (initiator) | Initiates N58 calls, receives notifications | `test/e2e/fullchain/mocks/amf_initiator.go` |
| AUSF Mock (initiator) | Initiates N60 calls | `test/e2e/fullchain/mocks/ausf_initiator.go` |
| UDM Mock | Nudm_UECM_Get returns auth subscription, AAA server config | `test/mocks/udm.go` (extend) |
| NRF Mock | Service discovery (UDM, AUSF), NF registration, heartbeat | `test/mocks/nrf.go` (extend) |
| HTTP Gateway | TLS terminator, request routing | Real component (docker-compose) |
| Biz Pod | EAP engine, session management, NF integration | Real component with full wiring |
| AAA Gateway | RADIUS/Diameter encoding, active-standby, CER/CEA, watchdog | Real component |
| mock-aaa-s | RADIUS EAP (UDP), Diameter EAP (TCP) with CER/CEA handshake | `test/aaa_sim/` |
| PostgreSQL | Session storage, monthly partitions | Real infrastructure |
| Redis | Session cache, rate limiting | Real infrastructure |

### 2.3 Diameter Transport Requirements

Diameter requires full RFC 6733 compliance for E2E testing:

| Feature | Requirement | Spec Reference |
|---------|-------------|----------------|
| CER/CEA Handshake | Mandatory before any DER/DEA | RFC 6733 §5.3 |
| Watchdog (DWR/DWA) | Mandatory for connection health | RFC 6733 §5.5 |
| SCTP Support | Required for production-like testing | RFC 6733 §3 (future) |
| TLS on Diameter | Optional, configurable | RFC 6733 §2.4 (future) |

**Why TCP/SCTP matters:** UDP-based testing misses real-world failure modes:
- Connection failures that SCTP handles gracefully
- Message ordering and fragmentation
- Path MTU discovery
- Multi-stream benefits in SCTP

### 2.4 NRF Integration Requirements

The NSSAA flow requires NRF for service discovery:

| API | Purpose | Test Coverage |
|-----|---------|---------------|
| Nnrf_NFDiscovery | Discover UDM/AUSF by service name | Correct endpoint returned |
| Nnrf_NFRegistration | Register NSSAAF with NRF | Heartbeat timing |
| Nnrf_NFHeartBeat | Maintain registration | TTL verification |

**NRF Mock must support:**
- Configurable service endpoint URLs (not hardcoded 127.0.0.1)
- Discovery by service name (nudm-uem, nausf-auth)
- Error injection (NRF unavailable, etc.)

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
    │   ├── nrf_scenarios.go   # NRF integration scenarios
    │   └── resilience.go       # Resilience/failure scenarios
    └── harness_fullchain.go   # Extended harness for full chain

test/aaa_sim/              # Existing - AAA-S simulator
├── diameter.go            # CER/CEA handshake, DER/DEA handling (RFC 6733)
├── radius.go              # RADIUS EAP handling (RFC 3579)
├── mode.go                # Mode configuration
└── aaa_sim_test.go       # Unit tests

test/mocks/
├── udm.go                 # Existing - extend for auth subscription
├── nrf.go                 # Existing - extend for service endpoints
├── amf.go                 # Existing - notification receiver
├── ausf.go                # Existing - N60 API
└── compose.go             # Existing - docker compose integration
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

### 4.3 UDM Integration Scenarios

| ID | Scenario | Spec | Expected Result |
|----|----------|------|----------------|
| TC-UDM-01 | UDM Returns Auth Subscription | TS 23.502 §4.2.9.2 | Correct AAA server selected |
| TC-UDM-02 | UDM Subscriber Not Found | TS 29.526 | 404 from NSSAAF |
| TC-UDM-03 | UDM Update After Auth | TS 29.526 §7.3.3 | Nudm_UECM_UpdateAuthContext called |
| TC-UDM-04 | UDM Timeout | TS 29.526 | 504 Gateway Timeout |

### 4.4 NRF Integration Scenarios

| ID | Scenario | Spec | Expected Result |
|----|----------|------|----------------|
| TC-NRF-01 | UDM Discovery | TS 29.510 §6.2.6 | Correct endpoint URL returned |
| TC-NRF-02 | AUSF Discovery | TS 29.510 §6.2.6 | Correct endpoint URL returned |
| TC-NRF-03 | NRF Unavailable | TS 29.510 | NSSAAF operates in degraded mode |
| TC-NRF-04 | Discovery Cache | TS 29.510 | Cached for 5 min TTL |
| TC-NRF-05 | UDM Not Registered | TS 29.510 | 404 from NSSAAF |

### 4.5 Diameter Transport Scenarios

| ID | Scenario | Spec | Expected Result |
|----|----------|------|----------------|
| TC-DIA-01 | CER/CEA Handshake | RFC 6733 §5.3 | Connection established |
| TC-DIA-02 | DWR/DWA Watchdog | RFC 6733 §5.5 | Connection health maintained |
| TC-DIA-03 | DER/DEA EAP | RFC 4072 | EAP-Success in DEA |
| TC-DIA-04 | TCP Transport | RFC 6733 §3 | Connection working |
| TC-DIA-05 | Connection Failure | RFC 6733 | Reconnection attempted |
| TC-DIA-06 | SCTP Transport | RFC 6733 §3 | Multi-stream working (future) |

### 4.6 Resilience Scenarios

| ID | Scenario | Spec | Expected Result |
|----|----------|------|----------------|
| TC-RES-01 | Pod Kill | - | Session recoverable |
| TC-RES-02 | DB Failover | - | Operations continue |
| TC-RES-03 | Redis Down | - | Fallback to DB |
| TC-RES-04 | DLQ Processing | - | Notification eventually delivered |
| TC-RES-05 | Circuit Recovery | - | Circuit closes after timeout |

### 4.7 AAA Simulator Selection

**Use `test/aaa_sim/` (package-based) — NOT `compose/mock_aaa_s.go`**

| Feature | `test/aaa_sim/` | `compose/mock_aaa_s.go` |
|---------|-----------------|-------------------------|
| RADIUS UDP | ✅ | ✅ |
| Diameter TCP | ✅ | ✅ |
| Diameter SCTP | ❌ (future) | ❌ |
| **CER/CEA Handshake (RFC 6733)** | ✅ `go-diameter/v4/sm` | ❌ Manual, non-compliant |
| **DWR/DWA Watchdog (RFC 6733)** | ✅ `go-diameter/v4/sm` | ❌ |
| Configurable Mode (EAP-TLS/Failure/Challenge) | ✅ | ❌ |
| Message-Auth Validation (RFC 3579) | ✅ | ❌ |

**Implementation:** `test/aaa_sim/` uses `go-diameter/v4/sm` for RFC 6733-compliant state machine.

---

## 5. Data Flow

### 5.1 NSSAA Flow with UDM/NRF (TS 23.502 §4.2.9)

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
                                                 │  (validate) │
                                                 └──────┬──────┘
                                                          │
Step 3: Biz Pod queries NRF for UDM                       ▼
                                                 ┌─────────────┐
                                                 │  NRF Mock  │
                                                 │ Nnrf_NF    │
                                                 │ Discovery   │
                                                 └──────┬──────┘
                                                          │
Step 4: NRF returns UDM endpoint                        ▼
       (e.g., http://udm-mock:8080)            ┌─────────────┐
                                                 │   Biz Pod   │
                                                 │ (query UDM) │
                                                 └──────┬──────┘
                                                          │
Step 5: Biz Pod queries UDM for auth subscription          ▼
                                                 ┌─────────────┐
                                                 │  UDM Mock  │
                                                 │ Nudm_UECM_ │
                                                 │    Get     │
                                                 └──────┬──────┘
                                                          │
Step 6: UDM returns auth subscription                     ▼
       (AAA server URL, EAP method)              ┌─────────────┐
                                                 │   Biz Pod   │
                                                 │ (AAA route) │
                                                 └──────┬──────┘
                                                          │
Step 7: Biz Pod forwards to AAA Gateway                   ▼
                                                 ┌─────────────┐
                                                 │ AAA Gateway │
                                                 │  (RADIUS/  │
                                                 │  Diameter)  │
                                                 └──────┬──────┘
                                                          │
Step 8: AAA Gateway sends to AAA-S                       ▼
                                                 ┌─────────────┐
                                                 │ test/       │
                                                 │ aaa_sim    │
                                                 │  (EAP-TLS) │
                                                 └──────┬──────┘
                                                          │
Step 9: AAA-S responds with Access-Challenge
                              Access-Challenge ◀────────────
                       (eapMessage: TLS handshake)
                                                          │
Step 10-12: Multi-round EAP-TLS handshake
       (repeat steps 7-9 until handshake complete)

Step 13: Final response to AMF
┌─────────────┐    200 OK                      ┌─────────────┐
│  AMF Mock  │ ◀────────────────────────────── │ HTTP Gateway│
│             │    {authCtxId, authResult:     │             │
└─────────────┘     EAP_SUCCESS}               └─────────────┘

Step 14: Biz Pod updates UDM
                                                 ┌─────────────┐
                                                 │  UDM Mock  │
                                                 │ Nudm_UECM_ │
                                                 │ UpdateAuth  │
                                                 └─────────────┘
```

### 5.2 AIW Flow with UDM/NRF (TS 29.526 §7.3)

```
AUSF Mock  ──▶  HTTP GW  ──▶  Biz Pod  ──▶  NRF Mock  ──▶  Biz Pod
  (initiate)                         Nnrf_NFDiscovery
                        │                          │
                        │                          ▼
                        │                    (UDM endpoint)
                        │                          │
                        │                          ▼
                        │                    ┌─────────────┐
                        │                    │  UDM Mock  │
                        │                    │ Nudm_UECM_ │
                        │                    │    Get     │
                        │                    └──────┬──────┘
                        │                          │
                        │                          ▼
                        │                    (auth subscription)
                        │                          │
                        ▼                          ▼
                   ┌─────────────┐          ┌─────────────┐
                   │ HTTP GW    │          │ AAA Gateway │
                   │  (201)     │          └──────┬──────┘
                   └──────┬──────┘                 │
                          │                       ▼
                          │                 ┌─────────────┐
                          │                 │ test/       │
                          │                 │ aaa_sim    │
                          │                 └──────┬──────┘
                          │◀───────────────────────┘
                          │    Access-Challenge
                          ▼
                     (eapMessage)
```

---

## 6. Error Handling

### 6.1 Error Scenarios

| Scenario | Trigger | Expected Behavior |
|----------|---------|-------------------|
| AAA Timeout | mock-aaa-s doesn't respond for 5s | 504 after retry exhaustion |
| AAA Unreachable | mock-aaa-s is down | 502 after circuit breaker |
| UDM Timeout | UDM mock doesn't respond | 504 Gateway Timeout |
| UDM Not Found | GPSI/SUPI not in UDM | 404 Not Found |
| NRF Unavailable | NRF mock is down | NSSAAF operates in degraded mode |
| NRF Discovery Fail | No UDM registered | 404 from NSSAAF |
| Invalid GPSI | GPSI doesn't match pattern | 400 with ProblemDetails |
| Session Not Found | PUT with unknown authCtxId | 404 Not Found |
| DB Down | PostgreSQL unavailable | 503 Service Unavailable |
| Redis Down | Redis unavailable | Fallback to PostgreSQL |
| Diameter CER Fail | CER/CEA handshake fails | 502 Bad Gateway |
| Diameter Watchdog | Watchdog timeout | Connection reset |

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
| 400 | INVALID_GPSI_FORMAT | GPSI doesn't match pattern |
| 400 | INVALID_SUPI_FORMAT | SUPI doesn't match `^imsi-[0-9]{5,15}$` |
| 400 | INVALID_SNSSAI | Snssai.sst > 255 or sd not 6 hex chars |
| 400 | BAD_REQUEST | Missing required fields |
| 403 | AAA_REJECTED | AAA-S rejected authentication |
| 404 | UDM_NOT_FOUND | GPSI/SUPI not found in UDM |
| 404 | NRF_NOT_FOUND | UDM/AUSF not registered with NRF |
| 404 | AAA_NOT_CONFIGURED | No AAA server for SUPI/GPSI range |
| 404 | SESSION_NOT_FOUND | authCtxId doesn't exist |
| 502 | BAD_GATEWAY | AAA-S unreachable, CER/CEA fails |
| 503 | SERVICE_UNAVAILABLE | UDM/AAA-S/NRF temporarily unavailable |
| 504 | GATEWAY_TIMEOUT | UDM/AAA-S/NRF timeout |

---

## 7. Test Execution

### 7.1 Execution Modes

```bash
# Full E2E (requires docker-compose)
make test-e2e

# Full chain tests only (skips existing tests)
go test -tags=e2e -run TestFullChain ./test/e2e/fullchain/...

# NSSAA scenarios only
go test -tags=e2e -run TestFullChain_N58 ./test/e2e/fullchain/...

# AIW scenarios only
go test -tags=e2e -run TestFullChain_N60 ./test/e2e/fullchain/...

# UDM integration tests only
go test -tags=e2e -run TestFullChain_UDM ./test/e2e/fullchain/...

# NRF integration tests only
go test -tags=e2e -run TestFullChain_NRF ./test/e2e/fullchain/...

# Diameter transport tests only
go test -tags=e2e -run TestFullChain_Diameter ./test/e2e/fullchain/...

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

## 8. test/aaa_sim Integration

### 8.1 AAA Simulator Implementation

The `test/aaa_sim/` package is the primary AAA-S simulator for E2E testing:

| File | Purpose |
|------|---------|
| `mode.go` | Mode configuration (EAP_TLS_SUCCESS/FAILURE/CHALLENGE) |
| `radius.go` | RADIUS UDP server with RFC 3579 EAP handling |
| `diameter.go` | Diameter TCP server with RFC 6733 CER/CEA, DWR/DWA |

### 8.2 Diameter CER/CEA Handshake

```go
// The Diameter server uses go-diameter/v4/sm for RFC 6733 compliance:
// 1. Client connects to port 3868
// 2. Client sends CER (Capabilities-Exchange-Request)
// 3. Server responds with CEA (Capabilities-Exchange-Answer)
// 4. Connection is now established for DER/DEA
// 5. Watchdog (DWR/DWA) maintains connection health
//
// The AAA Gateway client must:
// 1. Connect to mock-aaa-s
// 2. Wait for CER/CEA exchange to complete
// 3. Only then send DER messages
```

### 8.3 Configuration

```yaml
# compose/dev.yaml
services:
  mock-aaa-s:
    build:
      context: ./test/aaa_sim
      dockerfile: Dockerfile
    ports:
      - "1812:1812/udp"   # RADIUS
      - "3868:3868/tcp"   # Diameter TCP
    environment:
      AAA_SIM_MODE: EAP_TLS_SUCCESS
      AAA_SIM_SECRET: testing123
      AAA_SIM_DIAMETER_TRANSPORT: tcp
      AAA_SIM_DIAMETER_ADDR: ":3868"
      AAA_SIM_RADIUS_ADDR: ":1812"
```

---

## 9. Mock Extensions

### 9.1 UDM Mock Extension

#### 9.1.1 Current State

The existing `test/mocks/udm.go` returns Nudm_UECM registration data but lacks auth subscription endpoint.

#### 9.1.2 Required Extension

```go
// Extend test/mocks/udm.go with:

// SetAuthSubscription configures auth subscription for a SUPI.
// Used by NSSAAF to determine EAP method and AAA server.
func (m *UDMMock) SetAuthSubscription(supi, authType, aaaServer string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.authSubscriptions[supi] = &AuthSubscription{
        AuthType:  authType,
        AAAServer: aaaServer,
    }
}

// Add endpoint: GET /nudm-uem/v1/subscribers/{supi}/auth-contexts
// Returns: {"authContexts": [{"authType": "EAP_TLS", "aaaServer": "radius://..."}]}
```

### 9.2 NRF Mock Extension

#### 9.2.1 Current State

The existing `test/mocks/nrf.go` returns hardcoded `127.0.0.1:8080` for all service endpoints. For E2E tests with docker-compose, endpoints must route to other containers.

#### 9.2.2 Required Extension

```go
// Extend test/mocks/nrf.go with:

// ServiceEndpointConfig holds the endpoint configuration for a service.
type ServiceEndpointConfig struct {
    IPv4Address string
    Port        int
}

// SetServiceEndpoint configures the endpoint for an NF's service.
// This allows E2E tests to point to container DNS names.
func (m *NRFMock) SetServiceEndpoint(nfType, serviceName, host string, port int) {
    m.mu.Lock()
    defer m.mu.Unlock()
    key := fmt.Sprintf("%s:%s", nfType, serviceName)
    m.serviceEndpoints[key] = ServiceEndpointConfig{
        IPv4Address: host,
        Port:        port,
    }
}

// Usage:
// nrfMock.SetServiceEndpoint("UDM", "nudm-uem", "udm-mock", 8080)
// nrfMock.SetServiceEndpoint("AUSF", "nausf-auth", "ausf-mock", 8080)
```

#### 9.2.3 NRF Response Format

When service discovery is called:

```
GET /nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem
→ 200 OK
{
  "nfInstances": [
    {
      "nfInstanceId": "udm-001",
      "nfType": "UDM",
      "nfStatus": "REGISTERED",
      "nfServices": {
        "nudm-uem": {
          "serviceName": "nudm-uem",
          "ipEndPoints": [
            {"ipv4Address": "<container-name>", "port": 8080}
          ]
        }
      }
    }
  ]
}
```

### 9.3 SCTP Transport (Future)

Diameter over SCTP requires:
- Linux kernel module (`sctp.ko`)
- Docker container with `/proc/sys/net/sctp` mount
- Or use `github.com/ishidawataru/sctp` library

**Status:** Not implemented. TCP-only for initial E2E tests.

---

## 10. Acceptance Criteria

| ID | Criteria | Validation |
|----|----------|------------|
| AC-01 | All N58 scenarios pass | `go test -tags=e2e -run TestFullChain_N58 ./test/e2e/fullchain/...` |
| AC-02 | All N60 scenarios pass | `go test -tags=e2e -run TestFullChain_N60 ./test/e2e/fullchain/...` |
| AC-03 | UDM integration scenarios pass | `go test -tags=e2e -run TestFullChain_UDM ./test/e2e/fullchain/...` |
| AC-04 | NRF integration scenarios pass | `go test -tags=e2e -run TestFullChain_NRF ./test/e2e/fullchain/...` |
| AC-05 | Diameter transport scenarios pass | `go test -tags=e2e -run TestFullChain_Diameter ./test/e2e/fullchain/...` |
| AC-06 | Resilience scenarios pass | `go test -tags=e2e -run TestFullChain_Resilience ./test/e2e/fullchain/...` |
| AC-07 | Error codes match spec | All 4xx/5xx responses have ProblemDetails |
| AC-08 | CER/CEA handshake verified | TC-DIA-01 passes |
| AC-09 | Watchdog verified | TC-DIA-02 passes |
| AC-10 | Circuit breaker verified | TC-N58-05 passes |
| AC-11 | Re-auth flow verified | TC-N58-06 passes |
| AC-12 | Revocation flow verified | TC-N58-07 passes |
| AC-13 | UDM update verified | TC-UDM-03 passes |
| AC-14 | NRF discovery verified | TC-NRF-01, TC-NRF-02 pass |
| AC-15 | Degraded mode verified | TC-NRF-03 passes |

---

## 11. Implementation Plan

### Phase 1: Infrastructure
1. Create `test/e2e/fullchain/` directory structure
2. Add `test/aaa_sim/` to docker-compose
3. Implement `amf_initiator.go` and `ausf_initiator.go`
4. Implement `harness_fullchain.go`

### Phase 2: Mock Extensions
5. Extend NRF Mock with `SetServiceEndpoint()`
6. Extend UDM Mock with auth subscription endpoint
7. Verify CER/CEA handshake in test/aaa_sim

### Phase 3: NRF Integration
8. Implement TC-NRF-01 through TC-NRF-05
9. Verify service discovery with container DNS

### Phase 4: UDM Integration
10. Implement TC-UDM-01 through TC-UDM-04
11. Verify Nudm_UECM_UpdateAuthContext called

### Phase 5: Diameter Transport
12. Implement TC-DIA-01 through TC-DIA-05
13. Verify CER/CEA and DWR/DWA

### Phase 6: NSSAA Scenarios
14. Implement TC-N58-01 through TC-N58-10
15. Validate against existing NSSAA flow tests

### Phase 7: AIW Scenarios
16. Implement TC-N60-01 through TC-N60-07
17. Validate against existing AIW flow tests

### Phase 8: Resilience Scenarios
18. Implement TC-RES-01 through TC-RES-05
19. Add circuit breaker and DLQ verification

### Phase 9: SCTP Support (Future)
20. Add SCTP transport to test/aaa_sim
21. Add TC-DIA-06

---

*Design updated: 2026-05-02 (added NRF integration, mock extensions, AAA simulator selection)*
