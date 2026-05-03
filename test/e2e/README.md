# E2E Test Suite

End-to-end integration tests for the NSSAAF system, verifying the full flow from HTTP Gateway through Biz Pod to AAA Gateway and back.

## Architecture

The E2E test suite uses a **Driver interface** to support containerized backends:

```
+-----------------------------------------------------------------------------+
|                              Test Harness                                    |
|  +----------------------+     +---------------------------+                 |
|  | ContainerDriver      |     |  Harness (e2e.Harness)  |                 |
|  | (NRF/UDM/AAA-S     |     |  - Docker compose        |                 |
|  |  containers)         |     |  - PostgreSQL/Redis     |                 |
|  +----------------------+     |  - TLS client           |                 |
|                                |  - ResetState()        |                 |
|  Profile: "fullchain"          +---------------------------+                 |
+-----------------------------------------------------------------------------+
```

### Driver Profiles

The suite uses a single `ContainerDriver` with `compose/fullchain-dev.yaml`. AMF and AUSF callbacks are mocked in-process via httptest.Server.

| Profile | Driver | Compose File | Use Case |
|---------|--------|--------------|----------|
| `fullchain` (default) | ContainerDriver | `compose/fullchain-dev.yaml` | Full NF integration: NRF, UDM, AAA-S, NRM |

### Infrastructure

```
+-------------+    +-------------+    +-------------+    +-------------+
|  HTTP GW    |-->|   Biz Pod   |-->|  AAA GW     |-->|  AAA-S      |
|  (N58 API)  |    |  (EAP)      |    |  (RADIUS)   |    |  (Mock)     |
+-------------+    +-------------+    +-------------+    +-------------+
       |                  |                                    ^
       |                  |                                    |
       |                  v                                    |
       |           +-------------+                             |
       +----------->|    AMF      |------------------------->+
                  |  (Mock)     |     Nnssaaf_NSSAA_Notification
                  +-------------+
```

## Running Tests

```bash
# Full chain (ContainerDriver + compose/fullchain-dev.yaml)
make test-fullchain

# Fast dev loop for fullchain (skip docker build)
make test-fullchain-fast

# Run tests with existing images (no build)
make test-fullchain-no-build

# Run specific test
go test -tags=e2e -run TestE2E_NSSAA_HappyPath -v

# Skip E2E tests in short mode
go test ./...
```

## Environment Variables

| Variable | Description | Required For |
|----------|-------------|-------------|
| `E2E_PROFILE` | `fullchain` (default) or `mock` | TestMain |
| `E2E_DOCKER_MANAGED` | If set, skip compose up/down (Makefile owns lifecycle) | TestMain |
| `FULLCHAIN_NRF_URL` | Containerized NRF URL | ContainerDriver |
| `FULLCHAIN_UDM_URL` | Containerized UDM URL | ContainerDriver |
| `FULLCHAIN_AAA_SIM_URL` | AAA-S simulator URL | ContainerDriver |
| `FULLCHAIN_NRM_URL` | NRM RESTCONF URL | Harness, smoke tests |
| `E2E_TLS_CA` | CA certificate path for HTTPS health checks | Harness |
| `BIZ_PG_URL` | PostgreSQL connection URL | Harness |
| `BIZ_REDIS_URL` | Redis connection URL | Harness |

## Test Categories

### NSSAA Flow Tests (`n58_flow_test.go`)

| Test | Description | Spec Reference |
|------|-------------|----------------|
| `TestE2E_NSSAA_HappyPath` | Full AMF -> NSSAAF -> AAA-S -> NSSAAF -> AMF flow | TS 23.502 4.2.9.2 |
| `TestE2E_NSSAA_AuthFailure` | EAP-Failure response handling | TS 29.526 7.2 |
| `TestE2E_NSSAA_AuthChallenge` | Multi-step EAP handshake | RFC 5216 2.1 |
| `TestE2E_NSSAA_InvalidGPSI` | GPSI validation (empty) | TS 29.571 5.2.2 |
| `TestE2E_NSSAA_InvalidSnssai` | S-NSSAI validation | TS 29.526 7.2.3 |
| `TestE2E_NSSAA_AaaServerDown` | AAA-S unreachable handling | TS 29.526 7.2.3 |

### AIW Flow Tests (`aiw_flow_test.go`)

| Test | Description | Spec Reference |
|------|-------------|----------------|
| `TestE2E_AIW_BasicFlow` | Full AUSF -> NSSAAF -> AAA-S -> NSSAAF -> AUSF flow | TS 23.502 4.2.9 |
| `TestE2E_AIW_EAPFailure` | EAP-Failure response handling | TS 29.526 7.3 |
| `TestE2E_AIW_InvalidSupi` | SUPI validation | TS 29.571 5.4.4.2 |
| `TestE2E_AIW_TTLS` | EAP-TTLS with inner PAP | RFC 7170 |

### Re-Authentication Tests (`reauth_test.go`)

