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
| 4 | HA | `internal/resilience/` | P1 | 1 week |
| 5 | Security | `internal/auth/`, `internal/crypto/` | P1 | 1 week |
| 6 | Integration | `internal/nrf/`, `internal/udm/` | P1 | 1 week |
| 7 | Kubernetes | `deployments/` | P1 | 1 week |

---

## Quick Start

```
1. Read this README
2. Read relevant PHASE_*.md (below)
3. Read relevant design doc from docs/design/
4. Implement module
5. Update PHASE status
```

---

## Phase Status

| Phase | Status | Completed Modules |
|-------|--------|-----------------|
| Phase 0: Setup | ✅ DONE | `cmd/nssAAF/` |
| Phase 1: Foundation | ✅ DONE | `internal/types/`, `internal/api/nssaa/`, `internal/api/aiw/`, `internal/api/common/`, `internal/config/` |
| Phase 2: Protocol | ✅ DONE | `internal/eap/`, `internal/radius/`, `internal/diameter/`, `internal/aaa/` |
| Phase 3: Data & Storage | ✅ DONE | `internal/storage/`, `internal/cache/` |
| Phase 4: HA | ⏳ PENDING | `internal/resilience/` |
| Phase 5: Security | ⏳ PENDING | `internal/auth/`, `internal/crypto/` |
| Phase 6: Integration | ⏳ PENDING | `internal/nrf/`, `internal/udm/`, `internal/amf/` |
| Phase 7: Kubernetes | ⏳ PENDING | — |

---

## Module → Design Doc Mapping

| Module | Design Doc | Phase |
|--------|-----------|-------|
| `cmd/nssAAF/` | `docs/design/01_service_model.md` | 0 |
| `internal/api/common/` | `docs/design/02_nssaa_api.md` §Common | 1 |
| `internal/api/nssaa/` | `docs/design/02_nssaa_api.md` | 1 |
| `internal/api/aiw/` | `docs/design/03_aiw_api.md` | 1 |
| `internal/types/` | `docs/design/04_data_model.md` | 1 |
| `internal/nrf/` | `docs/design/05_nf_profile.md` | 6 |
| `internal/eap/` | `docs/design/06_eap_engine.md` | 2 |
| `internal/radius/` | `docs/design/07_radius_client.md` | 2 |
| `internal/diameter/` | `docs/design/08_diameter_client.md` | 2 |
| `internal/aaa/` | `docs/design/09_aaa_proxy.md` | 2 |
| `internal/storage/postgres/` | `docs/design/11_database_ha.md` | 3 |
| `internal/cache/redis/` | `docs/design/12_redis_ha.md` | 3 |
| `internal/resilience/` | `docs/design/10_ha_architecture.md` | 4 |
| `internal/auth/` | `docs/design/15_sbi_security.md`, `docs/design/16_aaa_security.md` | 5 |
| `internal/udm/` | `docs/design/22_udm_integration.md` | 6 |
| `internal/amf/` | `docs/design/21_amf_integration.md` | 6 |
| `internal/crypto/` | `docs/design/17_crypto.md` | 5 |

---

## Quality Gates

- [ ] All modules have unit tests (>80% coverage)
- [ ] All APIs have integration tests
- [ ] E2E tests pass
- [ ] 3GPP conformance tests pass (TS 29.526)
- [ ] Load test: 50K concurrent sessions sustained
- [ ] P99 latency <500ms at 1000 RPS
- [ ] Chaos tests: pod kill, DB failover, AAA failure

---

## Directory Structure

```
nssAAF/
├── cmd/nssAAF/           # Entry point
├── internal/             # Private packages
│   ├── api/            # HTTP handlers
│   ├── types/          # Data types
│   ├── eap/            # EAP engine
│   ├── radius/         # RADIUS client
│   ├── diameter/       # Diameter client
│   ├── storage/        # PostgreSQL
│   ├── cache/         # Redis
│   ├── nrf/           # NRF client
│   ├── udm/           # UDM client
│   ├── auth/           # Authentication
│   └── resilience/     # HA patterns
├── pkg/                # Public packages
├── deployments/        # K8s manifests
├── test/               # Integration/E2E
├── scripts/            # Build tools
└── docs/
    ├── roadmap/         # ← YOU ARE HERE
    ├── design/         # Design documents
    └── 3gppfilter/    # Filtered specs
```

---

## Reading Guide

| What you need | Read this first |
|--------------|---------------|
| How to start | `docs/roadmap/README.md` (this file) |
| Phase 0 details | `docs/roadmap/PHASE_0_ProjectSetup.md` |
| Phase details | `docs/roadmap/PHASE_1_Foundation.md` |
| Module details | `docs/roadmap/module_index.md` |
| Quick reference | `docs/quickref.md` |
| API spec | `docs/design/02_nssaa_api.md` |
| Database design | `docs/design/04_data_model.md` |
| 3GPP chunks | `docs/3gppfilter/INDEX.md` |
