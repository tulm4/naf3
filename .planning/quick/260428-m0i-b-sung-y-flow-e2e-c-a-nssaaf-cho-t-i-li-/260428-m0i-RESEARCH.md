# Quick Research: NSSAAF AIW E2E Test Flows

**Researched:** 2026-04-28
**Focus:** Supplement `docs/design/24_test_strategy.md` with AIW (Nnssaaf_AIW) E2E test flows
**Confidence:** HIGH — based on existing NSSAA patterns and 03_aiw_api.md spec

## Summary

The current `24_test_strategy.md` §5 E2E section only covers NSSAA flows (N58 interface, AMF consumer, GPSI). AIW (Nnssaaf_AIW, N60 interface, AUSF consumer) has distinct differences requiring separate E2E test cases. The key additions needed are:

1. AIW basic flow (AUSF → NSSAAF → AAA-S)
2. MSK extraction and verification (RFC 5216)
3. PVS Info extraction (AIW-specific output)
4. AIW-specific error cases (AAA reject, invalid SUPI, etc.)
5. Explicit documentation that re-auth/revocation is NOT in AIW scope

**Recommendation:** Add new §5.3 to `24_test_strategy.md` titled "AIW E2E Test Cases" following the same 3-component model as §5.2.

---

## 1. AIW vs NSSAA E2E Differences

| Aspect | NSSAA (§5.2) | AIW (to add) |
|--------|--------------|--------------|
| Consumer NF | AMF (gRPC mock) | AUSF (HTTP mock) |
| Subscriber ID | GPSI (`5-{digits}`) | SUPI (`imsi-{digits}`) |
| Interface | N58 | N60 |
| MSK output | Not in scope | **Required** (64-byte, base64) |
| PVS Info | Not in scope | **Required** (optional array) |
| Re-auth flow | TestE2E_NSSAA_Reauth_FromAAA | **NOT APPLICABLE** |
| Revocation flow | TestE2E_NSSAA_Revocation | **NOT APPLICABLE** |
| Error: AAA reject | 403 | 403 (same) |
| Error: AAA timeout | 504 | 504 (same) |
| Error: invalid ID | 400 GPSI format | 400 SUPI format |

Source: 03_aiw_api.md §2 comparison table

---

## 2. AIW E2E Test Cases to Add

### 2.1 Basic AIW Authentication Flow

**Purpose:** Verify AUSF can authenticate SUPI via NSSAAF to AAA-S (EAP-TLS multi-round).

**Components started:**
- HTTP Gateway (port 8443)
- Biz Pod
- AAA Gateway (UDP 1812)
- AUSF Mock (replaces AMF Mock)
- AAA Simulator (EAP_TLS mode)

**Test steps:**
1. AUSF Mock sends POST `/nnssaaf-aiw/v1/authentications` with SUPI + EAP Identity Response
2. Verify 201 Created, `authCtxId` returned, `eapMessage` present (if challenge)
3. Verify AAA Gateway received RADIUS Access-Request
4. If multi-round: PUT `/authentications/{authCtxId}` with EAP response from UE
5. On EAP Success: verify `authResult: "EAP_SUCCESS"`, `msk` is 64-byte base64, `pvsInfo` present (if server exposed)
6. Verify database state: `aiw_auth_sessions` table updated

**Assertions:**
- HTTP 201/200 responses
- `authCtxId` is UUIDv7 format
- `msk` is base64, decodes to 64 bytes
- `pvsInfo` matches what AAA Simulator configured
- `supportedFeatures` echoed from request to response

### 2.2 AIW MSK Extraction and Verification

**Purpose:** Verify MSK is correctly extracted from RADIUS Access-Accept and returned to AUSF per RFC 5216.

**AAA Simulator config:** EAP_TLS mode, AuthResult: SUCCESS, MSK: generated 64-byte

**Test steps:**
1. Send AIW authentication request
2. Complete EAP-TLS handshake (multi-round)
3. Extract `msk` from final response
4. Decode base64, verify length = 64 bytes
5. Verify MSK upper 32 bytes ≠ lower 32 bytes (MSK/EMSK split per RFC 5216)

**Assertions:**
- `msk` field present in final response
- `msk` decodes to exactly 64 bytes
- `msk[:32]` ≠ `msk[32:]`

### 2.3 AIW PVS Info Extraction

**Purpose:** Verify PVS (Privacy Violating Servers) information is returned when AAA-S indicates servers that saw UE identity.

**AAA Simulator config:** EAP_TLS mode, pvsInfo: `[{serverType: "PROSE", serverId: "pvs-001"}]`

**Test steps:**
1. Send AIW authentication request
2. Complete EAP-TLS handshake
3. Verify `pvsInfo` in final response matches AAA Simulator config
4. Verify `pvsInfo` is null when AAA-S does not include it

