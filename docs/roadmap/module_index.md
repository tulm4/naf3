# NSSAAF Module Index

## Module → Design Doc Mapping

|| Module | Design Doc | Phase | Status |
|--------|-----------|-------|--------|
| `cmd/nssAAF/` | `docs/design/01_service_model.md` | 0 | READY |
| `cmd/biz/` | `docs/design/01_service_model.md` §5.4 | R | READY |
| `cmd/http-gateway/` | `docs/design/01_service_model.md` §5.4.4 | R | READY |
| `cmd/aaa-gateway/` | `docs/design/01_service_model.md` §5.4.5 | R | READY |
| `internal/proto/` | `docs/design/01_service_model.md` §5.4.6 | R | READY |
| `internal/aaa/gateway/` | `docs/design/01_service_model.md` §5.4.5 | R | READY |
| `internal/api/common/` | `docs/design/02_nssaa_api.md` §Common | 1 | READY |
| `internal/types/` | `docs/design/04_data_model.md` | 1 | READY |
| `internal/api/nssaa/` | `docs/design/02_nssaa_api.md` | 1 | READY |
| `internal/api/aiw/` | `docs/design/03_aiw_api.md` | 1 | READY |
| `internal/config/` | `docs/design/04_data_model.md` | 1 | READY |
| `internal/eap/` | `docs/design/06_eap_engine.md` | 2 | READY |
| `internal/radius/` | `docs/design/07_radius_client.md` | 2 | READY |
| `internal/diameter/` | `docs/design/08_diameter_client.md` | 2 | READY |
| `internal/aaa/` | `docs/design/09_aaa_proxy.md` | 2 | READY |
| `internal/storage/postgres/` | `docs/design/11_database_ha.md` | 3 | READY |
| `internal/cache/redis/` | `docs/design/12_redis_ha.md` | 3 | READY |
| `internal/nrf/` | `docs/design/05_nf_profile.md` | 4 | READY |
| `internal/udm/` | `docs/design/22_udm_integration.md` | 4 | READY |
| `internal/amf/` | `docs/design/21_amf_integration.md` | 4 | READY |
| `internal/ausf/` | `docs/design/23_ausf_integration.md` | 4 | READY |
| `internal/resilience/` | `docs/design/10_ha_architecture.md`, `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | READY |
| `internal/metrics/` | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | READY |
| `internal/logging/` | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | READY |
| `internal/tracing/` | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | READY |
| `internal/auth/` | `docs/design/15_sbi_security.md`, `docs/design/16_aaa_security.md`, `docs/roadmap/PHASE_5_Security_Crypto.md` | 5 | READY |
| `internal/crypto/` | `docs/design/17_crypto.md`, `docs/roadmap/PHASE_5_Security_Crypto.md` | 5 | READY |
| `internal/nrm/` | `docs/design/18_nrm_fcaps.md`, `docs/roadmap/PHASE_6_Testing_NRM.md` | 6 | READY |
| *(cross-cutting)* | `docs/design/19_observability.md` | 4 | READY |
| *(cross-cutting)* | `docs/design/20_config_management.md` | ALL | TBD |
| *(cross-cutting)* | `docs/design/24_test_strategy.md` | 6 | READY |
| `deployments/helm/nssaa-http-gateway/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/helm/nssaa-biz/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/helm/nssaa-aaa-gateway/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/kustomize/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/argo/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |

---

## Dependency Graph (3-Component Architecture)

```
HTTP Gateway (cmd/http-gateway/)
    │
    ├── internal/proto/          # gRPC/HTTP2 internal protocol
    ├── internal/config/         # TLS, routing config
    └── internal/metrics/        # Prometheus metrics (Phase 4)

Biz Pod (cmd/biz/)
    │
    ├── internal/api/nssaa/      # N58 API handlers
    ├── internal/api/aiw/        # N60 API handlers
    ├── internal/eap/             # EAP engine
    ├── internal/types/           # Data types
    ├── internal/storage/         # PostgreSQL
    ├── internal/cache/           # Redis
    ├── internal/nrf/             # NRF client (Phase 4)
    ├── internal/udm/            # UDM client (Phase 4)
    ├── internal/amf/            # AMF notifier (Phase 4)
    ├── internal/ausf/           # AUSF N60 client (Phase 4)
    ├── internal/proto/           # Internal protocol
    ├── internal/auth/            # OAuth2/JWT (Phase 5)
    ├── internal/crypto/          # Encryption (Phase 5)
    ├── internal/resilience/       # Circuit breaker, retry (Phase 4)
    ├── internal/metrics/         # Prometheus metrics (Phase 4)
    └── internal/logging/         # Structured logging (Phase 4)

AAA Gateway (cmd/aaa-gateway/)
    │
    ├── internal/proto/            # Internal protocol
    ├── internal/aaa/gateway/     # AAA protocol library
    ├── internal/radius/           # RADIUS (legacy)
    ├── internal/diameter/         # Diameter (legacy)
    ├── internal/resilience/        # Circuit breaker (Phase 4)
    ├── internal/metrics/          # Prometheus metrics (Phase 4)
    └── internal/logging/          # Structured logging (Phase 4)

internal/proto/
    │
    ├── internal/aaa/gateway/     # AAA Gateway library
    ├── cmd/biz/                  # Biz Pod uses proto for AAA forwarding
    └── cmd/aaa-gateway/          # AAA Gateway uses proto for responses

NRF (internal/nrf/)
    │
    └── internal/types/           # Shared types

UDM (internal/udm/)
    │
    └── internal/nrf/             # Discovers UDM via NRF

AMF (internal/amf/)
    │
    └── internal/nrf/             # Discovers AMF via NRF
```

