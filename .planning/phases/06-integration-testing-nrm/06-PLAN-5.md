---
phase: 06
plan: 06-PLAN-5
wave: 5
depends_on: [06-PLAN-1, 06-PLAN-2]
requirements: [REQ-27, REQ-28, REQ-29(32 CASES), REQ-30, REQ-31, REQ-32, REQ-33, REQ-34]
files_modified:
  - test/e2e/harness.go
  - test/e2e/nssaa_flow_test.go
  - test/e2e/reauth_test.go
  - test/e2e/revocation_test.go
  - test/conformance/ts29526_test.go
  - test/conformance/rfc3579_test.go
  - test/conformance/rfc5216_test.go
  - compose/test.yaml (update)
---

<objective>

End-to-end tests running the full AMF → HTTP GW → Biz Pod → AAA GW → AAA-S flow plus conformance test suites validating TS 29.526 §7.2, RFC 3579, and RFC 5216. E2E tests use the AAA-S simulator container (Wave 1) and the full docker-compose stack. Conformance tests run without infrastructure using in-process mocks. This is Wave 5 — the final verification layer.

</objective>

<tasks>

## Task 1 — E2E Test Harness (`test/e2e/harness.go`)

<read_first>
- `cmd/biz/main.go` — Biz Pod startup (uses TLS, connects to Redis, PG, AAA GW)
- `cmd/http-gateway/main.go` — HTTP Gateway startup
- `cmd/aaa-gateway/main.go` — AAA Gateway startup
- `test/mocks/compose.go` — compose lifecycle helpers (Wave 1)
- `test/aaa_sim/main.go` — AAA-S simulator (Wave 1)
- `06-CONTEXT.md` — D-01: E2E uses full docker-compose stack
</read_first>

<action>
Create `test/e2e/harness.go` — E2E test harness for full-stack testing:
- `Harness` struct — holds all component connections (HTTP GW URL, Biz Pod URL, etc.)
- `NewHarness(t *testing.T)` — starts full stack, returns Harness:
  1. `docker-compose -f compose/dev.yaml -f compose/test.yaml up -d` (starts PG, Redis, AAA-S)
  2. Build and start Biz Pod binary (`cmd/biz`)
  3. Build and start HTTP Gateway binary (`cmd/http-gateway`)
  4. Build and start AAA Gateway binary (`cmd/aaa-gateway`)
  5. Wait for all services to be healthy
  6. Build and start `aaa-sim` binary (Wave 1) on UDP 1812, TCP 3868
  7. Return `Harness` with all connection URLs
- `Harness.Close()` — graceful shutdown: stop binaries, `docker-compose down`
- `Harness.BizURL() string` — returns Biz Pod URL
- `Harness.HTTPGWURL() string` — returns HTTP Gateway URL
- `Harness.AAAGWURL() string` — returns AAA Gateway URL
- `Harness.NRMURL() string` — returns NRM RESTCONF URL (from Wave 2)
- Environment variables override binary paths: `BIZ_BINARY`, `HTTPGW_BINARY`, `AAAGW_BINARY`
- Package: `test/e2e`
</action>

<acceptance_criteria>
- `NewHarness` starts all 3 components and waits for healthy
- `Harness.Close()` cleanly stops all services
- `Harness.BizURL()` returns correct URL
- `go build ./test/e2e/...` compiles without error
</acceptance_criteria>

---

## Task 2 — NSSAA E2E Flow Test (`test/e2e/nssaa_flow_test.go`)

<read_first>
- `06-RESEARCH.md` §1.3 — E2E test runner architecture
- `06-RESEARCH.md` §5.2 — CI pipeline pattern
- `internal/api/nssaa/handler_test.go` — existing handler test patterns
- `test/e2e/harness.go` — harness (this wave)
</read_first>

<action>
Create `test/e2e/nssaa_flow_test.go` — full NSSAA flow E2E tests:
- `TestE2E_NSSAA_HappyPath` — AMF → HTTP GW → Biz Pod → AAA GW → AAA-S → success
  1. HTTP POST to HTTP GW (N58 API) with valid GPSI, snssai, eapIdRsp
  2. Verify 201 response, Location header, authCtxId
  3. Verify session stored in PostgreSQL (via direct DB query)
  4. HTTP PUT to confirm (EAP-TLS handshake rounds)
  5. Verify 200 response with EAP-Message
