---
spec: 3GPP TS 26.526 / RFC 3579 / RFC 5216 / ETSI NFV-TST 008
section: Testing & Conformance
interface: N/A
service: Testing
---

# NSSAAF Testing Strategy

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, testing spans three separate processes (HTTP Gateway, Biz Pod, AAA Gateway). Each component has its own test suite. E2E tests must verify the full flow across all three components. See `docs/design/01_service_model.md` §5.4 for the architecture overview.

Testing strategy toàn diện cho NSSAAF đạt telecom-grade quality, bao gồm unit tests, integration tests, conformance tests, load tests, và chaos tests.

---

## 2. Test Pyramid

```
                    ┌──────────────────────┐
                    │    E2E Tests         │  ~50 test cases
                    │  (Full flow)        │  3GPP spec compliance
                    ├──────────────────────┤
                    │  Integration Tests  │  ~200 test cases
                    │  (Components)       │  API, DB, Redis, AAA
                    ├──────────────────────┤
                    │    Unit Tests       │  ~1000 test cases
                    │  (Functions)       │  Business logic
                    └──────────────────────┘
```

### 2.1 Test Distribution

| Level | Count | Coverage Target | Execution Time |
|-------|-------|----------------|---------------|
| Unit | ~1000 | 80% line, 90% branch | <5 min |
| Integration | ~200 | All APIs, DB, Redis, AAA | <15 min |
| E2E | ~50 | Critical flows | <30 min |
| Performance | ~20 | SLA compliance | <1 hour |
| Chaos | ~15 | Failure scenarios | <30 min |

---

## 3. Unit Tests

### 3.1 Test Framework

```go
// Go: testify + gock for mocking
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/h2non/gock"
)

func TestEapMessageEncode(t *testing.T) {
    msg := &EapMessage{
        Code: EAP_CODE_RESPONSE,
        Id:   0x05,
        Type: EAP_TYPE_IDENTITY,
        Data: []byte("user@example.com"),
    }

    encoded := msg.Encode()

    assert.Equal(t, byte(0x02), encoded[0])  // Code = Response
    assert.Equal(t, byte(0x05), encoded[1])  // Id
    assert.Equal(t, byte(0x04), encoded[2]) // Type
    assert.Equal(t, []byte("user@example.com"), encoded[4:])
}

func TestSnssaiVSAEncode(t *testing.T) {
    snssai := Snssai{Sst: 1, Sd: "000001"}
    vsa := EncodeSnssaiVSA(snssai)

    assert.Equal(t, byte(26), vsa[0])          // VSA type
    assert.Equal(t, byte(6), vsa[2])            // Vendor-Id byte 1
    assert.Equal(t, byte(0x28), vsa[3])         // Vendor-Id byte 2
    assert.Equal(t, byte(0x9F), vsa[4])        // Vendor-Id byte 3
    assert.Equal(t, byte(200), vsa[5])          // Vendor-Type
    assert.Equal(t, byte(1), vsa[6])           // SST
    assert.Equal(t, []byte{0x00, 0x00, 0x01}, vsa[7:10])  // SD
}

func TestMessageAuthenticator(t *testing.T) {
    packet := []byte{ /* valid RADIUS Access-Request */ }
    secret := "testing123"

    ma := ComputeMessageAuthenticator(packet, secret)

    assert.Len(t, ma, 16)  // HMAC-MD5 = 16 bytes
    assert.NotEqual(t, make([]byte, 16), ma)  // Not zero
}
```

### 3.2 Test Coverage

```bash
# Run with coverage
go test -v -coverprofile=coverage.out -covermode=atomic ./...

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html

# Coverage targets:
# - Overall: >80% line coverage
# - Business logic: >90% line coverage
# - Error paths: >85% branch coverage
```

### 3.3 Mock Patterns

