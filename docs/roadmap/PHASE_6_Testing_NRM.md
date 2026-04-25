# Phase 6: Integration Testing & NRM

## Overview

Phase 6 combines testing strategy with NRM (Network Resource Model) management. The testing strategy covers unit tests, integration tests, E2E tests, 3GPP conformance tests, load tests, and chaos tests. NRM management implements FCAPS (Fault, Configuration, Accounting, Performance, Security) according to 3GPP TS 28.541.

**Spec Foundation:** TS 29.526 §7.2, TS 23.502 §4.2.9, TS 28.541 §5.3.145-148, RFC 3579, RFC 5216

---

## Part 1: Testing Strategy

### 1.1 Test Pyramid

```
                    ┌──────────────────────┐
                    │    E2E Tests         │  ~50 test cases
                    │  (Full 3-component) │  3GPP spec compliance
                    ├──────────────────────┤
                    │  Integration Tests   │  ~200 test cases
                    │  (HTTP GW→Biz→AAA GW)│  API, DB, Redis, AAA
                    ├──────────────────────┤
                    │    Unit Tests        │  ~1000 test cases
                    │  (Functions)         │  Business logic
                    └──────────────────────┘
```

### 1.2 Unit Tests (`internal/*/`)

```go
// Example: internal/types/snssai_test.go
package types

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestSnssai_Validate(t *testing.T) {
    tests := []struct {
        name    string
        snssai  Snssai
        wantErr bool
    }{
        {
            name:   "valid_snssai",
            snssai: Snssai{Sst: 1, Sd: "000001"},
            wantErr: false,
        },
        {
            name:   "valid_snssai_no_sd",
            snssai: Snssai{Sst: 1},
            wantErr: false,
        },
        {
            name:   "invalid_sst_too_high",
            snssai: Snssai{Sst: 256},
            wantErr: true,
        },
        {
            name:   "invalid_sd_wrong_length",
            snssai: Snssai{Sst: 1, Sd: "001"},
            wantErr: true,
        },
        {
            name:   "invalid_sd_not_hex",
            snssai: Snssai{Sst: 1, Sd: "XXXXXX"},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.snssai.Validate()
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

// Example: internal/eap/tls_test.go
func TestEAPTLS_MSKDerivation(t *testing.T) {
    // Mock TLS session with known master secret
    session := &MockTLSSession{
        MasterSecret: make([]byte, 48),
    }
    copy(session.MasterSecret, testMasterSecret)

    // Derive MSK per RFC 5216
    msk := DeriveMSK(session, "EAP-TLS MSK")

    assert.Len(t, msk, 64, "MSK must be 64 bytes")
    assert.NotEqual(t, msk[:32], msk[32:], "MSK and EMSK must differ")
}
```

### 1.3 Integration Tests (`test/integration/`)

```go
// test/integration/nssaa_api_test.go
func TestIntegration_NSSAA_CreateSession(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    // Setup test infrastructure
    infra := integration.NewInfrastructure(t,
        integration.WithHTTPGateway(":8443"),
        integration.WithBizPod(":8080"),
        integration.WithAAAGateway(":9090"),
        integration.WithPostgres(),
        integration.WithRedis(),
    )
    defer infra.Teardown()

    // Start components
    require.NoError(t, infra.Start())

    // Test: Create session via HTTP Gateway
    body := `{
        "gpsi": "5-208046000000001",
        "snssai": { "sst": 1, "sd": "000001" },
        "eapIdRsp": "AG5nZXQtaWQAdXNlckBleGFtcGxlLmNvbQ=="
    }`

    req, _ := http.NewRequest("POST",
        infra.HTTPGatewayURL()+"/nnssaaf-nssaa/v1/slice-authentications",
        strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+testToken)
    req.Header.Set("X-Request-ID", "test-123")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, 201, resp.StatusCode)

    // Verify response
    var authCtx types.SliceAuthContext
    json.NewDecoder(resp.Body).Decode(&authCtx)
    assert.NotEmpty(t, authCtx.AuthCtxId)
    assert.Equal(t, "5-208046000000001", authCtx.Gpsi)
    assert.NotEmpty(t, authCtx.EapMessage)
    assert.NotEmpty(t, resp.Header.Get("Location"))
}

func TestIntegration_AAAGateway_RADIUS(t *testing.T) {
    // Test: RADIUS DER forwarded to AAA-S
    // Test: RADIUS DEA parsed correctly
    // Test: Circuit breaker triggers on AAA failure
}
```

