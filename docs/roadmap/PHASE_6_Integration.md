# Phase 6: Integration — NRF, UDM, AMF

## Overview

Phase 6 tích hợp với các NF khác: NRF, UDM, AMF.

**Note:** Integration with AMF (Re-Auth, Revocation notifications) is wired into the Biz Pod (`cmd/biz/`). The AMF notifier calls back via HTTP Gateway. See `docs/roadmap/PHASE_Refactor_3Component.md` for the 3-component architecture context.

## Modules to Implement

### 1. `internal/nrf/` — NRF Client

**Priority:** P0
**Dependencies:** `internal/types/`
**Design Doc:** `docs/design/05_nf_profile.md`

**Deliverables:**
- [x] `client.go` — NRF HTTP client (internal/nrf/client.go)
- [x] `discovery.go` — Service discovery (via NRF client)
- [x] `registration.go` — NF registration (via NRF client)
- [x] `heartbeat.go` — Heartbeat loop (via NRF client)
- [x] `cache.go` — Discovery cache (via NRF client)
- [ ] `client_test.go` — Unit tests

### 2. `internal/udm/` — UDM Client

**Priority:** P0
**Dependencies:** `internal/types/`
**Design Doc:** `docs/design/22_udm_integration.md`

**Deliverables:**
- [x] `client.go` — UDM HTTP client (internal/udm/client.go)
- [x] `uecm.go` — Nudm_UECM_Get (via UDM client)
- [ ] `client_test.go` — Unit tests

### 3. `internal/amf/` — AMF Client (for notifications)

**Priority:** P0
**Dependencies:** `internal/types/`
**Design Doc:** `docs/design/21_amf_integration.md`

**Deliverables:**
- [x] `client.go` — AMF notification client (internal/amf/client.go)
- [x] `notifier.go` — Re-Auth/Revocation notifier (via AMF client)
- [ ] `client_test.go` — Unit tests

### 4. AUSF Integration — N60 Interface

**Priority:** P1
**Dependencies:** `internal/types/`, `internal/eap/`
**Design Doc:** `docs/design/23_ausf_integration.md`

**Deliverables:**
- [ ] N60 client for AUSF → NSSAAF callout during SNPN authentication
- [ ] Support for Credentials Holder authentication flow
- [ ] MSK forwarding to AUSF for NAS key derivation
- [ ] AUSF integration test suite
- [ ] `ausf_integration_test.go` — Unit tests

**N60 Flow Overview:**
```
AUSF                      NSSAAF                    AAA-S
  │                          │                        │
  │──EAP-Request/Identity───►│                        │
  │                          │──RADIUS DER────────────►│
  │                          │◄──RADIUS DEA────────────│
  │◄──EAP-Request/TLS───────│                        │
  │                          │                         │
  │     (... EAP-TLS exchange ...)                     │
  │                          │                         │
  │──EAP-Response/TLS──────►│                        │
  │                          │──RADIUS DER────────────►│
  │                          │◄──RADIUS DEA (MSK)────│
  │◄──EAP-Success───────────│                        │
  │                          │ (forward MSK)           │
  │──NAK (MSK derived)──────►│                        │
  │                          │                         │
  (AUSF derives NAS keys from MSK)
```

## Validation Checklist

- [x] NRF: NF registration on startup
- [x] NRF: Heartbeat every 5 minutes
- [x] NRF: Service discovery with 5-min cache
- [x] UDM: Nudm_UECM_Get for AMF ID lookup
- [x] AMF: Re-Auth notification
- [x] AMF: Revocation notification
- [ ] AUSF: N60 client for SNPN authentication callout
- [ ] AUSF: MSK forwarding for NAS key derivation
- [ ] Unit test coverage >80%