```go
// Mock database
func TestCreateSession(t *testing.T) {
    mockDB, sqlmock, _ := sqlmock.New()
    defer mockDB.Close()

    sqlmock.ExpectExec("INSERT INTO slice_auth_sessions").
        WithArgs("auth123", "5-208046000000001", 1, "000001", sqlmock.AnyArg()).
        WillReturnResult(sqlmock.NewResult(1, 1))

    repo := NewSessionRepository(mockDB)
    err := repo.Create(context.Background(), &Session{AuthCtxId: "auth123"})

    require.NoError(t, err)
    assert.Empty(t, sqlmock.ExpectationsWereMet())
}

// Mock Redis
func TestGetSessionFromCache(t *testing.T) {
    mockRedis := miniredis.RunT(t)
    defer mockRedis.Close()

    mockRedis.Set("nssaa:session:auth123", `{"gpsi":"5-208046000000001","status":"PENDING"}`)

    cache := NewRedisCache(mockRedis.Client())
    session, err := cache.GetSession(context.Background(), "auth123")

    require.NoError(t, err)
    assert.Equal(t, "5-208046000000001", session.Gpsi)
    assert.Equal(t, "PENDING", session.Status)
}

// Mock AAA server
func TestRADIUSAccessRequest(t *testing.T) {
    // Mock RADIUS server
    server, client := mockradius.NewUDPServer()
    defer server.Close()

    server.Handle(func(req *radius.Packet) *radius.Packet {
        assert.Equal(t, radius.CodeAccessRequest, req.Code)
        assert.Contains(t, req.Attributes.String(radius.AcctSessionID), "auth123")

        return radius.NewAccessAccept(req, []radius.Attribute{
            radius.ReplyMessage("Welcome"),
        })
    })

    // Test client
    resp, err := client.RAUTH("auth123", "testing")

    require.NoError(t, err)
    assert.Equal(t, radius.CodeAccessAccept, resp.Code)
}
```

---

## 4. Integration Tests

### 4.1 API Integration Tests

```go
// Integration tests using real HTTP server
func TestNSSAAAPI_CreateSession(t *testing.T) {
    // Setup test server
    cfg := &testConfig{
        db:      testDB,
        redis:   testRedis,
        aaaMock: mockAAAServer,
    }
    server := NewTestServer(cfg)
    defer server.Close()

    // Test request
    body := `{
        "gpsi": "5-208046000000001",
        "snssai": { "sst": 1, "sd": "000001" },
        "eapIdRsp": "AG5nZXQtaWQAdXNlckBleGFtcGxlLmNvbQ=="
    }`

    req, _ := http.NewRequest("POST", server.URL+"/slice-authentications", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+testToken)
    req.Header.Set("X-Request-ID", "test-123")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, 201, resp.StatusCode)

    // Verify response body
    var ctx SliceAuthContext
    json.NewDecoder(resp.Body).Decode(&ctx)
    assert.NotEmpty(t, ctx.AuthCtxId)
    assert.Equal(t, "5-208046000000001", ctx.Gpsi)
    assert.NotEmpty(t, ctx.EapMessage)

    // Verify Location header
    location := resp.Header.Get("Location")
    assert.Contains(t, location, ctx.AuthCtxId)

    // Verify DB state
    session, err := db.GetSession(ctx.AuthCtxId)
    require.NoError(t, err)
    assert.Equal(t, "PENDING", session.Status)
}

func TestNSSAAAPI_InvalidGpsi(t *testing.T) {
    server := NewTestServer(testCfg)

    body := `{
        "gpsi": "invalid-format",
        "snssai": { "sst": 1 }
    }`

    req, _ := http.NewRequest("POST", server.URL+"/slice-authentications", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    resp, _ := http.DefaultClient.Do(req)
    defer resp.Body.Close()

    assert.Equal(t, 400, resp.StatusCode)

    var problem ProblemDetails
    json.NewDecoder(resp.Body).Decode(&problem)
    assert.Equal(t, "BAD_REQUEST", problem.Cause)
    assert.Contains(t, problem.Detail, "gpsi")
}
```

### 4.2 Database Integration

```go
func TestDatabase_PartitionCreation(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping partition test in short mode")
    }

    // Create partition for next month
    err := createMonthlyPartition(db, time.Now().AddDate(0, 1, 0))
    require.NoError(t, err)

    // Insert into partition
    _, err = db.Exec(`
        INSERT INTO slice_auth_sessions
        (auth_ctx_id, gpsi, snssai_sst, snssai_sd, eap_session_state, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6)`,
        "test-auth-001", "5-208046000000001", 1, "000001", []byte("state"), time.Now().Add(5*time.Minute),
    )
    require.NoError(t, err)

    // Query from partition
    row := db.QueryRow("SELECT gpsi FROM slice_auth_sessions WHERE auth_ctx_id = $1", "test-auth-001")
    var gpsi string
    err = row.Scan(&gpsi)
    require.NoError(t, err)
    assert.Equal(t, "5-208046000000001", gpsi)
}
```

### 4.3 Redis Integration

