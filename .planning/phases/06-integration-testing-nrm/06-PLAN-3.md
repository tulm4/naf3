---
phase: 06
plan: 06-PLAN-3
wave: 3
depends_on: [06-PLAN-1, 06-PLAN-2]
requirements: [REQ-26, REQ-27]
files_modified:
  - test/unit/radius/rfc3579_test.go
  - test/unit/diameter/rfc6733_test.go
  - test/unit/crypto/msk_derivation_test.go
  - test/unit/nrm/alarm_test.go
  - test/unit/api/nssaa_handler_gaps_test.go
  - test/unit/api/aiw_handler_gaps_test.go
  - test/unit/resilience/circuit_breaker_test.go
  - test/unit/e2e_amf/amf_notification_test.go
---

<objective>

Fill unit test coverage gaps across packages that need uplift to meet the >80% overall target. Focus on packages identified in the research: RADIUS/Diameter encoding, crypto MSK derivation, NRM alarm manager, API handler edge cases, circuit breaker, and AMF notification. All tests in this wave use in-process mocks (httptest, sqlmock, miniredis) — no infrastructure required.

</objective>

<tasks>

## Task 1 — RADIUS RFC 3579 EAP-Message Tests (`test/unit/radius/rfc3579_test.go`)

<read_first>
- `internal/radius/radius_test.go` — existing RADIUS encoding tests
- `internal/radius/client.go` — RADIUS client, Message-Authenticator computation
- `docs/design/07_radius_client.md` — RADIUS encoding
- `06-RESEARCH.md` §7.2 — RFC 3579 test cases (TC-RADIUS-001 through TC-RADIUS-010)
</read_first>

<action>
Create `test/unit/radius/rfc3579_test.go` — RFC 3579 conformance tests:
- `TestRFC3579_EAPMessagePresent` (TC-RADIUS-001) — verify EAP-Message attribute in Access-Request
- `TestRFC3579_MessageAuthenticator` (TC-RADIUS-002) — verify Message-Authenticator computed as HMAC-MD5 over entire packet
- `TestRFC3579_EAPMessageFragmentation` (TC-RADIUS-003) — large EAP (>253 bytes) split across multiple EAP-Message attributes
- `TestRFC3579_EAPMessageReassembly` (TC-RADIUS-004) — receiver reassembles fragmented EAP-Message
- `TestRFC3579_MessageAuthenticatorInChallenge` (TC-RADIUS-005) — Message-Authenticator in Access-Challenge
- `TestRFC3579_MessageAuthenticatorInAccept` (TC-RADIUS-006) — Message-Authenticator in Access-Accept
- `TestRFC3579_MessageAuthenticatorInReject` (TC-RADIUS-007) — Message-Authenticator in Access-Reject
- `TestRFC3579_InvalidMessageAuthenticator` (TC-RADIUS-008) — invalid Message-Authenticator → packet dropped
- `TestRFC3579_ProxyStatePreserved` (TC-RADIUS-009) — Proxy-State attribute preserved end-to-end
- `TestRFC3579_UserNameUTF8` (TC-RADIUS-010) — User-Name attribute UTF-8 encoding
- Use `testify/assert` and `testify/require` throughout
- Package: `test/unit/radius`
</action>

<acceptance_criteria>
- All 10 RFC 3579 test cases exist
- Tests use `testify/assert` and `testify/require`
- `go test ./test/unit/radius/...` passes
- `go build ./test/unit/radius/...` compiles without error
</acceptance_criteria>

---

## Task 2 — Diameter RFC 6733 Tests (`test/unit/diameter/rfc6733_test.go`)

<read_first>
- `internal/diameter/diameter_test.go` — existing Diameter tests
- `internal/diameter/diameter.go` — Diameter encoding, AVP parsing
- `docs/design/08_diameter_client.md` — Diameter protocol
</read_first>

<action>
Create `test/unit/diameter/rfc6733_test.go` — RFC 6733 conformance tests:
- `TestDiameter_CERCEAExchange` — Capabilities-Exchange-Request/Answer
- `TestDiameter_DERDEAExchange` — Device-in-Gateway-Request/Answer (NSSAA-specific)
- `TestDiameter_AVPParsing` — parse raw AVP bytes into struct
- `TestDiameter_AVPBuilder` — build AVP from struct to bytes
- `TestDiameter_ResultCodeAVP` — Result-Code AVP encoding/decoding
- `TestDiameter_EAPPayloadAVP` — 3GPP vendor-specific EAP-Payload AVP (Vendor-Id=10415, code=1265)
- `TestDiameter_MultipleAVPsInMessage` — DER with Result-Code + Auth-Application-Id + EAP-Payload
- `TestDiameter_MessageHeaderParsing` — parse Diameter header (version, length, command code, flags)
- Use `testify/assert` and `testify/require`
- Package: `test/unit/diameter`
</action>

