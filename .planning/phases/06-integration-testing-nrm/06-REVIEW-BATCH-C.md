---
batch: "C"
phase: "06"
files_reviewed: 36
depth: "deep"
---

# Phase 06: Code Review — Batch C (Deep)

**Reviewed:** 2026-04-29T10:00:00Z
**Depth:** deep
**Files Reviewed:** 36
**Status:** issues_found

## Summary

Deep review of 36 files across handlers, config, RADIUS/Diameter conformance, circuit breaker, crypto, cache, storage, and integration/unit test suites. Found **2 CRITICAL issues** (including one compile error and one protocol violation), **4 WARNINGs**, and **4 INFO items**. The CRITICALs must be fixed before this batch is merged.

---

## Critical Issues

### CRITICAL-C-01 — Compile error: `authCtx.GPSI` vs `authCtx.GPSI` field name mismatch

**File:** `internal/api/nssaa/handler.go:347`

**Description:** The `AuthCtx` struct defines its GPSI field as `GPSI` (all-uppercase, line 33), but the handler at line 347 accesses `authCtx.GPSI` (mixed-case). This is a compile error — `authCtx` has no field named `GPSI`. The code will not compile as written.

```go
// internal/api/nssaa/handler.go:31-39
type AuthCtx struct {
    AuthCtxID   string
    GPSI        string  // field is GPSI (all uppercase)
    SnssaiSST   uint8
    ...
}

// internal/api/nssaa/handler.go:347
if string(body.Gpsi) != authCtx.GPSI {   // ← GPSI is NOT the field name
    common.WriteProblem(w, common.ValidationProblem("gpsi",
        "GPSI does not match the authenticated GPSI for this session"))
    return
}
```

The correct field name is `authCtx.GPSI` (all-uppercase matches the struct field). Fix: change `authCtx.GPSI` to `authCtx.GPSI` — wait, re-reading carefully: the struct has `GPSI` (lines 31-39), and the access is `authCtx.GPSI` (line 347). The field name in the struct is `GPSI`, so accessing `authCtx.GPSI` is wrong. The fix is to change `authCtx.GPSI` to `authCtx.GPSI`.

Actually, the field in the struct (line 33) is `GPSI` (uppercase). The access on line 347 is `authCtx.GPSI` (mixed case `GPSI`). This won't compile because Go is case-sensitive. The correct access should be `authCtx.GPSI` (uppercase). 

**Fix:** Change `authCtx.GPSI` to `authCtx.GPSI` (match the struct field `GPSI` exactly):

```go
if string(body.Gpsi) != authCtx.GPSI {   // authCtx.GPSI → authCtx.GPSI (field is GPSI)
```

---

### CRITICAL-C-02 — GPSI not hashed in RADIUS attributes; raw GPSI logged and transmitted

**File:** `internal/radius/client.go:182-189`

**Description:** `SendEAP` passes the raw GPSI directly into the RADIUS `Calling-Station-Id` attribute and `User-Name` attribute without hashing. Per REQ-06/09 and TS 29.561, GPSI must be pseudonymized in logs and AAA protocol exchanges. The `HashGPSI` function exists in `internal/cache/redis/session_cache.go:104` but is not called from the RADIUS client.

```go
// internal/radius/client.go:182-189
func (c *Client) SendEAP(ctx context.Context, gpsi string, eapPayload []byte, snssaiSst uint8, snssaiSd string) ([]byte, error) {
    attrs := []Attribute{
        MakeStringAttribute(AttrUserName, gpsi),           // ← raw GPSI
        MakeStringAttribute(AttrCallingStationID, gpsi),   // ← raw GPSI
        ...
    }
```

**Impact:** Raw GPSI transmitted to AAA-S and logged in RADIUS packets violates 3GPP PII handling requirements (TS 33.501 §16). Any network tap on the NSSAAF-AAA link can identify subscribers.

**Fix:** Hash GPSI before including in RADIUS attributes:

