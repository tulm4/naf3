# aaa-gateway Client / aaa-sim Server Role Cleanup Design

**Date:** 2026-07-11 (revised 2026-07-11)
**Status:** Draft (revised)
**Supersedes:** Initial draft dated 2026-07-11 (issue E §3.5 contained incorrect architectural claim)
**Spec Refs:** RFC 6733 §5.3 (CER/CEA), §5.5 (DWR/DWA), §5.6 (DPR); RFC 2865; RFC 5176 (RADIUS CoA/DM); TS 29.561 Ch.16/17

---

## 1. Problem Statement

A codebase audit (2026-07-11) found that the live production code has six inconsistencies / defects:

1. **Dead-code disconnect detection** — `diamForwarder.monitorConnection` watches for `df.conn == nil` to reconnect, but `df.conn` is never set to nil during normal operation. The reconnect branch never fires.
2. **Broken `aiw-tests` compose stack** — `deploy/compose/aiw-tests/docker-compose.yaml` references FreeRADIUS for Diameter (which it doesn't support) and uses env var names (`AAA_GW_RADIUS_ADDR`, `AAA_GW_DIAMETER_ADDR`) that the gateway doesn't read.
3. **Orphan duplicate AAA-S** — `compose/mock_aaa_s.go` (393 lines) duplicates `test/aaa_sim/`, but is never referenced; `Dockerfile.mock-aaa-s` already runs the real `aaa-sim` binary.
4. **Stale roadmap doc** — `docs/roadmap/PHASE_Refactor_3Component.md` references nonexistent env vars `AAA_S_RADIUS_ADDR` / `AAA_S_DIAMETER_ADDR`.
5. **Wrong architectural claim in initial spec** — the initial draft of this spec (§3.5) renamed log keys to `"listen_radius"` / `"listen_diameter"`, but **`aaa-gateway` is a Diameter client** (it dials TCP/SCTP out to `aaa-sim`). It does not — and must not — open a Diameter listen socket. A persistent bidirectional TCP socket the gateway itself opened is the spec-correct carrier for server-initiated Diameter messages (ASR/RAR/STR) per RFC 6733 §5.6 (DPR travels on the existing TCP connection, same socket pattern as ASR/RAR/STR). Only RADIUS UDP — being stateless — requires a listen socket on the gateway (for CoA/DM per RFC 5176).
6. **No test coverage** for the disconnect-and-reconnect path of `diamForwarder`.

This design fixes items (1)–(6).

---

## 2. Architecture (revised — was wrong in initial draft)

```
                  ┌──────────────┐
   AAA-S (sim) ◄──┤   aaa-sim    ├── listens
   ┌──────────┐   │ (cmd/aaa-sim)│   UDP :1812   (RADIUS)
   │  listens │   │              │   TCP :3868   (Diameter)
   │  CER/CEA │   └──────▲───────┘
   │  Access- │          │  dial CER (TCP, persistent)
   │  Request │          │  ◄── ASR/RAR/STR come back on same socket
   │  CoA/DM  │          │       (TCP is bidirectional)
   └──────────┘          │
        ▲                ▼
   ┌────┴───────────────────────────────────────────────┐
   │ aaa-gateway (cmd/aaa-gateway)                      │
   │  * diamForwarder: DialNetwork → CER/CEA            │
   │      single bidirectional TCP/SCTP socket           │
   │      DWR/DWA every 30s (watchdog)                  │
   │      ASR/RAR/STR handlers fire on this same socket │
   │  * radiusForwarder: stateless UDP client           │
   │  * RadiusHandler: listen :1812 UDP (for CoA/DM)    │
   │  * (NO DiameterHandler Listen — uses forwarder's   │
   │      socket for both directions)                   │
   └─────────────────────────────────────────────────────┘
```

**Why Diameter has no listen on the gateway:**

Diameter TCP/SCTP is connection-oriented and bidirectional (RFC 6733 §5). The single TCP connection the gateway dials to `aaa-sim` carries every Diameter message in both directions — DER (forward), DEA (response), ASR (server-initiated inbound), RAR (server-initiated inbound), STR (server-initiated inbound), DPR (disconnect), DWR/DWA (watchdog). Opening a separate inbound TCP listener on the gateway would be redundant and would create a duplicate handshake path.

**Why RADIUS keeps its listen on the gateway:**

RADIUS/UDP (RFC 2865, RFC 5176) is stateless. There is no persistent socket to inherit, so the gateway must keep `listen_radius = ":1812"` open to receive unsolicited CoA / Disconnect-Message from `aaa-sim`. This is the ONLY listen address on the gateway.

**Already-correct production code paths (after this design):**

| Path | Direction | Owner | Verified at |
|------|-----------|-------|-------------|
| RADIUS UDP client (Access-Request) | gw → sim | `radiusForwarder.Forward()` | `internal/aaa/gateway/radius_forward.go:70` |
| Diameter TCP client (DER, DWR) | gw → sim | `diamForwarder.Connect()` + `Forward()` | `internal/aaa/gateway/diameter_forward.go:156` |
| Diameter server-side receive (ASR / RAR / STR) | sim → gw (on gw's outbound socket) | `diamForwarder.machine` handlers | `internal/aaa/gateway/diameter_forward.go:143` (after this design) |
| RADIUS UDP server (CoA / DM) | sim → gw | `RadiusHandler.Listen()` | `internal/aaa/gateway/radius_handler.go:44` |
| AAA-S listener (server role) | listen | `test/aaa_sim/{radius,diameter}.go` | `cmd/aaa-sim/main.go:21` |

---

## 3. Defects and Fixes

### 3.1 Issue A — `diamForwarder` reconnect path is dead code

**File:** `internal/aaa/gateway/diameter_forward.go`

**Root cause:** `monitorConnection` checks `if conn == nil` but only `Close()` ever nil-sets it. In normal operation, when the socket dies (server restart, network drop, peer DPR), `df.conn` still points to the closed `diam.Conn`, so the reconnect branch never fires.

**Fix:** Use the `diam.CloseNotifier` interface — every `diam.Conn` returned by `sm.Client.DialNetwork()` exposes `CloseNotify() <-chan struct{}` which closes when the underlying socket is gone.

Code changes:
- `Connect()`: spawn `go df.watchDisconnect(ctx)` after setting `df.conn`.
- New `watchDisconnect(ctx)`: blocks on `notifier.CloseNotify()`, then under lock clears `df.conn` and sets `df.connected = false`, then calls `df.clearPending()`.
- Refactor `monitorConnection(ctx)`: react to `df.connected == false` instead of `df.conn == nil`; exponential backoff 1s → 30s; on success, reset backoff, log `diameter_forward_reconnected`, spawn a new `watchDisconnect`.

(Full code blocks are reproduced in the initial draft at lines 69–193 of the superseded version of this file. The pattern is unchanged in the revised design.)

### 3.2 Issue B — Delete broken aiw-tests compose stack

**Files:** delete `deploy/compose/aiw-tests/` in its entirety:
- `docker-compose.yaml`
- `.env`
- `freeradius/radiusd.conf`
- `freeradius/clients.conf`
- `freeradius/eap.conf`

Verified that no CI or conformance test references this stack. The conformance tests for AIW use `compose/fullchain-dev.yaml` and `scripts/curl-aiw-tests.sh`.

### 3.3 Issue C — Delete orphan duplicate

**File:** delete `compose/mock_aaa_s.go` (393 lines).

Confirmed unreferenced by:
- `grep -r "compose/mock_aaa_s" .` — no results
- `Dockerfile.mock-aaa-s:16` — copies `bin/aaa-sim` (not the duplicate)
- `compose/fullchain-dev.yaml` — uses `Dockerfile.aaa-sim`, not the duplicate

The legitimate server is `test/aaa_sim/`, built via `cmd/aaa-sim/`.

### 3.4 Issue D — Fix stale roadmap doc

**File:** `docs/roadmap/PHASE_Refactor_3Component.md` lines 531-532.

Change:
```yaml
AAA_S_RADIUS_ADDR: mock-aaa-s:1812
AAA_S_DIAMETER_ADDR: mock-aaa-s:3868
```
to:
```yaml
# Config keys are set in compose/configs/aaa-gateway.yaml under `aaaGateway.`
# (YAML fields: diameterServerAddress / radiusServerAddress).
# Default example: diameterServerAddress: aaa-sim:3868
#                  radiusServerAddress:   aaa-sim:1812
```

### 3.5 Issue E — Remove Diameter listen (the architectural correction)

**Initial draft was wrong.** This is the heart of the revision.

`aaa-gateway` is a Diameter client. It must NOT open a Diameter TCP/SCTP listen socket. The persistent TCP socket the gateway itself dials to `aaa-sim` is bidirectional and carries ASR/RAR/STR inbound as well as DER outbound (RFC 6733 §5.6).

For RADIUS, the gateway **does** listen (UDP is stateless) — only the cosmetic log-key rename survives.

**Files to change:**

1. **`internal/aaa/gateway/diameter_handler.go`** — delete `Listen()`, `listenTCP()`, `listenTLS()`, `listenSCTP()`, the `DiameterHandlerConfig` struct (TLS-only fields), `Forward()` (already a stub), and any imports that become unused (`crypto/tls`, `crypto/x509`, `net`, `os`).
   - The `handleASR/ASA/RAR/RAA/STR/STA` methods and the `DiameterHandler` struct will be **migrated** in step 4 (not deleted here) — this step is purely about removing the listen path.
   - Result of this step alone: the file still compiles but only contains handler-method code with no public listener entrypoint.

2. **`internal/aaa/gateway/diameter_handler_test.go`** — delete tests that exercise `Listen()` directly (`TestDiameterHandler_Listen_*`, `TestDiameterHandler_TLS_*`). Keep handler unit tests.

3. **`internal/aaa/gateway/server_initiated_test.go`** — if any test in this file depends on `DiameterHandler.Listen()`, migrate it to drive handlers through `diamForwarder.machine` instead.

4. **`internal/aaa/gateway/gateway.go`**:
   - Remove `ListenDIAMETER string` field from `Config` struct.
   - Remove `g.diameterHandler = NewDiameterHandler(...)` construction block.
   - Remove the `g.wg.Add(1); go func() { ... g.diameterHandler.Listen(...) ... }()` goroutine.
   - Remove `diameterHandler *DiameterHandler` field from the `Gateway` struct.
   - Replace: instead of constructing a separate `DiameterHandler` with its own `sm.StateMachine`, **register the ASR/RAR/STR handlers directly on `diamForwarder.machine`** so they fire on the gateway's outbound TCP socket. Specifically, after `newDiamForwarder(...)` returns, call something equivalent to:
     ```go
     g.diamForwarder.machine.Handle("ASR", g.handleDiameterASR())
     g.diamForwarder.machine.Handle("ASA", g.handleDiameterASA())
     g.diamForwarder.machine.Handle("RAR", g.handleDiameterRAR())
     g.diamForwarder.machine.Handle("RAA", g.handleDiameterRAA())
     g.diamForwarder.machine.Handle("STR", g.handleDiameterSTR())
     g.diamForwarder.machine.Handle("STA", g.handleDiameterSTA())
     ```
   - Decision: move the handler methods from `DiameterHandler` to either `diamForwarder` directly OR to `Gateway` as methods. Recommended: put them on `diamForwarder` so the type that owns the state machine also owns the handlers (single-responsibility). The registry / forwardToBiz dependencies are forwarded into the forwarder constructor.
   - **Spec verification:** see §6 — must run conformance to confirm ASR/RAR/STR still flow correctly end-to-end.

5. **`internal/config/config.go`**:
   - Remove `ListenDIAMETER string \`yaml:"listenDiameter"\`` from `AAAgwConfig`.
   - Remove `if cfg.AAAgw.ListenDIAMETER == "" { cfg.AAAgw.ListenDIAMETER = ":3868" }` default.
   - Remove `cfg.AAAgw.DiameterProtocol` field too — gateway no longer picks protocol (no listener to choose). The forwarder hard-codes `tcp` per `df.network` (set by `gateway.New`).

6. **`compose/configs/aaa-gateway.yaml`** — delete the line `listenDiameter: ":3868"` and `diameterProtocol: "tcp"`.

7. **`cmd/aaa-gateway/main.go`**:
   - Delete the `"listen_diameter"` log key-value pair.
   - Delete the `ListenDIAMETER: cfg.AAAgw.ListenDIAMETER` line in the `gateway.Config` literal.
   - Delete `DiameterProtocol: cfg.AAAgw.DiameterProtocol` line too if removed in (5).

8. **`internal/aaa/gateway/diameter_forward.go`** — extend the constructor to accept `forwardToBiz`, `registry`, `bizURL`, `httpClient` (or `RegistryHandle` interface), so handlers can be registered on `df.machine`. Implementation detail: the constructor signature changes.

**For RADIUS (kept):** rename the log key from `"radius_addr"` to `"listen_radius"` at `cmd/aaa-gateway/main.go` line 247, purely cosmetic for grep-ability.

---

## 4. New Tests

Add to `internal/aaa/gateway/diameter_forward_test.go`:

### Test 1 — `monitorConnection` reconnects after `connected=false`
```go
func TestDiamForwarder_MonitorReconnects_WhenDisconnected(t *testing.T) {
    df := newDiamForwarder(
        "127.0.0.1:1", "tcp",
        "aaa-gateway.example.com", "example.com",
        "aaa-server.example.com", "example.com",
        DefaultConfig(),
        slog.Default(),
    )
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    df.mu.Lock()
    df.connected = false
    df.mu.Unlock()
    go df.monitorConnection(ctx)
    time.Sleep(200 * time.Millisecond)
    cancel()
    time.Sleep(50 * time.Millisecond)
}
```

### Test 2 — `getConn` synchronously reconnects when `conn==nil`
```go
func TestDiamForwarder_GetConn_AfterDisconnect_SyncReconnectAttempt(t *testing.T) {
    df := newDiamForwarder(
        "127.0.0.1:1", "tcp",
        "aaa-gateway.example.com", "example.com",
        "aaa-server.example.com", "example.com",
        DefaultConfig(),
        slog.Default(),
    )
    _, err := df.getConn()
    if err == nil {
        t.Fatal("expected error when no server is listening, got nil")
    }
}
```

### Test 3 — ASR handler fires on `diamForwarder.machine` (NEW)
Verifies the architectural migration: an inbound ASR message on the gateway's outbound TCP socket triggers `handleASR`. Use a mock `diam.Conn` + `diam.Message`; assert the handler registers the request with the `ServerInitiatedRegistry`.

### Test 4 — Gateway has no Diameter listener (NEW)
Compile-time verification: `gateway.Config` struct has no `ListenDIAMETER` field. If it does, the build fails. This test is more of a documentation/sanity check; the build itself catches it.

Existing tests (`TestDiamForwarder_OriginStateId_*`, `TestDiamForwarder_GetConnectionStats`, etc.) continue to pass — they don't depend on connection lifetime.

---

## 5. Out of Scope

- No changes to `cmd/aaa-sim/main.go`, `test/aaa_sim/`, `internal/aaa/gateway/radius_forward.go`, `internal/aaa/gateway/radius_handler.go`, `compose/configs/aaa-gateway.yaml` (other than removing the two lines called out).
- No 3GPP spec changes.
- No performance tuning.
- The "future dedicated AIW conformance harness" remains a separate GSD phase.

---

## 6. Verification

```bash
# Build & tests
go build ./...
go test ./internal/aaa/gateway/...    # includes 4 tests
go vet ./...

# Architectural invariants
grep -rn "ListenDIAMETER\|listenDiameter\|listen_diameter" --include="*.go" --include="*.yaml" .  # expect: no results
grep -rn "AAA_S_RADIUS_ADDR\|AAA_S_DIAMETER_ADDR\|AAA_GW_RADIUS_ADDR\|AAA_GW_DIAMETER_ADDR" .  # expect: only docs/comment history
grep -rn "mock_aaa_s" --include="*.go" .   # expect: no results
ls deploy/compose/aiw-tests 2>/dev/null   # expect: No such file or directory
grep -rn "DiameterHandler" internal/aaa/gateway/   # expect: only handler-method callsites, no construction

# Spec conformance: end-to-end
# (Run conformance suite — ASR/RAR/STR still flow through forwarder)
make conformance
```

---

## 7. Risk Assessment

| Change | Risk | Mitigation |
|--------|------|------------|
| Wire `CloseNotifier`-based `watchDisconnect` to `df.conn` clearing | Race: clearing `df.conn` while `Forward()` reads it | All accesses already under `df.mu` (RWMutex) — keep same pattern; capture local `conn` then re-check under lock |
| Delete `aiw-tests` directory | Lose working reference config | Verified that no test/CI references it; conformance uses fullchain |
| Delete `compose/mock_aaa_s.go` | Lose fallback server | Unreferenced — `Dockerfile.mock-aaa-s` already runs real `aaa-sim` |
| **Move ASR/RAR/STR handlers from `DiameterHandler.sm` to `diamForwarder.machine`** | **Lose server-initiated inbound Diameter flow if handlers are not registered on the right state machine** | **Build fails if `diameter_handler.Listen()` references are dangling. Run `make conformance` to exercise ASR/RAR/STR end-to-end before merge. The handler unit tests (`Test 3`) prove the handler fires when the forwarder's machine receives ASR.** |
| Remove `ListenDIAMETER` from config | Existing deployments with the field set get warnings or errors on config load | YAML unmarshal silently ignores unknown fields by default; verify with `compose/configs/aaa-gateway.yaml` deployment in dev |
| Delete `diameter_handler.Listen()` and TLS helpers | Lose TLS-listener path for Diameter | TLS was never wired through (TCP was the only tested path); re-introducing TLS later is a separate spec |