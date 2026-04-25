# NSSAAF Roadmap Audit — 2026-04-25

## 1. Complete NSSAAF Flow Matrix

| # | Interface | Direction | Protocol | Code Status | Roadmap Coverage |
|---|-----------|-----------|----------|-------------|------------------|
| 1 | **N58** (AMF → NSSAAF) | AMF → NSSAAF | HTTP/2 (SBI) | READY — handlers wired to `cmd/biz/main.go`, but AMF endpoint unknown (NRF discovery not wired) | Phase 1 ✅ → Phase 4 (NRF discovery missing) |
| 2 | **N59** (NSSAAF → UDM) | NSSAAF → UDM | HTTP/2 (SBI) — Nudm_UECM_Get | **NOT YET** — `internal/udm/` is a stub (4 lines, no implementation), not imported in `cmd/biz/main.go` | Phase 6 (mapped but unwired) |
| 3 | **N60** (AUSF → NSSAAF) | AUSF → NSSAAF | HTTP/2 (SBI) — Nudm_UEAuthentication callout | **MISSING** — `internal/ausf/` directory does not exist; no `Nudm_UEAuthentication_Get` handler in `cmd/biz/main.go` | Phase 6 (ausf/ listed as TBD) |
| 4 | **Namf_Communication** (NSSAAF → AMF) | NSSAAF → AMF | HTTP/2 (SBI) — Re-Auth/Revocation notifications | **NOT YET** — `internal/amf/` is a stub (3 lines), `cmd/biz/main.go` has placeholder handlers (`handleReAuth`, `handleRevocation`) that return hardcoded bytes instead of actually POSTing to AMF's `reauthNotifUri`/`revocNotifUri` | Phase 6 (amf/ listed as READY but is a stub) |
| 5 | **Nnrf_NFRegistration** (NSSAAF → NRF) | NSSAAF → NRF | HTTP/2 (SBI) | **NOT YET** — `internal/nrf/` is a stub (4 lines), not imported in any `cmd/*/main.go`. No startup Nnrf_NFRegistration call. | Phase 6 (nrf/ listed as READY but is a stub) |
| 6 | **Nnrf_NFDiscovery** (NSSAAF → NRF) | NSSAAF → NRF | HTTP/2 (SBI) | **NOT YET** — same stub, no discovery of AMF or AUSF FQDNs | Phase 6 (same stub) |
| 7 | **Nnrf_NFHeartBeat** (NSSAAF → NRF) | NSSAAF → NRF | HTTP/2 (SBI) | **NOT YET** — no periodic heartbeat goroutine in any `cmd/*/main.go` | Phase 6 (missing) |
| 8 | **Nnrf_NFStatusSubscribe** (NRF → NSSAAF) | NRF → NSSAAF | HTTP/2 (SBI) | **NOT YET** — no listener endpoint for NRF callbacks in any `cmd/*/main.go` | Phase 6 (missing) |

### AAA-Server-Initiated Procedures

| Procedure | Spec Reference | Code Status | Notes |
|-----------|---------------|-------------|-------|
| RADIUS CoA-Request (Re-Auth) | TS 23.502 §4.2.9.3 | PARTIAL — `proto.MessageTypeRAR` handled in `cmd/biz/main.go:handleServerInitiated`, but delegates to stub `handleReAuth` that returns `[]byte{2,0,0,12}` without actually calling AMF notification | Phase 2 ✅ → Phase 6 (notification not wired) |
| Diameter ASR (Re-Auth) | TS 23.502 §4.2.9.3 | SAME as above | Phase 2 ✅ → Phase 6 |
| RADIUS Disconnect-Request (Revocation) | TS 23.502 §4.2.9.4 | PARTIAL — `proto.MessageTypeASR` in `handleServerInitiated`, delegates to stub `handleRevocation` that returns `[]byte{}` | Phase 2 ✅ → Phase 6 |
| Diameter STR (Revocation) | TS 23.502 §4.2.9.4 | SAME as above | Phase 2 ✅ → Phase 6 |
| RADIUS DM (Session timeout) | TS 23.502 §4.2.9 | PARTIAL — `proto.MessageTypeCoA` handled but no cleanup action | Phase 2 ✅ → Phase 6 |
| MSK forwarding to AUSF | TS 23.502 §4.2.9.2 | **MISSING** — no `ForwardMSK` call after successful EAP-TLS in any handler | Phase 6 (ausf/ missing) |

