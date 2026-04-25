# State: NSSAAF

**Project:** NSSAAF — 5G Network Slice-Specific Authentication and Authorization Function
**Core value:** AMF can invoke NSSAAF for slice-specific authentication and NSSAAF correctly relays EAP to/from enterprise AAA servers, returning the authorization decision to AMF.
**Language:** English
**Initialized:** 2026-04-25

## Project Reference

See: `.planning/PROJECT.md` (updated 2026-04-25)

## Current Milestone

**Phase 4: NF Integration & Observability**

## Milestone Progress

### Phases

| Phase | Status | Notes |
|-------|--------|-------|
| 0: Setup | ✅ Done | `cmd/nssAAF/` |
| 1: Foundation | ✅ Done | Types, N58/N60 API, config |
| 2: Protocol | ✅ Done | EAP engine, RADIUS, Diameter |
| 3: Data & Storage | ✅ Done | PostgreSQL, Redis |
| R: 3-Component Refactor | ✅ Done | HTTP GW, Biz Pod, AAA GW |
| **4: NF Integration & Observability** | ⏳ Pending | **Current phase** |
| 5: Security & Crypto | ⏳ Pending | TLS, mTLS, encryption |
| 6: Integration Testing & NRM | ⏳ Pending | E2E, conformance, NRM |
| 7: Kubernetes Deployment | ⏳ Pending | Helm, Kustomize, ArgoCD |
| 8: Performance & Load Testing | ⏳ Pending | Load, chaos |

## Recent Commits

| Commit | Description |
|--------|-------------|
| `a5cb6a4` | docs(phase-4): capture NF Integration & Observability context |
| ... | (see `git log --oneline`) |

## Session Notes

### 2026-04-25 — Phase 4 discussion

Phase 4 context gathered. Key decisions:
- Full cross-component OTel tracing
- AMF notification DLQ on retry exhaustion
- Per host:port circuit breaker
- Startup in degraded mode when NRF unavailable

See: `.planning/phases/04-NFIntegration_Observability/04-CONTEXT.md`

---

*Last updated: 2026-04-25*