- `TestE2E_NSSAA_AuthFailure` — AAA-S returns failure → 200 with EAP-Failure
- `TestE2E_NSSAA_AuthChallenge` — AAA-S returns challenge → continues handshake
- `TestE2E_NSSAA_InvalidGPSI` — invalid GPSI → HTTP GW returns 400
- `TestE2E_NSSAA_InvalidSnssai` — invalid snssai → 400
- `TestE2E_NSSAA_Unauthorized` — missing auth → 401
- `TestE2E_NSSAA_AaaServerDown` — stop AAA-S → CB opens → 502
- `TestE2E_NSSAA_CircuitBreakerAlarm` — CB OPEN → NRM alarm raised (REQ-34)
- Uses `test/e2e/harness.go`
- Uses `t.SkipIf(testing.Short(), "E2E tests skipped in short mode")`
- Uses `testify/assert` and `testify/require`
- Package: `test/e2e`
</action>

<acceptance_criteria>
- All 8 NSSAA E2E flow test cases exist
- Happy path verifies full AMF → HTTP GW → Biz → AAA GW → AAA-S flow
- REQ-34 verified: CB OPEN raises NRM alarm
- `go test ./test/e2e/... -run NSSAA` passes
- `go build ./test/e2e/...` compiles without error
</acceptance_criteria>

---

## Task 3 — Re-Authentication E2E Test (`test/e2e/reauth_test.go`)

<read_first>
- `docs/3gppfilter/02_procedures/Reauth_flow_AAA.md` — re-auth procedure
- `internal/amf/notifier.go` — AMF notification sender
- `test/e2e/harness.go` — harness
</read_first>

<action>
Create `test/e2e/reauth_test.go` — AAA-S triggered re-authentication E2E:
- `TestE2E_ReAuth_HappyPath` — AAA-S → NSSAAF → AMF re-auth notification
  1. Establish baseline session (happy path)
  2. AAA-S triggers RAR (Re-Auth-Request) via RADIUS CoA
  3. AAA Gateway forwards RAR to Biz Pod
  4. Biz Pod sends SLICE_RE_AUTH notification to AMF mock (Wave 1)
  5. Verify AMF mock received correct notification
- `TestE2E_ReAuth_AmfUnreachable` — AMF down → notification goes to DLQ
- `TestE2E_ReAuth_MultipleReAuth` — multiple re-auth requests for same session
- `TestE2E_ReAuth_CircuitBreakerOpen` — CB open during re-auth → graceful failure
- Uses `test/mocks/amf.go` AMF mock (Wave 1)
- Uses `test/e2e/harness.go`
- Package: `test/e2e`
</action>

<acceptance_criteria>
- All 4 re-authentication E2E tests exist
- AMF mock receives and stores SLICE_RE_AUTH notification
- DLQ behavior verified when AMF is unreachable
- `go test ./test/e2e/... -run ReAuth` passes
- `go build ./test/e2e/...` compiles without error
</acceptance_criteria>

---

## Task 4 — Revocation E2E Test (`test/e2e/revocation_test.go`)

<read_first>
- `docs/3gppfilter/02_procedures/Revocation_flow.md` — revocation procedure
- `test/e2e/harness.go` — harness
</read_first>

<action>
Create `test/e2e/revocation_test.go` — AAA-S triggered revocation E2E:
- `TestE2E_Revocation_HappyPath` — AAA-S → NSSAAF → AMF revocation notification
  1. Establish baseline session (happy path)
  2. AAA-S triggers revocation via RADIUS Disconnect-Request
  3. AAA Gateway forwards to Biz Pod
  4. Biz Pod sends SLICE_REVOCATION notification to AMF mock
  5. Verify AMF mock received correct notification
- `TestE2E_Revocation_AmfUnreachable` — AMF down → notification goes to DLQ
- `TestE2E_Revocation_ConcurrentRevocations` — multiple simultaneous revocations
- Uses `test/mocks/amf.go` AMF mock
- Package: `test/e2e`
</action>

<acceptance_criteria>
- All 3 revocation E2E tests exist
- AMF mock receives SLICE_REVOCATION notification
- DLQ behavior verified
- `go test ./test/e2e/... -run Revocation` passes
- `go build ./test/e2e/...` compiles without error
</acceptance_criteria>

---

## Task 5 — TS 29.526 §7.2 Conformance Tests (`test/conformance/ts29526_test.go`)

<read_first>
- `06-RESEARCH.md` §7.1 — TS 29.526 §7.2 test cases (TC-NSSAA-001 through TC-NSSAA-032)
- `internal/api/nssaa/handler_test.go` — existing N58 handler tests
- `internal/api/aiw/handler_test.go` — existing N60 handler tests
</read_first>

