# Phase 6: Integration — NRF, UDM, AMF

## Overview

Phase 6 tích hợp với các NF khác: NRF, UDM, AMF.

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

## Validation Checklist

- [x] NRF: NF registration on startup
- [x] NRF: Heartbeat every 5 minutes
- [x] NRF: Service discovery with 5-min cache
- [x] UDM: Nudm_UECM_Get for AMF ID lookup
- [x] AMF: Re-Auth notification
- [x] AMF: Revocation notification
- [ ] Unit test coverage >80%
