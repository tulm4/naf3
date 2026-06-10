# NSSAAF Implementation Roadmap

**Project:** NSSAAF — 5G Network Slice-Specific Authentication and Authorization Function
**Language:** English
**Spec Foundation:** 3GPP Release 18 (TS 23.502, TS 29.526, TS 33.501, TS 29.561, TS 29.571, TS 28.541)
**Tech Stack:** Go 1.22+, stdlib net/http, PostgreSQL, Redis, Prometheus, OpenTelemetry
**Deployment:** Kubernetes-native, telecom-grade (>99.999% availability)
**Phase Mapping:** See `.planning/REQUIREMENTS.md` for requirement traceability
**Last Updated:** 2026-05-30 (Phase 6 complete)

---

## Phases

### Phase 0: Project Setup

| Field | Value |
|-------|-------|
| **Name** | Project Setup |
| **Goal** | Bootstrap project structure, CI/CD, tooling |
| **Modules** | `cmd/nssAAF/` |
| **Requirements** | (Infrastructure — not tracked in REQUIREMENTS.md) |
| **Design Docs** | `docs/design/01_service_model.md` |
| **Status** | ✅ Done |

### Phase 1: Foundation

| Field | Value |
|-------|-------|
| **Name** | Foundation |
| **Goal** | Implement N58/N60 API handlers, data types, config loading |
| **Modules** | `internal/types/`, `internal/api/nssaa/`, `internal/api/aiw/`, `internal/api/common/`, `internal/config/` |
| **Requirements** | (Validated by completion — see docs/roadmap/README.md) |
| **Design Docs** | `docs/design/02_nssaa_api.md`, `docs/design/03_aiw_api.md`, `docs/design/04_data_model.md` |
| **Status** | ✅ Done |

### Phase 2: Protocol

| Field | Value |
|-------|-------|
| **Name** | Protocol |
| **Goal** | Implement EAP engine, RADIUS/Diameter encoding, AAA routing |
| **Modules** | `internal/eap/`, `internal/radius/`, `internal/diameter/`, `internal/aaa/` |
| **Requirements** | (Validated by completion — see docs/roadmap/README.md) |
| **Design Docs** | `docs/design/06_eap_engine.md`, `docs/design/07_radius_client.md`, `docs/design/08_diameter_client.md`, `docs/design/09_aaa_proxy.md` |
| **Status** | ✅ Done |

### Phase 3: Data & Storage

| Field | Value |
|-------|-------|
| **Name** | Data & Storage |
| **Goal** | Implement PostgreSQL session storage, Redis caching |
| **Modules** | `internal/storage/`, `internal/cache/` |
| **Requirements** | (Validated by completion — see docs/roadmap/README.md) |
| **Design Docs** | `docs/design/11_database_ha.md`, `docs/design/12_redis_ha.md` |
| **Status** | ✅ Done |

### Phase R: 3-Component Refactor

| Field | Value |
|-------|-------|
| **Name** | 3-Component Refactor |
| **Goal** | Split monolithic NSSAAF into HTTP Gateway, Biz Pod, and AAA Gateway |
| **Modules** | `cmd/biz/`, `cmd/http-gateway/`, `cmd/aaa-gateway/`, `internal/proto/`, `internal/aaa/gateway/` |
| **Requirements** | (Validated by completion — see docs/roadmap/README.md) |
| **Design Docs** | `docs/design/01_service_model.md` §5.4, `PHASE_REFACTOR_3COMPONENT_RESEARCH.md` |
| **Status** | ✅ Done |

### Phase 4: NF Integration & Observability

| Field | Value |
|-------|-------|
| **Name** | NF Integration & Observability |
| **Goal** | Wire NRF, UDM, AMF, AUSF clients; implement resilience patterns and observability |
| **Modules** | `internal/nrf/`, `internal/udm/`, `internal/amf/`, `internal/ausf/`, `internal/resilience/`, `internal/metrics/`, `internal/logging/`, `internal/tracing/` |
| **Requirements** | REQ-01 through REQ-19 (19 requirements) |
| **Design Docs** | `docs/design/05_nf_profile.md`, `docs/design/10_ha_architecture.md`, `docs/design/19_observability.md`, `docs/design/21_amf_integration.md`, `docs/design/22_udm_integration.md`, `docs/design/23_ausf_integration.md` |
| **Plans** | **5 plans** — All complete |
| **Status** | ✅ Done |

