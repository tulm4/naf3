---
phase: 06
plan: 06-PLAN-4
wave: 4
depends_on: [06-PLAN-1, 06-PLAN-2]
requirements: [REQ-27, REQ-33, REQ-34]
files_modified:
  - test/integration/nssaa_api_test.go
  - test/integration/aiw_api_test.go
  - test/integration/postgres_test.go
  - test/integration/redis_test.go
  - test/integration/circuit_breaker_test.go
  - test/integration/nrf_mock_test.go
  - test/integration/udm_mock_test.go
  - test/integration/ausf_mock_test.go
  - test/integration/alarm_test.go
  - compose/test.yaml
---

<objective>

Integration tests exercising all API endpoints against real PostgreSQL and Redis via docker-compose. Tests use NF httptest mocks from Wave 1 and exercise real component integration (HTTP handler → PostgreSQL → Redis → AAA GW). This wave requires infrastructure (PG, Redis) but not the full E2E stack (AAA-S container, HTTP Gateway, Biz Pod binary).

</objective>

<tasks>

## Task 1 — Integration Test Compose (`compose/test.yaml`)

<read_first>
- `compose/dev.yaml` — existing compose structure
- `compose/configs/biz.yaml` — Biz Pod config reference
- `06-CONTEXT.md` — D-01: Docker-compose for real PostgreSQL and Redis
</read_first>

<action>
Create `compose/test.yaml` — extends `compose/dev.yaml` for test isolation:
```yaml
# compose/test.yaml — Test isolation overlay
services:
  # Override: Biz Pod, HTTP GW, AAA GW started by test binary
  # Add: test-specific database
  postgres_test:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: nssaa_test
      POSTGRES_PASSWORD: nssaa_test
      POSTGRES_DB: nssaa_test
    ports: ["5433:5432"]
    volumes:
      - postgres_test_data:/var/lib/postgresql/data

  # Use dev Redis for integration tests
  redis_test:
    image: redis:7-alpine
    ports: ["6380:6379"]
    command: redis-server --save "" --appendonly no
```
- Extends `compose/dev.yaml` via `docker-compose -f compose/dev.yaml -f compose/test.yaml`
- Separate test database (`nssaa_test`) to avoid polluting dev data
- Separate Redis port (6380) for test isolation
- No HTTP GW, Biz Pod, AAA GW (test binary starts these)
</action>

<acceptance_criteria>
- `docker-compose -f compose/dev.yaml -f compose/test.yaml config` validates without error
- `docker-compose -f compose/dev.yaml -f compose/test.yaml up -d postgres_test redis_test` starts services
- Test database `nssaa_test` is created
</acceptance_criteria>

---

## Task 2 — N58 API Integration Tests (`test/integration/nssaa_api_test.go`)

<read_first>
- `internal/api/nssaa/handler_test.go` — existing handler tests (mock pattern)
- `internal/api/nssaa/handler.go` — N58 handler implementation
- `test/mocks/udm.go` — UDM mock (Wave 1)
- `06-CONTEXT.md` — D-01: real PostgreSQL and Redis for integration tests
</read_first>

<action>
Create `test/integration/nssaa_api_test.go` — full N58 API integration against real DB:
- `TestIntegration_NSSAA_CreateSession` — POST /slice-authentications → 201, session in PostgreSQL
- `TestIntegration_NSSAA_CreateSession_GPSIStoredEncrypted` — GPSI encrypted at rest in PostgreSQL
- `TestIntegration_NSSAA_ConfirmSession` — PUT /slice-authentications/{id} → 200
- `TestIntegration_NSSAA_GetSession` — GET /slice-authentications/{id} → 200
- `TestIntegration_NSSAA_GetSession_NotFound` — GET nonexistent → 404
- `TestIntegration_NSSAA_SessionInRedis` — session cached in Redis after creation
- `TestIntegration_NSSAA_ConfirmSession_InvalidBase64` — invalid base64 → 400
- `TestIntegration_NSSAA_ConfirmSession_GPSIMismatch` — GPSI mismatch → 400
- `TestIntegration_NSSAA_ConfirmSession_SnssaiMismatch` — snssai mismatch → 400
- `TestIntegration_NSSAA_ConcurrentSessions` — 10 concurrent creates → all succeed
- `TestIntegration_NSSAA_SessionExpiry` — session expires after configured TTL
- Uses real PostgreSQL via `TEST_DATABASE_URL` env var (defaults to `postgres://nssaa_test:nssaa_test@localhost:5433/nssaa_test`)
- Uses real Redis via `TEST_REDIS_URL` env var (defaults to `localhost:6380`)
- Uses `test/mocks/udm.go` for UDM mock
- Uses `testify/require` for infrastructure checks
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 11 N58 integration tests pass against real PostgreSQL and Redis
- Session data is encrypted in PostgreSQL (verify via direct DB query)
- Redis caching is verified
- `go build ./test/integration/...` compiles without error
- `go test ./test/integration/... -count=1 -run NSSAA` passes
</acceptance_criteria>