```go
func TestRedis_Cluster(t *testing.T) {
    // Requires real Redis Cluster
    redisAddr := os.Getenv("TEST_REDIS_CLUSTER")
    if redisAddr == "" {
        t.Skip("TEST_REDIS_CLUSTER not set")
    }

    client := redis.NewClusterClient(&redis.ClusterOptions{
        Addrs:    strings.Split(redisAddr, ","),
        PoolSize: 10,
    })

    ctx := context.Background()

    // Test session caching
    err := client.Set(ctx, "nssaa:session:test1", `{"gpsi":"5-001"}`, 5*time.Minute).Err()
    require.NoError(t, err)

    val, err := client.Get(ctx, "nssaa:session:test1").Result()
    require.NoError(t, err)
    assert.Contains(t, val, "5-001")

    // Test TTL
    ttl, err := client.TTL(ctx, "nssaa:session:test1").Result()
    require.NoError(t, err)
    assert.True(t, ttl > 4*time.Minute && ttl <= 5*time.Minute)
}
```

---

## 5. End-to-End Tests

### 5.1 E2E Test Architecture

> **Note (Phase R):** In the 3-component model, E2E tests start all three components (HTTP Gateway, Biz Pod, AAA Gateway) plus supporting infrastructure.

```
┌─────────────────────────────────────────────────────────────┐
│                  E2E Test Runner                             │
│                   (Go test)                                  │
└──────────────────────┬──────────────────────────────────────┘
                       │
         ┌────────────┼────────────┐
         ▼            ▼            ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│  AMF Mock  │ │ HTTP Gateway │ │  Biz Pod    │  AAA Mock  │
│  (gRPC)   │ │  (Real)    │ │  (Real)    │  (UDP/TCP) │
└─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘
                             │            │
                             │            ▼
                             │     ┌─────────────┐
                             │     │ AAA Gateway │  (Real UDP/TCP)
                             │     └─────────────┘
                             │
                             ▼
                       ┌─────────────┐
                       │ PostgreSQL  │
                       │   Redis     │
                       │   NRF Mock  │
                       └─────────────┘
```

### 5.2 E2E Test Cases

```go
// Full NSSAA flow E2E test (3-component model)
func TestE2E_NSSAA_Flow(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    // Setup: start all 3 components
    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443", // AAA Gateway internal HTTP
    })
    defer bizPod.Stop()

    aaaGW := StartAAAGateway(&AAAGatewayConfig{
        Port: 1812, // RADIUS UDP
        BizPodURL:   "http://localhost:8080", // internal HTTP callback
    })
    defer aaaGW.Stop()

    // Mock external services
    amfMock := StartAMFMock()
    defer amfMock.Stop()

    aaaMock := StartAAASimulator(&AAASimulatorConfig{
        Mode:       "EAP_TLS",
        AuthResult: "SUCCESS",
    })
    defer aaaMock.Stop()

    // AMF sends initial request via HTTP Gateway
    ctx := &Nnssaaf_NSSAA_Authenticate_Request{
        Gpsi:    "5-208046000000001",
        Snssai:  &Snssai{Sst: 1, Sd: "000001"},
        EapIdRsp: EncodeEAPIdentityResponse("user@example.com"),
        AmfInstanceId: "amf-test-001",
    }

    resp1, err := amfMock.Authenticate(ctx)
    require.NoError(t, err)

    // Verify: HTTP Gateway routed to Biz Pod, Biz Pod sent to AAA Gateway
    assert.NotEmpty(t, resp1.AuthCtxId)
    assert.NotEmpty(t, resp1.EapMessage)
    assert.True(t, aaaGW.ReceivedRequest()) // AAA Gateway received RADIUS
}

func TestE2E_NSSAA_Reauth_FromAAA(t *testing.T) {
    // Test AAA-S triggered re-authentication
    nssAAFSvc := StartNSSAAFService()
    defer nssAAFSvc.Stop()

    amfMock := StartAMFMock()
    defer amfMock.Stop()

    // Pre-condition: establish existing session
    established := EstablishSession(amfMock, "5-001", "1:000001")

    // AAA-S triggers re-auth
    aaaMock := GetAAASimulator()
    err := aaaMock.TriggerReAuth("5-001", "1:000001")
    require.NoError(t, err)

    // Verify AMF received notification
    notif := amfMock.WaitForNotification(5 * time.Second)
    assert.Equal(t, "SLICE_RE_AUTH", notif.Type)
    assert.Equal(t, "5-001", notif.Gpsi)
}

func TestE2E_NSSAA_Revocation(t *testing.T) {
    // Test AAA-S triggered authorization revocation
    nssAAFSvc := StartNSSAAFService()
    defer nssAAFSvc.Stop()

    amfMock := StartAMFMock()
    defer amfMock.Stop()

    // Pre-condition: establish existing session
    established := EstablishSession(amfMock, "5-001", "1:000001")

    // AAA-S triggers revocation
    aaaMock := GetAAASimulator()
    err := aaaMock.TriggerRevocation("5-001", "1:000001")
    require.NoError(t, err)

    // Verify AMF received notification
    notif := amfMock.WaitForNotification(5 * time.Second)
    assert.Equal(t, "SLICE_REVOCATION", notif.Type)

    // Verify AMF updated Allowed NSSAI
    allowedNssai := amfMock.GetAllowedNssai()
    assert.NotContains(t, allowedNssai, "1:000001")
}
```

