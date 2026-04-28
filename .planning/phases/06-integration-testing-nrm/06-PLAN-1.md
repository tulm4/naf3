---
phase: 06
plan: 06-PLAN-1
wave: 1
depends_on: []
requirements: [REQ-26, REQ-27]
files_modified:
  - test/mocks/nrf.go
  - test/mocks/udm.go
  - test/mocks/amf.go
  - test/mocks/ausf.go
  - test/mocks/compose.go
  - test/aaa_sim/main.go
  - test/aaa_sim/radius.go
  - test/aaa_sim/diameter.go
  - test/integration/integration.go (update)
  - test/e2e/e2e.go (update)
  - go.mod (add DATA-DOG/go-sqlmock)
---

<objective>

Scaffold the test infrastructure foundation: NF httptest mocks (NRF, UDM, AMF, AUSF), AAA-S simulator for E2E, and compose helpers. All mocks implement the actual HTTP API contracts and run inside the test binary. This is Wave 1 — the foundation that Waves 3-5 depend on.

</objective>

<tasks>

## Task 1 — NRF Mock (`test/mocks/nrf.go`)

<read_first>
- `internal/nrf/client.go` — NRF client interface, NF profile structure
- `docs/3gppfilter/05_data_management/NSSAAF_DataTypes.md` — NF profile fields
- `docs/design/05_nf_profile.md` — NRF registration/discovery spec
</read_first>

<action>
Create `test/mocks/nrf.go` — an `httptest.Server` implementing the NRF Nnrf_NFM API:
- `GET /nnrf-nfm/v1/nf_instances/{nfInstanceId}` — returns NF profile JSON
- `POST /nnrf-nfm/v1/nf_instances` — registration
- `PUT /nnrf-nfm/v1/subscriptions/{subscriptionId}` — heartbeat
- Support `nfStatus=REGISTERED` query param for service discovery
- Implement UDM, AMF, AUSF, AAA-GW profiles with configurable `nfStatus`
- Follow the `mockAAAClient` pattern from `internal/eap/engine_test.go`: configurable via struct fields, mutex-protected state
- Package: `test/mocks`
</action>

<acceptance_criteria>
- `grep -r "type NRFMock" test/mocks/` returns the mock type
- Mock responds to `GET /nnrf-nfm/v1/nf_instances/udm-001` with valid NF profile JSON
- Mock supports `nfStatus=REGISTERED` filter
- `go build ./test/mocks/...` compiles without error
</acceptance_criteria>

---

## Task 2 — UDM Mock (`test/mocks/udm.go`)

<read_first>
- `internal/udm/client.go` — UDM client, Nudm_UECM_Get response structure
- `docs/3gppfilter/05_data_management/NSSAAF_DataTypes.md` — SUPI/GPSI types
</read_first>

<action>
Create `test/mocks/udm.go` — an `httptest.Server` implementing the UDM Nudm_UECM API:
- `GET /nudm-uemm/v1/{supi}/registration` — returns registration data
- Support two variants: GPSI known (returns SUCI/mapped SUPI + GPSI) and GPSI unknown (returns 404)
- Configurable SUPI-to-GPSI mapping via `UDMMock.SetGPSI(supi, gpsi)`
- Follow `mockStore` pattern from `internal/api/nssaa/handler_test.go`
- Package: `test/mocks`
</action>

<acceptance_criteria>
- Mock returns valid Nudm_UECM_Get JSON for known GPSI
- Mock returns 404 for unknown GPSI
- `go build ./test/mocks/...` compiles without error
</acceptance_criteria>

---

## Task 3 — AMF Mock (`test/mocks/amf.go`)

<read_first>
- `internal/amf/notifier.go` — AMF notification sender, Nssaa-Notification payload
- `docs/3gppfilter/02_procedures/NSSAA_flow_AMF.md` — AMF notification spec
</read_first>

<action>
Create `test/mocks/amf.go` — an `httptest.Server` implementing the AMF callback receiver:
- `POST /namf-callback/v1/{amfId}/Nssaa-Notification` — receives re-auth/revocation notifications
- Returns 204 on success
- Stores received notifications for test assertions: `AMFMock.GetNotifications()` returns slice
- Implements `SLICE_RE_AUTH` and `SLICE_REVOCATION` NotificationType parsing
- Configurable: respond with error code (503) for failure injection tests
- Package: `test/mocks`
</action>

<acceptance_criteria>
- Mock receives POST with valid NssaaNotification JSON
- Mock stores notification for retrieval via `GetNotifications()`
- Mock can be configured to return 503 for failure injection
- `go build ./test/mocks/...` compiles without error
</acceptance_criteria>

---

## Task 4 — AUSF Mock (`test/mocks/ausf.go`)

<read_first>
- `internal/ausf/client.go` — AUSF client, Nnssaaf_AIW_Get response
- `docs/design/23_ausf_integration.md` — AUSF N60 interface spec
</read_first>

<action>
Create `test/mocks/ausf.go` — an `httptest.Server` implementing the AUSF N60 API:
- `GET /nausf-auth/v1/ue-identities/{gpsi}` — returns UE authentication data for N60
- Configurable via `AUSFMock.SetUEAuthData(gpsi, authData)` and `SetError(gpsi, statusCode)`
- Returns valid `UeAuthData` JSON per TS 29.518
- Package: `test/mocks`
</action>