### 1.4 E2E Tests (`test/e2e/`)

```go
// test/e2e/full_flow_test.go
func TestE2E_FullNSSAAFlow(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E test in short mode")
    }

    // Setup: Start all 3 components + infrastructure
    suite := e2e.NewTestSuite(t)
    defer suite.Teardown()

    require.NoError(t, suite.Start())

    // Setup: Start mock AMF and AAA-S
    amfMock := suite.StartAMFMock()
    aaaMock := suite.StartAAASimulator(e2e.AAASimulatorConfig{
        Mode:       "EAP_TLS",
        AuthResult: "SUCCESS",
    })

    // Test: AMF initiates NSSAA
    req := &Nnssaaf_NSSAA_Authenticate_Request{
        Gpsi:        "5-208046000000001",
        Snssai:      &Snssai{Sst: 1, Sd: "000001"},
        EapIdRsp:    EncodeEAPIdentityResponse("user@example.com"),
        AmfInstanceId: "amf-test-001",
    }

    resp, err := amfMock.Authenticate(req)
    require.NoError(t, err)

    // Verify: Session created
    assert.NotEmpty(t, resp.AuthCtxId)
    assert.NotEmpty(t, resp.EapMessage)

    // Verify: AAA-S received RADIUS DER
    assert.True(t, aaaMock.ReceivedRequest())

    // Verify: Database state
    session, err := suite.GetDB().GetSession(resp.AuthCtxId)
    require.NoError(t, err)
    assert.Equal(t, "PENDING", session.Status)
}

func TestE2E_ReauthFromAAA(t *testing.T) {
    // Test: AAA-S triggers re-authentication
    // Test: AMF receives notification
    // Test: Session updated
}

func TestE2E_Revocation(t *testing.T) {
    // Test: AAA-S triggers revocation
    // Test: AMF receives notification
    // Test: Allowed NSSAI updated
}
```

### 1.5 3GPP Conformance Tests (`test/conformance/`)

```go
// test/conformance/ts29_526_test.go
func TestConformance_TS29526_7_2_2(t *testing.T) {
    // TS 29.526 §7.2.2: CreateSliceAuthenticationContext

    tests := []struct {
        name     string
        request  *SliceAuthInfo
        expected int
        cause    string
    }{
        {
            name: "valid_request_returns_201",
            request: &SliceAuthInfo{
                Gpsi:    "5-208046000000001",
                Snssai:  &Snssai{Sst: 1, Sd: "000001"},
                EapIdRsp: EncodeEAPIdentityResponse("user@example.com"),
            },
            expected: 201,
            cause:    "",
        },
        {
            name: "missing_gpsi_returns_400",
            request: &SliceAuthInfo{
                Snssai:  &Snssai{Sst: 1},
            },
            expected: 400,
            cause:    "BAD_REQUEST",
        },
        {
            name: "invalid_snssai_sst_returns_400",
            request: &SliceAuthInfo{
                Gpsi:    "5-208046000000001",
                Snssai:  &Snssai{Sst: 256},  // Invalid: > 255
            },
            expected: 400,
            cause:    "BAD_REQUEST",
        },
        {
            name: "no_aaa_config_returns_404",
            request: &SliceAuthInfo{
                Gpsi:    "5-999999999999999",
                Snssai:  &Snssai{Sst: 99, Sd: "FFFFFF"},
            },
            expected: 404,
            cause:    "NOT_FOUND",
        },
        {
            name: "aaa_unreachable_returns_502",
            setup: func(t *testing.T) {
                // Block AAA server
                BlockAAAServer()
            },
            teardown: func(t *testing.T) {
                UnblockAAAServer()
            },
            expected: 502,
            cause:    "AAA_UNREACHABLE",
        },
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            if tc.setup != nil {
                tc.setup(t)
                defer tc.teardown(t)
            }

            resp := CreateSession(tc.request)
            assert.Equal(t, tc.expected, resp.StatusCode)

            if tc.cause != "" {
                var problem ProblemDetails
                json.NewDecoder(resp.Body).Decode(&problem)
                assert.Equal(t, tc.cause, problem.Cause)
            }
        })
    }
}
```

### 1.6 Load Tests (`test/load/`)

