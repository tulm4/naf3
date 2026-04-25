# Requirements: NSSAAF

**Defined:** 2026-04-25
**Core Value:** AMF can invoke NSSAAF for slice-specific authentication and NSSAAF correctly relays EAP to/from enterprise AAA servers, returning the authorization decision to AMF.

## v1 Requirements

### NF Integration

- [ ] **REQ-01**: NSSAAF registers with NRF on Biz Pod startup (Nnrf_NFRegistration) — retries in background, does not block startup
- [ ] **REQ-02**: Nnrf_NFHeartBeat sent every 5 minutes with load percentage
- [ ] **REQ-03**: AMF discovered via Nnrf_NFDiscovery before sending notifications
- [ ] **REQ-04**: UDM Nudm_UECM_Get wired to N58 handler — gates AAA routing based on auth subscription
- [ ] **REQ-05**: UDM Nudm_UECM_UpdateAuthContext called after EAP completion
- [ ] **REQ-06**: AMF Re-Auth notification POSTed to reauthNotifUri on RADIUS CoA-Request
- [ ] **REQ-07**: AMF Revocation notification POSTed to revocNotifUri on Diameter ASR
- [ ] **REQ-08**: AUSF N60 client created (internal/ausf/) with ForwardMSK
- [ ] **REQ-09**: PostgreSQL session store replaces in-memory store (NewSessionStore/NewAIWSessionStore)
- [ ] **REQ-10**: DLQ for AMF notification failures after retries exhausted

### Resilience

- [ ] **REQ-11**: Circuit breaker per host:port — CLOSED → OPEN (5 consecutive failures) → HALF_OPEN (30s recovery) → CLOSED (3 successes)
- [ ] **REQ-12**: Retry with exponential backoff — 1s, 2s, 4s with max 3 attempts
- [ ] **REQ-13**: Timeout: 30s EAP round, 10s AAA request, 5s DB, 100ms Redis

### Observability

- [ ] **REQ-14**: Prometheus metrics at /metrics (requests, latency, EAP sessions, AAA stats, circuit breakers)
- [ ] **REQ-15**: ServiceMonitor CRDs for HTTP Gateway, Biz Pod, AAA Gateway
- [ ] **REQ-16**: Structured JSON logs with GPSI hashed (SHA256, first 8 bytes, base64url)
- [ ] **REQ-17**: Full cross-component OpenTelemetry tracing (AMF→HTTP GW→Biz Pod→AAA GW) via W3C TraceContext
- [ ] **REQ-18**: Health endpoints /healthz/live (always 200) and /healthz/ready (dependency checks)
- [ ] **REQ-19**: Prometheus alerting rules: error rate >1%, P99 >500ms, circuit breaker open, session table full

### Security (Phase 5)

- [ ] **REQ-20**: TLS 1.3 for all external SBI interfaces
- [ ] **REQ-21**: mTLS between Biz Pod and AAA Gateway
- [ ] **REQ-22**: JWT token validation with NRF public key
- [ ] **REQ-23**: AES-256-GCM encryption for session state
- [ ] **REQ-24**: KEK/DEK envelope encryption hierarchy with 30-day overlap rotation
- [ ] **REQ-25**: HSM/KMS interface defined

### Integration Testing (Phase 6)

- [ ] **REQ-26**: Unit test coverage >80% overall
- [ ] **REQ-27**: Integration tests for all API endpoints
- [ ] **REQ-28**: E2E tests: AMF → HTTP GW → Biz → AAA GW → AAA-S
- [ ] **REQ-29**: TS 29.526 §7.2 API conformance (~30 test cases)
- [ ] **REQ-30**: RFC 3579 RADIUS EAP conformance
- [ ] **REQ-31**: RFC 5216 EAP-TLS MSK derivation
- [ ] **REQ-32**: NSSAAFFunction IOC via RESTCONF
- [ ] **REQ-33**: Alarm raised on failure rate >10%
- [ ] **REQ-34**: Alarm raised on circuit breaker open

### Kubernetes Deployment (Phase 7)

- [ ] **REQ-35**: Helm charts lint for all 3 components
- [ ] **REQ-36**: HTTP Gateway HPA: min 3, max 20 replicas; PDB minAvailable: 2
- [ ] **REQ-37**: Biz Pod HPA: min 3, max 50 replicas; PDB maxUnavailable: 1
- [ ] **REQ-38**: AAA Gateway: replicas=2, strategy=Recreate, keepalived VIP
- [ ] **REQ-39**: Multus CNI NetworkAttachmentDefinition for VLAN
- [ ] **REQ-40**: Kustomize overlays: dev, staging, production
- [ ] **REQ-41**: ArgoCD ApplicationSet syncs to production

### Performance Testing (Phase 8)

- [ ] **REQ-42**: 50K concurrent sessions sustained
- [ ] **REQ-43**: 1000 RPS sustained for 5 minutes
- [ ] **REQ-44**: P99 latency <500ms
- [ ] **REQ-45**: Error rate <1%
- [ ] **REQ-46**: Chaos: pod kill during active session
- [ ] **REQ-47**: Chaos: database failover
- [ ] **REQ-48**: Chaos: AAA server failure with circuit breaker
- [ ] **REQ-49**: RTO <30s for all failure scenarios

## v2 Requirements

### Advanced Features

- **REQ-50**: Per S-NSSAI circuit breaker (sst+sd+host granularity)
- **REQ-51**: Multi-PLMN isolation (per-schema tenant routing)
- **REQ-52**: OAuth2 with delegated SCIM provisioning
- **REQ-53**: EAP-TTLS support (beyond EAP-TLS)
- **REQ-54**: EAP-AKA' support
- **REQ-55**: Envoy-based HTTP Gateway (replacing stdlib)
- **REQ-56**: SCEF integration for non-3GPP access

## Out of Scope

| Feature | Reason |
|---------|--------|
| TLS/mTLS between components | Phase 5 — must be completed before production |
| Kubernetes manifests | Phase 7 — development mode works with docker-compose |
| Load testing | Phase 8 — functional completeness first |
| NRM/FCAPS management | Phase 6 — API completeness first |
| Envoy gateway | Unnecessary complexity for Phase 1; stdlib sufficient |
| Multi-PLMN isolation | Post-Phase 8 refinement |
| OAuth2 SCIM provisioning | Nice-to-have, not required for core function |

## Traceability

|| Requirement | Phase | Status |
|-------------|--------|--------|
| REQ-01 through REQ-10 | 4 | Pending |
| REQ-11 through REQ-13 | 4 | Pending |
| REQ-14 through REQ-19 | 4 | Pending |
| REQ-20 through REQ-25 | 5 | Pending |
| REQ-26 through REQ-34 | 6 | Pending |
| REQ-35 through REQ-41 | 7 | Pending |
| REQ-42 through REQ-49 | 8 | Pending |

**Coverage:**
- v1 requirements: 49 total
- Phase 4: 19 (NF Integration + Resilience + Observability)
- Phase 5: 6 (Security)
- Phase 6: 9 (Testing + NRM)
- Phase 7: 7 (K8s)
- Phase 8: 8 (Performance)
- Mapped to phases: 49
- Unmapped: 0

---

*Requirements defined: 2026-04-25*
*Last updated: 2026-04-25 after initial GSD project initialization*
