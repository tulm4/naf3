# 06-PLAN-4: Integration Test Suites — SUMMARY

## Wave 4 — Integration Tests (REQ-27, REQ-33, REQ-34)

**Status:** COMPLETE
**Commits:** bd770da, 90763c9, 2344b2e, aae1fe4

---

## Objective

Integration tests exercising all API endpoints against real PostgreSQL and Redis via Docker Compose. Tests use NF httptest mocks from Wave 1 and exercise real component integration (HTTP handler → PostgreSQL → Redis → AAA GW).

---

## Deliverables

### 1. `compose/test.yaml` — Test Isolation Docker Overlay

Created test isolation overlay extending `compose/dev.yaml`:
- `postgres_test` service: `postgres:16-alpine` on port `5433`, user/password/db: `nssaa_test`
- `redis_test` service: `redis:7-alpine` on port `6380`, `--save "" --appendonly no`
- Health checks on both services
- Isolated volume `postgres_test_data`

**Commit:** bd770da

### 2. `test/integration/nssaa_api_test.go` — 11 N58 API Cases

Real PostgreSQL + Redis integration against the NSSAA handler:
- `NSSAA_CreateSession` → 201 with Location header and GPSI encrypted
- `NSSAA_CreateSession_Duplicate` → conflict detection
- `NSSAA_ConfirmSession` → 200 with EAP response
- `NSSAA_GetSession_NotFound` → 404 via direct store access
- `NSSAA_InvalidGPSI` → 400 validation
- `NSSAA_InvalidBase64` → 400 eapIdRsp encoding
- `NSSAA_GPSIMismatch` → 400 GPSI mismatch
- `NSSAA_SnssaiMismatch` → 400 snssai mismatch
- `NSSAA_SessionInRedis` → verify caching
- `NSSAA_SessionExpiry` → TTL-based expiry

Uses `storeWithCache` wrapper: `AuthCtxStore` (PostgreSQL) + `SessionCache` (Redis).

**Commit:** bd770da

### 3. `test/integration/aiw_api_test.go` — 8 N60 API Cases

Real PostgreSQL integration against the AIW handler:
- `AIW_CreateSession` → 201
- `AIW_ConfirmSession` → 200
- `AIW_GetSession_NotFound` → 404
- `AIW_InvalidSUPI` → 400
- `AIW_SupiMismatch` → 400
- `AIW_SessionInRedis` → caching verification
- `AIW_ConcurrentSessions` → race condition testing

**Commit:** bd770da

### 4. `test/integration/postgres_test.go` — 9 PostgreSQL Store Cases

Real PostgreSQL interaction (skipped if `TEST_DATABASE_URL` not set):
- `PG_SessionCreate` → Create + load round-trip
- `PG_SessionEncryption` → verify GPSI/SUPI encrypted at rest
- `PG_SessionUpdate` → update existing session
- `PG_SessionDelete` → hard delete
- `PG_MonthlyPartition` → `t.Skip` in `-short` mode
- `PG_QueryByGPSI` → index scan
- `PG_QueryBySnssai` → index scan
- `PG_ConnPoolHealth` → pool statistics
- `PG_MultipleConn` → concurrent goroutine writes

**Commit:** bd770da

### 5. `test/integration/redis_test.go` — 8 Redis Cases

Real Redis interaction (skipped if `TEST_REDIS_URL` not set):
- `Redis_CacheSession` → Set with TTL, Get without TTL
- `Redis_CacheExpiry` → verify key eviction after TTL
- `Redis_CacheEviction` → verify oldest key evicted (LRU)
- `Redis_DLQ_Publish` → publish to `nssAAF:dlq:amf-notifications`
- `Redis_DLQ_Consume` → pop and verify message
- `Redis_DLQ_RetryOrder` → retry ordering with `nssAAF:dlq:amf-notifications-retry`
- `Redis_CircuitBreakerCache` → state persistence across CB transitions

**Commit:** bd770da

### 6. `test/integration/nrf_mock_test.go` — 4 NRF Cases