---

## Task 3 — N60 API Integration Tests (`test/integration/aiw_api_test.go`)

<read_first>
- `internal/api/aiw/handler_test.go` — existing handler tests (mock pattern)
- `internal/api/aiw/handler.go` — N60 handler implementation
- `test/mocks/ausf.go` — AUSF mock (Wave 1)
</read_first>

<action>
Create `test/integration/aiw_api_test.go` — full N60 API integration against real DB:
- `TestIntegration_AIW_CreateSession` — POST /authentications → 201
- `TestIntegration_AIW_ConfirmSession` — PUT /authentications/{id} → 200
- `TestIntegration_AIW_GetSession` — GET /authentications/{id} → 200
- `TestIntegration_AIW_GetSession_NotFound` — GET nonexistent → 404
- `TestIntegration_AIW_SessionInRedis` — session cached in Redis
- `TestIntegration_AIW_InvalidSupi` — invalid SUPI → 400
- `TestIntegration_AIW_SupiMismatch` — SUPI mismatch in body → 400
- `TestIntegration_AIW_ConcurrentSessions` — 10 concurrent creates → all succeed
- Uses `test/mocks/ausf.go` for AUSF mock
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 8 N60 integration tests pass
- `go test ./test/integration/... -run AIW` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

---

## Task 4 — PostgreSQL Integration Tests (`test/integration/postgres_test.go`)

<read_first>
- `internal/storage/postgres/session_store_test.go` — existing session store tests
- `internal/storage/postgres/session_store.go` — session store implementation
- `compose/test.yaml` — test database configuration
</read_first>

<action>
Create `test/integration/postgres_test.go` — PostgreSQL session store integration:
- `TestIntegration_PG_SessionCreate` — insert session, query back
- `TestIntegration_PG_SessionEncryption` — verify GPSI/SUPI encrypted in DB
- `TestIntegration_PG_SessionUpdate` — update session state
- `TestIntegration_PG_SessionDelete` — delete session
- `TestIntegration_PG_MonthlyPartition` — partition creation for next month
- `TestIntegration_PG_QueryByGPSI` — query sessions by GPSI
- `TestIntegration_PG_QueryBySnssai` — query sessions by SNSSAI
- `TestIntegration_PG_ConnPoolHealth` — pool health check passes
- `TestIntegration_PG_MultipleConn` — concurrent writes with multiple connections
- Uses `TEST_DATABASE_URL` env var
- Partition tests use `t.Skip()` if `testing.Short()` is set
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 9 PostgreSQL integration tests pass against real DB
- Session data is encrypted at rest
- Partition creation succeeds
- `go test ./test/integration/... -run PG` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

---

## Task 5 — Redis Integration Tests (`test/integration/redis_test.go`)

<read_first>
- `internal/cache/redis/cache_test.go` — existing Redis cache tests (uses miniredis)
- `internal/cache/redis/cache.go` — cache implementation
- `internal/cache/redis/dlq_test.go` — existing DLQ tests
</read_first>

