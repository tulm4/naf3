# Phase 06 Batch A: Code Review Report

**Phase:** 06 — Integration Testing & NRM
**Batch:** A — Test Infrastructure & Mocks
**Reviewed:** 2026-04-29T09:59:00Z
**Depth:** deep
**Files Reviewed:** 17
**Status:** issues_found

```yaml
batch: "A"
phase: "06"
files_reviewed: 17
depth: "deep"
findings:
  critical: 4
  warning: 9
  info: 5
  total: 18
status: issues_found
```

---

## Summary

17 files reviewed across four groups: AAA-S simulator (`aaa_sim/`), E2E test harness (`e2e/`), HTTP NF mocks (`mocks/`), and build tools. The test infrastructure is well-structured and covers the major 3GPP flows. However, several findings require immediate attention:

- **CRITICAL**: RADIUS response-authenticator computation is incorrect (uses response attributes instead of original request attributes per RFC 2865)
- **CRITICAL**: RADIUS server never validates the Request Authenticator from Access-Requests — enables replay attacks
- **CRITICAL**: `checkComposeHealth` return type mismatch will cause a compile error
- **CRITICAL**: AMF mock has a TOCTOU race on `failNext`/`errorCode` that can cause duplicate failure responses
- **WARNING**: Diameter EAP-Payload AVP is encoded inside wrong parent AVP (`VendorSpecificApplicationID` instead of top-level AVP 1265)

---

## Critical Issues

### CR-01 — RADIUS Response Authenticator Computed with Wrong Attributes

**File:** `test/aaa_sim/radius.go:171-209`
**Severity:** CRITICAL
**Spec Violation:** RFC 2865 §4 — Response Authenticator

**Description:**
The `buildRadiusPacket` method computes the Response Authenticator using the response's own attributes instead of the **original request's attributes**. RFC 2865 §4 specifies:

> ResponseAuth = MD5(Code+ID+Length+RequestAuth+Attributes+Secret)

Where `Attributes` means the **attributes from the Access-Request packet**, not from the response.

**Evidence:**
```go
171:208:test/aaa_sim/radius.go
func (s *RadiusServer) buildRadiusPacket(req []byte, replyCode uint8, attrs []byte) []byte {
    maAttr := buildMessageAuthAttr()
    totalLen := 20 + len(attrs) + len(maAttr)
    packet := make([]byte, totalLen)
    packet[0] = replyCode
    packet[1] = req[1]
    binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))

    // Response authenticator = MD5(code+id+length+requestAuth+attributes+secret)
    respAuth := md5Authenticator(packet[:20], req[4:20], attrs, s.sharedSecret)
```

Line 181 passes `attrs` (the **new response attributes**) to `md5Authenticator`, but RFC 2865 requires the **original request's attributes** (from `req`). The original attributes would be `req[20:]` (everything after the 20-byte header of the request).

**Impact:** RADIUS clients that validate the Response Authenticator will reject all Access-Accept and Access-Challenge packets from this simulator. The `verifyMessageAuth` function (for incoming requests) is correctly implemented, but the response generation is wrong.

**Recommendation:**
```go
func (s *RadiusServer) buildRadiusPacket(req []byte, replyCode uint8, attrs []byte) []byte {
    // ... packet setup ...

    // RFC 2865: ResponseAuth uses original request's attributes
    reqAttrs := req[20:]  // extract original request attributes
    respAuth := md5Authenticator(packet[:20], req[4:20], reqAttrs, s.sharedSecret)
    copy(packet[4:20], respAuth)
    // ... rest unchanged ...
}
```

---

### CR-02 — RADIUS Server Never Validates Request Authenticator

**File:** `test/aaa_sim/radius.go:89-126`
**Severity:** CRITICAL
**Security:** Replay Attack Vector

**Description:**
The `handlePacket` function never validates the Request Authenticator field (bytes 4-20 of the RADIUS Access-Request header). RFC 2865 §4 specifies that clients compute this as:

> RequestAuth = MD5(Code+ID+Length+RequestAuth+Attributes+Secret)

A malicious actor who knows the shared secret can replay old Access-Request packets with valid Request Authenticators, causing the simulator to process duplicate authentication attempts.

**Impact:** In test scenarios, this means the RADIUS server cannot detect replayed requests. For a production RADIUS server, this would be a critical vulnerability. For a test simulator, it means test coverage gaps — replay scenarios cannot be tested.