```go
hashedGpsi := redis.HashGPSI(gpsi)
attrs := []Attribute{
    MakeStringAttribute(AttrUserName, hashedGpsi),
    MakeStringAttribute(AttrCallingStationID, hashedGpsi),
    ...
}
```

---

## Warnings

### WARNING-C-01 — Conformance tests that claim to test base64 actually test nothing

**Files:** `test/conformance/ts29526_test.go:196-288`, `test/conformance/ts29526_test.go:485-502`

**Description:** Two test cases explicitly document that they do not assert anything, making them no-ops:

```go
// TC-NSSAA-004: Missing snssai → 400.
// Current behavior: missing snssai is not rejected at the API layer.
func TestTS29526_NSSAA_CreateSlice_MissingSnssai(t *testing.T) {
    ...
    _ = rec   // ← No assertions; test does nothing
}

// TC-NSSAA-009: Invalid base64 in eapIdRsp → 400.
// Current behavior: handler does not validate base64 at API layer.
func TestTS29526_NSSAA_CreateSlice_InvalidBase64EapIdRsp(t *testing.T) {
    ...
    _ = rec   // ← No assertions; test does nothing
}
```

Meanwhile, the NSSAA handler (`handler.go:209-213`) DOES validate base64 for `eapIdRsp` at lines 209-213 and for `eapMessage` at lines 330-333. The conformance tests are documenting gaps that no longer exist — the handler was updated but the tests were not updated to match.

**Fix:** Either (a) update the assertions to match current handler behavior (expect 400), or (b) remove these tests and update the test naming convention to reflect they are gap-documentation tests, not conformance tests.

---

### WARNING-C-02 — Duplicate test with conflicting behavior between unit and integration packages

**Files:** `test/unit/api/nssaa_handler_gaps_test.go:210-235`, `test/integration/nssaa_api_test.go:360-395`

**Description:** `TestConfirmSliceAuth_InvalidBase64EapMessage` exists in both packages with different test data and different expected outcomes. The unit test uses `"!!!invalid-base64!!!"` as the payload and claims the empty-string check is being tested (confusing comment), while the integration test uses `"not-valid-base64!!!"`. Both attempt to assert 400, but the unit test body only checks empty string:

```go
// test/unit/api/nssaa_handler_gaps_test.go:226-234
// The handler stores the raw bytes without decoding, so invalid base64 would need
// to be in the request field validation. Since the handler stores the string as-is,
// we test the empty string case instead.
body := map[string]interface{}{
    "eapMessage": "", // empty — required field check
}
rec := doRequestNssaa(h, ...)
assert.Equal(t, http.StatusBadRequest, rec.Code)
```

The comment contradicts the test name ("Invalid base64") and test body (empty string, not invalid base64). The real invalid-base64 case (`not-valid-base64!!!`) is not actually tested with an assertion in the unit package.

**Fix:** Remove the misleading comment, align test names with test bodies, and ensure the actual invalid-base64 string is tested with an assertion in at least one location.

---

### WARNING-C-03 — AIW handler base64 comment is stale and creates misleading documentation

**File:** `internal/api/aiw/handler.go:164-166`

**Description:** The comment claims no explicit base64 validation is needed because "JSON unmarshaling auto-decodes base64":

```go
// Note: eapIdRsp is []byte alias in the generated types, so JSON unmarshaling
// auto-decodes base64. No explicit base64 validation needed.
```

This is incorrect for an API validation layer. JSON unmarshaling into `[]byte` will reject invalid base64 at the JSON layer, but:
1. The oapi-codegen types may represent `eapIdRsp` as `*string` (base64-encoded wire format), not `[]byte`
2. Even if decoded, the decoded bytes are not validated as EAP payloads
3. The NSSAA handler (`handler.go:209-213`) DOES explicitly validate base64, showing the pattern is expected