<acceptance_criteria>
- Tests cover DER/DEA exchange, AVP encoding/decoding
- Tests use `testify/assert` and `testify/require`
- `go test ./test/unit/diameter/...` passes
- `go build ./test/unit/diameter/...` compiles without error
</acceptance_criteria>

---

## Task 3 — EAP-TLS MSK Derivation Tests (`test/unit/crypto/msk_derivation_test.go`)

<read_first>
- `internal/crypto/crypto_test.go` — existing crypto tests
- `docs/design/17_crypto.md` — crypto key hierarchy, MSK derivation
- `06-RESEARCH.md` §7.3 — RFC 5216 MSK derivation test cases (TC-EAPTLS-001 through TC-EAPTLS-010)
</read_first>

<action>
Create `test/unit/crypto/msk_derivation_test.go` — RFC 5216 MSK derivation conformance:
- `TestMSKDerivation_Length` (TC-EAPTLS-001) — MSK length is exactly 64 bytes
- `TestMSKDerivation_MSKeySplit` (TC-EAPTLS-002) — MSK = first 32 bytes of TLS-exported key material
- `TestMSKDerivation_EMSK` (TC-EAPTLS-003) — EMSK = last 32 bytes
- `TestMSKDerivation_MSKemSKDifferent` (TC-EAPTLS-004) — MSK and EMSK are different
- `TestMSKDerivation_EmptySession` (TC-EAPTLS-005) — insufficient TLS data → error
- `TestMSKDerivation_InsufficientKeyMaterial` (TC-EAPTLS-006) — <64 bytes exported → error
- `TestMSKDerivation_ExportLabel` (TC-EAPTLS-007) — key export label is "EAP-TLS MSK" per RFC 5216
- `TestMSKDerivation_SessionIDInContext` (TC-EAPTLS-008) — session ID included in derivation context
- `TestMSKDerivation_HandshakeMessages` (TC-EAPTLS-009) — server handshake_messages included
- `TestMSKDerivation_PeerCertificate` (TC-EAPTLS-010) — peer cert used in derivation when available
- Strategy: Use mock TLS session returning pre-defined master secret (from research §7.3)
- Use `testify/assert` and `testify/require`
- Package: `test/unit/crypto`
</action>

<acceptance_criteria>
- All 10 RFC 5216 MSK derivation test cases exist
- Uses mock TLS session for deterministic test vectors
- `go test ./test/unit/crypto/...` passes
- `go build ./test/unit/crypto/...` compiles without error
</acceptance_criteria>

---

## Task 4 — NRM Alarm Manager Unit Tests (`test/unit/nrm/alarm_test.go`)

<read_first>
- `internal/nrm/alarm.go` — AlarmStore and AlarmManager (from Wave 2)
- `docs/design/18_nrm_fcaps.md` §3.1 — alarm types, deduplication
- `06-RESEARCH.md` §6.1 — NRM package (built in Wave 2)
</read_first>

<action>
Create `test/unit/nrm/alarm_test.go` — NRM alarm manager unit tests:
- `TestAlarmStore_SaveAndList` — save alarm, list returns it
- `TestAlarmStore_Deduplication` — same (alarmType, backupObject) within 5 min → deduplicated
- `TestAlarmStore_DifferentKeys` — different keys → both stored
- `TestAlarmStore_Clear` — clear removes alarm
- `TestAlarmStore_Get` — get returns stored alarm
- `TestAlarmStore_Count` — count reflects active alarms
- `TestAlarmManager_RaiseAlarm` — raise generates unique alarm ID
- `TestAlarmManager_ClearAlarm` — clear removes by ID
- `TestAlarmManager_ListAlarms` — list returns all active
- `TestAlarmManager_All7AlarmTypes` — all predefined alarm type constants raise correctly
- `TestAlarmManager_FailureRateAlarm` — evaluate with >10% failure rate → alarm raised
- `TestAlarmManager_CircuitBreakerOpenAlarm` — circuit breaker open → alarm raised
- `TestAlarmManager_DeduplicationAcrossTypes` — same backupObject with different alarmType → both stored
- Use `testify/assert` and `testify/require`
- Package: `test/unit/nrm`
<acceptance_criteria>
- All 13 alarm manager test cases exist (TC-NRM-ALM-001 through TC-NRM-ALM-013)
- Deduplication logic verified: same (AlarmType, BackupObject) within 5 min → deduplicated; different types → both stored
- All 7 alarm type constants tested
- `go test ./test/unit/nrm/... -count=1` passes
- `go build ./test/unit/nrm/...` compiles without error
</acceptance_criteria>

---

## Task 5 — N58 API Handler Coverage Gaps (`test/unit/api/nssaa_handler_gaps_test.go`)