---

## 2. Startup Wiring Audit

| Component | NRF Registration | UDM Client | AMF Client | AUSF Client | Redis | PostgreSQL |
|-----------|-----------------|------------|------------|-------------|-------|------------|
| `cmd/biz/main.go` | **NO** | **NO** | **NO** | **NO** | ✅ (heartbeat only) | **NO** (uses `nssaa.NewInMemoryStore()`) |
| `cmd/http-gateway/main.go` | **NO** | N/A | N/A | N/A | **NO** | **NO** |
| `cmd/aaa-gateway/main.go` | **NO** | N/A | N/A | N/A | ✅ (via `gateway.New()`) | **NO** |

### Critical Gaps in `cmd/biz/main.go`

1. **NRF not wired** — `internal/nrf/` is imported nowhere. On startup, NSSAAF must call `Nnrf_NFRegistration` to register its NF profile (NFType=NSSAAF, FQDN, SBI bindings). Heartbeat (`Nnrf_NFHeartBeat`) must run every 5 minutes. Status-subscribe (`Nnrf_NFStatusSubscribe`) must be set up to receive AMF/AUSF status updates.

2. **UDM client not wired** — The `Nudm_UECM_Get` call (TS 29.526 §7.3.2) is required to verify subscription data before AAA routing. `internal/udm/` is a stub. Even if it were implemented, `cmd/biz/main.go` does not create a UDM client and pass it to the N58 handler.

3. **AMF notifier not wired** — The `handleReAuth` and `handleRevocation` functions in `cmd/biz/main.go` (lines 188-201) are stubs. They must POST to the AMF's `reauthNotifUri`/`revocNotifUri` (from the original `SliceAuthInfo` request). `internal/amf/` is a stub.

4. **AUSF client missing entirely** — `internal/ausf/` does not exist. `cmd/biz/main.go` has no handler for `POST /nnssaaf-aiw/...` (N60 endpoint). The AIW handler (`aiw.NewHandler`) is created but no AUSF client is injected.

5. **PostgreSQL not wired** — `cmd/biz/main.go` uses `nssaa.NewInMemoryStore()` (line 59) instead of `internal/storage/postgres/`. This means session state is lost on restart — no persistence, no monthly partitions, no audit logging.

6. **Redis wired only for heartbeat** — `cmd/biz/main.go` uses Redis for pod heartbeat only (line 130), not for session caching (`nssaa:session:{authCtxId}`), idempotency cache, or rate limiting.

---

## 3. Missing Items

### 3.1 Missing from All Phases (Critical)