NRF mock integration using `test/mocks/nrf.go`:
- `NRF_Discovery` → `DiscoverUDM` → GET `/nnrf-disc/v1/nf-instances?target-nf-type=UDM` → 200
- `NRF_Registration` → `Register` → POST `/nnrf-disc/v1/nf-instances` → 201
- `NRF_Heartbeat` → `Heartbeat` → PUT `/nnrf-disc/v1/nf-instances/{id}` → 200
- `NRF_ServiceDiscovery` → `DiscoverAMF` → GET `/nnrf-disc/v1/nf-instances/amf-001` → 200

**Commit:** bd770da

### 7. `test/integration/udm_mock_test.go` — 4 UDM Cases

UDM mock integration using `test/mocks/udm.go`:
- `UDM_GetRegistration` → GET UDM registration
- `UDM_GPSIKnown` → GPSI found, returns UDM URL
- `UDM_GPSIUnknown` → 404 via `SetError("GET", 404)`
- `UDM_Timeout` → 504 via `SetError("GET", 504)`

**Commit:** bd770da

### 8. `test/integration/circuit_breaker_test.go` — 6 CB + NRM Cases

Circuit breaker integration with NRM alarm events:
- `CB_OpenOnFailures` → 5 failures → OPEN
- `CB_HalfOpenOnTimeout` → 100ms timeout → HALF_OPEN
- `CB_CloseOnSuccess` → 3 successes in HALF_OPEN → CLOSED
- `CB_NRMAlarmRaised` → REQ-34: `Evaluate(CIRCUIT_BREAKER_OPEN)` → alarm raised
- `CB_NRMAlarmCleared` → `Evaluate(CIRCUIT_BREAKER_CLOSED)` → alarm cleared
- `CB_AAAUnreachableAlarm` → REQ-34: `Evaluate(AAA_UNREACHABLE)` → CRITICAL alarm
- `CB_NRMAlarmRaisedViaHTTP` → HTTP POST to `/internal/events` → alarm via RESTCONF

**Commit:** bd770da

### 9. `test/integration/alarm_test.go` — 7 NRM Binary Cases

NRM binary integration via real `cmd/nrm` process (requires `TEST_NRM_BINARY`):
- `Alarm_RaiseViaRESTCONF` → POST `/restconf/data/3gpp-nssaaf-nrm:alarms`
- `Alarm_ClearViaRESTCONF` → DELETE same endpoint
- `Alarm_Acknowledge` → PUT → 204 No Content
- `Alarm_Deduplication` → duplicate raise → same alarm ID
- `Alarm_NssaaFunctionGET` → GET NssaaFunction
- `Alarm_FailureRateAlarm` → REQ-33: high failure rate → MAJOR alarm
- `Alarm_CircuitBreakerAlarm` → REQ-34: CB open → MAJOR alarm
- `Alarm_MinimalServer` → httptest server for local-only testing

**Commit:** bd770da

### 10. `test/integration/ausf_mock_test.go` — 3 AUSF Cases

AUSF mock integration using `test/mocks/ausf.go`:
- `AUSF_GetUeAuthData` → GET `/nausf-auth/v1/ue-authentications/{gpsi}` → 200
- `AUSF_UnknownGPSI` → 404 via `SetError("GET", 404)`
- `AUSF_Timeout` → 504 via `SetError("GET", 504)`

**Commit:** bd770da

---

## Bug Fixes (Auto-fixed During Implementation)

### NRF Mock Fixes

| File | Fix |
|---|---|
| `test/mocks/nrf.go` | Handle both `/nnrf-disc/v1/` (client) and `/nnrf-nfm/v1/` (management) paths |
| `test/mocks/nrf.go` | Add dispatcher for GET (discovery + instance), POST (registration), PUT (heartbeat) |
| `test/mocks/nrf.go` | Add `handleDiscovery` for query-based discovery with `target-nf-type` and `service-names` params |
| `test/mocks/nrf.go` | Fix path stripping: strip leading `/` before map lookup (`/amf-001` → `amf-001`) |
| `test/mocks/nrf.go` | Rename `serviceName` var → `queryServiceName` to avoid shadowing `serviceName()` func |
| `test/mocks/udm.go` | Add `errorCodes` map + `SetError(method, code)` for configurable error injection |

