---
phase: 06
plan: 06-PLAN-6
wave: 5
depends_on: [06-PLAN-1, 06-PLAN-2]
requirements: [REQ-27, REQ-28, REQ-29, REQ-31]
files_modified:
  - test/e2e/aiw_flow_test.go
  - test/conformance/aiw_conformance_test.go
---

<objective>

AIW (Nnssaaf_AIW, N60 interface to AUSF) E2E tests at 3-component level plus TS 29.526 §7.3 conformance tests. AIW tests use AUSF mock (httptest server, Wave 1) + mock-aaa-s container. MSK verification is the critical check: RFC 5216 §2.1.4 requires 64-octet MSK and MSK != EMSK. D-08: both Biz Pod unit tests (PLAN-3) and 3-component E2E for AIW.

</objective>

<tasks>

## Task 1 — AIW 3-Component E2E Tests (`test/e2e/aiw_flow_test.go`)

<read_first>
- `docs/design/24_test_strategy.md` §5.3 — AIW E2E test cases (MSK extraction, TTLS, EAP failure, invalid SUPI, AAA not configured)
- `test/e2e/harness.go` — E2E harness (PLAN-5 Task 1)
- `test/mocks/ausf.go` — AUSF mock httptest server (Wave 1)
- `test/aaa_sim/main.go` — AAA-S simulator (Wave 1)
- `internal/api/aiw/handler_test.go` — existing AIW handler test patterns
</read_first>

<action>
Create `test/e2e/aiw_flow_test.go` — full AIW 3-component E2E tests using the harness:

- `TestE2E_AIW_BasicFlow` — AUSF → HTTP GW → Biz Pod → AAA GW → AAA-S → success
  1. Start full harness (3 components + AUSF mock + mock-aaa-s)
  2. POST to HTTP GW (N60 API) with valid SUPI (`imu-208046000000001`), eapIdRsp
  3. Verify 201 response, Location header, authCtxId
  4. PUT confirm (EAP-TLS handshake rounds)
  5. Verify 200 response with EAP-Message or authResult
- `TestE2E_AIW_MSKExtraction` — MSK forwarded to AUSF mock, 64 octets, MSK != EMSK
  1. Establish baseline AIW session
  2. AAA-S returns MSK=64-octet random bytes per RFC 5216 §2.1.4
  3. Verify AUSF mock received MSK in response
  4. Decode base64 MSK → must be exactly 64 bytes
  5. Verify MSK[:32] != MSK[32:] (MSK != EMSK)
- `TestE2E_AIW_EAPFailure` — AAA-S returns Access-Reject → HTTP 200 with authResult=EAP_FAILURE (not HTTP 403)
  1. Configure AAA-S mode: REJECT
  2. Send AIW auth request
  3. Verify 200 OK (not 403) with authResult=EAP_FAILURE
  4. Verify Msk is empty, PvsInfo is nil
- `TestE2E_AIW_InvalidSupi` — invalid SUPI format → HTTP 400, cause=INVALID_SUPI
  1. Send AIW auth request with SUPI="invalid-supi-format"
  2. Verify 400 Bad Request, ProblemDetails.cause=INVALID_SUPI
  3. SUPI must match `^imu-[0-9]{15}$`
- `TestE2E_AIW_AAA_NotConfigured` — SUPI range with no AAA config → HTTP 404
  1. Send AIW auth with SUPI in unconfigured range (`imu-999999999999999`)
  2. Verify 404 Not Found, ProblemDetails.cause=AAA_SERVER_NOT_CONFIGURED
- `TestE2E_AIW_TTLS` — EAP-TTLS with inner PAP method, PVSInfo returned
  1. Configure AAA-S mode: EAP_TTLS with inner PAP
  2. Send AIW auth with ttlsInnerMethodContainer
  3. Verify EAP-TTLS handshake completes
  4. Verify PvsInfo returned with ServerType=PROSE

- Uses `test/e2e/harness.go`
- Uses `t.SkipIf(testing.Short(), "E2E tests skipped in short mode")`
- Uses `testify/assert` and `testify/require`
- Package: `test/e2e`
</action>