| # | Item | Why Needed | Phase | Priority |
|---|------|-----------|-------|----------|
| 1 | **AUSF N60 handler** — `POST /nnssaaf-aiw/v1/ue-authentications` (TS 29.526 §7.3) | Required for SNPN authentication flow (TS 23.502 §4.2.9.2). Without it, AUSF cannot call out to NSSAAF. | 6 | P0 |
| 2 | **`internal/ausf/` package** — N60 client for MSK forwarding to AUSF | After successful EAP-TLS, NSSAAF must forward MSK to AUSF via `POST /nausf-auth/v1/ue-authentications/{authCtxId}/msk` so AUSF derives NAS keys. Without this, N60 flow is incomplete. | 6 | P0 |
| 3 | **Nudm_UECM_Get integration** — NSSAAF calls UDM to verify subscription before AAA routing | TS 23.502 §4.2.9.1: "NSSAAF shall obtain the authentication subscription data from UDM." Current code has no UDM call. AAA routing must be gated on UDM response. | 6 | P0 |
| 4 | **AMF notification sender** — NSSAAF POSTs to `reauthNotifUri` and `revocNotifUri` | TS 23.502 §4.2.9.3 and §4.2.9.4: NSSAAF must notify AMF of re-auth and revocation via `Namf_Communication_N1N2MessageTransfer`. Current stubs return hardcoded bytes. | 6 | P0 |
| 5 | **PostgreSQL wired in cmd/biz/main.go** | Production requires session persistence, audit logging, monthly partitions. Current in-memory store is unsuitable. | 6 | P0 |
| 6 | **NRF lifecycle wired in cmd/biz/main.go** — Nnrf_NFRegistration on startup, Nnrf_NFHeartBeat every 5 min | TS 29.510: NFs must register with NRF and send heartbeats. Without this, AMF cannot discover NSSAAF via `Nnrf_NFDiscovery`. | 6 | P0 |

### 3.2 Missing NRF Interactions (Not in Any Phase)

| # | Item | Why Needed | Phase |
|---|------|-----------|-------|
| 7 | **Nnrf_NFDiscovery** — Discover AMF FQDN before sending notifications | `cmd/biz/main.go` needs to look up AMF's SBI binding to POST re-auth/revocation notifications. Currently no AMF FQDN resolution. |
| 8 | **Nnrf_NFStatusSubscribe** — Receive NRF callbacks for NF status changes | NSSAAF should subscribe to AMF and AUSF status changes to update local cache. Not modeled in any phase. |
| 9 | **NF profile attributes** — `sBIFQDN`, `sBIAddresses`, `nssaaInfo` fields in Nnrf_NFRegistration | NSSAAF NF profile must include supported EAP methods, CNSI IDs, and PLMN info (TS 29.510 §6.2.3.2). Not specified in design docs. |

### 3.3 Missing NSSAA State Management

| # | Item | Why Needed | Phase |
|---|------|-----------|-------|
| 10 | **Nudm_UECM_UpdateAuthContext** — Update UDM with `NssaaStatus` after auth completes | TS 29.526 §7.2.3.3: After EAP exchange completes (SUCCESS/FAILURE), NSSAAF must update UDM with final `NssaaStatus`. This is referenced in the design doc (`docs/design/02_nssaa_api.md` §1) but not implemented. |
| 11 | **Re-Auth state transition** — `EAP_SUCCESS → PENDING` when CoA/ASR received | The state machine in `docs/design/02_nssaa_api.md` §2.6 shows `EAP_SUCCESS → PENDING` on re-auth trigger. Current code has no session state transition logic. |
| 12 | **Revocation state transition** — `EAP_SUCCESS → revoked` in DB | TS 23.502 §4.2.9.4: NssaaStatus must be updated to reflect revocation. |

### 3.4 Missing from Phase 4 (NRF Items)

| # | Item | Why Needed | Phase |
|---|------|-----------|-------|
| 13 | **NRF client startup initialization** — Wire `internal/nrf/` into `cmd/biz/main.go` | Without NRF wiring, NSSAAF is invisible to the 5G network. AMF cannot discover it. |
| 14 | **NRF heartbeat goroutine** — Send Nnrf_NFHeartBeat every 5 min | Required by TS 29.510 §6.1.3.2. Without it, NRF marks NSSAAF as unavailable after heartbeat interval. |

### 3.5 Missing from Phase 6 (AUSF Items)