<acceptance_criteria>
- Mock returns valid UE auth data for known GPSI
- Mock returns configured error code for unknown/error cases
- `go build ./test/mocks/...` compiles without error
</acceptance_criteria>

---

## Task 5 — Docker-Compose Lifecycle Helper (`test/mocks/compose.go`)

<read_first>
- `compose/dev.yaml` — existing service definitions
- `compose/mock_aaa_s.go` — existing AAA-S mock structure
</read_first>

<action>
Create `test/mocks/compose.go` — helpers for E2E test docker-compose lifecycle:
- `ComposeUp(ctx, composeFile string)` — starts services via `docker-compose up -d`
- `ComposeDown(ctx, composeFile string)` — tears down via `docker-compose down`
- `WaitForHealthy(ctx, service string, timeout time.Duration)` — polls health checks
- `GetServiceAddr(service string) (host string, port int)` — reads published port
- Uses `os/exec` to run docker-compose commands
- Logs output on failure
- Package: `test/mocks`
</action>

<acceptance_criteria>
- `ComposeUp` starts services and returns when all are healthy
- `ComposeDown` stops all services cleanly
- `WaitForHealthy` returns error on timeout
- `go build ./test/mocks/...` compiles without error
</acceptance_criteria>

---

## Task 6 — AAA-S Simulator (`test/aaa_sim/`)

<read_first>
- `compose/mock_aaa_s.go` — existing mock implementation (reference)
- `internal/radius/client.go` — RADIUS client send/recv pattern
- `docs/design/07_radius_client.md` — RADIUS encoding
- `docs/design/08_diameter_client.md` — Diameter encoding
</read_first>

<action>
Create `test/aaa_sim/` as a standalone Go package (not a `cmd/` binary — compiled via separate go build):
- `test/aaa_sim/main.go` — reads `AAA_SIM_MODE` env var (`EAP_TLS_SUCCESS`, `EAP_TLS_FAILURE`, `EAP_TLS_CHALLENGE`), starts RADIUS and Diameter listeners
- `test/aaa_sim/radius.go` — RADIUS EAP handling:
  - `Access-Request` → `Access-Accept` (EAP-Success) in SUCCESS mode
  - `Access-Request` → `Access-Reject` (EAP-Failure) in FAILURE mode
  - `Access-Request` → `Access-Challenge` (EAP-TLS handshake) in CHALLENGE mode
  - Message-Authenticator (RFC 3579) validation and computation
  - Configurable shared secret via `AAA_SIM_SECRET` env (default: "testing123")
- `test/aaa_sim/diameter.go` — Diameter EAP handling:
  - CER/CEA capabilities exchange
  - DER → DEA with configurable result (success/failure/challenge)
- Uses real `net.PacketConn` (UDP) for RADIUS, `net.Listener` (TCP) for Diameter
- Package: `test/aaa_sim`
</action>

<acceptance_criteria>
- Simulates EAP-TLS success flow (RADIUS Access-Accept)
- Simulates EAP-TLS failure flow (RADIUS Access-Reject)
- Simulates EAP-TLS challenge flow (RADIUS Access-Challenge → Access-Accept)
- Builds as standalone binary: `go build -o aaa-sim ./test/aaa_sim/`
- Message-Authenticator correctly computed and validated
</acceptance_criteria>

---

## Task 7 — Add sqlmock to `go.mod`

<read_first>
- `go.mod` — existing dependencies
</read_first>

<action>
Run `go get github.com/DATA-DOG/go-sqlmock@v1.5.2` and `go mod tidy`. This is the only new Go dependency allowed per the phase constraints.
</action>

<acceptance_criteria>
- `grep "DATA-DOG/go-sqlmock" go.mod` returns the dependency
- `go mod tidy` produces clean go.sum
</acceptance_criteria>

---

## Task 8 — Update `test/integration/integration.go` and `test/e2e/e2e.go`

<read_first>
- `test/integration/integration.go` — existing (empty) integration test scaffold
- `test/e2e/e2e.go` — existing (empty) E2E test scaffold
</read_first>

<action>
Update both files to import `test/mocks` and `test/aaa_sim`. Add package declaration and placeholder TestMain functions that can be expanded in Waves 3-5. Keep minimal — Wave 1 scaffolding only.
</action>

<acceptance_criteria>
- Both files have valid package declarations
- `go build ./test/...` compiles without error
</acceptance_criteria>

</tasks>

<verification>

Overall verification for Wave 1:
- `go build ./...` compiles without errors (all new packages)
- `go build ./test/mocks/...` compiles without errors
- `go build -o aaa-sim ./test/aaa_sim/` builds the AAA-S simulator binary
- `go get github.com/DATA-DOG/go-sqlmock@v1.5.2 && go mod tidy` succeeds
- `go test ./test/mocks/... -count=1` passes (test discovery + basic mock correctness)
- `go test ./test/aaa_sim/... -count=1` passes (test discovery + AAA-S simulator correctness)
- All mocks implement the expected HTTP handlers

</verification>

<success_criteria>

- All 5 mock packages (`test/mocks/nrf`, `test/mocks/udm`, `test/mocks/amf`, `test/mocks/ausf`, `test/mocks/compose`) compile without error
- AAA-S simulator builds as standalone binary
- `go-sqlmock` added to `go.mod` without introducing other new dependencies
- NF mocks can be instantiated in tests and respond to expected HTTP endpoints
- AMF mock stores received notifications for test assertion
- `test/integration/integration.go` and `test/e2e/e2e.go` are valid Go packages

</success_criteria>