<action>
Create `test/conformance/ts29526_test.go` — TS 29.526 §7.2 API conformance:
- **CreateSliceAuthenticationContext (TC-NSSAA-001 to TC-NSSAA-014):**
  - TC-NSSAA-001: Valid request → 201, Location, X-Request-ID
  - TC-NSSAA-002: Missing GPSI → 400
  - TC-NSSAA-003: Invalid GPSI format → 400
  - TC-NSSAA-004: Missing snssai → 400
  - TC-NSSAA-005: snssai.sst out of range (0-255) → 400
  - TC-NSSAA-006: snssai.sd invalid hex (not 6 chars) → 400
  - TC-NSSAA-007: Missing eapIdRsp → 400
  - TC-NSSAA-008: Empty eapIdRsp → 400
  - TC-NSSAA-009: Invalid base64 in eapIdRsp → 400
  - TC-NSSAA-010: AAA not configured for snssai → 404
  - TC-NSSAA-011: Invalid JSON → 400
  - TC-NSSAA-012: Missing Authorization → 401
  - TC-NSSAA-013: Invalid Authorization → 401
  - TC-NSSAA-014: No AMF instance ID → 201 (warning in log)
- **ConfirmSliceAuthenticationContext (TC-NSSAA-020 to TC-NSSAA-029):**
  - TC-NSSAA-020: Valid confirm → 200
  - TC-NSSAA-021: Session not found → 404
  - TC-NSSAA-022: GPSI mismatch → 400
  - TC-NSSAA-023: Snssai mismatch → 400
  - TC-NSSAA-024: Missing eapMessage → 400
  - TC-NSSAA-025: Invalid base64 in eapMessage → 400
  - TC-NSSAA-026: Session already completed → 409 Conflict
  - TC-NSSAA-027: Invalid authCtxId format → 404
  - TC-NSSAA-028: Redis unavailable → 503
  - TC-NSSAA-029: AAA GW unreachable → 502
- **GetSliceAuthenticationContext (TC-NSSAA-030 to TC-NSSAA-032):**
  - TC-NSSAA-030: Session exists → 200
  - TC-NSSAA-031: Session not found → 404
  - TC-NSSAA-032: Session expired → 404
- Uses `newMockStore()` pattern from existing handler tests
- Uses `httptest.NewServer` for in-process testing (no infrastructure)
- Uses `testify/assert` and `testify/require`
- Package: `test/conformance`
</action>

<acceptance_criteria>
- All 32 TS 29.526 conformance test cases exist
- Each test case verifies exact HTTP status code and response
- `go test ./test/conformance/... -run TS29526` passes
- `go build ./test/conformance/...` compiles without error
</acceptance_criteria>

---

## Task 6 — RFC 3579 Conformance Tests (`test/conformance/rfc3579_test.go`)

<read_first>
- `06-RESEARCH.md` §7.2 — RFC 3579 test cases (TC-RADIUS-001 to TC-RADIUS-010)
- `internal/radius/radius.go` — RADIUS encoding
- `test/unit/radius/rfc3579_test.go` — unit tests (Wave 3)
</read_first>

<action>
Create `test/conformance/rfc3579_test.go` — RFC 3579 RADIUS EAP conformance:
- TC-RADIUS-001: EAP-Message attribute present in Access-Request
- TC-RADIUS-002: Message-Authenticator computed as HMAC-MD5 over entire packet
- TC-RADIUS-003: EAP-Message fragmentation (>253 bytes split)
- TC-RADIUS-004: EAP-Message reassembly at receiver
- TC-RADIUS-005: Message-Authenticator in Access-Challenge
- TC-RADIUS-006: Message-Authenticator in Access-Accept
- TC-RADIUS-007: Message-Authenticator in Access-Reject
- TC-RADIUS-008: Invalid Message-Authenticator → packet dropped
- TC-RADIUS-009: Proxy-State attribute preserved end-to-end
- TC-RADIUS-010: User-Name attribute UTF-8 encoding
- Uses `testify/assert` and `testify/require`
- Package: `test/conformance`
</action>

<acceptance_criteria>
- All 10 RFC 3579 conformance test cases exist
- `go test ./test/conformance/... -run RFC3579` passes
- `go build ./test/conformance/...` compiles without error
</acceptance_criteria>

---

## Task 7 — RFC 5216 EAP-TLS MSK Conformance Tests (`test/conformance/rfc5216_test.go`)

<read_first>
- `06-RESEARCH.md` §7.3 — RFC 5216 test cases (TC-EAPTLS-001 to TC-EAPTLS-010)
- `internal/crypto/crypto.go` — crypto implementation
- `test/unit/crypto/msk_derivation_test.go` — unit tests (Wave 3)
</read_first>