| # | Item | Why Needed | Phase |
|---|------|-----------|-------|
| 15 | **`internal/ausf/` directory and package** — Complete N60 client | `docs/roadmap/PHASE_6_Testing_NRM.md` §1.2 has code template, but the directory does not exist on disk. |
| 16 | **AUSF N60 callout handler** — `POST /nnssaaf-aiw/v1/ue-authentications` | This endpoint must accept AUSF's Nudm_UEAuthentication_Get callout, forward to AAA-S, and return EAP payload. Currently not implemented. |
| 17 | **MSK forwarding flow** — After EAP-TLS success, forward MSK to AUSF | TS 23.502 §4.2.9.2: "AUSF requests MSK from NSSAAF." Code template exists in PHASE_6 but implementation missing. |

---

## 4. Phase 4 Additions

The following NRF lifecycle items are **missing from Phase 4** and should be added to `docs/roadmap/PHASE_4_HA_Observability.md`:

### 4.1 Add NRF Client Initialization (NEW Section)

**Rationale:** Phase 4 currently covers resilience and observability, but NRF client is a cross-cutting dependency that must be wired in Phase 4 (not Phase 6) because all subsequent phases depend on NF discovery.

```markdown
### 6. `internal/nrf/` — NRF Client Wiring

**Priority:** P0 (cross-cutting, blocks Phase 6 NF integration)
**Dependencies:** `internal/config/`, `internal/metrics/`
**Design Doc:** `docs/design/05_nf_profile.md`

#### 6.1 Startup Registration (`nrf.go`)

```go
// cmd/biz/main.go additions:

import "github.com/operator/nssAAF/internal/nrf"

// After config load, before starting HTTP server:
nrfClient := nrf.NewClient(nrf.Config{
    NrfURL:     cfg.NRF.BaseURL,
    NfType:     "NSSAAF",
    NfInstanceID: podID,
    HeartbeatInterval: 5 * time.Minute,
})

// Register with NRF on startup (blocks server start until registered or timeout)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
if err := nrfClient.Register(ctx); err != nil {
    slog.Error("NRF registration failed", "error", err)
    cancel()
    os.Exit(1)
}
cancel()

// Start heartbeat goroutine
go nrfClient.StartHeartbeat(context.Background())
```

#### 6.2 Nnrf_NFDiscovery Usage

```go
// Discover AMF FQDN before sending re-auth/revocation notifications
amfProfile, err := nrfClient.DiscoverNF(ctx, "AMF", servingPlmnId)
if err != nil {
    return fmt.Errorf("AMF discovery failed: %w", err)
}
amfNotifyURI := amfProfile.SBIAddresses[0].IPv4Address + "/namf-comm/v1/"
```

#### 6.3 Nnrf_NFStatusSubscribe Usage

```go
// Subscribe to AMF status changes
if err := nrfClient.SubscribeStatus(ctx, "AMF", []string{amfInstanceID}); err != nil {
    slog.Warn("AMF status subscription failed", "error", err)
}

// Register callback endpoint for NRF → NSSAAF notifications
http.HandleFunc("/nrf/callback", nrfClient.HandleStatusChange)
```

#### 6.4 NRF Client Interface

```go
type NRFClient interface {
    Register(ctx context.Context) error          // Nnrf_NFRegistration
    StartHeartbeat(ctx context.Context)          // Nnrf_NFHeartBeat (periodic)
    DiscoverNF(ctx context.Context, nfType string, plmnID string) (*NFProfile, error) // Nnrf_NFDiscovery
    SubscribeStatus(ctx context.Context, nfType string, ids []string) error           // Nnrf_NFStatusSubscribe
    HandleStatusChange(w http.ResponseWriter, r *http.Request)                        // Callback receiver
}
```

### 4.2 Update Phase 4 Dependencies Table

Add:

```markdown
| `internal/nrf/` | Phase 4 | P0 | No | Startup NRF registration, heartbeat, discovery |
```

### 4.3 Update Phase 4 Validation Checklist

Add:

```markdown
- [ ] NSSAAF registers with NRF on startup (Nnrf_NFRegistration)
- [ ] Nnrf_NFHeartBeat sent every 5 minutes
- [ ] AMF discovered via Nnrf_NFDiscovery before sending notifications
- [ ] NRF status subscription callback endpoint registered
- [ ] NRF client unit tests pass (>85% coverage)
```

---

## 5. Phase 6 Additions

Phase 6 is the most affected by the audit findings. The following items must be added:

### 5.1 Add `internal/ausf/` Implementation (NEW Section in PHASE_6)

The directory does not exist. Add to `docs/roadmap/PHASE_6_Testing_NRM.md`:

```markdown
### 1.3 `internal/ausf/` — AUSF N60 Client (CREATE)