### 5.3 AIW E2E Test Cases (N60 / AUSF / SUPI)

> **Note:** Nnssaaf_AIW (N60 interface) uses AUSF as consumer with SUPI instead of GPSI. MSK is returned on EAP_SUCCESS (RFC 5216 §2.1.4). Re-authentication and revocation are **not applicable** to AIW (per TS 29.526 AC8). AIW tests use the same 3-component model as §5.2, with AUSF Mock replacing AMF Mock.

```go
// Full AIW flow E2E test (3-component model)
func TestE2E_AIW_BasicFlow(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    // Setup: start all 3 components
    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443", // AAA Gateway internal HTTP
    })
    defer bizPod.Stop()

    aaaGW := StartAAAGateway(&AAAGatewayConfig{
        Port: 1812, // RADIUS UDP
        BizPodURL:   "http://localhost:8080", // internal HTTP callback
    })
    defer aaaGW.Stop()

    // Mock external services
    ausfMock := StartAUSFMock()
    defer ausfMock.Stop()

    aaaSim := StartAAASimulator(&AAASimulatorConfig{
        Mode:       "EAP_TLS",
        AuthResult: "SUCCESS",
        MSK:        generateRandomBytes(64), // RFC 5216: 64-byte MSK
        PVSInfo: []PVSInfo{
            {ServerType: "PROSE", ServerId: "pvs-001"},
        },
    })
    defer aaaSim.Stop()

    // AUSF sends initial request via HTTP Gateway
    ctx := &AiwAuthInfo{
        Supi:         "imsi-208046000000001",
        EapIdRsp:     EncodeEAPIdentityResponse("user@example.com"),
        SupportedFeatures: "3GPP-R18-AIW",
    }

    resp1, err := ausfMock.Authenticate(ctx)
    require.NoError(t, err)

    // Verify: HTTP Gateway routed to Biz Pod, Biz Pod sent to AAA Gateway
    assert.NotEmpty(t, resp1.AuthCtxId)
    assert.NotEmpty(t, resp1.EapMessage)

    // Multi-round confirmation if EAP challenge returned
    for i := 0; i < 10; i++ {
        if resp1.AuthResult != nil {
            break
        }

        resp1, err = ausfMock.Confirm(resp1.AuthCtxId, &AiwConfirmInfo{
            Supi:       ctx.Supi,
            EapMessage: resp1.EapMessage,
        })
        require.NoError(t, err)
    }

    // Final verification
    assert.Equal(t, "EAP_SUCCESS", resp1.AuthResult)
    assert.NotEmpty(t, resp1.Msk)
    assert.NotNil(t, resp1.PvsInfo)
    assert.Len(t, resp1.PvsInfo, 1)
    assert.Equal(t, "pvs-001", resp1.PvsInfo[0].ServerId)
}

func TestE2E_AIW_MSKExtraction(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443",
    })
    defer bizPod.Stop()

    aaaGW := StartAAAGateway(&AAAGatewayConfig{Port: 1812, BizPodURL: "http://localhost:8080"})
    defer aaaGW.Stop()

    ausfMock := StartAUSFMock()
    defer ausfMock.Stop()

    expectedMSK := generateRandomBytes(64) // RFC 5216 §2.1.4: MSK = 64 octets
    aaaSim := StartAAASimulator(&AAASimulatorConfig{
        Mode:       "EAP_TLS",
        AuthResult: "SUCCESS",
        MSK:        expectedMSK,
    })
    defer aaaSim.Stop()

    ctx := &AiwAuthInfo{
        Supi:         "imsi-208046000000001",
        EapIdRsp:     EncodeEAPIdentityResponse("user@example.com"),
        SupportedFeatures: "3GPP-R18-AIW",
    }

    resp, err := ausfMock.Authenticate(ctx)
    require.NoError(t, err)

    // Multi-round to completion
    for resp.AuthResult == nil {
        resp, err = ausfMock.Confirm(resp.AuthCtxId, &AiwConfirmInfo{
            Supi:       ctx.Supi,
            EapMessage: resp.EapMessage,
        })
        require.NoError(t, err)
    }

    // Decode base64 MSK
    mskBytes, err := base64.StdEncoding.DecodeString(resp.Msk)
    require.NoError(t, err)

    // Verify MSK length: RFC 5216 §2.1.4
    assert.Len(t, mskBytes, 64, "MSK must be 64 octets per RFC 5216 §2.1.4")

    // Verify MSK[:32] != MSK[32:] (MSK != EMSK)
    assert.NotEqual(t, mskBytes[:32], mskBytes[32:],
        "MSK lower 32 octets must differ from upper 32 (EMSK)")
}

func TestE2E_AIW_EAPFailure(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443",
    })
    defer bizPod.Stop()

    aaaGW := StartAAAGateway(&AAAGatewayConfig{Port: 1812, BizPodURL: "http://localhost:8080"})
    defer aaaGW.Stop()

    ausfMock := StartAUSFMock()
    defer ausfMock.Stop()

    // AAA Simulator returns Access-Reject
    aaaSim := StartAAASimulator(&AAASimulatorConfig{
        Mode:       "EAP_TLS",
        AuthResult: "REJECT",
    })
    defer aaaSim.Stop()

    ctx := &AiwAuthInfo{
        Supi:         "imsi-208046000000001",
        EapIdRsp:     EncodeEAPIdentityResponse("user@example.com"),
        SupportedFeatures: "3GPP-R18-AIW",
    }

    resp, err := ausfMock.Authenticate(ctx)
    require.NoError(t, err)

    // Multi-round to completion
    for resp.AuthResult == nil {
        resp, err = ausfMock.Confirm(resp.AuthCtxId, &AiwConfirmInfo{
            Supi:       ctx.Supi,
            EapMessage: resp.EapMessage,
        })
        require.NoError(t, err)
    }

    // On EAP Failure: HTTP 200 with authResult=EAP_FAILURE in body (not HTTP 403)
    assert.Equal(t, "EAP_FAILURE", resp.AuthResult)
    assert.Empty(t, resp.Msk)
    assert.Empty(t, resp.EapMessage)
    assert.Nil(t, resp.PvsInfo)
}

func TestE2E_AIW_InvalidSupi(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443",
    })
    defer bizPod.Stop()

    ausfMock := StartAUSFMock()
    defer ausfMock.Stop()

    // Send request with invalid SUPI format
    ctx := &AiwAuthInfo{
        Supi:         "invalid-supi-format",
        EapIdRsp:     EncodeEAPIdentityResponse("user@example.com"),
        SupportedFeatures: "3GPP-R18-AIW",
    }

    resp, err := ausfMock.Authenticate(ctx)
    require.NoError(t, err)

    // HTTP 400: invalid SUPI does not match ^imsi-[0-9]{5,15}$
    assert.Equal(t, 400, resp.StatusCode)

    var problem ProblemDetails
    json.NewDecoder(resp.Body).Decode(&problem)
    assert.Equal(t, "INVALID_SUPI", problem.Cause)
    assert.Contains(t, problem.Detail, "supi")
}

func TestE2E_AIW_AAA_NotConfigured(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443",
    })
    defer bizPod.Stop()

    ausfMock := StartAUSFMock()
    defer ausfMock.Stop()

    // SUPI in range that has no AAA config
    ctx := &AiwAuthInfo{
        Supi:         "imsi-999999999999999", // No AAA server for this SUPI range
        EapIdRsp:     EncodeEAPIdentityResponse("user@example.com"),
        SupportedFeatures: "3GPP-R18-AIW",
    }

    resp, err := ausfMock.Authenticate(ctx)
    require.NoError(t, err)

    // HTTP 404: no AAA server configured for this SUPI range
    assert.Equal(t, 404, resp.StatusCode)

    var problem ProblemDetails
    json.NewDecoder(resp.Body).Decode(&problem)
    assert.Equal(t, "AAA_SERVER_NOT_CONFIGURED", problem.Cause)
}

func TestE2E_AIW_TTLS(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443",
    })
    defer bizPod.Stop()

    aaaGW := StartAAAGateway(&AAAGatewayConfig{Port: 1812, BizPodURL: "http://localhost:8080"})
    defer aaaGW.Stop()

    ausfMock := StartAUSFMock()
    defer ausfMock.Stop()

    // EAP-TTLS with inner method
    ttlsInner := encodeTTLSInnerMethod("PAP", "user", "pass")
    aaaSim := StartAAASimulator(&AAASimulatorConfig{
        Mode:       "EAP_TTLS",
        AuthResult: "SUCCESS",
        MSK:        generateRandomBytes(64),
        PVSInfo: []PVSInfo{
            {ServerType: "PROSE", ServerId: "pvs-ttls-001"},
        },
    })
    defer aaaSim.Stop()

    ctx := &AiwAuthInfo{
        Supi:                   "imsi-208046000000001",
        EapIdRsp:               EncodeEAPIdentityResponse("user@example.com"),
        TtlsInnerMethodContainer: base64.StdEncoding.EncodeToString(ttlsInner),
        SupportedFeatures:      "3GPP-R18-AIW",
    }

    resp, err := ausfMock.Authenticate(ctx)
    require.NoError(t, err)

    // ttlsInnerMethodContainer echoed back in response
    assert.Equal(t, ctx.TtlsInnerMethodContainer, resp.TtlsInnerMethodContainer)

    // Multi-round TTLS
    for resp.AuthResult == nil {
        resp, err = ausfMock.Confirm(resp.AuthCtxId, &AiwConfirmInfo{
            Supi:       ctx.Supi,
            EapMessage: resp.EapMessage,
        })
        require.NoError(t, err)
    }

    // Verify TTLS completed successfully
    assert.Equal(t, "EAP_SUCCESS", resp.AuthResult)
    assert.NotEmpty(t, resp.Msk)
    assert.NotNil(t, resp.PvsInfo)
}

// AIW Conformance Tests: TS 29.526 §7.3.2.2, §7.3.2.3 / TS 33.501 §I.2.2.2 / RFC 5216 §2.1.4

func TestConformance_AIW_01_BasicAuthFlow(t *testing.T) { /* AIW-01: TS 29.526 §7.3.2.2 */ }
func TestConformance_AIW_02_MSKReturnedOnSuccess(t *testing.T) { /* AIW-02: RFC 5216 §2.1.4 */ }
func TestConformance_AIW_03_PVSInfoReturned(t *testing.T) { /* AIW-03: TS 33.501 §I.2.2.2 */ }
func TestConformance_AIW_04_EAPFailureInBody(t *testing.T) { /* AIW-04: TS 29.526 §7.3.2.3 */ }
func TestConformance_AIW_05_InvalidSupiRejected(t *testing.T) { /* AIW-05: TS 29.526 §7.3.2.2 */ }
func TestConformance_AIW_06_AAA_NotConfigured(t *testing.T) { /* AIW-06: TS 29.526 §7.3.2.2 */ }
func TestConformance_AIW_07_MultiRoundChallenge(t *testing.T) { /* AIW-07: TS 29.526 §7.3.2.3 */ }
func TestConformance_AIW_08_SupportedFeaturesEcho(t *testing.T) { /* AIW-08: TS 29.526 §7.3.2.2 */ }
func TestConformance_AIW_09_TTLSInnerMethodContainer(t *testing.T) { /* AIW-09: TS 33.501 §I.2.2.2 */ }
func TestConformance_AIW_10_MSKLength64Octets(t *testing.T) { /* AIW-10: RFC 5216 §2.1.4 */ }
func TestConformance_AIW_11_MSKNotEqualEMSK(t *testing.T) { /* AIW-11: RFC 5216 §2.1.4 */ }
func TestConformance_AIW_12_NoReauthSupport(t *testing.T) { /* AIW-12: TS 29.526 AC8 — AIW excludes re-auth */ }
func TestConformance_AIW_13_NoRevocationSupport(t *testing.T) { /* AIW-13: TS 29.526 AC8 — AIW excludes revocation */ }
```