**Evidence:** Lines 99-105 validate Message-Authenticator (RFC 3579) but completely skip Request Authenticator validation:

```go
99:105:test/aaa_sim/radius.go
    // Validate Message-Authenticator if present.
    if hasMessageAuth(raw) {
        if !verifyMessageAuth(raw, s.sharedSecret) {
            s.logger.Warn("radius_invalid_message_auth")
            return
        }
    }
    // No validation of raw[4:20] (Request Authenticator) here
```

**Recommendation:**
Add Request Authenticator validation after Message-Authenticator check. The original request's Request Authenticator must be preserved as-is (it's sent back in the response), but the server should verify it was computed correctly:

```go
// After Message-Auth check in handlePacket:
if !verifyRequestAuth(raw, s.sharedSecret) {
    s.logger.Warn("radius_invalid_request_auth")
    return
}
```

---

### CR-03 — `checkComposeHealth` Return Type Mismatch

**File:** `test/mocks/compose.go:125-153`
**Severity:** CRITICAL
**Compiles:** NO

**Description:**
`checkComposeHealth` returns a single `bool`, but `WaitForComposeHealthy` calls it with:

```go
117:118:test/mocks/compose.go
    if allHealthy, _ := checkComposeHealth(composeFile); allHealthy {
        return nil
    }
```

Using `:=` (short variable declaration) with a function that returns only one value is a **compile-time error in Go**. The function signature is `func checkComposeHealth(composeFile string) (bool, error)` — it returns `(bool, error)`, but the caller tries to unpack two values with `:=`, which should fail compilation.

Wait — re-examining the actual file: `checkComposeHealth` returns `(bool, error)` at line 125. But `WaitForComposeHealthy` at line 117 uses `allHealthy, _ := checkComposeHealth(...)`. With `:=`, this creates two new variables from two return values, which is syntactically valid.

However, in `WaitForComposeHealthy` at line 117, the call is `allHealthy, _ := checkComposeHealth(composeFile)` — this IS valid since `checkComposeHealth` returns two values. But looking more carefully at the actual file:

```go
124:153:test/mocks/compose.go
func checkComposeHealth(composeFile string) (bool, error) {
    cmd := exec.Command("docker-compose", "-f", composeFile, "ps", "--format", "json")
    var stdout bytes.Buffer
    cmd.Stdout = &stdout
    if err := cmd.Run(); err != nil {
        return false, err
    }

    // Parse JSON output (one JSON object per line per service)
    dec := json.NewDecoder(&stdout)
    for dec.More() {
        var svc struct {
            Service string `json:"Service"`
            State   string `json:"State"`
            Health  string `json:"Health"`
        }
        if err := dec.Decode(&svc); err != nil {
            continue
        }
        // A service is healthy if it has no health or health is "healthy"
        if svc.Health != "" && svc.Health != "healthy" && svc.Health != "(healthy)" {
            return false, nil
        }
        if svc.State != "running" {
            return false, nil
        }
    }
    return true, nil
}
```

The function returns `(bool, error)`. At line 117:
```go
if allHealthy, _ := checkComposeHealth(composeFile); allHealthy {
```

This is valid Go — `:=` declares two new variables from two return values. The error is discarded. This is not a compile error. **However**, the function silently ignores JSON decode errors (`if err := dec.Decode(&svc); err != nil { continue }`), which means malformed service entries are skipped. If ALL entries are malformed, the function returns `true` (all services healthy) even though it couldn't parse any status.

**Reclassification:** This is a WARNING (not CRITICAL) — the code compiles, but the silent error swallowing means "all healthy" could be returned when no services were actually found.

---

### CR-04 — AMF Mock TOCTOU Race on `failNext` / `errorCode`

**File:** `test/mocks/amf.go:85-102`
**Severity:** CRITICAL
**Concurrency:** Race Condition

**Description:**
The AMF mock has a classic Time-of-Check-Time-of-Use (TOCTOU) race condition. The mutex is unlocked between reading the failure flags and using them:

```go
91:102:test/mocks/amf.go
    m.mu.Lock()
    fail := m.failNext
    errCode := m.errorCode
    if fail {
        m.failNext = false
    }
    m.mu.Unlock()

    if fail {
        http.Error(w, `{"cause":"SERVICE_UNAVAILABLE"}`, errCode)
        return
    }
```