```javascript
// test/load/nssaa_load_test.js (k6)
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '2m', target: 100 },
    { duration: '5m', target: 100 },
    { duration: '2m', target: 500 },
    { duration: '5m', target: 500 },
    { duration: '2m', target: 1000 },
    { duration: '5m', target: 1000 },
    { duration: '2m', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
  },
};

export default function () {
  const baseUrl = 'https://nssaa.operator.com';
  const token = __ENV.TEST_TOKEN;

  // Create session
  const createRes = http.post(
    `${baseUrl}/nnssaaf-nssaa/v1/slice-authentications`,
    JSON.stringify({
      gpsi: `5-208046${Math.floor(Math.random() * 100000000).toString().padStart(7, '0')}`,
      snssai: { sst: 1, sd: '000001' },
      eapIdRsp: base64Encode('user@example.com'),
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      },
    }
  );

  check(createRes, {
    'create session status 201': (r) => r.status === 201,
    'has authCtxId': (r) => r.json('authCtxId') !== undefined,
  });
}
```

---

## Part 2: NRM (Network Resource Model) & FCAPS

### 2.1 NSSAAFFunction IOC (TS 28.541 §5.3.145)

**Priority:** P1
**Dependencies:** RESTCONF/NETCONF server
**Design Doc:** `docs/design/18_nrm_fcaps.md`

```go
// internal/nrm/manager.go

package nrm

import (
    "encoding/json"
    "net/http"
    "time"

    "nssAAF/internal/metrics"
    "nssAAF/internal/storage"
)

// NSSAAFFunction IOC attributes
type NSSAAFFunction struct {
    ManagedElementId   string   `json:"managedElementId"`
    PLMNInfoList       []string `json:"pLMNInfoList"`
    SBIFQDN            string   `json:"sBIFQDN"`
    CNSIIdList         []string `json:"cNSIIdList,omitempty"`
    CommModelList      []string `json:"commModelList"`
    SupportedEapMethods []string `json:"nssaaInfo.supportedSecurityAlgo,omitempty"`
    SupiRanges         []string   `json:"nssaaInfo.supiRanges,omitempty"`
}

// FaultManager handles alarm generation
type FaultManager struct {
    circuitBreakers map[string]*CircuitBreaker
    metricsProvider *metrics.Provider
    db              *storage.PostgresDB
}

const (
    AlarmNssaaAaaServerUnreachable = "NSSAA_AAA_SERVER_UNREACHABLE"
    AlarmNssaaSessionTableFull      = "NSSAA_SESSION_TABLE_FULL"
    AlarmNssaaDatabaseUnreachable   = "NSSAA_DB_UNREACHABLE"
    AlarmNssaaHighAuthFailureRate   = "NSSAA_HIGH_AUTH_FAILURE_RATE"
    AlarmNssaaCircuitBreakerOpen    = "NSSAA_CIRCUIT_BREAKER_OPEN"
)

type Alarm struct {
    AlarmId          string    `json:"alarmId"`
    AlarmType        string    `json:"alarmType"`
    ProbableCause    string    `json:"probableCause"`
    SpecificProblem  string    `json:"specificProblem"`
    Severity         string    `json:"severity"`  // CRITICAL, MAJOR, MINOR, WARNING
    PerceivedSeverity string   `json:"perceivedSeverity"`
    EventTime        time.Time `json:"eventTime"`
    ProposedRepairActions string `json:"proposedRepairActions"`
}

// EvaluateAlarms periodically checks conditions and raises/clears alarms
func (f *FaultManager) EvaluateAlarms() {
    // High failure rate: >10% over 5 min
    rate := f.calculateAuthFailureRate(5 * time.Minute)
    if rate > 0.10 {
        f.raiseAlarm(AlarmNssaaHighAuthFailureRate, "MAJOR", map[string]string{
            "failure_rate": fmt.Sprintf("%.2f%%", rate*100),
        })
    }

    // Session table near capacity
    active := metrics.GetActiveSessions()
    if active > 45000 {
        f.raiseAlarm(AlarmNssaaSessionTableFull, "CRITICAL", map[string]string{
            "active_sessions": fmt.Sprintf("%d/50000", active),
        })
    }

    // Circuit breaker open
    for name, cb := range f.circuitBreakers {
        if cb.State() == CB_OPEN {
            f.raiseAlarm(AlarmNssaaCircuitBreakerOpen, "MAJOR", map[string]string{
                "aaa_server": name,
            })
        }
    }
}
```

### 2.2 RESTCONF API

