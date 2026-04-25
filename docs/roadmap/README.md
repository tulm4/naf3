# NSSAAF Implementation Roadmap

## Project Overview

**Project:** NSSAAF - 5G Network Slice-Specific Authentication and Authorization Function
**Language:** Go 1.22+
**Framework:** Standard library + minimal dependencies (no heavy frameworks)
**Deployment:** Kubernetes-native, kubeadm, telecom-grade
**Target:** Ericsson/Nokia-class availability (>99.999%)

**Spec Foundation:**
- 3GPP Release 18: TS 23.502, TS 29.526, TS 33.501, TS 29.561, TS 29.571, TS 28.541
- IETF: RFC 3748 (EAP), RFC 5216 (EAP-TLS), RFC 2865/3579 (RADIUS), RFC 4072/7155 (Diameter)

---

## Development Phases

| Phase | Name | Module | Priority | Est. Time |
|-------|------|--------|----------|-----------|
| 0 | Project Setup | `cmd/nssAAF/` | P0 | 1 week |
| 1 | Foundation | `internal/types/`, `internal/api/nssaa/`, `internal/api/aiw/`, `internal/api/common/`, `internal/config/` | P0 | 2 weeks |
| 2 | Protocol | `internal/eap/`, `internal/radius/`, `internal/diameter/` | P0 | 2 weeks |
| 3 | Data & Storage | `internal/storage/`, `internal/cache/` | P0 | 1 week |
| **R** | **3-Component Refactor** | `cmd/biz/`, `cmd/http-gateway/`, `cmd/aaa-gateway/`, `internal/proto/`, `internal/aaa/gateway/` | **P0** | **4 weeks** |
| 4 | NF Integration & Observability | `internal/nrf/`, `internal/udm/`, `internal/amf/`, `internal/resilience/`, `internal/metrics/`, `internal/logging/`, `internal/tracing/` | P1 | 2 weeks |
| 5 | Security & Crypto | `internal/auth/`, `internal/crypto/` | P1 | 1 week |
| 6 | Integration Testing & NRM | `test/`, `internal/nrm/` | P1 | 1 week |
| 7 | Kubernetes Deployment | `deployments/helm/`, `deployments/kustomize/`, `deployments/argo/` | P1 | 1 week |
| 8 | Performance & Load Testing | `test/load/`, chaos testing | P2 | 1 week |

---

## Quick Start

```
# 1. Build all 3 components
make build

# 2. Run with Docker Compose (local dev)
make compose-up

# 3. Or run each component individually
make run-biz          # Biz Pod on :8080
make run-http-gateway # HTTP GW on :8443
make run-aaa-gateway  # AAA GW on :9090
```

---

## Phase Status

| Phase | Status | Completed Modules |
|-------|--------|-----------------|
| Phase 0: Setup | ✅ DONE | `cmd/nssAAF/` |
| Phase 1: Foundation | ✅ DONE | `internal/types/`, `internal/api/nssaa/`, `internal/api/aiw/`, `internal/api/common/`, `internal/config/` |
| Phase 2: Protocol | ✅ DONE | `internal/eap/`, `internal/radius/` (Biz Pod only), `internal/diameter/` (Biz Pod only), `internal/aaa/` |
| Phase 3: Data & Storage | ✅ DONE | `internal/storage/`, `internal/cache/` |
| **Phase R: 3-Component Refactor** | ✅ DONE | `internal/proto/`, `cmd/biz/`, `cmd/http-gateway/`, `cmd/aaa-gateway/`, `internal/aaa/gateway/` |
| Phase 4: NF Integration & Observability | ⏳ PENDING | `internal/nrf/` (NRF client wired, startup registration, heartbeat), `internal/udm/` (Nudm_UECM_Get wired to N58 handler, UpdateAuthContext), `internal/amf/` (AMF notifier wired, Re-Auth/Revocation POSTs), `internal/resilience/`, `internal/metrics/`, `internal/logging/`, `internal/tracing/` |
| Phase 5: Security & Crypto | ⏳ PENDING | `internal/auth/`, `internal/crypto/` |
| Phase 6: Integration Testing & NRM | ⏳ PENDING | `test/`, `internal/nrm/` |
| Phase 7: Kubernetes Deployment | ⏳ PENDING | `deployments/helm/`, `deployments/kustomize/`, `deployments/argo/` |
| Phase 8: Performance & Load Testing | ⏳ PENDING | `test/load/`, chaos testing |