<read_first>
- `internal/api/nssaa/handler_test.go` — existing N58 API tests (16 cases)
- `internal/api/nssaa/handler.go` — N58 handler implementation
- `06-RESEARCH.md` §6.1 — coverage gap analysis
</read_first>

<action>
Create `test/unit/api/nssaa_handler_gaps_test.go` — fill N58 API test gaps:
- `TestCreateSliceAuth_AAAConfigured` — valid request with AAA server configured for snssai → 201
- `TestCreateSliceAuth_AAANotConfigured` (TC-NSSAA-010) — no AAA server for snssai → 404
- `TestCreateSliceAuth_InvalidAuthHeader` (TC-NSSAA-012/013) — invalid Authorization header → 401
- `TestCreateSliceAuth_MissingAuthHeader` (TC-NSSAA-012) — missing Authorization → 401
- `TestCreateSliceAuth_InvalidBase64EapIdRsp` (TC-NSSAA-009) — invalid base64 in eapIdRsp → 400
- `TestConfirmSliceAuth_SessionNotFound` (TC-NSSAA-021) — session not found → 404
- `TestConfirmSliceAuth_SnssaiMismatch` (TC-NSSAA-023) — snssai mismatch → 400
- `TestConfirmSliceAuth_InvalidBase64EapMessage` (TC-NSSAA-025) — invalid base64 in eapMessage → 400
- `TestConfirmSliceAuth_SessionAlreadyCompleted` (TC-NSSAA-026) — completed session → 409 Conflict
- `TestConfirmSliceAuth_RedisUnavailable` (TC-NSSAA-028) — store error → 503
- `TestConfirmSliceAuth_AAAGWUnreachable` (TC-NSSAA-029) — AAA GW unreachable → 502
- `TestGetSliceAuth_SessionExists` (TC-NSSAA-030) — session exists → 200
- `TestGetSliceAuth_SessionNotFound` (TC-NSSAA-031) — session not found → 404
- `TestGetSliceAuth_SessionExpired` (TC-NSSAA-032) — expired session → 404
- Use `newMockStore()` pattern from existing handler_test.go
- Package: `test/unit/api`
</action>

<acceptance_criteria>
- All 14 gap-filling N58 test cases exist
- Uses `newMockStore()` pattern from existing tests
- `go test ./test/unit/api/...` passes
- `go build ./test/unit/api/...` compiles without error
</acceptance_criteria>

---

## Task 6 — N60 API Handler Coverage Gaps (`test/unit/api/aiw_handler_gaps_test.go`)

<read_first>
- `internal/api/aiw/handler_test.go` — existing N60 API tests
- `internal/api/aiw/handler.go` — N60 handler implementation
- `06-RESEARCH.md` §6.1 — coverage gap analysis
</read_first>

<action>
Create `test/unit/api/aiw_handler_gaps_test.go` — fill N60 API test gaps:
- `TestCreateAuth_InvalidAuthHeader` — invalid Authorization → 401
- `TestCreateAuth_MissingAuthHeader` — missing Authorization → 401
- `TestCreateAuth_InvalidBase64EapIdRsp` — invalid base64 → 400
- `TestCreateAuth_SupiMismatch` — GPSI/SUPI mismatch → 400
- `TestCreateAuth_StoreLoadError` — UDM store load error → 503
- `TestConfirmAuth_SessionNotFound` — session not found → 404
- `TestConfirmAuth_SupiMismatchInBody` — SUPI in body ≠ stored SUPI → 400
- `TestConfirmAuth_InvalidBase64EapMessage` — invalid base64 → 400
- `TestConfirmAuth_SessionAlreadyCompleted` — completed session → 409 Conflict
- `TestConfirmAuth_StoreSaveError` — store save error → 503
- `TestGetAuth_SessionExists` — session exists → 200
- `TestGetAuth_SessionNotFound` — session not found → 404
- Use `newMockStore()` pattern from existing handler_test.go
- Package: `test/unit/api`
</action>

<acceptance_criteria>
- All 12 gap-filling N60 test cases exist
- Uses `newMockStore()` pattern from existing tests
- `go test ./test/unit/api/...` passes
- `go build ./test/unit/api/...` compiles without error
</acceptance_criteria>

---

## Task 7 — Circuit Breaker Unit Tests (`test/unit/resilience/circuit_breaker_test.go`)

<read_first>
- `internal/resilience/circuit_breaker_test.go` — existing CB tests
- `internal/resilience/circuit_breaker.go` — CB implementation
- `06-RESEARCH.md` §6.1 — coverage gap (REQ-34 related)
</read_first>