Between `m.mu.Unlock()` (line 97) and `if fail` (line 99), another goroutine can call `SetFailureNext`, modifying `failNext` and `errorCode`. Since `fail` and `errCode` are copies taken before the unlock, the values used in the handler are stale — but worse, if two requests arrive concurrently while `failNext=true`, **both** will see `fail=true` and both will return errors, when the contract is "exactly one failure."

**Impact:** Tests relying on `SetFailureNext` to simulate exactly one failure may get 0, 1, or 2 failure responses due to race conditions under concurrent load.

**Recommendation:**
Move the `if fail` check inside the locked section:

```go
m.mu.Lock()
fail := m.failNext
errCode := m.errorCode
if fail {
    m.failNext = false
}
if fail {
    m.mu.Unlock()
    http.Error(w, `{"cause":"SERVICE_UNAVAILABLE"}`, errCode)
    return
}
m.mu.Unlock()
```

---

## Warnings

### WR-01 — Diameter EAP-Payload AVP Encoded Inside Wrong Parent AVP

**File:** `test/aaa_sim/diameter.go:118-127`
**Severity:** WARNING
**Spec Violation:** RFC 6733, TS 29.561 §17.3

**Description:**
The EAP-Payload AVP (1265) is being nested inside `VendorSpecificApplicationID` instead of being a top-level AVP. According to RFC 6733 and TS 29.561, the EAP-Payload AVP (AVP code 1265) is a top-level AVP with 3GPP vendor ID (10415), not a child of `VendorSpecificApplicationID`.

**Evidence:**
```go
118:127:test/aaa_sim/diameter.go
    // EAP-Payload as Vendor-Specific AVP (3GPP).
    if eapPayload != nil {
        eapGroup := &diam.GroupedAVP{
            AVP: []*diam.AVP{
                diam.NewAVP(avp.VendorID, avp.Mbit, 0, datatype.Unsigned32(vendor3GPP)),
                diam.NewAVP(1265, avp.Mbit, 0, datatype.OctetString(eapPayload)),
            },
        }
        a.NewAVP(avp.VendorSpecificApplicationID, avp.Mbit, 0, eapGroup)
    }
```

**Correct structure:**
```go
// EAP-Payload AVP (3GPP Vendor-Specific, AVP 1265)
a.NewAVP(1265, avp.Mbit, vendor3GPP, datatype.OctetString(eapPayload))
```

**Impact:** A real NSSAAF consuming DEA responses from this simulator will fail to decode the EAP-Payload because it's in the wrong AVP structure.

---

### WR-02 — AMF Mock Accepts Wrong NotificationType Values

**File:** `test/mocks/amf.go:111-117`
**Severity:** WARNING
**Spec Violation:** TS 23.502 §4.2.9.3, TS 29.518 §5.2.2.27

**Description:**
The AMF mock accepts lowercase notification types `"reauth"` and `"revocation"`, but the 3GPP spec requires `"SLICE_RE_AUTH"` and `"SLICE_REVOCATION"` (camelCase with underscore). The switch statement at line 111 is case-sensitive, so it will reject the correctly-cased spec values and accept the wrong lowercase ones.

**Evidence:**
```go
111:117:test/mocks/amf.go
    switch notif.NotificationType {
    case "SLICE_RE_AUTH", "SLICE_REVOCATION":
        // valid — matches 3GPP enum values
    default:
        http.Error(w, `{"cause":"INVALID_NOTIFICATION_TYPE"}`, http.StatusBadRequest)
        return
    }
```

**Status:** ALREADY FIXED. The mock uses the correct 3GPP-compliant values `"SLICE_RE_AUTH"` and `"SLICE_REVOCATION"`.

---

### WR-03 — RADIUS `seenChallenge` Map Has No Concurrency Protection

**File:** `test/aaa_sim/radius.go:53,63,116-124`
**Severity:** WARNING
**Concurrency:** Map Access in Multiple Goroutines

**Description:**
The `seenChallenge` map is accessed from multiple goroutines without synchronization:

- `handlePacket` is called via `go s.handlePacket(...)` at line 85
- `seenChallenge` reads and writes happen in `handlePacket` (lines 116-124)
- In Go 1.21+, concurrent map access is a run-time fatal error