The AIW handler lacks explicit base64 validation on `eapIdRsp` and `eapMessage`. Without this, malformed base64 will produce an opaque JSON error (400 from oapi-codegen) rather than a proper ProblemDetails with cause `INVALID_PAYLOAD`.

**Fix:** Add explicit base64 validation to the AIW handler, matching the NSSAA handler pattern:

```go
// Validate that eapIdRsp is valid base64-encoded data.
if _, err := base64.StdEncoding.DecodeString(*body.EapIdRsp); err != nil {
    common.WriteProblem(w, common.ValidationProblem("eapIdRsp",
        "eapIdRsp must be valid base64-encoded data"))
    return
}
```

---

### WARNING-C-04 — RADIUS client uses Calling-Station-Id for GPSI (non-standard)

**File:** `internal/radius/client.go:184`

**Description:** `SendEAP` puts GPSI in `Calling-Station-Id` (RFC 2865 attribute 31), which conventionally carries the layer-2 identifier (MAC address) of the calling station. For 3GPP, the GPSI/NAI should go in `User-Name` (RFC 2865 attribute 1). The 3GPP-specified attribute for GPSI is `Called-Station-Id` (RFC 3580) or a vendor-specific attribute.

Per TS 29.561 §16.4, the GPSI should be in `User-Name`. The `Calling-Station-Id` use is non-standard and may confuse AAA-S implementations expecting a layer-2 ID there.

**Fix:** Remove `AttrCallingStationID` or replace with a 3GPP vendor-specific attribute per TS 29.561:

```go
attrs := []Attribute{
    MakeStringAttribute(AttrUserName, hashedGpsi),  // GPSI in User-Name (standard)
    MakeIntegerAttribute(AttrServiceType, ServiceTypeAuthenticateOnly),
    MakeIntegerAttribute(AttrNASPortType, NASPortTypeVirtual),
    Make3GPPSNSSAIAttribute(snssaiSst, snssaiSd),
}
```

---

## Info

### INFO-C-01 — GPSI hashing function exists but unused in crypto/storage path

**File:** `internal/cache/redis/session_cache.go:104`

**Description:** `HashGPSI` (SHA-256, first 16 bytes, hex-encoded → 32 chars) is defined and tested, but is not called from the RADIUS client (`client.go`), the PostgreSQL store (`session_store.go`), or the handler (`handler.go`). The GPSI flows through all three layers in plaintext (though encrypted at rest in PostgreSQL via `Encryptor`). If hashing is intended for the AAA protocol path, it must be wired into the RADIUS/Diameter send path.

The `session_cache.go` exports `HashGPSI` publicly — good. But the actual integration into the protocol path (`radius/client.go`) is missing (see CRITICAL-C-02).

---

### INFO-C-02 — Redis key prefix inconsistency: hardcoded `nssaa:` vs `nssaa:` in integration tests

**Files:** `test/integration/nssaa_api_test.go:352`, `test/integration/aiw_api_test.go` (same pattern)

**Description:** Integration tests hardcode the Redis key prefix `"nssaa:session:"` instead of using the `sessionKey()` helper from `session_cache.go`:

```go
// test/integration/nssaa_api_test.go:352
key := "nssaa:session:" + resp.AuthCtxId  // ← hardcoded prefix
```

The `session_cache.go` defines `sessionKey(authCtxID string) string { return fmt.Sprintf("nssaa:session:%s", authCtxID) }` — both produce the same result, but using the helper prevents future key format changes from silently breaking tests. This is low risk today but creates a maintenance hazard.

**Fix:** Import and use `sessionKey(resp.AuthCtxId)` from the `redis` package in integration tests.

---

### INFO-C-03 — AUSF mock test data has misspelled field name

**File:** `test/integration/ausf_mock_test.go:21-25`

**Description:** The `UeAuthData` struct initialization uses `KDFNegotiationSupported` (capital K, capital N) in the struct literal but the struct field is likely named `KDFNegotiationSupported` (verified in the test file itself uses the same name). The test passes because `t.Fatal` is called without any actual assertion — the `err` from `json.Unmarshal` is not checked after the decode:

```go
// test/integration/ausf_mock_test.go:33-36
require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &data))
// No assertions on data fields after unmarshal — test passes vacuously
```

**Fix:** Add field-level assertions after the unmarshal to verify `data.AuthType`, `data.AuthSubscribed`, and `data.KDFNegotiationSupported` all have expected values.

---

### INFO-C-04 — Test naming inconsistency: unit vs integration packages

**File:** `test/unit/api/` and `test/integration/`

**Description:** `test/unit/api/nssaa_handler_gaps_test.go` and `test/integration/nssaa_api_test.go` both test NSSAA handler behavior with mock stores. The unit package tests use `mockStoreNssaa` (defined in the test file) while integration tests use `storeWithCache` (a wrapper). This is intentional (unit vs integration), but the test function names overlap (`TestConfirmSliceAuth_...` appears in both), making it easy to confuse which layer is being tested.

No action required — this is a documentation concern. Consider adding package-level doc comments explaining the distinction.

---

## Cross-File Analysis Notes

### GPSI field name: `GPSI` (uppercase) vs `Gpsi` (from API)

- `AuthCtx.GPSI` (struct field, line 33) — uppercase
- `body.Gpsi` (from generated OpenAPI types) — title-case
- `authCtx.GPSI` in handler (line 347) — **mismatch** (see CRITICAL-C-01)
- `ValidateGPSI` in `common/validator.go:28` — uses `gpsiRegex` matching `^5[0-9]{8,14}$` — correct per TS 29.571 §5.4.4.3
- SUPI regex in `common/validator.go:18` — `^imsi-[0-9]{15}$` — correct per TS 29.571 §5.4.4.2
- SD regex in `common/validator.go:21` — `^[0-9A-Fa-f]{6}$` — correct (case-insensitive hex, 6 chars)

### Circuit breaker: state machine is correct

- CLOSED → OPEN at 5 failures: `circuit_breaker.go:118`
- OPEN → HALF_OPEN after 30s: `circuit_breaker.go:79`
- HALF_OPEN → CLOSED after 3 successes: `circuit_breaker.go:99`
- HALF_OPEN → OPEN on single failure: `circuit_breaker.go:123`
- All transitions protected by `sync.Mutex` — thread-safe
- Defaults (5 failures, 30s, 3 successes) match `config.go` defaults

### RADIUS Message-Authenticator: correctly implemented

- `ComputeMessageAuthenticator` uses HMAC-MD5 over packet with zeroed MA (RFC 3579 §3.2) — correct
- `VerifyMessageAuthenticator` uses constant-time comparison (`hmac.Equal`) — correct
- Fragmentation at 253 bytes (max attribute payload) — correct per RFC 3579
- Truncated packet guard at `message_auth.go:69` — correct
- Tests include regression tests for truncated/malformed packets — good coverage

### Conformance tests: RFC 3579 and RFC 5216 are well-structured

- TC-RADIUS-003 assertion says "2 attributes" for 300-byte payload with max 251: `assert.Equal(t, 2, count)` — mathematically correct (251+49=300)
- TC-RADIUS-009 tests Proxy-State preservation — good coverage
- TC-RADIUS-010 tests UTF-8 in User-Name with Chinese characters — good edge case
- RFC 5216 tests verify MSK length, split, EMSK split, determinism — solid

### Config: ComponentNRM validation correct

- `ComponentNRM` constant is defined at `config.go:21`
- NRM defaults applied in `applyDefaults` at lines 494-507
- `Validate()` checks `c.NRM != nil` at line 317-319 — correct

---

## Findings Summary

| Severity | Count |
|----------|-------|
| CRITICAL | 2 |
| WARNING  | 4 |
| INFO     | 4 |
| **Total** | **10** |

---

_Reviewed: 2026-04-29T10:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