**Priority:** P0
**Status:** DIRECTORY DOES NOT EXIST — must be created
**Dependencies:** `internal/types/`, `internal/eap/`, `internal/config/`
**Design Doc:** `docs/design/23_ausf_integration.md`

The `internal/ausf/` package does not exist in the codebase. It must be created with the following structure:

```
internal/ausf/
├── client.go       # N60Client with Authenticate() and ForwardMSK()
├── types.go        # UEEuthRequest, UEEuthResponse types
└── client_test.go  # Unit tests
```

See §1.2 above for the full code template. Key additions needed:

```go
// N60Client forwards EAP messages to AAA-S and MSK back to AUSF
type N60Client struct {
    httpClient *http.Client
    ausfBaseURL string  // Discovered via NRF
    tokenValidator *auth.TokenValidator
}

// HandleN60Callout processes Nudm_UEAuthentication_Get from AUSF (TS 29.526 §7.3)
// This is called by the Biz Pod's N60 handler when AUSF POSTs to /nnssaaf-aiw/
func (h *Handler) HandleN60Callout(w http.ResponseWriter, r *http.Request) {
    // 1. Parse UEEuthRequest from AUSF
    // 2. Create authCtxId
    // 3. Forward EAP Identity to AAA-S via RADIUS DER / Diameter DEA
    // 4. Return EAP payload to AUSF
    // 5. On final EAP result, call ForwardMSK() to send MSK to AUSF
}
```

### 1.4 N60 Endpoint Handler (UPDATE existing AIW handler)

The `internal/api/aiw/` handler in `cmd/biz/main.go` must be wired to use `internal/ausf/`:

```go
// cmd/biz/main.go — update AIW handler creation:
ausfClient := ausf.NewClient(ausf.Config{
    BaseURL: cfg.AUSF.BaseURL,
    HTTPClient: &http.Client{Timeout: 30 * time.Second},
})

aiwHandler := aiw.NewHandler(aiwStore,
    aiw.WithAPIRoot(apiRoot),
    aiw.WithAUSFClient(ausfClient),  // NEW: inject AUSF client
    aiw.WithAAA(aaaClient),
)
```

### 1.5 AUSF N60 Integration Tests (NEW test suite)

```go
// test/integration/ausf_n60_test.go
func TestIntegration_AUSF_N60_Callout(t *testing.T) {
    // Setup: Mock AUSF server
    ausfMock := integration.StartAUSFMock()

    // Test: AUSF initiates SNPN authentication via N60
    resp, err := ausfMock.Nudm_UEAuthentication_Get(&ausf.UEEuthRequest{
        AuthType: "EAP_TLS",
        Gpsi: "5-208046000000001",
        Snssai: struct{Sst:1, Sd:"000001"}{},
    })
    require.NoError(t, err)
    assert.NotEmpty(t, resp.EAPMessage)

    // Test: MSK forwarded to AUSF after EAP-TLS success
    assert.True(t, ausfMock.MSKReceived())
}

// test/integration/amf_notification_test.go
func TestIntegration_AMF_ReauthNotification(t *testing.T) {
    // Test: NSSAAF POSTs to reauthNotifUri when CoA-Request received
    // Test: AMF acknowledges with 204 No Content
    // Test: AMF triggers new NSSAA procedure
}

func TestIntegration_AMF_RevocationNotification(t *testing.T) {
    // Test: NSSAAF POSTs to revocNotifUri when ASR received
    // Test: AMF acknowledges with 204 No Content
    // Test: S-NSSAI removed from Allowed NSSAI
}
```

