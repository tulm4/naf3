# nssAAF — 5G Network Slice-Specific Authentication and Authorization Function

[![CI](https://github.com/operator/nssAAF/actions/workflows/ci.yml/badge.svg)](https://github.com/operator/nssAAF/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22-blue)](https://go.dev)
[![3GPP Release](https://img.shields.io/badge/3GPP-Release%2018-green)](https://www.3gpp.org)

**nssAAF** implements the 3GPP NSSAAF (Network Slice-Specific Authentication and Authorization Function) for 5G standalone networks, as specified in TS 29.526. It provides slice-specific authentication and authorization services for UEs accessing network slices via AMF.

## Overview

The NSSAAF is a 5G Network Function that:

- Receives slice authentication requests from AMF via the **N58 interface** (Nnssaaf_NSSAA)
- Handles EAP-based authentication flows between UE and NSS-AAA servers
- Supports **RADIUS** (TS 29.561 Ch.16) and **Diameter** (TS 29.561 Ch.17) AAA protocols
- Manages authentication context state via UDM on the **N59 interface**
- Provides OAM and configuration interfaces via **N60 interface** (Nnssaaf_AIW)

## Architecture

```
UE ←─────── NAS (EAP) ────────→ AMF (EAP Authenticator)
                                 │
                                 │ Nnssaaf_NSSAA_Authenticate (SBI HTTP/2)
                                 │ N58 Interface
                                 ▼
                               NSSAAF
                           ┌─────┴─────┐
                           │           │
                    Nudm_UECM_Get   │ AAA Protocol
                       (N59)        │ (RADIUS/Diameter)
                           │         │
                           ▼         ▼
                         UDM    NSS-AAA Server
```

## Features

- **N58 Interface**: AMF-triggered slice authentication via Nnssaaf_NSSAA service
- **EAP Methods**: EAP-TLS, EAP-AKA', EAP-TTLS (Phase 2)
- **AAA Protocols**: RADIUS and Diameter interworking (Phase 2)
- **High Availability**: Circuit breakers, retries, active-standby failover (Phase 4)
- **Data Persistence**: PostgreSQL with monthly partitions, Redis caching (Phase 3)
- **TLS/Security**: mTLS for SBI, EAP key derivation per TS 33.501 (Phase 5)
- **NRM Support**: 3GPP network resource model management (Phase 6)
- **Kubernetes**: Helm chart, Kustomize overlays, ArgoCD Application manifests (Phase 7)

## Quick Start

### Prerequisites

- Go 1.22+
- Docker (optional, for containerized deployment)
- PostgreSQL 15+ (Phase 3)
- Redis 7+ (Phase 3)

### Build

```bash
# Clone and enter the project
cd nssAAF

# Download dependencies
go mod download

# Build the binary
make build

# Run the binary
make run

# Or run directly
./bin/nssAAF -config configs/staging.yaml
```

### Development

```bash
# Run tests
make test

# Run linter
make lint

# Full CI pipeline
make ci

# Run with hot reload (requires air)
make run-dev
```

### Docker

```bash
# Build Docker image
make docker-build

# Run container
make docker-run
```

## Project Structure

```
nssAAF/
├── cmd/nssAAF/           # Entry point
├── internal/             # Private packages (3GPP/SBI logic)
│   ├── api/
│   │   ├── common/       # Shared: ProblemDetails, middleware, validators
│   │   ├── nssaa/        # Nnssaaf_NSSAA handler (N58)
│   │   └── aiw/           # Nnssaaf_AIW handler (N60)
│   ├── types/           # 3GPP data types (Snssai, GPSI, SUPI, NssaaStatus)
│   ├── eap/             # EAP engine (Phase 2)
│   ├── radius/          # RADIUS client (Phase 2)
│   ├── diameter/        # Diameter client (Phase 2)
│   ├── aaa/             # AAA proxy (Phase 2)
│   ├── storage/         # PostgreSQL layer (Phase 3)
│   ├── cache/           # Redis layer (Phase 3)
│   ├── resilience/      # HA patterns (Phase 4)
│   ├── auth/            # SBI TLS/auth (Phase 5)
│   ├── crypto/         # Cryptographic utilities (Phase 5)
│   ├── nrf/             # NRF client (Phase 6)
│   ├── udm/             # UDM client (Phase 6)
│   └── amf/             # AMF client utilities (Phase 6)
├── configs/             # YAML configuration files
├── deployments/         # Kubernetes manifests
├── test/               # Integration and E2E tests
├── scripts/            # Build and migration tools
└── docs/
    ├── roadmap/         # Implementation phases
    ├── design/          # Detailed design documents
    └── 3gppfilter/     # Filtered 3GPP spec references
```

## 3GPP Specification References

| Spec | Title | Usage |
|------|-------|-------|
| TS 23.502 | Procedures for 5G systems | NSSAA procedure flows §4.2.9 |
| TS 29.526 | NSSAAF service API | N58/N60 SBI interface |
| TS 33.501 | Security architecture | EAP methods, key derivation |
| TS 29.561 | 3GPP interworking | RADIUS/Diameter AAA |
| TS 29.571 | Common data types | Snssai, GPSI, SUPI, NssaaStatus |
| TS 28.541 | NRM for NSSAAF | Management interface |

## Configuration

See `configs/example.yaml` for a full annotated configuration reference.

Key configuration sections:
- `server`: HTTP server timeouts and address
- `database`: PostgreSQL connection settings
- `redis`: Redis cluster addresses and pool settings
- `eap`: EAP round limits and timeouts
- `aaa`: AAA-S response timeouts and retry policy
- `rateLimit`: Per-GPSI, per-AMF, and global rate limits
- `nrf`: NRF service discovery settings
- `udm`: UDM API base URL and timeout

## API Endpoints (Phase 1+)

### N58 Interface (Nnssaaf_NSSAA)

```
POST /nnssaaf/v1/authenticate
    → Slice authentication request from AMF

PUT  /nnssaaf/v1/authenticate/{authCtxId}
    → Update authentication context
```

### N60 Interface (Nnssaaf_AIW)

```
GET  /nnssaaaiw/v1/nssaa-config
    → Retrieve NSSAAF configuration

PUT  /nnssaaaiw/v1/nssaa-config
    → Update NSSAAF configuration
```

### OAM

```
GET  /health     → Health check
GET  /ready      → Readiness check
GET  /metrics    → Prometheus metrics (Phase 4)
```

## Roadmap

See `docs/roadmap/README.md` for the full implementation roadmap.

- Phase 0: Project setup, CI/CD, Docker (current)
- Phase 1: API handlers, data types, configuration
- Phase 2: EAP engine, RADIUS/Diameter clients
- Phase 3: PostgreSQL storage, Redis caching
- Phase 4: High availability and resilience
- Phase 5: Security (TLS, mTLS, EAP keys)
- Phase 6: NRF, UDM, AMF integrations
- Phase 7: Kubernetes deployment

## Contributing

1. Read `docs/roadmap/README.md` and the relevant phase document
2. Follow the 3GPP spec references in every design decision
3. Run `make ci` before submitting a PR
4. Update roadmap docs when completing modules

## License

Apache License 2.0