---

## 6. 3GPP Conformance Tests

### 6.1 Conformance Test Specs

| Spec | Test Cases | Coverage |
|------|-----------|---------|
| TS 29.526 | ~30 | API operations, error handling |
| TS 23.502 | ~15 | Procedure flows |
| TS 33.501 | ~10 | Security requirements |
| RFC 3579 | ~10 | RADIUS EAP extension |
| RFC 5216 | ~10 | EAP-TLS |
| TS 29.561 | ~10 | AAA interworking |

### 6.2 Conformance Test Examples

```go
// TS 29.526 §7.2.2: CreateSliceAuthenticationContext
func TestConformance_TS29526_7_2_2(t *testing.T) {
    tests := []struct {
        name     string
        request  *SliceAuthInfo
        expected int  // Expected HTTP status
    }{
        {
            name: "valid_request",
            request: &SliceAuthInfo{
                Gpsi:    "5-208046000000001",
                Snssai:  &Snssai{Sst: 1, Sd: "000001"},
                EapIdRsp: EncodeEAPIdentityResponse("user@example.com"),
            },
            expected: 201,
        },
        {
            name: "missing_gpsi",
            request: &SliceAuthInfo{
                Snssai:  &Snssai{Sst: 1},
                EapIdRsp: EncodeEAPIdentityResponse("user@example.com"),
            },
            expected: 400,
        },
        {
            name: "invalid_snssai_sst",
            request: &SliceAuthInfo{
                Gpsi:    "5-208046000000001",
                Snssai:  &Snssai{Sst: 256},  // Invalid: > 255
                EapIdRsp: EncodeEAPIdentityResponse("user@example.com"),
            },
            expected: 400,
        },
        {
            name: "aaa_server_not_configured",
            request: &SliceAuthInfo{
                Gpsi:    "5-999999999999999",  // No AAA config for this
                Snssai:  &Snssai{Sst: 99, Sd: "FFFFFF"},  // Non-existent slice
                EapIdRsp: EncodeEAPIdentityResponse("user@example.com"),
            },
            expected: 404,
        },
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            resp := CreateSession(tc.request)
            assert.Equal(t, tc.expected, resp.StatusCode, tc.name)
        })
    }
}

// RFC 3579: EAP-Message attribute handling
func TestConformance_RFC3579_EAPMessage(t *testing.T) {
    // Test fragmentation
    largeEAP := GenerateLargeEAPMessage(8000)  // > RADIUS MTU
    vsa := EncodeEAPMessageVSA(largeEAP)

    // Verify fragmentation
    fragments := FragmentEAPMessage(largeEAP, 4096)
    assert.Greater(t, len(fragments), 1)

    // Verify reassembly
    reassembled := ReassembleEAPMessage(fragments)
    assert.Equal(t, largeEAP, reassembled)
}

// RFC 5216: EAP-TLS MSK derivation
func TestConformance_RFC5216_MSK(t *testing.T) {
    tlsSession := &MockTLSSession{
        MasterSecret: generateRandomBytes(48),
    }

    msk := DeriveMSK(tlsSession, "EAP-TLS MSK")
    assert.Len(t, msk, 64)  // MSK = 64 bytes

    // Verify MSK/EMSK split
    assert.NotEqual(t, msk[:32], msk[32:])
}
```