<action>
Create `test/conformance/rfc5216_test.go` — RFC 5216 EAP-TLS MSK derivation:
- TC-EAPTLS-001: MSK length is exactly 64 bytes
- TC-EAPTLS-002: MSK = first 32 bytes of TLS-exported key material
- TC-EAPTLS-003: EMSK = last 32 bytes
- TC-EAPTLS-004: MSK and EMSK are different
- TC-EAPTLS-005: Empty TLS session → error
- TC-EAPTLS-006: Insufficient key material (<64 bytes) → error
- TC-EAPTLS-007: Key export label is "EAP-TLS MSK"
- TC-EAPTLS-008: Session ID included in derivation context
- TC-EAPTLS-009: Server handshake_messages included in derivation
- TC-EAPTLS-010: Peer certificate used in derivation when available
- Strategy: Mock TLS session returning pre-defined master secret (per research §7.3)
- Uses `testify/assert` and `testify/require`
- Package: `test/conformance`
</action>

<acceptance_criteria>
- All 10 RFC 5216 MSK derivation test cases exist
- `go test ./test/conformance/... -run RFC5216` passes
- `go build ./test/conformance/...` compiles without error
</acceptance_criteria>

---

## Task 8 — Phase Validation Checklist (`docs/roadmap/PHASE_6_Testing_NRM.md`)

<read_first>
- `docs/roadmap/PHASE_4_NFIntegration_Observability.md` — existing phase validation checklist format
- All plan files from Waves 1-5
</read_first>

<action>
Create or update `docs/roadmap/PHASE_6_Testing_NRM.md` with validation checklist:
```markdown
## Validation Checklist

- [ ] `go build ./...` compiles without errors
- [ ] `go test ./... -short` passes (unit + conformance only)
- [ ] `go test -coverprofile=coverage.out ./...` — overall coverage >80%
- [ ] `go test ./test/conformance/...` — all ~52 conformance cases pass
- [ ] `docker-compose -f compose/dev.yaml up -d` starts full stack
- [ ] `go test ./test/e2e/...` — all E2E tests pass
- [ ] `go test ./test/integration/...` — all integration tests pass
- [ ] `curl http://localhost:8081/restconf/data/3gpp-nssaaf-nrm:nssaa-function` returns valid JSON
- [ ] `curl http://localhost:8081/restconf/data/3gpp-nssaaf-nrm:alarms` returns alarm list
- [ ] REQ-26: Coverage report shows >80% overall
- [ ] REQ-27: All API endpoints have integration tests
- [ ] REQ-28: E2E tests verify AMF→HTTP GW→Biz→AAA GW→AAA-S flow
- [ ] REQ-29: ~30 TS 29.526 §7.2 test cases pass
- [ ] REQ-30: ~10 RFC 3579 test cases pass
- [ ] REQ-31: ~10 RFC 5216 test cases pass
- [ ] REQ-32: NSSAAFFunction IOC readable via RESTCONF
- [ ] REQ-33: Alarm raised when failure rate >10%
- [ ] REQ-34: Alarm raised when circuit breaker opens
- [ ] `go fmt ./...` produces clean output
- [ ] `golangci-lint run ./...` passes
```

</action>

<acceptance_criteria>
- Checklist file exists at `docs/roadmap/PHASE_6_Testing_NRM.md`
- All 9 REQs have corresponding checklist items
- Checklist items are grep-verifiable

</acceptance_criteria>

</tasks>

<verification>

Overall verification for Wave 5:
- `go build ./test/e2e/...` compiles without error
- `go build ./test/conformance/...` compiles without error
- `go test ./test/conformance/...` passes (all 52 cases)
- `docker-compose -f compose/dev.yaml -f compose/test.yaml up -d` starts full stack
- `go test ./test/e2e/...` passes (all 15 E2E cases)
- NRM RESTCONF endpoints respond correctly

</verification>

<success_criteria>

- REQ-28: E2E tests cover full AMF → HTTP GW → Biz Pod → AAA GW → AAA-S flow
- REQ-29: All 32 TS 29.526 §7.2 conformance test cases pass
- REQ-30: All ~10 RFC 3579 conformance test cases pass
- REQ-31: All ~10 RFC 5216 EAP-TLS MSK derivation test cases pass
- REQ-32: NSSAAFFunction IOC readable via RESTCONF
- REQ-33: Alarm raised when auth failure rate >10%
- REQ-34: Alarm raised when circuit breaker opens
- All 7 new test files created and passing:
  - `test/e2e/harness.go`
  - `test/e2e/nssaa_flow_test.go` (8 cases)
  - `test/e2e/reauth_test.go` (4 cases)
  - `test/e2e/revocation_test.go` (3 cases)
  - `test/conformance/ts29526_test.go` (32 cases)
  - `test/conformance/rfc3579_test.go` (10 cases)
  - `test/conformance/rfc5216_test.go` (10 cases)
- `docs/roadmap/PHASE_6_Testing_NRM.md` checklist updated with all REQ verifications
- Phase 6 validation checklist complete and grep-verifiable

</success_criteria>