<action>
Create `test/integration/redis_test.go` — Redis cache and DLQ integration:
- `TestIntegration_Redis_CacheSession` — cache session with TTL
- `TestIntegration_Redis_GetCachedSession` — retrieve cached session
- `TestIntegration_Redis_CacheExpiry` — TTL expiration
- `TestIntegration_Redis_CacheEviction` — LRU eviction under memory pressure
- `TestIntegration_Redis_DLQ_Publish` — publish failed AMF notification to DLQ
- `TestIntegration_Redis_DLQ_Consume` — consume from DLQ
- `TestIntegration_Redis_DLQ_RetryOrder` — FIFO retry order
- `TestIntegration_Redis_CircuitBreakerCache` — CB state cached in Redis
- Uses `TEST_REDIS_URL` env var
- Uses `t.Skip()` if `TEST_REDIS_URL` is not set
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 8 Redis integration tests pass against real Redis
- Cache TTL and expiry verified
- DLQ publish/consume verified
- `go test ./test/integration/... -run Redis` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

---

## Task 6 — NRF Mock Integration Tests (`test/integration/nrf_mock_test.go`)

<read_first>
- `internal/nrf/client_test.go` — existing NRF client tests
- `test/mocks/nrf.go` — NRF mock (Wave 1)
- `06-CONTEXT.md` — D-02: NRF mocked as httptest server
</read_first>

<action>
Create `test/integration/nrf_mock_test.go` — NRF mock integration with Biz Pod:
- `TestIntegration_NRF_Discovery` — Biz Pod discovers UDM via NRF mock
- `TestIntegration_NRF_Registration` — Biz Pod registers with NRF mock
- `TestIntegration_NRF_Heartbeat` — heartbeat PUT succeeds
- `TestIntegration_NRF_ServiceDiscovery` — query by service name returns NF instances
- Uses `test/mocks/nrf.go` NRF mock
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 4 NRF mock integration tests pass
- `go test ./test/integration/... -run NRF` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

---

## Task 7 — UDM Mock Integration Tests (`test/integration/udm_mock_test.go`)

<read_first>
- `internal/udm/client_test.go` — existing UDM client tests
- `test/mocks/udm.go` — UDM mock (Wave 1)
</read_first>

<action>
Create `test/integration/udm_mock_test.go` — UDM mock integration:
- `TestIntegration_UDM_GetRegistration` — N58 handler calls UDM mock
- `TestIntegration_UDM_GPSIKnown` — GPSI known → returns SUCI/SUPI mapping
- `TestIntegration_UDM_GPSIUnknown` — GPSI unknown → N58 handler returns 404
- `TestIntegration_UDM_Timeout` — UDM timeout → N58 handler returns 504
- Uses `test/mocks/udm.go` UDM mock
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 4 UDM mock integration tests pass
- `go test ./test/integration/... -run UDM` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

---

## Task 8 — Circuit Breaker Integration Tests (`test/integration/circuit_breaker_test.go`)

<read_first>
- `internal/resilience/circuit_breaker.go` — CB implementation
- `internal/resilience/circuit_breaker_test.go` — existing CB tests (unit)
- `test/mocks/compose.go` — compose lifecycle helpers (Wave 1)
- `06-CONTEXT.md` — REQ-34: circuit breaker alarm integration
</read_first>

<action>
Create `test/integration/circuit_breaker_test.go` — circuit breaker with real components:
- `TestIntegration_CB_OpenOnFailures` — consecutive failures open CB
- `TestIntegration_CB_HalfOpenOnTimeout` — recovery timeout → HALF_OPEN
- `TestIntegration_CB_CloseOnSuccess` — success in HALF_OPEN → CLOSED
- `TestIntegration_CB_NRMAlarmRaised` — CB OPEN → NRM alarm raised (REQ-34)
- `TestIntegration_CB_NRMAlarmCleared` — CB CLOSED → NRM alarm cleared
- `TestIntegration_CB_AAAUnreachableAlarm` — AAA unreachable → NSSAA_AAA_SERVER_UNREACHABLE alarm raised
- Uses `test/mocks/compose.go` to start services
- Sends NRM events via HTTP POST to NRM server
- Package: `test/integration`
</acceptance_criteria>
- All 6 circuit breaker integration tests pass
- REQ-34 verified: NRM alarm raised on CB OPEN
- `go test ./test/integration/... -run CB` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

---