```go
// internal/nrm/restconf.go

// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function
func (m *NRMManager) HandleGet(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    nssaaFunc := m.getNSSAAFFunction()

    w.Header().Set("Content-Type", "application/yang.data+json")
    json.NewEncoder(w).Encode(map[string]any{
        "3gpp-nssaaf-nrm:nssaa-function": nssaaFunc,
    })
}

// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function.../performance-data
func (m *NRMManager) HandleGetPerformance(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    perfData := m.getPerformanceData()

    w.Header().Set("Content-Type", "application/yang.data+json")
    json.NewEncoder(w).Encode(map[string]any{
        "performance-data": perfData,
    })
}

type PerformanceData struct {
    AuthTotal     int64   `json:"authTotal"`
    AuthSuccess   int64   `json:"authSuccess"`
    AuthFailure   int64   `json:"authFailure"`
    AuthPending   int64   `json:"authPending"`
    AvgLatencyMs  float64 `json:"avgLatencyMs"`
    P99LatencyMs  float64 `json:"p99LatencyMs"`
    ActiveSessions int64   `json:"activeSessions"`
    AAAServerStats []AAAServerStat `json:"aaaServerStats"`
}

type AAAServerStat struct {
    ServerId    string  `json:"serverId"`
    Requests    int64   `json:"requests"`
    Failures    int64   `json:"failures"`
    AvgLatencyMs float64 `json:"avgLatencyMs"`
}
```

---

## Part 3: Integration Tests

### 3.1 NRF Integration Tests

```go
// test/integration/nrf_integration_test.go
func TestIntegration_NRF_Registration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    // Setup: Start NRF mock
    nrfMock := integration.StartNRFMock()

    // Test: NSSAAF registers on startup
    bizPod := integration.StartBizPod(integration.BizConfig{
        NRFBaseURL: nrfMock.URL(),
    })
    defer bizPod.Teardown()

    // Verify: NRF received registration
    profile := nrfMock.GetNSSAAFProfile()
    assert.NotNil(t, profile)
    assert.Equal(t, "NSSAAF", profile.NfType)
    assert.NotEmpty(t, profile.SBIFQDN)
}

func TestIntegration_NRF_Discovery(t *testing.T) {
    // Test: AMF discovered via Nnrf_NFDiscovery
    // Test: AUSF discovered via Nnrf_NFDiscovery
    // Test: UDM discovered via Nnrf_NFDiscovery
}

func TestIntegration_NRF_Heartbeat(t *testing.T) {
    // Test: Nnrf_NFHeartBeat sent every 5 minutes
    // Test: NRF marks NSSAAF as unavailable after missed heartbeats
}
```

### 3.2 AMF Notification Integration Tests

```go
// test/integration/amf_notification_test.go
func TestIntegration_AMF_ReauthNotification(t *testing.T) {
    // Setup: Start AMF mock
    amfMock := integration.StartAMFMock()

    // Test: NSSAAF POSTs to reauthNotifUri when CoA-Request received
    // Test: AMF acknowledges with 204 No Content
    // Test: AMF triggers new NSSAA procedure
}

func TestIntegration_AMF_RevocationNotification(t *testing.T) {
    // Test: NSSAAF POSTs to revocNotifUri when ASR received
    // Test: AMF acknowledges with 204 No Content
    // Test: S-NSSAI removed from Allowed NSSAI
}

func TestIntegration_AMF_RetryOnFailure(t *testing.T) {
    // Test: AMF notification retries 3x on 5xx errors
    // Test: DLQ enqueued after max retries
}
```

### 3.3 UDM Integration Tests

```go
// test/integration/udm_integration_test.go
func TestIntegration_UDM_GetAuthSubscription(t *testing.T) {
    // Setup: Start UDM mock
    udmMock := integration.StartUDMMock()

    // Test: NSSAAF retrieves auth subscription before AAA routing
    authSub, err := udmClient.GetAuthSubscription(ctx, gpsi, snssai)
    require.NoError(t, err)
    assert.Equal(t, "EAP_TLS", authSub.EapMethod)
}

func TestIntegration_UDM_UpdateAuthContext(t *testing.T) {
    // Test: NSSAAF updates UDM with final NssaaStatus after EAP completion
    // Test: UDM 404 if no subscription for (gpsi, snssai)
}
```

### 3.4 AUSF N60 Integration Tests

Note: AUSF N60 implementation is in Phase 4. These tests verify the Phase 4 implementation.