---

## 7. Performance & Load Tests

### 7.1 k6 Load Test

```javascript
// k6 load test script
// k6 run load-test.js

import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '2m', target: 100 },    // Ramp up to 100 RPS
    { duration: '5m', target: 100 },    // Steady state
    { duration: '2m', target: 500 },   // Ramp up to 500 RPS
    { duration: '5m', target: 500 },    // Steady state
    { duration: '2m', target: 1000 },   // Ramp up to 1000 RPS
    { duration: '5m', target: 1000 },  // Steady state
    { duration: '2m', target: 0 },     // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],  // 500ms p95, 1s p99
    http_req_failed: ['rate<0.01'],                     // <1% failure rate
    checks: ['rate>0.99'],                             // >99% checks pass
  },
};

export default function () {
  const baseUrl = 'https://nssAAF.operator.com';
  const token = 'Bearer ' + __ENV.TEST_TOKEN;

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
        'Authorization': token,
      },
    }
  );

  check(createRes, {
    'create session: status 201': (r) => r.status === 201,
    'create session: has authCtxId': (r) => r.json('authCtxId') !== undefined,
    'create session: has Location': (r) => r.headers['Location'] !== undefined,
  });

  const authCtxId = createRes.json('authCtxId');

  // Multi-round confirmations
  for (let i = 0; i < 3; i++) {
    sleep(0.1);

    const confirmRes = http.put(
      `${baseUrl}/nnssaaf-nssaa/v1/slice-authentications/${authCtxId}`,
      JSON.stringify({
        gpsi: createRes.json('gpsi'),
        snssai: { sst: 1, sd: '000001' },
        eapMessage: base64Encode(`challenge-${i}`),
      }),
      {
        headers: {
          'Content-Type': 'application/json',
          'Authorization': token,
        },
      }
    );

    check(confirmRes, {
      'confirm: status 200': (r) => r.status === 200,
    });

    if (confirmRes.json('authResult') !== null) {
      break;
    }
  }
}
```