### 5.2 Add Nudm_UECM_Get Integration (NEW Section in PHASE_6)

```markdown
### 1.6 Nudm_UECM_Get Integration

**Priority:** P0
**Status:** `internal/udm/` is a stub — must be implemented
**Spec:** TS 29.526 §7.3.2, TS 23.502 §4.2.9.1

The UDM client is required for the AMF-initiated NSSAA procedure:

```go
// internal/udm/uecm.go — implement Nudm_UECM_Get
func (c *UDMClient) GetAuthSubscription(ctx context.Context, gpsi string, snssai Snssai) (*AuthSubscription, error) {
    // GET {nudmUECMBaseURL}/nudm-uecm/v1/{gpsi}/auth-subscriptions?snssai={sst}&snssai.sd={sd}
    // Returns: AuthenticationSubscription data for EAP method selection
}
```

Wire in `cmd/biz/main.go`:

```go
import "github.com/operator/nssAAF/internal/udm"

udmClient := udm.NewClient(udm.Config{
    BaseURL: cfg.UDM.BaseURL,
    NrfClient: nrfClient,  // Discover UDM via NRF
})

nssaaHandler := nssaa.NewHandler(nssaaStore,
    nssaa.WithUDMClient(udmClient),  // NEW
    nssaa.WithAAA(aaaClient),
)
```

Integration test:

```go
func TestIntegration_UDM_GetAuthSubscription(t *testing.T) {
    // Test: NSSAAF retrieves auth subscription before AAA routing
    // Test: UDM returns 404 if no subscription for (gpsi, snssai)
}
```

### 1.7 Nudm_UECM_UpdateAuthContext Integration (NEW Section)

**Priority:** P1
**Spec:** TS 29.526 §7.2.3.3

After EAP exchange completes, update UDM with final NssaaStatus:

```go
// internal/udm/uecm_update.go
func (c *UDMClient) UpdateAuthContext(ctx context.Context, authCtxId string, status NssaaStatus) error {
    // PUT {nudmUECMBaseURL}/nudm-uecm/v1/{gpsi}/auth-contexts/{authCtxId}
    // Body: { "nssaaStatus": "EAP_SUCCESS" | "EAP_FAILURE" | "PENDING" }
}
```

### 1.8 AMF Notification Sender Integration (NEW Section)

**Priority:** P0
**Spec:** TS 23.502 §4.2.9.3, §4.2.9.4

The `internal/amf/` package must send notifications when AAA-S triggers re-auth/revocation:

```go
// internal/amf/notifier.go — implement AMF notification sender
func (n *AMFNotifier) SendReAuthNotification(ctx context.Context, uri string, req *ReAuthRequest) error {
    // POST {uri}
    // Body: { "notifType": "SLICE_RE_AUTH", "gpsi": "...", "snssai": {...} }
    // Retry 3x with exponential backoff
}

func (n *AMFNotifier) SendRevocationNotification(ctx context.Context, uri string, req *RevocRequest) error {
    // POST {uri}
    // Body: { "notifType": "SLICE_REVOCATION", "gpsi": "...", "snssai": {...} }
    // On persistent failure: enqueue to DLQ
}
```

Replace stub handlers in `cmd/biz/main.go`:

```go
// REPLACE stubs (lines 188-201) with actual implementation:
amfNotifier := amf.NewNotifier(amf.Config{
    HTTPClient: &http.Client{Timeout: 5 * time.Second},
    MaxRetries: 3,
})