---

## Test Coverage Targets

|| Module | Target Coverage |
|--------|----------------|
| `internal/types/` | 95% |
| `internal/api/nssaa/` | 90% |
| `internal/api/aiw/` | 90% |
| `internal/eap/` | 85% |
| `internal/radius/` | 85% |
| `internal/diameter/` | 85% |
| `internal/storage/postgres/` | 80% |
| `internal/cache/redis/` | 80% |
| `internal/resilience/` | 90% |
| `internal/auth/` | 85% |
| `internal/crypto/` | 90% |
| `internal/nrf/` | 85% |
| `internal/udm/` | 85% |
| `internal/ausf/` | 85% |
| `internal/nrm/` | 75% |
| `cmd/biz/` | 80% |
| `cmd/http-gateway/` | 80% |
| `cmd/aaa-gateway/` | 80% |
| `internal/api/common/` | 90% |
| *observability* | 70% |
| *test strategy* | 75% |
| **Overall** | **>80%** |

---

## Phase R Modules (3-Component Architecture)

These modules implement the 3-component refactor and are the foundation for Phases 4-8.

### Core Components

|| Module | Description | Status |
|--------|-------------|--------|
| `cmd/biz/` | NSSAAF business logic, EAP engine, N58/N60 SBI | READY |
| `cmd/http-gateway/` | TLS terminator, request routing, mTLS to Biz | READY |
| `cmd/aaa-gateway/` | RADIUS/Diameter transport, active-standby | READY |
| `internal/proto/` | Internal gRPC/HTTP2 protocol definitions | READY |
| `internal/aaa/gateway/` | AAA protocol library for AAA Gateway | READY |

### Supporting Infrastructure

These modules are wired into `cmd/biz/main.go` during Phase 4.

|| Module | Description | Phase |
|--------|---------|-------|
| `internal/nrf/` | NRF client: startup registration, heartbeat, NF discovery | 4 |
| `internal/udm/` | UDM client: Nudm_UECM_Get, UpdateAuthContext | 4 |
| `internal/amf/` | AMF notifier: Re-Auth/Revocation POSTs | 4 |
| `internal/ausf/` | AUSF N60 client: MSK forwarding (directory does NOT exist) | 4 |

---

## Cross-Cutting Concerns

These modules span multiple phases and are referenced by all components.

|| Module | Description | Phase | Status |
|--------|-------------|-------|--------|
| `internal/resilience/` | Circuit breaker, retry, timeout | 4 | READY |
| `internal/metrics/` | Prometheus metrics | 4 | READY |
| `internal/logging/` | Structured JSON logging | 4 | READY |
| `internal/tracing/` | OpenTelemetry distributed tracing | 4 | READY |
| `internal/auth/` | OAuth2/JWT validation, mTLS | 5 | TBD |
| `internal/crypto/` | AES-256-GCM, KEK/DEK hierarchy | 5 | TBD |
| `internal/nrm/` | NRM/FCAPS management | 6 | READY |
| `internal/config/` | Configuration management | 1 | READY |

---

## Kubernetes Deployment

Helm charts for the 3-component architecture.

|| Chart | Description | Replicas | HPA |
|-------|-------------|----------|-----|
| `nssaa-http-gateway/` | TLS terminator, router | 3-20 | CPU 70% |
| `nssaa-biz/` | Business logic | 3-50 | Custom metrics |
| `nssaa-aaa-gateway/` | AAA transport | 2 (active-standby) | N/A |

### Kustomize Overlays

|| Overlay | Purpose | Configuration |
|---------|---------|----------------|
| `base/` | Shared manifests | Common labels, annotations |
| `development/` | Local dev | Single replica, debug logs |
| `staging/` | Staging env | 2 replicas, moderate scaling |
| `production/` | Production | Full HA, 3-50 replicas |

### ArgoCD Integration

|| Application | Sync Policy | Target |
|--------------|-------------|--------|
| `nssaa-http-gateway` | Automated | production |
| `nssaa-biz` | Automated | production |
| `nssaa-aaa-gateway` | Automated | production |
