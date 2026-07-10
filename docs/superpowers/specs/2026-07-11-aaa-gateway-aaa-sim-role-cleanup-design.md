# aaa-gateway Client / aaa-sim Server Role Cleanup Design

**Date:** 2026-07-11
**Status:** Draft
**Spec Refs:** RFC 6733 §5.3 (CER/CEA), §5.5 (DWR/DWA), §5.6 (DPR); RFC 2865; TS 29.561 Ch.16/17

---

## 1. Problem Statement

A codebase audit (2026-07-11) revealed that the live production code correctly implements `aaa-gateway` as a RADIUS/Diameter **client** and `aaa-sim` as a **server**, but six inconsistencies / defects remain:

1. **Dead-code disconnect detection** — `diamForwarder.monitorConnection` watches for `df.conn == nil` to reconnect, but `df.conn` is never set to nil during normal operation. The reconnect branch never fires.
2. **Broken `aiw-tests` compose stack** — `deploy/compose/aiw-tests/docker-compose.yaml` references FreeRADIUS for Diameter (which it doesn't support) and uses env var names (`AAA_GW_RADIUS_ADDR`, `AAA_GW_DIAMETER_ADDR`) that the gateway doesn't read.
3. **Orphan duplicate AAA-S** — `compose/mock_aaa_s.go` (393 lines) duplicates `test/aaa_sim/`, but is never referenced; `Dockerfile.mock-aaa-s` already runs the real `aaa-sim` binary.
4. **Stale roadmap doc** — `docs/roadmap/PHASE_Refactor_3Component.md` references nonexistent env vars `AAA_S_RADIUS_ADDR` / `AAA_S_DIAMETER_ADDR`.
5. **Cosmetic log ambiguity** — `cmd/aaa-gateway/main.go` logs `"radius_addr"` / `"diameter_addr"` for the server-initiated listening ports.
6. **No test coverage** for the disconnect-and-reconnect path of `diamForwarder`.

This design fixes items (1)–(5) and adds tests for item (6).

---

## 2. Architecture (current — unchanged)

```
                  ┌──────────────┐
   AAA-S (sim) ◄──┤   aaa-sim    ├── listens
   ┌──────────┐   │ (cmd/aaa-sim)│   UDP :1812   (RADIUS)
   │  listens │   │              │   TCP :3868   (Diameter)
   │  CER/CEA │   └──────▲───────┘
   │  Access- │          │  dial CER (TCP)      server-initiated path
   │  Request │          │                      (RADIUS CoA, Diameter
   └──────────┘          │                       ASR/RAR/STR)
        ▲                │
        │                ▼
   ┌────┴───────────────────────────────────────────────┐
   │ aaa-gateway (cmd/aaa-gateway)                      │
   │  * diamForwarder: DialNetwork → CER/CEA            │
   │                  DWR/DWA every 30s (watchdog)       │
   │  * radiusForwarder: stateless UDP client           │
   │  * DiameterHandler: listen :3868 (server-init.)    │
   │  * RadiusHandler:   listen :1812 UDP (server-init.)│
   └─────────────────────────────────────────────────────┘
```

**Already-correct production code paths:**

| Path | Direction | Owner | Verified at |
|------|-----------|-------|-------------|
| RADIUS UDP client (Access-Request) | gw → sim | `radiusForwarder.Forward()` | `internal/aaa/gateway/radius_forward.go:70` |
| Diameter TCP client (DER, DWR) | gw → sim | `diamForwarder.Connect()` + `Forward()` | `internal/aaa/gateway/diameter_forward.go:156` |
| RADIUS UDP server (CoA / DM) | sim → gw | `RadiusHandler.Listen()` | `internal/aaa/gateway/radius_handler.go:44` |
| Diameter TCP server (ASR / RAR / STR) | sim → gw | `DiameterHandler.Listen()` | `internal/aaa/gateway/diameter_handler.go:107` |
| AAA-S listener (server role) | listen | `test/aaa_sim/{radius,diameter}.go` | `cmd/aaa-sim/main.go:21` |

---

## 3. Defects and Fixes

### 3.1 Issue A — `diamForwarder` reconnect path is dead code

**File:** `internal/aaa/gateway/diameter_forward.go`

**Root cause:** `monitorConnection` checks `if conn == nil` but only `Close()` ever nil-sets it. In normal operation, when the socket dies (server restart, network drop, peer DPR), `df.conn` still points to the closed `diam.Conn`, so the reconnect branch never fires.

**Fix:** Use the `diam.CloseNotifier` interface — every `diam.Conn` returned by `sm.Client.DialNetwork()` exposes `CloseNotify() <-chan struct{}` which closes when the underlying socket is gone (used by `sm/client.go:286` for the watchdog itself).

```go
// newDiamForwarder (add field):
type diamForwarder struct {
    // ... existing fields
}

// (no client.NotifyLost — that field doesn't exist; we use CloseNotify)
```

**`Connect()`:**
```go
func (df *diamForwarder) Connect(ctx context.Context) error {
    conn, err := df.smClient.DialNetwork(df.network, df.addr)
    if err != nil {
        return fmt.Errorf("diameter_forward: failed to connect to %s: %w", df.addr, err)
    }

    df.mu.Lock()
    df.conn = conn
    df.connected = true
    df.connStats.ConnectedAt = time.Now()
    df.connStats.HandshakeAt = time.Now()
    df.mu.Unlock()

    // Increment Origin-State-Id on each new connection (GAP-DIA-02).
    osi := df.incrementOriginStateID()

    df.logger.Info("diameter_forward_connected",
        "server", df.addr, "network", df.network,
        "origin_host", df.originHost, "origin_state_id", osi)

    // Watch peer disconnect via diam.CloseNotifier (RFC 6733 §5.6 + sm internals).
    go df.watchDisconnect(ctx)

    // Drive reconnect loop independently.
    go df.monitorConnection(ctx)

    return nil
}

// watchDisconnect blocks until the underlying socket signals CloseNotify.
// On notification, clears df.conn so monitorConnection will reconnect.
func (df *diamForwarder) watchDisconnect(ctx context.Context) {
    df.mu.RLock()
    conn := df.conn
    df.mu.RUnlock()
    if conn == nil {
        return
    }
    notifier, ok := conn.(diam.CloseNotifier)
    if !ok {
        // CloseNotify isn't supported on this conn; fall back to no-op.
        return
    }
    select {
    case <-ctx.Done():
        return
    case <-notifier.CloseNotify():
        df.mu.Lock()
        if df.conn == conn {
            df.conn = nil
            df.connected = false
            df.logger.Warn("diameter_forward_peer_lost", "server", df.addr)
            df.mu.Unlock()
            df.clearPending()
        } else {
            df.mu.Unlock()
        }
    }
}
```

**`monitorConnection` (refactored — react to connected flag):**
```go
func (df *diamForwarder) monitorConnection(ctx context.Context) {
    backoff := 1 * time.Second
    const maxBackoff = 30 * time.Second

    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        df.mu.RLock()
        connected := df.connected
        df.mu.RUnlock()

        if !connected {
            df.mu.Lock()
            newConn, err := df.smClient.DialNetwork(df.network, df.addr)
            if err != nil {
                df.mu.Unlock()
                df.logger.Error("diameter_forward_reconnect_failed",
                    "error", err, "backoff", backoff)
                select {
                case <-ctx.Done():
                    return
                case <-time.After(backoff):
                }
                backoff = min(backoff*2, maxBackoff)
                continue
            }
            df.conn = newConn
            df.connected = true
            df.connStats.ConnectedAt = time.Now()
            df.mu.Unlock()

            df.logger.Info("diameter_forward_reconnected", "server", df.addr)
            backoff = 1 * time.Second
            df.clearPending()

            // Watch the new connection.
            go df.watchDisconnect(ctx)
        }

        select {
        case <-ctx.Done():
            return
        case <-time.After(2 * time.Second):
        }
    }
}
```

**Bounded retry:** exponential backoff remains 1s → 30s.

### 3.2 Issue B — Delete broken aiw-tests compose stack

**File:** Delete `deploy/compose/aiw-tests/` in its entirety:
- `docker-compose.yaml` (uses non-existent env vars, sets Diameter to FreeRADIUS which doesn't speak Diameter)
- `.env`
- `freeradius/radiusd.conf`
- `freeradius/clients.conf`
- `freeradius/eap.conf`

This stack was never wired into CI or `test/conformance/`. The conformance tests for AIW (`test/conformance/nssaa_create_test.go`, `test/e2e/aiw_curl_test.go`) use the fullchain stack (`compose/fullchain-dev.yaml`) and shell scripts (`scripts/curl-aiw-tests.sh`). A future dedicated AIW conformance harness is a separate GSD phase.

### 3.3 Issue C — Delete orphan duplicate

**File:** delete `compose/mock_aaa_s.go` (393 lines).

Confirmed unreferenced by:
- `grep -r "compose/mock_aaa_s" .` — no results
- `Dockerfile.mock-aaa-s:16` — copies `bin/aaa-sim` (not the duplicate)
- `compose/fullchain-dev.yaml` — uses `Dockerfile.aaa-sim`, not the duplicate

The legitimate server is `test/aaa_sim/` package, built via `cmd/aaa-sim/`.

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

### 3.5 Issue E — Cosmetic log field rename

**File:** `cmd/aaa-gateway/main.go` lines 39-40.

Change:
```go
"radius_addr", cfg.AAAgw.ListenRADIUS,
"diameter_addr", cfg.AAAgw.ListenDIAMETER,
```
to:
```go
"listen_radius", cfg.AAAgw.ListenRADIUS,
"listen_diameter", cfg.AAAgw.ListenDIAMETER,
```

Purely cosmetic for grep-ability between server-initiated listen vs. client-initiated forwarding addrs.

---

## 4. New Tests

Add to `internal/aaa/gateway/diameter_forward_test.go`:

The disconnect-detection path requires a live socket, which is hard to fake in a unit test. Instead, the tests below cover the side-effects we can control:

### Test 1 — `monitorConnection` reconnects after `connected=false`
```go
func TestDiamForwarder_MonitorReconnects_WhenDisconnected(t *testing.T) {
    df := newDiamForwarder(
        "127.0.0.1:1", // nothing listening — DialNetwork will fail
        "tcp",
        "aaa-gateway.example.com", "example.com",
        "aaa-server.example.com", "example.com",
        DefaultConfig(),
        slog.Default(),
    )

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Simulate "we used to have a connection, then we lost it"
    df.mu.Lock()
    df.connected = false
    df.mu.Unlock()

    go df.monitorConnection(ctx)

    // Give it one backoff cycle, then verify it tried to reconnect (and failed because nothing listens).
    // Log assertion: "diameter_forward_reconnect_failed" should appear via the configured logger.
    time.Sleep(200 * time.Millisecond)

    cancel()
    // allow goroutine to exit
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

Existing tests (`TestDiamForwarder_OriginStateId_*`, `TestDiamForwarder_GetConnectionStats`, etc.) continue to pass — they don't depend on connection lifetime.

---

## 5. Out of Scope

- No changes to `cmd/aaa-sim/main.go`, `test/aaa_sim/`, `internal/aaa/gateway/radius_forward.go`, `internal/aaa/gateway/{radius,diameter}_handler.go`, `compose/configs/aaa-gateway.yaml`, `internal/config/`.
- No 3GPP spec changes.
- No performance tuning.

---

## 6. Verification

```bash
go build ./...
go test ./internal/aaa/gateway/...    # includes 2 new tests
go vet ./...
grep -r "AAA_S_RADIUS_ADDR\|AAA_S_DIAMETER_ADDR\|AAA_GW_RADIUS_ADDR\|AAA_GW_DIAMETER_ADDR" . # expect: only docs/comment history
grep -r "mock_aaa_s" .                # expect: no results
ls deploy/compose/aiw-tests 2>/dev/null  # expect: No such file or directory
```

---

## 7. Risk Assessment

| Change | Risk | Mitigation |
|--------|------|------------|
| Wire `CloseNotify`-based watchDisconnect to `df.conn` clearing | Race: clearing `df.conn` while `Forward()` reads it | All accesses already under `df.mu` (RWMutex) — keep same pattern; capture local `conn` then re-check under lock |
| Delete aiw-tests directory | Lose working reference config | Verified that no test/CI references it; conformance uses fullchain |
| Delete `compose/mock_aaa_s.go` | Lose fallback server | Unreferenced — `Dockerfile.mock-aaa-s` already runs real `aaa-sim` |