## Task 9 — NRM Alarm Integration Tests (`test/integration/alarm_test.go`)

<read_first>
- `internal/nrm/alarm.go` — alarm store/manager (Wave 2)
- `internal/nrm/server.go` — RESTCONF server (Wave 2)
- `cmd/nrm/main.go` — NRM binary (Wave 2)
</read_first>

<action>
Create `test/integration/alarm_test.go` — end-to-end alarm integration:
- `TestIntegration_Alarm_RaiseViaRESTCONF` — POST /internal/events → alarm stored → GET /restconf/data/3gpp-nssaaf-nrm:alarms shows alarm
- `TestIntegration_Alarm_ClearViaRESTCONF` — clear alarm → GET shows cleared
- `TestIntegration_Alarm_Acknowledge` — POST ack → alarm acknowledged
- `TestIntegration_Alarm_Deduplication` — raise same alarm twice within 5 min → deduplicated
- `TestIntegration_Alarm_NssaaFunction` — GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function returns function data
- `TestIntegration_Alarm_FailureRateAlarm` — push auth failures with >10% rate → NSSAA_HIGH_AUTH_FAILURE_RATE alarm raised (REQ-33)
- `TestIntegration_Alarm_CircuitBreakerAlarm` — push CB OPEN event → NSSAA_CIRCUIT_BREAKER_OPEN alarm raised (REQ-34)
- Starts real `cmd/nrm` binary for these tests
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 7 alarm integration tests pass against real NRM binary
- REQ-33: >10% failure rate triggers alarm
- REQ-34: CB OPEN triggers alarm
- Deduplication verified
- `go test ./test/integration/... -run Alarm` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

---

## Task 10 — AUSF Mock Integration Tests (`test/integration/ausf_mock_test.go`)

<read_first>
- `internal/ausf/client_test.go` — existing AUSF client tests
- `test/mocks/ausf.go` — AUSF mock (Wave 1)
</read_first>

<action>
Create `test/integration/ausf_mock_test.go` — AUSF mock integration:
- `TestIntegration_AUSF_GetUeAuthData` — N60 handler calls AUSF mock → returns auth data
- `TestIntegration_AUSF_UnknownGPSI` — unknown GPSI → N60 handler returns 404
- `TestIntegration_AUSF_Timeout` — AUSF timeout → N60 handler returns 504
- Uses `test/mocks/ausf.go` AUSF mock
- Package: `test/integration`
</action>

<acceptance_criteria>
- All 3 AUSF mock integration tests pass
- `go test ./test/integration/... -run AUSF` passes
- `go build ./test/integration/...` compiles without error
</acceptance_criteria>

</tasks>

<verification>

Overall verification for Wave 4:
- `docker-compose -f compose/dev.yaml -f compose/test.yaml up -d` starts test infrastructure
- `go test ./test/integration/...` passes
- All infrastructure-dependent tests check `testing.Short()` and skip appropriately
- REQ-33 and REQ-34 are verified through the alarm integration tests
- `go fmt ./test/integration/...` produces clean output

</verification>

<success_criteria>

- REQ-27: All API endpoints have integration tests (N58 + N60)
- REQ-33: Alarm raised when auth failure rate >10% (verified in alarm_test.go)
- REQ-34: Alarm raised when circuit breaker opens (verified in circuit_breaker_test.go)
- All 9 integration test files created and passing:
  - `test/integration/nssaa_api_test.go` (11 cases)
  - `test/integration/aiw_api_test.go` (8 cases)
  - `test/integration/postgres_test.go` (9 cases)
  - `test/integration/redis_test.go` (8 cases)
  - `test/integration/circuit_breaker_test.go` (6 cases)
  - `test/integration/alarm_test.go` (7 cases)
  - `test/integration/nrf_mock_test.go` (4 cases)
  - `test/integration/udm_mock_test.go` (4 cases)
  - `test/integration/ausf_mock_test.go` (3 cases)
- `compose/test.yaml` extends `compose/dev.yaml` correctly
- Test infrastructure starts cleanly via `docker-compose`
- All tests use environment variables for DB/Redis URLs (not hardcoded)

</success_criteria>