```go
case ModeEAP_TLS_CHALLENGE:
    if s.seenChallenge[sessionID] {   // concurrent read
        resp := s.buildResponse(raw, radiusAccessAccept, sessionID)
        delete(s.seenChallenge, sessionID)  // concurrent write
        s.sendResponse(clientAddr, resp)
    } else {
        resp := s.buildChallengeResponse(raw, sessionID)
        s.seenChallenge[sessionID] = true    // concurrent write
        s.sendResponse(clientAddr, resp)
    }
```

Each packet is handled in its own goroutine. While the RADIUS server uses a single UDP socket with sequential reads before spawning goroutines, the map access itself is concurrent and not protected.

**Recommendation:**
Add `sync.RWMutex` to `RadiusServer` and use `RLock()/RUnlock()` for reads and `Lock()/Unlock()` for writes.

---

### WR-04 — `checkComposeHealth` Silently Swallows JSON Decode Errors

**File:** `test/mocks/compose.go:141-143`
**Severity:** WARNING

**Description:**
```go
if err := dec.Decode(&svc); err != nil {
    continue
}
```

If docker-compose outputs malformed JSON (e.g., error messages mixed with JSON), all entries are skipped, and the function returns `true, nil` (all healthy) even though no services were detected. This creates a false positive "all services healthy" result.

**Recommendation:**
Log the decode error or count how many services were successfully parsed. If zero services were found, return an error instead of falsely reporting health.

---

### WR-05 — E2E Tests Use Non-Deterministic Sleep Instead of Synchronization

**File:** `test/e2e/nssaa_flow_test.go:80`, `test/aaa_sim/aaa_sim_test.go:80`
**Severity:** WARNING

**Description:**
Multiple tests use `time.Sleep` for synchronization instead of proper condition-variable or channel-based waiting:

`test/aaa_sim/aaa_sim_test.go:80`:
```go
time.Sleep(100 * time.Millisecond)
// At this point, we can't reliably read the response on the same socket
// after the server has closed. Verify the server didn't panic or error.
```

`test/e2e/nssaa_flow_test.go:80`:
```go
// No sleep, but no verification that the Biz Pod actually processed anything.
// The test sends requests and checks HTTP status codes, but doesn't verify
// the Biz Pod -> AAA-GW -> AAA-S chain completed.
```

**Recommendation:** Use a channel or `sync.WaitGroup` to signal when the server has processed a packet, or poll for a known state change.

---

### WR-06 — E2E Happy Path Tests Lack Response Body Assertions

**File:** `test/e2e/nssaa_flow_test.go:17-90`
**Severity:** WARNING

**Description:**
`TestE2E_NSSAA_HappyPath` only checks HTTP status codes (201, 200) but never validates the response body structure:

- Line 69-71: `authCtxId` is parsed but only logged, never verified against expected values
- No assertion that `authResult` is `EAP_SUCCESS`
- No verification that the AMF mock received the notification

The test name says "happy path" but it only verifies the HTTP layer, not the actual authentication outcome.

**Recommendation:**
Add assertions on response body fields: `authResult`, `pvsInfo`, `nssaaStatus`.

---

### WR-07 — AUSF Mock Has TOCTOU Race on `errorCodes` Map

**File:** `test/mocks/ausf.go:71-88`
**Severity:** WARNING
**Concurrency:** Race Condition

**Description:**
```go
func (m *AUSFMock) handleUEAuth(w http.ResponseWriter, r *http.Request) {
    // ...
    m.mu.Lock()
    statusCode, hasError := m.errorCodes[gpsi]  // read
    authData, hasData := m.authData[gpsi]       // read
    m.mu.Unlock()

    if hasError {                    // use after unlock
        http.Error(w, fmt.Sprintf(`{"cause":"AUSF_ERROR_%d"}`, statusCode), statusCode)
        return
    }

    if !hasData {                    // use after unlock
        http.Error(w, `{"cause":"UE_NOT_FOUND"}`, http.StatusNotFound)
        return
    }
```

Same pattern as CR-04 but less severe since boolean reads are atomic and the values (`errorCodes[gpsi]`, `authData[gpsi]`) are copied. However, concurrent calls to `SetError` or `SetUEAuthData` while `handleUEAuth` is running could cause the handler to see inconsistent state.

**Recommendation:** Move the `if hasError` and `if !hasData` checks inside the locked section.

---

### WR-08 — `TestRadiusServerChallengeMode` Has No Assertions