### 7.2 Performance Targets

| Metric | Target | SLA |
|--------|--------|-----|
| Concurrent sessions | 50,000 / instance | Hard limit |
| Requests per second | 10,000 / instance | Sustained |
| P50 latency | <20ms | Per request |
| P95 latency | <100ms | Per request |
| P99 latency | <500ms | Per request |
| Error rate | <0.1% | Sustained |
| Session setup rate | >5,000 / sec | Peak |
| RADIUS transaction rate | >50,000 / sec | Per instance |

---

## 8. Chaos Tests

### 8.1 Chaos Testing Framework

```yaml
# chaos-experiments/nssAAF
apiVersion: chaos-mesh.org/v1alpha1
kind: Schedule
metadata:
  name: nssAAF-chaos
spec:
  schedule: "0 */6 * * *"
  type: Schedule
  chaos:
    - kind: PodFailure
      name: pod-failure
      spec:
        action: pod-failure
        mode: one
        duration: 60s
        selector:
          namespaces:
            - nssAAF
          labelSelectors:
            app: nssAAF

    - kind: NetworkPartition
      name: network-partition
      spec:
        action: all
        mode: one
        duration: 30s
        selector:
          namespaces:
            - nssAAF
          labelSelectors:
            app: nssAAF

    - kind: PodKill
      name: pod-kill
      spec:
        action: pod-kill
        mode: random-max-1
        duration: 0s
        selector:
          namespaces:
            - nssAAF
          labelSelectors:
            app: nssAAF
```