**Assertions:**
- `pvsInfo` matches configured server list
- `serverType` is valid enum value (PROSE, LOCATION, OTHER)
- `serverId` is non-empty string

### 2.4 AIW Error: AAA-S Access-Reject

**Purpose:** Verify NSSAAF correctly propagates AAA-S rejection as EAP_FAILURE.

**AAA Simulator config:** AuthResult: REJECT, Reply-Message: "Certificate revoked"

**Test steps:**
1. Send AIW authentication request
2. Complete EAP-TLS handshake up to final round
3. AAA Simulator returns Access-Reject
4. Verify final response: `authResult: "EAP_FAILURE"`, `msk: null`, HTTP 200 (not 403 — failure is in body, not HTTP status)

**Assertions:**
- HTTP 200 (terminal state reached)
- `authResult: "EAP_FAILURE"`
- `msk: null`
- `eapMessage: null`

### 2.5 AIW Error: Invalid SUPI Format

**Purpose:** Verify request validation rejects malformed SUPI.

**Test steps:**
1. Send POST with `supi: "invalid-supi"`
2. Verify HTTP 400
3. Verify ProblemDetails `cause: "INVALID_SUPI"` and `detail` mentions format requirement

**Assertions:**
- HTTP 400
- ProblemDetails.cause = "INVALID_SUPI"
- `detail` contains validation error message

### 2.6 AIW Error: AAA-S Timeout

**Purpose:** Verify NSSAAF handles AAA-S non-response within round timeout.

**AAA Simulator config:** Mode: TIMEOUT (no response)

**Test steps:**
1. Send AIW authentication request
2. AAA Gateway retries 3 times with backoff (0ms, 100ms, 200ms)
3. After 3 retries exhausted, verify HTTP 504 Gateway Timeout
4. Verify session state in database is marked as failed

**Assertions:**
- HTTP 504
- ProblemDetails.cause = "AAA_TIMEOUT"
- Session status in DB = EAP_FAILURE or TIMEOUT

### 2.7 AIW Error: AuthContext Expired

**Purpose:** Verify PUT fails when session TTL has expired.

**Test steps:**
1. Create AIW session (POST)
2. Wait for session TTL to expire (or use test config to shorten TTL)
3. Send PUT with valid `authCtxId`
4. Verify HTTP 410 Gone
5. Verify ProblemDetails `cause: "AUTH_CONTEXT_EXPIRED"`

**Assertions:**
- HTTP 410
- ProblemDetails.cause = "AUTH_CONTEXT_EXPIRED"

### 2.8 AIW Error: AuthAlreadyCompleted

**Purpose:** Verify PUT rejects when session already reached terminal state.

**Test steps:**
1. Create and complete AIW session (EAP_SUCCESS)
2. Send second PUT with same `authCtxId` and valid EAP message
3. Verify HTTP 409 Conflict
4. Verify ProblemDetails `cause: "AUTH_ALREADY_COMPLETED"`

**Assertions:**
- HTTP 409
- ProblemDetails.cause = "AUTH_ALREADY_COMPLETED"

---

## 3. 3GPP Conformance Test Mapping (AIW)

| Test Case | Spec Reference | What to Test |
|-----------|---------------|--------------|
| AIW-01 | TS 29.526 §7.3.2.2 | CreateAuthenticationContext — valid SUPI |
| AIW-02 | TS 29.526 §7.3.2.2 | CreateAuthenticationContext — missing SUPI → 400 |
| AIW-03 | TS 29.526 §7.3.2.2 | CreateAuthenticationContext — invalid SUPI regex → 400 |
| AIW-04 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — multi-round EAP continuation |
| AIW-05 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — EAP_SUCCESS, MSK present |
| AIW-06 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — EAP_FAILURE |
| AIW-07 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — PVS Info present |
| AIW-08 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — authCtx expired → 410 |
| AIW-09 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — already completed → 409 |
| AIW-10 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — AAA timeout → 504 |
| AIW-11 | TS 29.526 §7.3.2.3 | ConfirmAuthentication — supi mismatch → 400 |
| AIW-12 | RFC 5216 §2.1.4 | MSK is 64 bytes, MSK ≠ EMSK |
| AIW-13 | TS 33.501 §I.2.2.2 | MSK returned to AUSF for SNPN key derivation |

Source: TS 29.526 v18.7.0 §7.3, 03_aiw_api.md §8 acceptance criteria

---

## 4. Go Test Function Structure (3-Component Model)

The AIW E2E tests follow the same 3-component model as NSSAA (§5.2), with AUSF Mock replacing AMF Mock:

```go
// AIW E2E test structure (mirrors §5.2 TestE2E_NSSAA_Flow)
func TestE2E_AIW_Flow(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping E2E in short mode")
    }

    // Start all 3 components
    httpGW := StartHTTPGateway(&HTTPGatewayConfig{Port: 8443})
    defer httpGW.Stop()

    bizPod := StartBizPod(&BizPodConfig{
        DBURL:       testDBURL,
        RedisURL:    testRedisURL,
        AAAProxyURL: "http://localhost:9443",
    })
    defer bizPod.Stop()

    aaaGW := StartAAAGateway(&AAAGatewayConfig{
        Port:      1812,
        BizPodURL: "http://localhost:8080",
    })
    defer aaaGW.Stop()

    // Mock external services — AUSF replaces AMF
    ausfMock := StartAUSFMock()
    defer ausfMock.Stop()

    aaaSim := StartAAASimulator(&AAASimulatorConfig{
        Mode:       "EAP_TLS",
        AuthResult: "SUCCESS",
        MSK:        generateTestMSK(), // 64 bytes
        PVSInfo: []PVSInfo{
            {ServerType: "PROSE", ServerId: "pvs-001"},
        },
    })
    defer aaaSim.Stop()

    // AUSF initiates AIW authentication
    ctx := &Nnssaaf_AIW_Authenticate_Request{
        Supi:    "imsi-208046000000001",
        EapIdRsp: EncodeEAPIdentityResponse("user@example.com"),
    }

    resp1, err := ausfMock.Authenticate(ctx)
    require.NoError(t, err)

    // Verify routing through all 3 components
    assert.NotEmpty(t, resp1.AuthCtxId)
    assert.NotEmpty(t, resp1.EapMessage)
    assert.True(t, aaaGW.ReceivedRequest())

    // Multi-round continuation (if needed)
    // ... PUT /authentications/{authCtxId} loop ...

    // Final verification: MSK extracted
    finalResp, err := ausfMock.AuthenticateComplete(resp1.AuthCtxId)
    require.NoError(t, err)
    assert.Equal(t, "EAP_SUCCESS", finalResp.AuthResult)
    assert.NotEmpty(t, finalResp.Msk)
    mskBytes, _ := base64.StdEncoding.DecodeString(finalResp.Msk)
    assert.Len(t, mskBytes, 64)
}
```

**Key differences from NSSAA E2E:**
- `StartAUSFMock()` replaces `StartAMFMock()`
- `Nnssaaf_AIW_Authenticate_Request` uses `supi` not `gpsi`
- Response struct includes `msk` and `pvsInfo` fields
- No re-auth or revocation tests

---

## 5. What NOT to Include (Out of Scope)

Per 03_aiw_api.md §2:

| Flow | NSSAA | AIW | Reason |
|------|-------|-----|--------|
| Re-authentication | ✅ TestE2E_NSSAA_Reauth_FromAAA | ❌ Not in scope | AIW has no SLICE_RE_AUTH |
| Revocation | ✅ TestE2E_NSSAA_Revocation | ❌ Not in scope | AIW has no SLICE_REVOCATION |
| GPSI usage | ✅ | ❌ SUPI only | AIW uses SUPI, not GPSI |
| N58 interface | ✅ | ❌ N60 only | AIW is N60 (AUSF-NSSAAF) |

Source: 03_aiw_api.md AC8

---

## 6. Recommended Section Structure for 24_test_strategy.md

```
## 5. End-to-End Tests

### 5.1 E2E Test Architecture
   [existing — keep as-is]

### 5.2 NSSAA E2E Test Cases (N58 / AMF / GPSI)
   [existing — keep as-is]

### 5.3 AIW E2E Test Cases (N60 / AUSF / SUPI)     ← NEW
   [add all 8 test cases above]

### 5.4 3GPP Conformance Tests
   [existing — extend with AIW-01 through AIW-13]
```

---

## 7. Open Questions

1. **AUSF Mock implementation:** Does the existing mock framework have an AUSF mock, or does it need to be added? Based on Phase 4 context (internal/ausf/ client exists), likely already implemented.

2. **AAA Simulator PVS Info config:** Does the AAA Simulator support configuring PVS Info for E2E tests? May need extension if not already supported.

3. **MSK VSA encoding:** Which Vendor-Id and Vendor-Type does the AAA Simulator use for MSK VSA? Need to verify against 03_aiw_api.md §4.2 implementation.

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — follows existing §5.2 patterns exactly
- Architecture: HIGH — 3-component model already proven
- Pitfalls: MEDIUM — AUSF mock and PVS Info simulator config unverified

**Research date:** 2026-04-28
**Valid until:** 30 days (AIW API spec is stable, 3GPP TS 29.526 v18.7.0)