<acceptance_criteria>
- All 6 AIW E2E test cases exist
- Happy path verifies full AUSF → HTTP GW → Biz Pod → AAA GW → AAA-S flow
- MSK extraction verified: 64 bytes, MSK != EMSK
- EAP failure verified: HTTP 200 with authResult=EAP_FAILURE in body
- `go test ./test/e2e/... -run AIW` passes
- `go build ./test/e2e/...` compiles without error
</acceptance_criteria>

---

## Task 2 — AIW Conformance Tests (`test/conformance/aiw_conformance_test.go`)

<read_first>
- `docs/design/24_test_strategy.md` §5.3 — AIW conformance cases (TC-AIW-01 through TC-AIW-13)
- `06-RESEARCH.md` §7.1 — conformance test strategy
- `internal/api/aiw/handler.go` — N60 handler implementation
- `test/unit/api/aiw_handler_gaps_test.go` — Biz Pod unit tests (PLAN-3 Task 6)
</read_first>

<action>
Create `test/conformance/aiw_conformance_test.go` — TS 29.526 §7.3 AIW conformance tests using table-driven subtests:

- TC-AIW-01: BasicAuthFlow — valid SUPI + eapIdRsp → 201 Created, Location header
- TC-AIW-02: MSKReturnedOnSuccess — EAP_SUCCESS → 200 with 64-octet MSK in body (RFC 5216 §2.1.4)
- TC-AIW-03: PVSInfoReturned — EAP_SUCCESS → PvsInfo array present in response
- TC-AIW-04: EAPFailureInBody — EAP_FAILURE → 200 OK with authResult=EAP_FAILURE in body (not HTTP 403)
- TC-AIW-05: InvalidSupiRejected — SUPI not matching `^imu-[0-9]{15}$` → 400 Bad Request
- TC-AIW-06: AAA_NotConfigured — no AAA server for SUPI range → 404 Not Found
- TC-AIW-07: MultiRoundChallenge — multi-step EAP-TLS handshake → final authResult
- TC-AIW-08: SupportedFeaturesEcho — N60 SupportedFeatures echoed in response headers
- TC-AIW-09: TTLSInnerMethodContainer — ttlsInnerMethodContainer echoed in response
- TC-AIW-10: MSKLength64Octets — MSK must be exactly 64 bytes per RFC 5216 §2.1.4
- TC-AIW-11: MSKNotEqualEMSK — MSK[:32] != MSK[32:] (MSK and EMSK are distinct)
- TC-AIW-12: NoReauthSupport — AIW (N60) does not support SLICE_RE_AUTH per TS 29.526 AC8
- TC-AIW-13: NoRevocationSupport — AIW (N60) does not support SLICE_REVOCATION per TS 29.526 AC8

- Uses `newMockStore()` pattern from existing handler tests
- Uses `httptest.NewServer` for in-process testing
- Uses `testify/assert` and `testify/require`
- Package: `test/conformance`
</action>

<acceptance_criteria>
- All 13 AIW conformance test cases exist
- MSK length verified: exactly 64 bytes
- MSK/EMSK split verified: MSK[:32] != MSK[32:]
- EAP failure verified: 200 with authResult=EAP_FAILURE in body (not HTTP 403)
- No re-auth/revocation support verified for AIW
- `go test ./test/conformance/... -run AIW` passes
- `go build ./test/conformance/...` compiles without error
</acceptance_criteria>

</tasks>

<verification>

Overall verification for PLAN-6:
- `go build ./test/e2e/...` compiles without error
- `go build ./test/conformance/...` compiles without error
- `go test ./test/conformance/... -run AIW` passes (13 cases)
- `go test ./test/e2e/... -run AIW` passes (6 cases, skip in short mode)

</verification>

<success_criteria>

- REQ-28: AIW 3-component E2E tests verify AUSF → HTTP GW → Biz Pod → AAA GW → AAA-S flow
- REQ-29: AIW conformance tests cover TS 29.526 §7.3
- REQ-31: MSK derivation verified: 64 octets, MSK != EMSK
- All 19 new AIW test cases created and passing:
  - `test/e2e/aiw_flow_test.go` (6 cases)
  - `test/conformance/aiw_conformance_test.go` (13 cases)
- D-08 honored: both layers covered for AIW (Biz Pod unit + 3-component E2E)

</success_criteria>