```go
// test/integration/ausf_n60_test.go
func TestIntegration_AUSF_N60_Callout(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    // Setup: Mock AUSF server
    ausfMock := integration.StartAUSFMock()

    // Test: AUSF initiates SNPN authentication via N60
    resp, err := ausfMock.Nudm_UEAuthentication_Get(&ausf.UEEuthRequest{
        AuthType: "EAP_TLS",
        Gpsi: "5-208046000000001",
        Snssai: struct{Sst:1, Sd:"000001"}{},
    })
    require.NoError(t, err)
    assert.NotEmpty(t, resp.EAPMessage)

    // Test: MSK forwarded to AUSF after EAP-TLS success
    assert.True(t, ausfMock.MSKReceived())
}
```

---

## Validation Checklist

### Unit Tests

- [ ] `internal/types/` >95% coverage
- [ ] `internal/eap/` >85% coverage
- [ ] `internal/radius/` >85% coverage
- [ ] `internal/diameter/` >85% coverage
- [ ] `internal/crypto/` >90% coverage
- [ ] All public functions have test cases
- [ ] Edge cases and error paths covered

### Integration Tests

- [ ] API integration tests for all endpoints
- [ ] Database integration (partition creation, queries)
- [ ] Redis integration (caching, TTL)
- [ ] AAA protocol integration (RADIUS/Diameter roundtrip)
- [ ] Circuit breaker integration
- [ ] NRF registration on startup
- [ ] AMF notification POST on re-auth/revocation
- [ ] UDM Nudm_UECM_Get integration

### E2E Tests

- [ ] Full NSSAA flow: AMF → HTTP GW → Biz Pod → AAA GW → AAA-S
- [ ] Re-authentication from AAA-S
- [ ] Revocation from AAA-S
- [ ] Timeout handling
- [ ] Error recovery

### 3GPP Conformance

- [ ] TS 29.526 §7.2 API operations tested (~30 cases)
- [ ] TS 23.502 §4.2.9 procedure flows tested (~15 cases)
- [ ] RFC 3579 RADIUS EAP extension tested
- [ ] RFC 5216 EAP-TLS MSK derivation tested

### Load Tests

- [ ] 50K concurrent sessions sustained
- [ ] 1000 RPS sustained for 5 minutes
- [ ] P99 latency <500ms
- [ ] Error rate <1%

### Chaos Tests

- [ ] Pod kill during active session
- [ ] Database connection loss
- [ ] Redis connection loss
- [ ] AAA server unreachable
- [ ] Circuit breaker recovery

### NRM/FCAPS

- [ ] NSSAAFFunction IOC exposed via RESTCONF
- [ ] Alarm raised on failure rate >10%
- [ ] Alarm raised on circuit breaker open
- [ ] Alarm raised on session table near capacity
- [ ] Performance data exposed (authTotal, authSuccess, authFailure, latency)
- [ ] Configuration via YANG/NETCONF

---

## Success Criteria (What Must Be TRUE)

1. **Unit tests catch regressions** — >80% overall coverage, critical paths >90%
2. **Integration tests verify components** — HTTP GW → Biz → AAA GW flow works
3. **E2E tests verify 3GPP compliance** — All procedure flows from TS 23.502 work
4. **Load tests meet SLA** — 50K sessions, P99 <500ms, <1% error rate
5. **Chaos tests validate resilience** — Failures handled gracefully, RTO <30s
6. **NRM exposes state** — Operations can query alarms and performance via RESTCONF
7. **Alarms fire on degradation** — On-call notified when failure rate exceeds threshold

---

## Dependencies

|| Module | Status | Blocking |
|--------|--------|----------|
| `internal/types/` | READY (Phase 1) | No |
| `internal/eap/` | READY (Phase 2) | No |
| `internal/radius/` | READY (Phase 2) | No |
| `internal/storage/` | READY (Phase 3) | No |
| `internal/cache/` | READY (Phase 3) | No |
| `internal/resilience/` | Phase 4 | No |
| `internal/metrics/` | Phase 4 | No |
| `internal/auth/` | Phase 5 | No |
| `internal/crypto/` | Phase 5 | No |
| `internal/nrf/` | Phase 4 | No |
| `internal/udm/` | Phase 4 | No |
| `internal/amf/` | Phase 4 | No |
| `internal/ausf/` | Phase 4 | No |

---

## Next Phase

Phase 7: Kubernetes Deployment — Helm charts, HPA, PDB, keepalived, ArgoCD
