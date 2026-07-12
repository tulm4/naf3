# AIW Conformance Test Suite - Implementation Plan

## Context

The NSSAAF project has comprehensive tests for the NSSAA (UE Authentication) flow but lacks
end-to-end tests for the AIW (AF Interaction With NSSAAF) interface. This plan implements
a full conformance test suite for the `Nnssaaf_Aiw` service-based interface per TS 29.526.

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Test scenarios | C (Comprehensive) | 5 categories covering all major flows |
| Infrastructure | C (Full) | FreeRADIUS enables real EAP-TLS testing |
| Output format | C (Both) | Shell for manual testing, Go for CI |
| Auth mode | C (Variable) | Flexibility for different environments |

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                     AIW Conformance Test Suite                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────────┐     ┌─────────────────────────────────────┐   │
│  │   Shell Script  │     │      Go Test Runner                 │   │
│  │ curl-aiw-tests  │     │   test/e2e/aiw_curl_test.go         │   │
│  └────────┬────────┘     └──────────────┬──────────────────────┘   │
│           │                            │                            │
│           └──────────────┬─────────────┘                            │
│                          ▼                                          │
│              ┌───────────────────────┐                              │
│              │   Test Scenarios      │                              │
│              │  ─────────────────── │                              │
│              │  1. Create (positive) │                              │
│              │  2. Create (negative) │                              │
│              │  3. Query/Confirm     │                              │
│              │  4. Delete            │                              │
│              │  5. Error Handling    │                              │
│              └───────────┬───────────┘                              │
│                          │                                          │
│                          ▼                                          │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │              Docker Compose Stack                          │   │
│  │  ───────────────────────────────────────────────────────   │   │
│  │  NSSAAF Components:                                        │   │
│  │    · naf3-http-gateway (port 8080)                         │   │
│  │    · naf3-biz-pod (internal)                               │   │
│  │    · naf3-aaa-gateway (port 1812)                         │   │
│  │  Infrastructure:                                          │   │
│  │    · postgresql (port 5432)                                │   │
│  │    · redis (port 6379)                                    │   │
│  │  AAA Simulator:                                           │   │
│  │    · freeradius (ports 1812, 1813) - real EAP-TLS        │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Test Scenarios

### 1. Create Positive Scenarios

| ID | Name | Expected | Description |
|----|------|----------|-------------|
| `create-01` | Basic Create | 201 + Location | Minimum valid request |
| `create-02` | With SupiRange | 201 | Request with SUPI range |
| `create-03` | With ValidNotifUri | 201 | With external notification URI |
| `create-04` | With Nssai | 201 | With slice configuration |
| `create-05` | With ExemptionInd | 201 | Authentication exemption indicator |

### 2. Create Negative Scenarios

| ID | Name | Expected | Description |
|----|------|----------|-------------|
| `create-10` | Missing Required Gpsi | 400 | GPSI is mandatory |
| `create-11` | Invalid Gpsi Format | 400 | Malformed GPSI |
| `create-12` | Invalid Snssai | 400 | Invalid slice syntax |
| `create-13` | Missing Supi | 400 | SUPI is mandatory |
| `create-14` | Invalid Supi Format | 400 | SUPI must be imsi-* |
| `create-15` | Missing NssaaInfo | 400 | NSSAA info is mandatory |
| `create-16` | Invalid NssaaInfo | 400 | Malformed NSSAA info |
| `create-17` | Missing SupiRange | 400 | SUPI range is mandatory |
| `create-18` | Invalid SupiRange | 400 | Malformed SUPI range |
| `create-19` | Invalid AuthSchemes | 400 | Invalid auth scheme list |
| `create-20` | Invalid NotifUri | 400 | Malformed notification URI |
| `create-21` | Invalid NotifMethod | 400 | Invalid HTTP method |
| `create-22` | Invalid ExemptionInd | 400 | Invalid exemption value |
| `create-23` | Missing AuthSchemes | 400 | Auth schemes required |
| `create-24` | Invalid SupiRangeFormat | 400 | Bad range format |
| `create-25` | Invalid SnssaiSd | 400 | SD must be 6 hex chars |

