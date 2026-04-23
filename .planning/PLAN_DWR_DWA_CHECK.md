# DWR/DWA Watchdog — Gap Analysis

**Date:** 2026-04-22
**Phase:** PHASE_Refactor_3Component
**Checked by:** gsd-plan-checker
**Question:** DWR/DWA watchdog — handled on both client and server sides?

---

## RFC 6733 Requirements

RFC 6733 §5.5 (Transport Failure Detection):

| Requirement | RFC Citation |
|---|---|
| DWR/DWA command code = **280** | RFC 6733 Table §2 — "DWR 280 \| DWA 280" |
| DWR sent when no traffic exchanged | RFC 6733 §5.5.1: "is sent to a peer when no traffic has been exchanged between two peers" |
| **Both peers** MUST respond to DWR | RFC 6733 §5.6 state machine: `R-Rcv-DWR → Process-DWR, R-Snd-DWA` |
| Connection stable after 3 watchdogs | RFC 6733 §5.5.3: "Three watchdog messages are exchanged with accepted round-trip times, and the connection to the peer is considered stabilized" |
| Both sides can initiate DWR | RFC 6733 §5.5.3: either peer can send DWR |
| CER/CEA/DWR/DWA carry App Id = 0 | RFC 6733 §2: "MUST carry an Application Id of zero (0)" |

**State machine evidence (RFC 6733 §5.6):**

```
Initiator (I) side:
  I-Rcv-DWR → Process-DWR, I-Snd-DWA
  I-Rcv-DWA → Process-DWA, I-Open

Responder (R) side:
  R-Rcv-DWR → Process-DWR, R-Snd-DWA
  R-Rcv-DWA → Process-DWA, R-Open
```

---

## Current Implementation Analysis

### 1. Client Side (`internal/diameter/client.go`) — **HANDLED**

```go
// client.go lines 94-100
c.smClient = &sm.Client{
    EnableWatchdog:     true,
    WatchdogInterval:   30 * time.Second,
}
```

**Verdict:** ✅ `go-diameter/v4` sm.Client handles DWR/DWA automatically. When enabled, it sends DWR every 30s if no traffic, responds to DWR from peer, and marks connection stable after 3 successful exchanges. RFC 6733 §5.5 compliant.

### 2. Server Side (`internal/aaa/gateway/diameter_handler.go`) — **MISSING**

The `HandleConnection()` function reads messages and routes by command code:

```go
// diameter_handler.go lines 138-144
switch commandCode {
case 268:
    // DEA — client-initiated response
case 274:
    // ASR — server-initiated
default:
    h.logger.Debug("Diameter unhandled command code", "command_code", commandCode)
}
```

**Three gaps:**

**Gap 1:** No handling for **command code 280** (DWR/DWA). If AAA-S sends DWR, it is logged as "unhandled" and dropped. The handler never sends DWA back. This violates RFC 6733 §5.6: "R-Rcv-DWR → Process-DWR, R-Snd-DWA."

**Gap 2:** The plan says "CER/CEA handled by go-diameter/v4/sm on server side" but the actual `diameter_handler.go` does **manual header parsing only** — no CER/CEA state machine, no DWR handling, no state tracking. This was partially corrected (note added in PLAN §2.3.3) but the **implementation** is still manual parsing.

**Gap 3:** Even if CER/CEA used go-diameter, the `go-diameter/v4` library uses `sm.Client` for client-side and `sm.Listener` for server-side. The current implementation uses neither — just raw `net.Conn.Read`.

---

## Watchdog State Machine Gap (Both Sides)

RFC 6733 §5.5.3 Transport Failure Algorithm:

```
Two peers establish connection → CER/CEA → connection open

Problem: how does each peer know the other is alive?

Answer: DWR/DWA — either peer sends DWR when no traffic for T seconds.
        Peer responds with DWA. If no DWA within timeout → suspect.
        Three consecutive failures → peer removed, failover triggered.
```