### 8.2 Chaos Test Cases

```go
// Chaos: pod killed during active session
func TestChaos_PodKill_DuringSession(t *testing.T) {
    // Start session
    session := StartSession("5-001", "1:000001")

    // Verify session active
    assert.Equal(t, "PENDING", session.Status)

    // Kill pod
    KillPod(session.PodName)

    // Wait for replacement
    WaitForPodReady(session.PodName)

    // Session should still be accessible
    session2 := GetSession(session.AuthCtxId)
    assert.NotNil(t, session2)
}

// Chaos: database connection lost
func TestChaos_DBConnectionLost(t *testing.T) {
    // Setup: use secondary DB connection
    primaryDB := GetPrimaryDB()
    secondaryDB := GetSecondaryDB()

    // Block primary DB
    BlockConnection(primaryDB)

    // Requests should fail gracefully
    resp, err := CreateSession(...)
    assert.Error(t, err)
    assert.Equal(t, 503, resp.StatusCode)

    // Unblock primary
    UnblockConnection(primaryDB)

    // Should recover
    resp, err = CreateSession(...)
    assert.NoError(t, err)
    assert.Equal(t, 201, resp.StatusCode)
}

// Chaos: AAA server unreachable
func TestChaos_AAAServerUnreachable(t *testing.T) {
    // Start session
    session := StartSession("5-001", "1:000001")

    // Block AAA server
    BlockAAAServer()

    // Wait for circuit breaker
    time.Sleep(5 * time.Second)

    // Requests should fail with 502
    resp, err := ConfirmSession(session.AuthCtxId, eapMsg)
    assert.Error(t, err)
    assert.Equal(t, 502, resp.StatusCode)

    // Unblock AAA server
    UnblockAAAServer()

    // Circuit breaker should close after recovery
    time.Sleep(35 * time.Second)  // Recovery timeout

    resp, err = ConfirmSession(session.AuthCtxId, eapMsg)
    assert.NoError(t, err)
}
```

---

## 9. Acceptance Criteria

| # | Criteria | Test Coverage |
|---|----------|--------------|
| AC1 | All TS 29.526 API operations tested | 30 test cases |
| AC2 | All TS 23.502 procedure flows tested | 15 test cases |
| AC3 | RFC 3579 RADIUS EAP conformance | 10 test cases |
| AC4 | RFC 5216 EAP-TLS MSK derivation | 10 test cases |
| AC5 | 50K concurrent sessions sustained | k6 load test |
| AC6 | P99 latency <500ms at 1000 RPS | k6 load test |
| AC7 | Graceful degradation on pod kill | Chaos test |
| AC8 | Circuit breaker recovers after AAA failure | Chaos test |
| AC9 | Database failover <30s RTO | Chaos test |
| AC10 | >80% code coverage (unit tests) | Coverage report |
