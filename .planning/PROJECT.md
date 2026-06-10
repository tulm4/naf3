# NSSAAF — Project

## What This Is

A production-grade implementation of the 3GPP NSSAAF (Network Slice-Specific Authentication and Authorization Function) for 5G networks. NSSAAF bridges AMF/AUSF (via SBI HTTP/2) and enterprise AAA servers (via RADIUS/Diameter), enabling per-slice authentication where the AAA server makes the authorization decision and NSSAAF relays EAP conversations. The project targets telecom-grade availability (>99.999%) on Kubernetes with Ericsson/Nokia-class feature parity.

The implementation follows a 3-component model: HTTP Gateway (TLS terminator + router), Biz Pod (EAP engine + session state), and AAA Gateway (RADIUS/Diameter transport with active-standby keepalived HA). This decoupling allows each component to scale independently and isolates external protocol handling from business logic.

## Core Value

AMF can invoke NSSAAF for slice-specific authentication and NSSAAF correctly relays EAP to/from enterprise AAA servers, returning the authorization decision to AMF.

## Requirements

### Validated

- 5G NSSAA procedure: AMF → NSSAAF → AAA-S → NSSAAF → AMF (TS 23.502 §4.2.9.2)
- N58 API: POST /nnssaaf-nssaa/v1/slice-authentications, PUT /nnssaaf-nssaa/v1/slice-authentications/{authCtxId}
- N60 API: POST /nnssaaf-aiw/v1/authentications for SNPN credential holder auth
- EAP-TLS support with certificate-based mutual authentication
- RADIUS protocol encoding/decoding (RFC 2865/3579)
- Diameter protocol encoding/decoding (RFC 4072/7155, RFC 6733)
- PostgreSQL session storage with monthly partitions
- Redis caching and rate limiting
- 3-component architecture: HTTP Gateway + Biz Pod + AAA Gateway
- GPSI, SUPI, Snssai, NssaaStatus data types
- 3GPP Release 18 compliance (TS 29.526, TS 23.502, TS 33.501)
- **REQ-01 to REQ-34** — All Phase 4-6 requirements validated (Phase 4: NF Integration & Observability, Phase 5: Security & Crypto, Phase 6: Integration Testing & NRM)

### Active

- [ ] **REQ-35**: Helm charts lint for all 3 components
- [ ] **REQ-36**: HTTP Gateway HPA: min 3, max 20 replicas; PDB minAvailable: 2
- [ ] **REQ-37**: Biz Pod HPA: min 3, max 50 replicas; PDB maxUnavailable: 1
- [ ] **REQ-38**: AAA Gateway: replicas=2, strategy=Recreate, keepalived VIP
- [ ] **REQ-39**: Multus CNI NetworkAttachmentDefinition for VLAN
- [ ] **REQ-40**: Kustomize overlays: dev, staging, production
- [ ] **REQ-41**: ArgoCD ApplicationSet syncs to production
- [ ] **REQ-42**: 50K concurrent sessions sustained
- [ ] **REQ-43**: 1000 RPS sustained for 5 minutes
- [ ] **REQ-44**: P99 latency <500ms
- [ ] **REQ-45**: Error rate <1%
- [ ] **REQ-46**: Chaos: pod kill during active session
- [ ] **REQ-47**: Chaos: database failover
- [ ] **REQ-48**: Chaos: AAA server failure with circuit breaker
- [ ] **REQ-49**: RTO <30s for all failure scenarios

### Out of Scope

- Kubernetes manifests (Helm, Kustomize, ArgoCD) — Phase 7
- Load and chaos testing — Phase 8
- Multi-PLMN isolation (per-schema tenant routing) — deferred to post-Phase 8
- Envoy-based HTTP Gateway or AAA Gateway proxy — deferred
- S-NSSAI-specific circuit breaker granularity — deferred

## Context

### Background

NSSAAF is a 3GPP-defined NF introduced in Release 16. Commercial implementations exist from Ericsson and Nokia, but open-source implementations (free5GC notes NSSAAF support as incomplete as of early 2026). This project fills that gap.

The project was bootstrapped with detailed domain research (`.planning/research/PROJECT_DOMAIN_RESEARCH.md`) and codebase structure analysis (`.planning/CODEBASE_STRUCTURE.md`). Phases 0 through 6 are complete. Phase 7 (Kubernetes Deployment) is the next work.

The codebase is Go 1.22+ using standard library where possible. The 3-component architecture was established in Phase R.

## Constraints

- **Tech stack**: Go 1.22+, stdlib `net/http`, minimal dependencies, no heavy frameworks
- **Deployment**: Kubernetes-native, kubeadm, telecom-grade
- **Availability target**: >99.999% (5-nines)
- **3-component constraint**: `internal/proto/` is the isolation boundary — zero internal dependencies
- **RADIUS/Diameter**: Used only by Biz Pod (AAA Gateway is a separate process after Phase R)
- **AAA Gateway hard limit**: Exactly 2 replicas, active-standby, never scale beyond 2
- **GPSI privacy**: Must hash in logs — never log raw GPSI
- **NRF startup**: Degraded mode — retry registration in background, do not block startup

## Key Decisions

| Decision | Rationale | Outcome |
|---------|-----------|---------|
| 3-component model (HTTP GW + Biz Pod + AAA GW) | Source-IP stability for RADIUS/Diameter; independent scaling; protocol isolation | ✅ Validated in Phase R |
| Go stdlib `net/http` for HTTP Gateway | Minimal dependencies; TLS 1.3 support; Go 1.22+ HTTP/2 | ✅ Validated |
| Redis pub/sub for AAA response correlation | Decouples Biz Pods from AAA Gateway response delivery | ✅ Validated |
| PostgreSQL monthly partitions | Audit compliance; efficient historical queries | ✅ Validated in Phase 3 |
| Circuit breaker per host:port | Simple, matches current `AAAConfig` scope | ✅ Validated in Phase 4 |
| DLQ for AMF notification failures | Telecom-grade reliability; enables reprocessing | ✅ Validated in Phase 4 |
| Full cross-component OTel tracing | Critical for debugging multi-service flows | ✅ Validated in Phase 4 |
| AES-256-GCM session encryption | Data protection for session state | ✅ Validated in Phase 5 |
| Vault/SoftHSM for KEK management | HSM/KMS interface for production key management | ✅ Validated in Phase 5 |
| E2E harness with docker-compose | Infrastructure-free testing via containers | ✅ Validated in Phase 6 |
| NRM RESTCONF as standalone binary | Separate lifecycle from Biz Pod | ✅ Validated in Phase 6 |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? Move to Out of Scope with reason
2. Requirements validated? Move to Validated with phase reference
3. New requirements emerged? Add to Active
4. Decisions to log? Add to Key Decisions
5. "What This Is" still accurate? Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---

*Last updated: 2026-05-30 after Phase 6 completion*