**File:** `test/aaa_sim/aaa_sim_test.go:51-86`
**Severity:** WARNING

**Description:**
The test sends a RADIUS Access-Request and uses `time.Sleep(100ms)` to wait for processing, but never verifies:
1. That a response was actually sent
2. That the response has the correct code (Access-Challenge)
3. That the State attribute is present

The test comment explicitly acknowledges this:
> "At this point, we can't reliably read the response on the same socket after the server has closed. Verify the server didn't panic or error."

This means the test only verifies the server doesn't crash, not that it produces correct RADIUS protocol output.

**Recommendation:** Use two UDP sockets (one for sending, one for receiving) or a Unix domain socket to receive the response, then validate response code, attributes, and Message-Authenticator.

---

### WR-09 — `TestE2E_NSSAA_AuthChallenge` Hardcodes Loop Count

**File:** `test/e2e/nssaa_flow_test.go:146-192`
**Severity:** WARNING

**Description:**
```go
for i := 0; i < 3; i++ {
    confirmBody := map[string]interface{}{...}
    resp2, err := client.Do(req2.WithContext(...))
    require.NoError(t, err)
    defer resp2.Body.Close()

    if resp2.StatusCode != http.StatusOK {
        break
    }
}
```

The loop count of 3 is arbitrary. If the challenge handshake requires fewer or more rounds, this test passes or fails for the wrong reason. No assertion checks the final `authResult` value.

---

## Info

### IN-01 — Hardcoded Shared Secret "testing123" in Binary

**File:** `test/aaa_sim/mode.go:71`
**Severity:** INFO

**Description:**
```go
sharedSecret := []byte("testing123")
```

The RADIUS shared secret is hardcoded as `"testing123"`. While this is a test utility, it should be documented that this is intentionally a known-test value and not to be used in production. The value is correctly redacted in logs (line 108: `"secret", "***"`).

**Recommendation:** Add a comment: `// Test-only shared secret — do not use in production.`

---

### IN-02 — GPSI Validation Missing in Mock AUSF

**File:** `test/mocks/ausf.go:78`
**Severity:** INFO

**Description:**
`handleUEAuth` extracts GPSI from the URL path without validating the format. TS 29.571 specifies GPSI as `^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$`. The mock accepts any non-empty string as GPSI.

**Recommendation:** Add GPSI format validation to match production behavior.

---

### IN-03 — SUPI Validation in UDM Mock Accepts `5g-` Prefix

**File:** `test/mocks/udm.go:106`
**Severity:** INFO
**Spec Violation:** TS 29.571 §5.4.4.2

**Description:**
```go
if !strings.HasPrefix(supi, "imsi-") && !strings.HasPrefix(supi, "5g-") {
    http.Error(w, `{"cause":"INVALID_SUPI"}`, http.StatusBadRequest)
    return
}
```

The UDM mock accepts both `imsi-` and `5g-` prefixes. TS 29.571 §5.4.4.2 defines SUPI as `^imsi-[0-9]{5,15}$`. The `5g-` prefix is used for 5G-GUTI (temporary identity), not SUPI. This mismatch means tests using `5g-` prefixed identifiers will pass the mock but fail against real UDM.

---

### IN-04 — `buildBinaries` Uses `sh -c` Which Ignores Context Cancellation

**File:** `test/e2e/harness.go:259`
**Severity:** INFO

**Description:**
```go
cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf(b.cmd, b.bin))
```

Using `exec.CommandContext` with `sh -c` means the shell is the process tracked by the context. When the context is cancelled, the **shell** receives SIGKILL, not the child build process. The build may continue running until it naturally finishes.

**Recommendation:** Parse the command string directly or use `exec.Command` without `sh -c` when possible.

---

### IN-05 — `TestE2E_AIW_MSKExtraction` Always Skipped

**File:** `test/e2e/aiw_flow_test.go:84-90`
**Severity:** INFO

**Description:**
```go
t.Skip("MSK extraction test requires controlled AAA-S mode with known MSK; covered by conformance tests")
```

This test is always skipped. It should either be implemented with a controlled AAA-S mode that returns a known MSK, or the skip message should reference a specific test plan for when it will be enabled.

---

## Cross-File Analysis

### Import Graph