func handleReAuth(ctx context.Context, req *proto.AaaServerInitiatedRequest) []byte {
    // Lookup session by authCtxId to get reauthNotifUri
    session, err := sessionStore.Get(ctx, req.AuthCtxID)
    if err != nil {
        slog.Error("session not found for re-auth", "auth_ctx_id", req.AuthCtxID)
        return nil
    }
    // POST to AMF reauthNotifUri
    err = amfNotifier.SendReAuthNotification(ctx, session.ReauthNotifURI, &amf.ReAuthRequest{
        Gpsi: session.Gpsi,
        Snssai: session.Snssai,
        Supi: session.Supi,
    })
    if err != nil {
        slog.Error("re-auth notification failed", "error", err)
    }
    return []byte{2, 0, 0, 12} // RADIUS CoA-Ack
}
```

### 1.9 Update Phase 6 Validation Checklist

Add:

```markdown
### AUSF N60 Integration

- [x] N60 client for AUSF → NSSAAF callout during SNPN authentication
- [ ] Support for Credentials Holder authentication flow
- [ ] MSK forwarding to AUSF for NAS key derivation
- [ ] N60 integration test suite

### UDM Integration

- [ ] Nudm_UECM_Get retrieves auth subscription before AAA routing
- [ ] Nudm_UECM_UpdateAuthContext updates UDM after EAP completion
- [ ] UDM discovery via NRF

### AMF Integration

- [ ] Re-Auth notification POSTed to AMF reauthNotifUri (on RADIUS CoA-Request)
- [ ] Revocation notification POSTed to AMF revocNotifUri (on Diameter ASR)
- [ ] AMF notification retry logic (3x, exponential backoff)
- [ ] DLQ for persistent notification failures
- [ ] AMF notification integration tests

### NRF Integration

- [ ] NSSAAF registers with NRF on startup (Nnrf_NFRegistration)
- [ ] Nnrf_NFHeartBeat every 5 minutes
- [ ] AMF discovered via Nnrf_NFDiscovery before notifications
- [ ] NRF status subscription for AMF/AUSF changes
```

### 5.3 Add PostgreSQL Wiring to Phase 6 (CRITICAL)

The `cmd/biz/main.go` uses in-memory store. This must be replaced:

```markdown
### 1.10 PostgreSQL Session Storage (CRITICAL — currently unwired)

**Priority:** P0
**Status:** Package exists (`internal/storage/postgres/`) but NOT wired in `cmd/biz/main.go`
**Spec:** TS 29.526 §7.2, `docs/design/11_database_ha.md`

```go
// cmd/biz/main.go — REPLACE in-memory store:

import (
    "github.com/operator/nssAAF/internal/storage/postgres"
)

db, err := postgres.New(postgres.Config{
    DSN: cfg.Database.DSN,
    MaxConns: 50,
    MaxIdleConns: 10,
})
if err != nil {
    slog.Error("database connection failed", "error", err)
    os.Exit(1)
}
defer db.Close()

// Migrate schema
if err := db.Migrate(context.Background()); err != nil {
    slog.Error("database migration failed", "error", err)
    os.Exit(1)
}

nssaaStore := postgres.NewSessionStore(db)
```

Validation checklist additions:

```markdown
- [ ] PostgreSQL session store wired in cmd/biz/main.go
- [ ] Monthly partition creation automated
- [ ] Audit log writes on every operation (GPSI hashed)
- [ ] Session expiry cleanup job running
```
```

---

## Summary of Priority Gaps

| Priority | Count | Items |
|----------|-------|-------|
| **P0 — Blocks 5G Integration** | 6 | ausf/ package, UDM client, AMF notifier, NRF wiring, PostgreSQL wiring, N60 handler |
| **P1 — Incomplete Flows** | 3 | Nudm_UECM_UpdateAuthContext, Re-Auth state transition, Revocation state transition |
| **P2 — NRF Discovery Gaps** | 3 | AMF FQDN discovery, AUSF FQDN discovery, Status subscription |

**Phase 4** needs 2 additions: NRF startup wiring section + heartbeat goroutine.
**Phase 6** needs the most work: 5 new/updated sections covering ausf/, udm/, amf/, nrf/, and PostgreSQL wiring.
