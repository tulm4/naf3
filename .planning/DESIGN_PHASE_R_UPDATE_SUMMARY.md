# Design Docs Phase R Update Summary

This document records all changes made to design docs to reflect the **3-component model** (HTTP Gateway + Biz Pod + AAA Gateway) after the Phase R refactor. The monolithic single-binary architecture references have been updated throughout.

Reference architecture: `docs/design/01_service_model.md` §5.4.

---

## Summary of Changes

| # | File | Changes Made |
|---|------|-------------|
| 1 | `03_aiw_api.md` | Added Phase R note to Overview. Updated processing logic steps 11-12 to show "via HTTP to AAA Gateway" send/receive pattern. Added Phase R note to Database Schema section. |
| 2 | `05_nf_profile.md` | Added Phase R note to Overview (Biz Pod performs NRF registration, registers HTTP Gateway address). Added note to NF Profile Specification about HTTP Gateway pod IPs. |
| 3 | `09_aaa_proxy.md` | Added Phase R note to Overview (routing logic lives in Biz Pod, AAA Gateway handles raw socket I/O). Rewrote Protocol Passthrough section to show Biz Pod relay via HTTP to AAA Gateway. |
| 4 | `11_database_ha.md` | Added Phase R note to Overview (only Biz Pods access database directly, HTTP Gateway has no DB access, AAA Gateway uses Redis not PostgreSQL). |
| 5 | `12_redis_ha.md` | Added Phase R note to Overview (Redis used for cross-component session correlation). Added new §4.5 "Cross-Component Session Correlation" covering: session correlation keys (`nssaa:session:{txId}`), Biz Pod registry (`nssaa:pods`), AAA response pub/sub (`nssaa:aaa-response`), and server-initiated queue (`nssaa:server-initiated:{podName}`). |
| 6 | `15_sbi_security.md` | Added Phase R note to Overview (TLS termination at HTTP Gateway). Updated Istio AuthorizationPolicy selector from `app: nssAAF` to `app: nssaa-http-gw`. Added comment clarifying HTTP Gateway pods. |
| 7 | `16_aaa_security.md` | Added Phase R note to Overview (raw RADIUS/Diameter socket handling runs in AAA Gateway, not Biz Pod). |
| 8 | `17_crypto.md` | Added Phase R note to Overview (cryptographic operations run in Biz Pods only). |
| 9 | `18_nrm_fcaps.md` | Added Phase R note to Overview (NRM reflects SBI-facing view via HTTP Gateway, AAA Gateway config in customInfo). |
| 10 | `19_observability.md` | Added Phase R note to Overview (each component exposes own metrics endpoint). Added comment to AAA protocol metrics about Biz Pod → AAA Gateway HTTP pattern. Updated §4.2 trace diagram to show HTTP Gateway → Biz Pod → AAA Gateway → AAA-S span hierarchy with separate trace context for AAA Gateway process. Updated §2.2 ServiceMonitor to include separate ServiceMonitors for each component (HTTP GW, Biz, AAA GW). |
| 11 | `20_config_management.md` | Added Phase R note to Overview. Rewrote repository structure to show `base/shared/`, `base/http-gateway/`, `base/biz/`, `base/aaa-gateway/` directories. Updated production overlay to show per-component replica counts and separate images. Rewrote config-patch.yaml to show per-component ConfigMaps. |
| 12 | `21_amf_integration.md` | Added Phase R note to Overview. Updated §3.1 flow diagram to show HTTP Gateway → Biz Pod → AAA Gateway routing with Redis pub/sub for responses. |
| 13 | `22_udm_integration.md` | Added Phase R note to Overview (UDM client runs in Biz Pod). |
| 14 | `23_ausf_integration.md` | Added Phase R note to Overview. Replaced monolithic AUSF flow diagram with 3-component diagram showing HTTP GW, Biz Pod, AAA Gateway, AAA-S. Updated "NSSAAF Actions" → "Biz Pod Actions" with explicit steps for HTTP POST to AAA Gateway and Redis pub/sub receive. |
| 15 | `24_test_strategy.md` | Added Phase R note to Overview. Updated E2E test architecture diagram to show HTTP Gateway, Biz Pod, AAA Gateway as separate boxes. Updated E2E test code to show startup of all 3 components. |
| 16 | `25_kubeadm_setup.md` | Added Phase R note to §1. Rewrote Helm chart structure to show `http-gateway/`, `biz/`, `aaa-gateway/` template directories. Updated values.yaml to show per-component values (replicaCount, image, resources, ports). Added section §3 with separate deployment templates for each component. Updated ArgoCD section with multi-component values. Updated Acceptance Criteria to reflect 3-component model. |

---

## Patterns Updated

### 1. Processing Logic Updates

**Before (monolithic):**
```
NSSAAF processes → RADIUS/Diameter → AAA-S
```

**After (3-component):**
```
Biz Pod encodes EAP → HTTP POST /aaa/forward → AAA Gateway → UDP:1812 → AAA-S
AAA-S → AAA Gateway → Redis pub/sub → Biz Pod → HTTP Gateway → AMF/AUSF
```

### 2. Database Access

**Before:** All components accessed the database.

**After:** Only Biz Pods access PostgreSQL. HTTP Gateway has no DB access. AAA Gateway uses Redis for session correlation, not PostgreSQL.

### 3. Redis Keys

**Before:** Session caching keys only.

**After:** Session caching keys + cross-component keys:
- `nssaa:session:{txId}` — AAA Gateway writes, Biz Pods read (session correlation)
- `nssaa:pods` — Biz Pods register on startup/heartbeat
- `nssaa:aaa-response` — pub/sub channel for AAA responses
- `nssaa:server-initiated:{podName}` — queue for server-initiated messages

### 4. Istio/Network Policies

**Before:** `app: nssAAF` selector.

**After:** `app: nssaa-http-gw` selector (HTTP Gateway only).

### 5. Deployment Structure

**Before:** Single `deployment.yaml`, single `service.yaml`, single `configmap.yaml`.

**After:** Separate `http-gateway/`, `biz/`, `aaa-gateway/` directories with component-specific manifests.

---

## Files Verified

- `go build ./...` — passes with exit code 0
- No 3GPP spec references were modified
- No API field descriptions were modified
- No protocol details were modified
- All changes are architecture/deployment/integration focused