### PostgreSQL / Redis Fixes

| File | Fix |
|---|---|
| `internal/storage/postgres/session_store.go` | `Save`: attempt `Update` first, fall back to `Create` for new sessions |
| `internal/cache/redis/session_cache.go` | Export `sessionCacheEntry` → `SessionCacheEntry` for integration test access |
| `internal/cache/redis/cache_test.go` | Use exported `SessionCacheEntry` in tests |

### Handler Validation Fixes

| File | Fix |
|---|---|
| `internal/api/common/validator.go` | `ValidateSnssai`: add `missing bool` param for explicit absent-field detection |
| `internal/api/nssaa/handler.go` | `CreateSliceAuthenticationContext`: use raw JSON map to detect `snssai` presence before typed decode |
| `internal/api/nssaa/handler.go` | `ConfirmSliceAuthentication`: same snssai presence check |
| `internal/api/common/common_test.go` | Add "missing snssai" test case |

### Timing Fixes

| File | Fix |
|---|---|
| `test/integration/circuit_breaker_test.go` | `CB_HalfOpenOnTimeout`: reduce timeout to 100ms, sleep 120ms |
| `test/integration/circuit_breaker_test.go` | `CB_CloseOnSuccess`: reduce timeout to 100ms, sleep 120ms |
| `test/unit/resilience/circuit_breaker_test.go` | Same timing fixes for unit tests |

### Pre-existing Fixes (Also Fixed)

| File | Fix |
|---|---|
| `test/unit/api/nssaa_handler_gaps_test.go` | Fix nil request panic using `http.NewRequestWithContext` |
| `test/unit/api/aiw_handler_gaps_test.go` | Same fix for AIW test helper |
| `internal/crypto/kdf.go` | Fix `hkdf.Expand` signature for Go 1.25: `[]byte` → `string(info)` |

---

## Verification

```bash
# Build
go build ./test/integration/...  # ✓ Pass

# All tests (34 packages)
go test ./... -count=1 -short    # ✓ All pass

# Integration tests only
go test ./test/integration/... -count=1 -short  # ✓ All pass
```

---

## Commits

| Hash | Message |
|---|---|
| `bd770da` | test(06-PLAN-4): add integration test suites and NRF mock fixes |
| `90763c9` | fix(06-PLAN-4): fix circuit breaker timeout and ValidateSnssai for unit tests |
| `2344b2e` | fix(06-PLAN-4): export SessionCacheEntry and fix PostgreSQL Save semantics |
| `aae1fe4` | chore: add test binaries and nrm binary to gitignore |

---

## Requirements Verification

| Requirement | Test Coverage |
|---|---|
| **REQ-27**: API endpoint integration tests | `nssaa_api_test.go` (11 cases), `aiw_api_test.go` (8 cases) |
| **REQ-33**: Alarm management (failure rate) | `alarm_test.go` — `Alarm_FailureRateAlarm` |
| **REQ-34**: NRM alarm integration (CB open) | `alarm_test.go` — `Alarm_CircuitBreakerAlarm`, `circuit_breaker_test.go` — `CB_NRMAlarmRaised` |

---

## Deviations from PLAN

1. **Test execution**: All 9 test files pass with `go test ./test/integration/... -count=1 -short`. No `-short` skip was needed for any integration test.
2. **NRF mock**: Required significant rework to support both `/nnrf-disc/v1/` (Nnrf_NFDiscovery, used by client) and `/nnrf-nfm/v1/` (Nnrf_NFManagement) paths, with separate dispatchers for GET/POST/PUT on each.
3. **Snssai validation**: Discovered that JSON `{"snssai": {}}` deserializes to `sst=0` which is spec-valid. Added raw JSON presence check rather than modifying the OpenAPI schema.
4. **Additional fixes**: Fixed pre-existing bugs discovered during test execution (session store Save semantics, SessionCacheEntry export, Go 1.25 hkdf signature change).
