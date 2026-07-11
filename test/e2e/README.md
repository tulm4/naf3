# AIW Conformance Test Suite

End-to-end conformance tests for the Nnssaaf_Aiw service-based interface.

## Overview

This test suite validates the AIW interface per TS 29.526:

- **Create** — §7.3.2 Nnssaaf_Aiw Create
- **Query** — §7.3.3 Nnssaaf_Aiw Query
- **Confirm** — §7.3.4 Nnssaaf_Aiw Confirm
- **Delete** — §7.3.5 Nnssaaf_Aiw Delete

## Test Categories

1. **Create Positive** — Valid request scenarios (5 tests)
2. **Create Negative** — Validation error scenarios (15 tests)
3. **Query & Confirm** — Session lookup scenarios (6 tests)
4. **Delete** — Session removal scenarios (3 tests)
5. **Error Handling** — Protocol error scenarios (4 tests)

**Total: 33 test scenarios**

## Usage

### Prerequisites

1. Start the NSSAAF stack:

```bash
docker compose -f deploy/compose/aiw-tests/docker-compose.yaml up -d
```

2. Generate TLS certificates (if not already present):

```bash
./scripts/generate-test-certs.sh
```

### Run Shell Tests

```bash
./scripts/curl-aiw-tests.sh
```

Options:

- `--auth-disabled` — Use auth-disabled mode (default)
- `--auth-oauth2` — Use real OAuth2
- `--scenario create` — Run only create scenarios
- `--verbose` — Show curl output

### Run Go Tests

```bash
go test -v ./test/e2e/...
```

Options:

- `NAF3_AUTH_DISABLED=1` — Disable auth (default)
- `NAF3_AUTH_DISABLED=0` — Use real OAuth2
- `NAF3_HTTP_GATEWAY_URL=https://localhost:8443` — Gateway URL

### Run Specific Test Category

```bash
# Create tests only
go test -v -run "TestAIW_Create" ./test/e2e/...

# Query tests only
go test -v -run "TestAIW_Query" ./test/e2e/...

# Delete tests only
go test -v -run "TestAIW_Delete" ./test/e2e/...
```

## Test Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `NAF3_AUTH_DISABLED` | `1` | Set to `0` to enable OAuth2 |
| `NAF3_HTTP_GATEWAY_URL` | `https://localhost:8443` | HTTP Gateway URL |
| `NAF3_NRF_MOCK_URL` | `http://localhost:8082` | NRF Mock URL |
| `E2E_TLS_DIR` | `/tmp/e2e-tls` | TLS certificate directory |
| `VERBOSE` | `0` | Set to `1` for verbose curl output |

## Architecture

```
test/e2e/
├── aiw_curl_test.go    # Go test runner (go test ./test/e2e/...)

deploy/compose/aiw-tests/
├── docker-compose.yaml  # Full stack with FreeRADIUS
└── freeradius/         # FreeRADIUS configuration

scripts/
└── curl-aiw-tests.sh   # Shell script runner
```

## Test Data

Test data uses deterministic identifiers:

- GPSI: `msisdn-12345678901` through `msisdn-12345678905`
- SUPI: `imsi-123456789012345` through `imsi-123456789012349`
- Snssai: SST=1, SD="000001"

## Troubleshooting

### Connection Refused

Ensure the docker compose stack is running:

```bash
docker compose -f deploy/compose/aiw-tests/docker-compose.yaml ps
```

### TLS Certificate Errors

Regenerate TLS certificates:

```bash
./scripts/generate-test-certs.sh
```

### Auth Failures

If using OAuth2 mode (`NAF3_AUTH_DISABLED=0`), ensure NRF mock is running and healthy:

```bash
curl -sf http://localhost:8082/nnrf-disc/v1/nf-instances
```

## References

- [TS 29.526](https://www.3gpp.org/release-18) — 5G System; NSSAAF services
- [TS 29.571](https://www.3gpp.org/release-18) — Common Data Types