<action>
Create `test/unit/resilience/circuit_breaker_test.go` — fill CB test gaps:
- `TestCircuitBreaker_FailureThresholdReached` — failure count = threshold → OPEN
- `TestCircuitBreaker_SuccessBelowThreshold` — successes below threshold → CLOSED stays closed
- `TestCircuitBreaker_RecoveryTimeout` — recovery timeout elapses → HALF_OPEN
- `TestCircuitBreaker_HalfOpenSuccess` — success in HALF_OPEN → CLOSED
- `TestCircuitBreaker_HalfOpenFailure` — failure in HALF_OPEN → OPEN
- `TestCircuitBreaker_StateReadout` — CB exposes current state for NRM monitoring
- `TestCircuitBreaker_ServerIdentification` — CB has server identifier for alarm correlation
- These tests verify REQ-34 alarm preconditions (circuit breaker state must be readable)
- Package: `test/unit/resilience`
</action>

<acceptance_criteria>
- CB state machine fully exercised (CLOSED → OPEN → HALF_OPEN → CLOSED)
- CB state is readable for NRM alarm integration
- `go test ./test/unit/resilience/...` passes
- `go build ./test/unit/resilience/...` compiles without error
</acceptance_criteria>

---

## Task 8 — AMF Notification Unit Tests (`test/unit/e2e_amf/amf_notification_test.go`)

<read_first>
- `internal/amf/notifier_test.go` — existing AMF notifier tests
- `internal/amf/notifier.go` — AMF notifier implementation
- `test/mocks/amf.go` — AMF mock (from Wave 1)
</read_first>

<action>
Create `test/unit/e2e_amf/amf_notification_test.go` — AMF notification tests:
- `TestNotifier_ReAuthNotification` — sends SLICE_RE_AUTH notification to AMF
- `TestNotifier_RevocationNotification` — sends SLICE_REVOCATION notification to AMF
- `TestNotifier_RetryOnFailure` — retries on HTTP 503
- `TestNotifier_RetryExhausted` — sends to DLQ after max retries
- `TestAMFMocksReceiveCallback` — AMF mock (httptest) receives and stores notification
- Uses `test/mocks/amf.go` AMF mock from Wave 1
- Uses `testify/assert` and `testify/require`
- Package: `test/unit/e2e_amf`
</action>

<acceptance_criteria>
- All 5 AMF notification test cases exist
- AMF mock receives and stores notifications for test assertion
- DLQ behavior verified
- `go test ./test/unit/e2e_amf/...` passes
- `go build ./test/unit/e2e_amf/...` compiles without error
</acceptance_criteria>

---

## Task 9 — Coverage Baseline and Gap Measurement

<read_first>
- `06-RESEARCH.md` §6 — coverage gap estimates
- `docs/roadmap/module_index.md` §Coverage Targets — authoritative per-package coverage targets
- `go.mod` — dependencies (DATA-DOG/go-sqlmock added in Wave 1)
</read_first>

<action>
After all unit tests are written, run coverage measurement:
```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | sort -t'%' -k1 -r | head -20
```
Identify any packages still below target. If a package falls short, add targeted test cases to the nearest test file. Focus on:
- `internal/radius/` — target 85% (RFC 3579 tests added)
- `internal/diameter/` — target 85% (RFC 6733 tests added)
- `internal/crypto/` — target 90% (MSK derivation tests added)
- `internal/nrm/` — target 75% (alarm tests added)
- `internal/api/nssaa/` — target 90% (gap tests added)
- `internal/api/aiw/` — target 90% (gap tests added)
</action>

<acceptance_criteria>
- `go test -cover ./...` shows overall coverage >80%
- Each package meets its target from the module index
- Coverage report generated without errors
</acceptance_criteria>

</tasks>

<verification>

Overall verification for Wave 3:
- `go test -cover ./test/unit/...` passes
- `go test -coverprofile=coverage.out ./...` produces valid coverage file
- `go tool cover -func=coverage.out | grep total` shows overall >80%
- All 50+ unit test cases across 7 test files pass
- `go fmt ./test/unit/...` produces clean output

</verification>

<success_criteria>

- REQ-26: Overall unit test coverage >80% (measured by `go test -cover`)
- All 8 new test files created and passing:
  - `test/unit/radius/rfc3579_test.go` (10 cases)
  - `test/unit/diameter/rfc6733_test.go` (8 cases)
  - `test/unit/crypto/msk_derivation_test.go` (10 cases)
  - `test/unit/nrm/alarm_test.go` (13 cases)
  - `test/unit/api/nssaa_handler_gaps_test.go` (14 cases)
  - `test/unit/api/aiw_handler_gaps_test.go` (12 cases)
  - `test/unit/resilience/circuit_breaker_test.go` (7 cases)
  - `test/unit/e2e_amf/amf_notification_test.go` (5 cases)
- Each package meets its coverage target from the module index
- No new dependencies introduced
- All tests follow testify pattern from existing codebase

</success_criteria>
