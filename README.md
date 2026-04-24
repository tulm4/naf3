# nssAAF — 5G Network Slice-Specific Authentication and Authorization Function

[![CI](https://github.com/operator/nssAAF/actions/workflows/ci.yml/badge.svg)](https://github.com/operator/nssAAF/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22-blue)](https://go.dev)
[![3GPP Release](https://img.shields.io/badge/3GPP-Release%2018-green)](https://www.3gpp.org)

**nssAAF** implements the 3GPP NSSAAF (Network Slice-Specific Authentication and Authorization Function) for 5G standalone networks, as specified in TS 29.526. It provides slice-specific authentication and authorization services for UEs accessing network slices via AMF.

## Architecture

The NSSAAF is deployed as **three independent Kubernetes pods**, each with a distinct responsibility:

```
UE ←─────── NAS (EAP) ────────→ AMF (EAP Authenticator)
                                     │
                                     │ Nnssaaf_NSSAA_Authenticate (SBI HTTP/2)
                                     │ N58 Interface
                                     ▼
                           ┌─────────────────┐
                           │  HTTP Gateway   │  TLS terminator, load balancing
                           │  (port :443)   │  Routes AMF → Biz Pod
                           └────────┬────────┘
                                    │ HTTP
                                    ▼
                           ┌─────────────────┐
                           │    Biz Pod      │  EAP engine, N58/N60 handlers
                           │  (port :8080)  │  Business logic, routing
                           └────────┬────────┘
                                    │ HTTP POST /aaa/forward
                                    ▼
                           ┌─────────────────┐
                           │  AAA Gateway    │  Diameter/RADIUS transport
                           │  (port :9090)  │  Active-standby, keepalived VIP
                           │ :1812 UDP      │
                           │ :3868 TCP      │
                           └────────┬────────┘
                                    │ RADIUS/Diameter
                                    ▼
                              NSS-AAA Server
```

### Components

| Component | Binary | Port | Responsibility |
|-----------|--------|------|----------------|
| HTTP Gateway | `bin/http-gateway` | `:443` (TLS) | TLS termination, load balancing to Biz Pods |
| Biz Pod | `bin/biz` | `:8080` | EAP engine, N58/N60 SBI handlers, routing |
| AAA Gateway | `bin/aaa-gateway` | `:9090`, `:1812`, `:3868` | Persistent RADIUS/Diameter connections to AAA-S |

### Data Flow

**Client-initiated (AMF → NSSAAF → AAA-S):**
AMF sends `POST /nnssaaf-nssaa/v1/slice-authentications` to HTTP Gateway → Biz Pod runs EAP engine → forwards to AAA Gateway via HTTP → AAA Gateway sends RADIUS/Diameter to AAA-S → response routed back via Redis pub/sub.

**Server-initiated (AAA-S → NSSAAF → AMF):**
AAA-S sends ASR/CoA/RAR to AAA Gateway → AAA Gateway forwards to Biz Pod via HTTP → Biz Pod handles session termination/re-auth → notifies AMF.

## Features

- **N58 Interface**: AMF-triggered slice authentication via Nnssaaf_NSSAA service (TS 29.526)
- **EAP Methods**: EAP-TLS, EAP-AKA', EAP-TTLS (Phase 2)
- **AAA Protocols**: RADIUS (RFC 2865/3579) and Diameter (RFC 6733/4072) interworking
- **High Availability**: Active-standby AAA Gateway with keepalived, circuit breakers, retries (Phase 4)
- **Data Persistence**: PostgreSQL with monthly partitions, Redis caching (Phase 3)
- **TLS/Security**: mTLS for SBI, EAP key derivation per TS 33.501 (Phase 5)
- **Kubernetes**: Per-component Helm charts, Kustomize overlays, ArgoCD Application manifests (Phase 7)

## Quick Start

### Prerequisites

- Go 1.22+
- Docker (for containerized deployment)
- Redis 7+

### Build

```bash
# Clone and enter the project
cd nssAAF

# Download dependencies
go mod download

# Build all 3 component binaries
make build

# Build individual components
make build-biz
make build-http-gateway
make build-aaa-gateway
```

### Run Locally

```bash
# Run all 3 components via Docker Compose (recommended)
make compose-up

# Or run each component individually
make run-biz          # Biz Pod on :8080
make run-http-gateway # HTTP Gateway on :8443
make run-aaa-gateway # AAA Gateway on :9090
```

### Development

```bash
# Run tests
make test

# Run linter
make lint

# Full CI pipeline
make ci
```

### Docker

```bash
# Build all component images
make docker-build

# Build individual images
make docker-build-biz
make docker-build-http-gateway
make docker-build-aaa-gateway
```

## Project Structure

```
nssAAF/
├── cmd/
│   ├── biz/               # Biz Pod entry point
│   ├── http-gateway/      # HTTP Gateway entry point
│   └── aaa-gateway/      # AAA Gateway entry point
├── internal/
│   ├── proto/             # Internal wire protocol (AAA Gateway ↔ Biz Pod)
│   ├── biz/               # Routing layer (Biz Pod)
│   ├── aaa/
│   │   └── gateway/      # RADIUS/Diameter transport (AAA Gateway)
│   ├── api/
│   │   ├── common/       # Shared: ProblemDetails, middleware, validators
│   │   ├── nssaa/        # Nnssaaf_NSSAA handler (N58)
│   │   └── aiw/           # Nnssaaf_AIW handler (N60)
│   ├── types/           # 3GPP data types (Snssai, GPSI, SUPI, NssaaStatus)
│   ├── eap/             # EAP engine (used by Biz Pod)
│   ├── radius/          # RADIUS encode/decode (used by AAA Gateway)
│   ├── diameter/         # Diameter encode/decode (used by AAA Gateway)
│   ├── storage/         # PostgreSQL layer
│   ├── cache/           # Redis layer
│   ├── resilience/      # HA patterns
│   ├── auth/            # SBI TLS/auth
│   ├── crypto/         # Cryptographic utilities
│   ├── nrf/             # NRF client
│   ├── udm/             # UDM client
│   └── amf/             # AMF client utilities
├── compose/
│   ├── dev.yaml          # Docker Compose (3-component topology)
│   └── configs/          # Per-component configuration
│       ├── biz.yaml
│       ├── aaa-gateway.yaml
│       └── http-gateway.yaml
├── configs/             # DEPRECATED — monolithic configs (use compose/configs/)
├── deployments/         # Kubernetes manifests (Helm, Kustomize, ArgoCD)
├── test/               # Integration and E2E tests
├── scripts/            # Build and migration tools
├── Dockerfile.biz      # Biz Pod container image
├── Dockerfile.http-gateway # HTTP Gateway container image
├── Dockerfile.aaa-gateway # AAA Gateway container image
├── Dockerfile.mock-aaa-s  # Mock AAA Server for local dev
├── Makefile             # 3-component build targets
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

Per-component configuration files are in `compose/configs/`:

- `biz.yaml` — Biz Pod: EAP settings, AAA Gateway URL, Redis
- `aaa-gateway.yaml` — AAA Gateway: RADIUS/Diameter settings, keepalived, AAA-S addresses
- `http-gateway.yaml` — HTTP Gateway: TLS cert paths, Biz Pod service URL

## API Endpoints

### N58 Interface — Biz Pod (Nnssaaf_NSSAA)

```
POST /nnssaaf-nssaa/v1/slice-authentications
    → Slice authentication request from AMF

PUT  /nnssaaf-nssaa/v1/slice-authentications/{authCtxId}
    → Update authentication context
```

### N60 Interface — Biz Pod (Nnssaaf_AIW)

```
GET  /nnssaaaiw/v1/nssaa-config
    → Retrieve NSSAAF configuration

PUT  /nnssaaaiw/v1/nssaa-config
    → Update NSSAAF configuration
```

### Internal — AAA Gateway (Biz Pod ↔ AAA Gateway)

```
POST /aaa/forward
    → Forward EAP payload to AAA-S (Biz Pod → AAA Gateway)

POST /aaa/subscribe
    → Subscribe to server-initiated messages (ASR/CoA/RAR)
```

### OAM

```
GET  /health     → Health check (all components)
GET  /metrics    → Prometheus metrics
```

## Roadmap

See `docs/roadmap/README.md` for the full implementation roadmap.

| Phase | Status | Description |
|-------|--------|-------------|
| Phase 0 | ✅ DONE | Project setup, CI/CD |
| Phase 1 | ✅ DONE | API handlers, data types, configuration |
| Phase 2 | ✅ DONE | EAP engine, RADIUS/Diameter clients |
| Phase 3 | ✅ DONE | PostgreSQL storage, Redis caching |
| Phase R | ✅ DONE | **3-Component Architecture** |
| Phase 4 | ⏳ PENDING | High availability and resilience |
| Phase 5 | ⏳ PENDING | Security (TLS, mTLS, EAP keys) |
| Phase 6 | ⏳ PENDING | NRF, UDM, AMF integrations |
| Phase 7 | ⏳ PENDING | Kubernetes deployment (Helm charts) |

## Contributing

1. Read `docs/roadmap/README.md` and the relevant phase document
2. Follow the 3GPP spec references in every design decision
3. Run `make ci` before submitting a PR
4. Update roadmap docs when completing modules

## License

Apache License 2.0
