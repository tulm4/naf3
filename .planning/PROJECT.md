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

### Active

- [ ] **REQ-01**: NRF registration on Biz Pod startup with heartbeat every 5 minutes
- [ ] **REQ-02**: UDM Nudm_UECM_Get wired to N58 handler — gates AAA routing
- [ ] **REQ-03**: AMF re-auth/revocation notifications POSTed to reauthNotifUri/revocNotifUri
- [ ] **REQ-04**: AUSF N60 client with MSK forwarding
- [ ] **REQ-05**: PostgreSQL session store replaces in-memory store
- [ ] **REQ-06**: Circuit breaker per host:port (CLOSED → OPEN → HALF_OPEN)
- [ ] **REQ-07**: Retry with exponential backoff (1s, 2s, 4s, max 3 attempts)
- [ ] **REQ-08**: Health endpoints /healthz/live and /healthz/ready
- [ ] **REQ-09**: Prometheus metrics at /metrics (requests, latency, EAP sessions, AAA stats)
- [ ] **REQ-10**: Full cross-component OpenTelemetry tracing (AMF→HTTP GW→Biz Pod→AAA GW)
- [ ] **REQ-11**: Structured JSON logs with GPSI hashed for privacy
- [ ] **REQ-12**: DLQ for AMF notification failures
- [ ] **REQ-13**: Prometheus alerting rules (error rate >1%, P99 >500ms, circuit breaker open)

### Out of Scope

- TLS 1.3 and mTLS between components — Phase 5
- Kubernetes manifests (Helm, Kustomize, ArgoCD) — Phase 7
- Load and chaos testing — Phase 8
- NRM/FCAPS management interface — Phase 6
- OAuth2 JWT token validation in NSSAAF (NRF provides tokens, consumer AMF validates) — deferred
- Multi-PLMN isolation (per-schema tenant routing) — deferred to post-Phase 8
- Envoy-based HTTP Gateway or AAA Gateway proxy — deferred
- S-NSSAI-specific circuit breaker granularity — deferred

## Context

### Background

NSSAAF is a 3GPP-defined NF introduced in Release 16. Commercial implementations exist from Ericsson and Nokia, but open-source implementations (free5GC notes NSSAAF support as incomplete as of early 2026). This project fills that gap.

The project was bootstrapped with detailed domain research (`.planning/research/PROJECT_DOMAIN_RESEARCH.md`) and codebase structure analysis (`.planning/CODEBASE_STRUCTURE.md`). Phases 0 through R are complete. Phase 4 (NF Integration & Observability) is the current work.

The codebase is Go 1.22+ using standard library where possible. The 3-component architecture was established in Phase R.

### Existing Code

- `cmd/biz/main.go` — Biz Pod entry point, uses in-memory stores (lines 59-60), has stub handlers for re-auth/revocation (lines ~188-201)
- `internal/config/config.go` — Config loading with YAML, has `NRFConfig`, `UDMConfig`; `AMFConfig`/`AUSFConfig` absent
- `internal/storage/postgres/pool.go` — Connection pool; `NewSessionStore()`/`NewAIWSessionStore()` wrappers needed
- `internal/api/nssaa/handler.go` — Has `WithAAA`, `WithAPIRoot` options; `WithNRFClient`/`WithUDMClient` needed
- `internal/api/aiw/handler.go` — Has `WithAAA`, `WithAPIRoot` options; `WithAUSFClient` needed

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
| 3-component model (HTTP GW + Biz Pod + AAA GW) | Source-IP stability for RADIUS/Diameter; independent scaling; protocol isolation | — Pending |
| Go stdlib `net/http` for HTTP Gateway | Minimal dependencies; TLS 1.3 support; Go 1.22+ HTTP/2 | — Pending |
| Redis pub/sub for AAA response correlation | Decouples Biz Pods from AAA Gateway response delivery | — Pending |
| PostgreSQL monthly partitions | Audit compliance; efficient historical queries | — Pending |
| Circuit breaker per host:port | Simple, matches current `AAAConfig` scope | — Pending |
| DLQ for AMF notification failures | Telecom-grade reliability; enables reprocessing | — Pending |
| Full cross-component OTel tracing | Critical for debugging multi-service flows | — Pending |

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

*Last updated: 2026-04-25 after initial GSD project initialization*