| Test | Description | Spec Reference |
|------|-------------|----------------|
| `TestE2E_ReAuth_HappyPath` | AAA-S -> NSSAAF -> AMF notification flow | TS 23.502 4.2.9.3 |
| `TestE2E_ReAuth_AmfUnreachable` | AMF unreachable -> DLQ | TS 23.502 4.2.9.3 |

### Revocation Tests (`revocation_test.go`)

| Test | Description | Spec Reference |
|------|-------------|----------------|
| `TestE2E_Revocation_HappyPath` | AAA-S -> NSSAAF -> AMF revocation flow | TS 23.502 4.2.9.4 |
| `TestE2E_Revocation_AmfUnreachable` | AMF unreachable -> DLQ | TS 23.502 4.2.9.4 |

### NF Integration Tests (`nf_integration_test.go`)

| Test | Description | Spec Reference | Driver |
|------|-------------|---------------|--------|
| `TestE2E_NF_NRFUDMDiscovery` | NRF returns correct UDM endpoint | TS 29.510 6.2.6 | fullchain |
| `TestE2E_NF_NRFCustomEndpoint` | Custom NRF endpoint configuration | TS 29.510 6.2.6 | fullchain |
| `TestE2E_NF_NRFNotRegistered` | Unregistered NFs excluded | TS 29.510 6.2.6 | fullchain |
| `TestE2E_NF_NRFAllRegistered` | All registered NFs returned | TS 29.510 6.2.6 | fullchain |
| `TestE2E_NF_UDMAuthSubscription` | UDM returns auth subscription | TS 29.526 7.2.2 | fullchain |
| `TestE2E_NF_UDMSubscriberNotFound` | 404 for unknown SUPI | TS 29.526 7.2.2 | fullchain |
| `TestE2E_NF_UDMErrorInjection` | UDM error handling | TS 29.526 7.2.2 | fullchain |
| `TestE2E_Resilience_CircuitBreaker` | Circuit breaker opens after failures | Internal | fullchain |
| `TestE2E_Resilience_DLQProcessing` | Dead letter queue processing | Internal | fullchain |
| `TestE2E_Resilience_AAASIMModes` | AAA-S mode configuration | Internal | fullchain |
| `TestE2E_Resilience_AAASIMConnectivity` | AAA-S reachability | Internal | fullchain |
| `TestE2E_Resilience_RedisUnavailable` | Redis unavailability handling | Internal | fullchain |
| `TestE2E_Resilience_PostgresUnavailable` | Postgres unavailability handling | Internal | fullchain |

## Coverage Matrix

| Flow | ContainerDriver | Notes |
|------|----------------|-------|
| **NSSAA** |
| NSSAA Happy Path | yes | |
| Multi-step EAP | yes | |
| EAP-Failure | yes | |
| GPSI Validation | yes | |
| Snssai Validation | yes | |
| AAA-S Down | partial | Route test; full fault injection in integration |
| **AIW** |
| AIW Happy Path | yes | |
| EAP-Failure | yes | |
| SUPI Validation | yes | |
| TTLS/PAP | yes | |
| MSK Extraction | no | Requires controlled AAA-S with known MSK |
| **NF Integration** |
| NRF Discovery | yes | |
| UDM Subscription | yes | |
| Circuit Breaker | yes | Requires real UDM |
| DLQ | yes | Requires metrics endpoint |
| **Re-Authentication** |
| RAR -> Notification | partial | E2E verifies notification format |
| AMF Unreachable | no | Requires controlled AMF shutdown |
| **Revocation** |
| DR -> Notification | partial | E2E verifies notification format |
| AMF Unreachable | no | Requires controlled AMF shutdown |

## Test Dependencies

- **HTTP Gateway**: Handles N58/N60 SBI
- **Biz Pod**: Core NSSAAF logic, EAP handling
- **AAA Gateway**: RADIUS/Diameter protocol
- **PostgreSQL**: Session storage
- **Redis**: Caching
- **NRM**: Network Resource Management

## Known Limitations

1. **Fault Injection**: Full fault injection (AAA-S kill, AMF shutdown) requires container control
2. **RAR/DR Injection**: Re-auth and revocation triggers require controlled AAA-S mock
3. **MSK Verification**: Requires controlled AAA-S with known MSK values
4. **NRM Alarms**: Requires full NRM integration
5. **AAA-S Mode Control**: Mode is set via env var at container startup, requires restart to change

These are covered by integration tests in `test/integration/`.

## Test Design Principles

1. **Happy Path First**: E2E focuses on verifying the full flow works
2. **Failure Path Delegation**: Error cases are covered by unit and integration tests
3. **Isolated Mock**: AMF/AUSF mocks are isolated from the Biz Pod
4. **Clean State**: Each test gets a clean DB/Redis via `h.ResetState()`
5. **Driver Abstraction**: Tests run with ContainerDriver for full NF integration