---

## Module → Design Doc Mapping

| Module | Design Doc | Phase | Status |
|--------|-----------|-------|--------|
| `cmd/nssAAF/` | `docs/design/01_service_model.md` | 0 | READY |
| `cmd/biz/` | `docs/design/01_service_model.md` §5.4 | R | READY |
| `cmd/http-gateway/` | `docs/design/01_service_model.md` §5.4.4 | R | READY |
| `cmd/aaa-gateway/` | `docs/design/01_service_model.md` §5.4.5 | R | READY |
| `internal/proto/` | `docs/design/01_service_model.md` §5.4.6 | R | READY |
| `internal/aaa/gateway/` | `docs/design/01_service_model.md` §5.4.5 | R | READY |
| `internal/api/common/` | `docs/design/02_nssaa_api.md` §Common | 1 | READY |
| `internal/api/nssaa/` | `docs/design/02_nssaa_api.md` | 1 | READY |
| `internal/api/aiw/` | `docs/design/03_aiw_api.md` | 1 | READY |
| `internal/types/` | `docs/design/04_data_model.md` | 1 | READY |
| `internal/config/` | `docs/design/04_data_model.md` | 1 | READY |
| `internal/eap/` | `docs/design/06_eap_engine.md` | 2 | READY |
| `internal/radius/` | `docs/design/07_radius_client.md` | 2 | READY |
| `internal/diameter/` | `docs/design/08_diameter_client.md` | 2 | READY |
| `internal/aaa/` | `docs/design/09_aaa_proxy.md` | 2 | READY |
| `internal/storage/postgres/` | `docs/design/11_database_ha.md` | 3 | READY |
| `internal/cache/redis/` | `docs/design/12_redis_ha.md` | 3 | READY |
| `internal/nrf/` | `docs/design/05_nf_profile.md` | 4 | STUB — NRF client wired in cmd/biz/main.go |
| `internal/udm/` | `docs/design/22_udm_integration.md` | 4 | STUB — Nudm_UECM_Get wired to N58 handler, UpdateAuthContext |
| `internal/amf/` | `docs/design/21_amf_integration.md` | 4 | STUB — AMF notifier wired, Re-Auth/Revocation POSTs |
| `internal/ausf/` | `docs/design/23_ausf_integration.md` | 4 | TBD — N60 client for MSK forwarding; directory does NOT exist |
| `internal/resilience/` | `docs/design/10_ha_architecture.md`, `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | TBD |
| `internal/metrics/` | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | TBD |
| `internal/logging/` | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | TBD |
| `internal/tracing/` | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` | 4 | TBD |
| `internal/auth/` | `docs/design/15_sbi_security.md`, `docs/design/16_aaa_security.md`, `docs/roadmap/PHASE_5_Security_Crypto.md` | 5 | TBD |
| `internal/crypto/` | `docs/design/17_crypto.md`, `docs/roadmap/PHASE_5_Security_Crypto.md` | 5 | TBD |
| `internal/nrm/` | `docs/design/18_nrm_fcaps.md`, `docs/roadmap/PHASE_6_Testing_NRM.md` | 6 | TBD |
| *(cross-cutting)* | `docs/design/19_observability.md` | 4 | TBD |
| *(cross-cutting)* | `docs/design/20_config_management.md` | ALL | TBD |
| *(cross-cutting)* | `docs/design/24_test_strategy.md` | 6 | TBD |
| `deployments/helm/nssaa-http-gateway/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/helm/nssaa-biz/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/helm/nssaa-aaa-gateway/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/kustomize/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |
| `deployments/argo/` | `docs/roadmap/PHASE_7_K8s.md` | 7 | TBD |

---

## Quality Gates

### Phase 4: NF Integration & Observability
- [ ] NSSAAF registers with NRF on startup (Nnrf_NFRegistration)
- [ ] Nnrf_NFHeartBeat sent every 5 minutes
- [ ] AMF discovered via Nnrf_NFDiscovery before sending notifications
- [ ] UDM Nudm_UECM_Get wired to N58 handler (gates AAA routing)
- [ ] UDM Nudm_UECM_UpdateAuthContext called after EAP completion
- [ ] AMF Re-Auth notification POSTed to reauthNotifUri (on RADIUS CoA-Request)
- [ ] AMF Revocation notification POSTed to revocNotifUri (on Diameter ASR)
- [ ] AUSF N60 handler created (internal/ausf/)
- [ ] AUSF MSK forwarding implemented (POST /nausf-auth/v1/.../msk)
- [ ] PostgreSQL session store wired in cmd/biz/main.go
- [ ] Circuit breaker: CLOSED → OPEN (5 failures) → HALF_OPEN (30s) → CLOSED
- [ ] Retry with exponential backoff 1s, 2s, 4s, max 3 retries
- [ ] Health endpoints /healthz/live, /healthz/ready per component
- [ ] Prometheus metrics: requests, latency, EAP sessions, AAA stats
- [ ] ServiceMonitor CRDs for all 3 components
- [ ] Structured JSON logs with trace context (slog/json)
- [ ] OpenTelemetry traces with W3C TraceContext propagation
- [ ] P99 latency tracking per component
- [ ] Unit test coverage >90%

### Phase 5: Security & Crypto
- [ ] TLS 1.3 for all external interfaces (SBI)
- [ ] mTLS between components (Biz↔AAA GW)
- [ ] JWT token validation with NRF public key
- [ ] OAuth2 scopes: `nnssaaf-nssaa`, `nnssaaf-aiw`
- [ ] AES-256-GCM encryption for session state
- [ ] KEK/DEK envelope encryption hierarchy
- [ ] KEK rotation with 30-day overlap window
- [ ] GPSI hashed in audit logs
- [ ] HSM/KMS interface defined
- [ ] Unit test coverage >90%

### Phase 6: Integration Testing & NRM
- [ ] Unit test coverage >80% overall
- [ ] Integration tests for all APIs
- [ ] E2E tests: AMF → HTTP GW → Biz → AAA GW → AAA-S
- [ ] TS 29.526 §7.2 API conformance (~30 test cases)
- [ ] TS 23.502 §4.2.9 procedure flows (~15 test cases)
- [ ] RFC 3579 RADIUS EAP conformance
- [ ] RFC 5216 EAP-TLS MSK derivation
- [ ] NSSAAFFunction IOC via RESTCONF
- [ ] Alarm raised on failure rate >10%
- [ ] Alarm raised on circuit breaker open

### Phase 7: Kubernetes Deployment
- [ ] `helm lint` passes for all 3 charts
- [ ] HTTP Gateway: HPA min 3, max 20; PDB minAvailable: 2
- [ ] Biz Pod: HPA min 3, max 50; PDB maxUnavailable: 1
- [ ] AAA Gateway: replicas=2, strategy=Recreate, keepalived VIP
- [ ] Multus CNI NetworkAttachmentDefinition for VLAN
- [ ] ServiceMonitor for Prometheus (all components)
- [ ] Kustomize overlays: dev, staging, production
- [ ] ArgoCD ApplicationSet syncs to production

### Phase 8: Performance & Load Testing
- [ ] 50K concurrent sessions sustained
- [ ] 1000 RPS sustained for 5 minutes
- [ ] P99 latency <500ms
- [ ] Error rate <1%
- [ ] Chaos: pod kill during active session
- [ ] Chaos: database failover
- [ ] Chaos: AAA server failure with circuit breaker
- [ ] RTO <30s for all failure scenarios

---

## Telecom-Grade Requirements

Target: Ericsson/Nokia-class availability (>99.999%)

| Metric | Target | Implementation |
|--------|--------|----------------|
| Availability | >99.999% (5-nines) | Multi-replica, PDB, circuit breakers |
| Failover | <5 seconds | keepalived active-standby AAA GW |
| Latency P99 | <500ms | HPA scaling, circuit breakers |
| Sessions | 50K concurrent | Redis caching, DB partitioning |
| Data Protection | AES-256-GCM | Envelope encryption hierarchy |
| Key Security | HSM/KMS | KEK in HSM, DEK per session |
| Traffic Encryption | TLS 1.3 + mTLS | All SBI and internal interfaces |

---

## Directory Structure

```
nssAAF/
├── cmd/
│   ├── biz/               # Biz Pod binary (EAP engine + N58/N60 SBI)
│   ├── http-gateway/      # HTTP Gateway binary (TLS terminator)
│   └── aaa-gateway/      # AAA Gateway binary (Diameter/RADIUS transport)
├── internal/
│   ├── proto/             # Internal component communication contracts (Phase R)
│   ├── aaa/
│   │   └── gateway/      # AAA Gateway library (Phase R)
│   ├── api/              # HTTP handlers
│   ├── types/            # Data types
│   ├── eap/              # EAP engine
│   ├── radius/           # RADIUS encode/decode (used by Biz Pod)
│   ├── diameter/         # Diameter encode/decode (used by Biz Pod)
│   ├── storage/          # PostgreSQL
│   ├── cache/            # Redis
│   ├── nrf/             # NRF client
│   ├── udm/             # UDM client
│   ├── amf/             # AMF client
│   ├── ausf/            # AUSF N60 client (Phase 4)
│   ├── auth/            # OAuth2/mTLS (Phase 5)
│   ├── crypto/          # Encryption (Phase 5)
│   ├── resilience/      # Circuit breaker, retry (Phase 4)
│   ├── metrics/        # Prometheus metrics (Phase 4)
│   ├── logging/         # Structured logging (Phase 4)
│   ├── tracing/         # OpenTelemetry (Phase 4)
│   └── nrm/             # NRM/FCAPS (Phase 6)
├── compose/              # Docker Compose for local dev
├── deployments/
│   ├── helm/
│   │   ├── nssaa-http-gateway/
│   │   ├── nssaa-biz/
│   │   └── nssaa-aaa-gateway/
│   ├── kustomize/
│   │   ├── base/
│   │   └── overlays/
│   └── argo/
├── test/
│   ├── unit/
│   ├── integration/
│   ├── e2e/
│   ├── conformance/
│   └── load/
├── scripts/              # Build tools
├── Dockerfile.biz        # Biz Pod container image
├── Dockerfile.http-gateway # HTTP Gateway container image
├── Dockerfile.aaa-gateway # AAA Gateway container image
├── Makefile             # 3-component build targets
└── docs/
    ├── roadmap/          # ← Roadmap files
    ├── design/          # Design documents
    └── 3gppfilter/     # Filtered specs
```

**Note:** After Phase R, `internal/radius/` and `internal/diameter/` are used only by the Biz Pod. The AAA Gateway (`cmd/aaa-gateway/`) uses go-diameter/v4 directly and does not depend on these packages.

---

## Reading Guide

| What you need | Read this first |
|--------------|---------------|
| How to start | `docs/roadmap/README.md` (this file) |
| Phase 0 details | `docs/roadmap/PHASE_0_ProjectSetup.md` |
| Phase 1 details | `docs/roadmap/PHASE_1_Foundation.md` |
| Phase 4 details | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` |
| Phase 5 details | `docs/roadmap/PHASE_5_Security_Crypto.md` |
| Phase 6 details | `docs/roadmap/PHASE_6_Testing_NRM.md` |
| Phase 7 details | `docs/roadmap/PHASE_7_K8s.md` |
| Module details | `docs/roadmap/module_index.md` |
| Quick reference | `docs/quickref.md` |
| API spec | `docs/design/02_nssaa_api.md` |
| Database design | `docs/design/04_data_model.md` |
| 3GPP chunks | `docs/3gppfilter/INDEX.md` |