### Phase 5: Security & Crypto

| Field | Value |
|-------|-------|
| **Name** | Security & Crypto |
| **Goal** | Implement TLS 1.3, mTLS, JWT validation, AES-256-GCM encryption |
| **Modules** | `internal/auth/`, `internal/crypto/` |
| **Requirements** | REQ-20 through REQ-25 (6 requirements) |
| **Design Docs** | `docs/design/15_sbi_security.md`, `docs/design/16_aaa_security.md`, `docs/design/17_crypto.md` |
| **Status** | ✅ Done |

### Phase 6: Integration Testing & NRM

| Field | Value |
|-------|-------|
| **Name** | Integration Testing & NRM |
| **Goal** | Unit/integration/E2E tests, TS 29.526 conformance, NSSAAFFunction NRM |
| **Modules** | `test/`, `internal/nrm/` |
| **Requirements** | REQ-26 through REQ-34 (9 requirements) |
| **Design Docs** | `docs/design/18_nrm_fcaps.md`, `docs/design/24_test_strategy.md` |
| **Plans** | **6 plans** — All complete |
| **Status** | ✅ Done |

### Phase 7: Kubernetes Deployment

| Field | Value |
|-------|-------|
| **Name** | Kubernetes Deployment |
| **Goal** | Helm charts, Kustomize overlays, ArgoCD, HPA/PDB configs |
| **Modules** | `deployments/helm/`, `deployments/kustomize/`, `deployments/argo/` |
| **Requirements** | REQ-35 through REQ-41 (7 requirements) |
| **Design Docs** | `docs/design/25_kubeadm_setup.md` |
| **Status** | ⏳ Pending |
| **Depends on** | Phase 6 |

### Phase 8: Performance & Load Testing

| Field | Value |
|-------|-------|
| **Name** | Performance & Load Testing |
| **Goal** | 50K concurrent sessions, 1000 RPS, chaos testing, RTO validation |
| **Modules** | `test/load/` |
| **Requirements** | REQ-42 through REQ-49 (8 requirements) |
| **Design Docs** | `docs/roadmap/PHASE_8_PerfLoadTesting.md` |
| **Status** | ⏳ Pending |
| **Depends on** | Phase 7 |

---

## Phase Summary

| # | Phase | Status | Requirements | Plans |
|---|-------|--------|--------------|-------|
| 0 | Project Setup | ✅ Done | — | — |
| 1 | Foundation | ✅ Done | — | — |
| 2 | Protocol | ✅ Done | — | — |
| 3 | Data & Storage | ✅ Done | — | — |
| R | 3-Component Refactor | ✅ Done | — | — |
| 4 | NF Integration & Observability | ✅ Done | REQ-01 to REQ-19 | 5/5 |
| 5 | Security & Crypto | ✅ Done | REQ-20 to REQ-25 | — |
| 6 | Integration Testing & NRM | ✅ Done | REQ-26 to REQ-34 | 6/6 |
| 7 | Kubernetes Deployment | ⏳ Pending | REQ-35 to REQ-41 | — |
| 8 | Performance & Load Testing | ⏳ Pending | REQ-42 to REQ-49 | — |

**Progress: 7/10 phases complete (70%)**

---

## Reading Guide

| What you need | Read this |
|---|---|
| Phase 4 details | `docs/roadmap/PHASE_4_NFIntegration_Observability.md` |
| Phase 4 decisions | `.planning/phases/04-NFIntegration_Observability/04-CONTEXT.md` |
| Phase 5 details | `docs/roadmap/PHASE_5_Security_Crypto.md` |
| Phase 5 decisions | `.planning/phases/05-security-crypto/05-CONTEXT.md` |
| Phase 6 details | `docs/roadmap/PHASE_6_Testing_NRM.md` |
| Phase 6 decisions | `.planning/phases/06-integration-testing-nrm/06-CONTEXT.md` |
| Module details | `docs/roadmap/module_index.md` |
| Quick reference | `docs/quickref.md` |
| Full roadmap with quality gates | `docs/roadmap/README.md` |

---

*Created: 2026-04-25 from docs/roadmap/README.md*
*Last updated: 2026-05-30 (Phase 6 complete)*