The current implementation:

| Component | DWR sent? | DWA sent on receipt? | Failure → reconnect? |
|---|---|---|---|
| `internal/diameter/client.go` | ✅ Yes (go-diameter sm.Client) | ✅ Yes (go-diameter sm.Client) | ✅ Yes (go-diameter sm.Client) |
| `diameter_handler.go` (server) | ❌ No | ❌ No — DWR dropped | ❌ No |

---

## Root Cause

1. **Server-side treated as stateless.** The plan assumed manual header parsing was sufficient. RFC 6733 §5.6 proves otherwise — the server-side state machine has explicit DWR/DWA transitions (`R-Rcv-DWR`, `R-Snd-DWA`, `R-Rcv-DWA`).

2. **Wrong assumption about who sends DWR.** The original design assumed only the client initiates DWR. RFC 6733 §5.5.1 says: "DWR is sent to a peer when no traffic has been exchanged between two peers" — meaning **either peer**.

3. **Two implementations needed, not one.** go-diameter/v4 provides `sm.Client` (initiator side) and `sm.Listener` (responder side). The plan only described the client forwarder (§2.3.5). The server-side listener (`HandleConnection`) needs its own go-diameter state machine — or the existing `HandleConnection` needs to be replaced with `sm.Listener`.

---

## What Needs to Be Added

### For Server Side (`diameter_handler.go`):

Option A: Replace `HandleConnection` with `sm.Listener` pattern:
```go
// Uses go-diameter/v4/sm.Listener for RFC 6733 §5.6 compliant state machine
listener, _ := sm.NewListener(conn, settings, handler)
listener.BindAndListen() // handles CER/CEA, DWR/DWA, DPR/DPA automatically
```

Option B: Extend current manual handler to handle DWR:
```go
case 280:
    // DWR or DWA
    if isRequest(header) {
        // Send DWA back
        h.sendDWA(conn, header)
    }
    // DWA: log only (client already handles this)
```

### For Plan Updates:

1. **Fix command code 280** in plan — DWR/DWA command code is 280, not a different value. The plan already references RFC 6733 §5.4 but never explicitly states the command code in the handler section.

2. **Add server-side state machine** to §2.3.3: `HandleConnection` must use `sm.Listener` (or equivalent) to handle CER/CEA AND DWR/DWA. Manual parsing is only safe for post-handshake application messages AFTER CER/CEA completes.

3. **Clarify watchdog trigger** in §2.3.5: DWR is sent when **no traffic exchanged** for 30s — either peer can initiate. go-diameter's `sm.Client` implements this on the client side. The server side needs equivalent logic.

4. **Add DWR command code to server-side handler** in plan template.

---

## Summary Table

| Component | Path | RFC 6733 §5.5 DWR/DWA | Status |
|---|---|---|---|
| `internal/diameter/client.go` | Client-initiated (NSSAAF→AAA-S) | Sends DWR, responds to DWR | ✅ READY |
| `diameter_handler.go` (server) | Server-initiated (AAA-S→NSSAAF) | **No DWR handling** | ❌ MISSING |
| Plan §2.3.5 `diamForwarder` | Client-initiated forwarder design | Described (go-diameter sm.Client) | ✅ In plan |
| Plan §2.3.3 `HandleConnection` | Server-side design | **No DWR/DWA in plan** | ❌ MISSING |

---

## Spec References

| Need | Spec | Section |
|---|---|---|
| DWR/DWA command code = 280 | RFC 6733 | Table §2 |
| When DWR is sent (inactivity) | RFC 6733 | §5.5.1 |
| DWA response required | RFC 6733 | §5.5.2 |
| Both peers process DWR | RFC 6733 | §5.6 state machine |
| App Id = 0 for DWR/DWA | RFC 6733 | §2 |
| 3 watchdog stabilization | RFC 6733 | §5.5.3 |