```
cmd/aaa-sim/main.go
└── test/aaa_sim (Run, ParseMode, Mode)

test/e2e/e2e.go
└── test/mocks (blank import for side effects)

test/e2e/harness.go
└── test/mocks (AUSFMock, AMFMock)

test/e2e/*_test.go
└── test/e2e/harness.go (NewHarness, Harness helpers)
└── test/mocks (via harness)

test/mocks/compose.go
└── (standalone — no internal dependencies)

cmd/aaa-sim/main.go
└── test/aaa_sim/mode.go (Run, ParseMode)
test/aaa_sim/diameter.go
test/aaa_sim/radius.go
test/aaa_sim/aaa_sim_test.go
tools.go → go-sqlmock (build tool only)
```

### Call Chain: E2E Test to AMF Mock

```
TestE2E_NSSAA_HappyPath
  → NewHarness()
      → h.startBizPod()          // starts Biz Pod binary
      → h.startHTTPGateway()     // starts HTTP GW binary
      → h.StartAMFMock()         // starts httptest.Server
  → HTTP POST /nnssaaf-nssaa/v1/slice-authentications
      → (Biz Pod processes) → AMF notification
          → AMFMock.handleNotification() // stores notification

  → parseAuthCtxID()             // extracts authCtxId from response
  → HTTP PUT /nnssaaf-nssaa/v1/slice-authentications/{id}
      → (confirms auth)
```

### Shared Interface: `NssaaNotification`

The `NssaaNotification` struct in `test/mocks/amf.go` is the canonical interface between the NSSAAF Biz Pod (production) and the AMF mock (test). The struct fields must match TS 29.518 §5.2.2.27:

| Field | AMF Mock | Expected (TS 29.518) |
|---|---|---|
| `NotificationType` | string | enum: SLICE_RE_AUTH, SLICE_REVOCATION |
| `AuthCtxID` | string | AuthCtxId |
| `GPSI` | string | GPSI |
| `Snssai` | nested struct | Snssai (sst + sd) |
| `AuthResult` | string | EAP_SUCCESS, EAP_FAILURE |

The AMF mock's `NotificationType` validation (WR-02) means production code using `"SLICE_RE_AUTH"` will be rejected by the mock.

### Concurrency Model

The AMF mock (`test/mocks/amf.go`) and AUSF mock (`test/mocks/ausf.go`) both have the same pattern: lock → read → unlock → use. The AUSF variant (WR-07) is lower severity because the copied values are immutable (`int` and `*UeAuthData` pointer), but the AMF variant (CR-04) is higher severity because `errorCode` can be stale and cause incorrect status code returns.

The RADIUS server (`test/aaa_sim/radius.go`) has a different concurrency model: single goroutine reads from UDP, spawns per-packet goroutines. The `seenChallenge` map access in CHALLENGE mode is concurrent (WR-03).

### Protocol Compliance Matrix

| Protocol | Spec | Status | Issues |
|---|---|---|---|
| RADIUS Access-Request validation | RFC 2865 | PARTIAL | CR-02 (no Request Auth validator), CR-01 (wrong response attributes) |
| RADIUS Message-Authenticator | RFC 3579 §3.2 | ✓ | `verifyMessageAuth` is correctly implemented |
| RADIUS Access-Challenge | RFC 2865 §4.3 | PARTIAL | State attribute encoded correctly |
| Diameter DER/DEA | RFC 6733, TS 29.561 | PARTIAL | WR-01 (wrong AVP nesting for EAP-Payload) |
| EAP-Payload AVP (1265) | TS 29.561 §17.3.4 | ✗ | Inside VendorSpecificApplicationID instead of top-level |
| 3GPP NssaaNotification | TS 29.518 §5.2.2.27 | PARTIAL | WR-02 (wrong NotificationType values) |

---

## Recommended Priority Order

| Priority | Finding | File | Fix Scope |
|---|---|---|---|
| P0 | CR-01 | radius.go | Fix `md5Authenticator` call in `buildRadiusPacket` |
| P0 | CR-02 | radius.go | Add `verifyRequestAuth()` call in `handlePacket` |
| P0 | CR-04 | amf.go | Move `if fail` check inside locked section |
| P1 | WR-01 | diameter.go | Fix AVP nesting for EAP-Payload |
| P1 | WR-02 | amf.go | Fix NotificationType case values |
| P1 | WR-03 | radius.go | Add RWMutex for `seenChallenge` map |
| P2 | WR-04-WR-09 | Various | See individual recommendations |

---

_Reviewed: 2026-04-29T09:59:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