### 3. Query and Confirm Scenarios

| ID | Name | Expected | Description |
|----|------|----------|-------------|
| `query-01` | Query By Gpsi | 200 | Find by GPSI |
| `query-02` | Query By Supi | 200 | Find by SUPI |
| `query-03` | Query Not Found | 404 | Non-existent identity |
| `query-04` | Confirm Success | 200 | Confirm NSSAA result |
| `query-05` | Confirm Not Found | 404 | Confirm non-existent session |
| `query-06` | Query All | 200 | List all sessions |

### 4. Delete Scenarios

| ID | Name | Expected | Description |
|----|------|----------|-------------|
| `delete-01` | Delete By Gpsi | 204 | Delete by GPSI |
| `delete-02` | Delete Not Found | 404 | Delete non-existent |
| `delete-03` | Delete All | 204 | Delete all sessions |

### 5. Error Handling Scenarios

| ID | Name | Expected | Description |
|----|------|----------|-------------|
| `error-01` | Invalid Json | 400 | Malformed JSON |
| `error-02` | Invalid ContentType | 415 | Wrong content type |
| `error-03` | Missing Header | 400 | Required header missing |
| `error-04` | Invalid Accept | 406 | Unsupported accept type |

## Implementation Tasks

### Phase 1: Infrastructure

- [ ] Create `deploy/compose/aiw-tests/docker-compose.yaml`
- [ ] Configure FreeRADIUS with EAP-TLS support
- [ ] Add NSSAAF service configurations
- [ ] Create environment configuration

### Phase 2: Shell Script

- [ ] Create `scripts/curl-aiw-tests.sh`
- [ ] Implement auth token fetching
- [ ] Implement test scenario functions
- [ ] Add output formatting and colors
- [ ] Add summary report

### Phase 3: Go Test Runner

- [ ] Create `test/e2e/aiw_curl_test.go`
- [ ] Implement table-driven tests
- [ ] Add auth helpers
- [ ] Add response validation helpers
- [ ] Integrate with existing test infrastructure

### Phase 4: Documentation

- [ ] Update `docs/superpowers/specs/2026-06-29-nssaa-callback-handler-conformance-test-design.md`
- [ ] Add README to test directory
- [ ] Update .gitignore if needed

## File Outputs

```
deploy/compose/aiw-tests/
├── docker-compose.yaml
├── freeradius/
│   ├── radcli.conf
│   ├── clients.conf
│   └── certs/
│       ├── server.pem
│       ├── server.key
│       └── ca.pem
└── .env

scripts/
└── curl-aiw-tests.sh

test/e2e/
├── aiw_curl_test.go
└── README.md
```

## Verification Criteria

1. `go test ./test/e2e/... -v` runs all tests and passes
2. `scripts/curl-aiw-tests.sh` runs all scenarios and shows pass/fail
3. Docker compose stack starts successfully
4. Tests can be run against any environment via environment variables

## Dependencies

- FreeRADIUS with EAP-TLS support
- Valid certificates for TLS
- PostgreSQL with NSSAAF schema
- Redis with NSSAAF schema
- NSSAAF components built and configured

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| FreeRADIUS certificate issues | Generate self-signed certs with script |
| Auth mode complexity | Document both modes clearly |
| Port conflicts | Use unique ports per service |

## Spec References

- TS 29.526 §7.3.2 — Nnssaaf_Aiw Create
- TS 29.526 §7.3.3 — Nnssaaf_Aiw Query
- TS 29.526 §7.3.4 — Nnssaaf_Aiw Confirm
- TS 29.526 §7.3.5 — Nnssaaf_Aiw Delete
- TS 29.571 — Common Data Types
- TS 29.561 — Nnssaaf Service Operations
