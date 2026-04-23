# Diameter CER/CEA & Connection Management — Gap Analysis

## The Question

Diameter TCP/SCTP requires maintaining a persistent connection to AAA-S with CER/CEA handshake and watchdog. RFC 6733 and RFC 4072 mandate that before any application messages (DER/DEA), both peers exchange Capabilities-Exchange-Request (CER) and Capabilities-Exchange-Answer (CEA). The AAA Gateway must also send Device-Watchdog-Request (DWR) periodically to detect connection failures. Why is the implementation missing this?

## Current Implementation Analysis

### 1. Client-Initiated Path (DER/DEA): NSSAAF → AAA-S

The AAA Gateway's `diameter_handler.Forward()` is a **stub** that returns empty bytes:

```169:173:internal/aaa/gateway/diameter_handler.go
// Forward sends a Diameter message to AAA-S and returns the response.
// This is a stub — the actual implementation forwards to AAA-S.
func (h *DiameterHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	// TODO: Implement actual Diameter forwarding to AAA-S server
	// For now, return a placeholder empty response
	return []byte{}, nil
}
```

This is called from `gateway.go` line 165:

```164:166:internal/aaa/gateway/gateway.go
	case proto.TransportDIAMETER:
		response, err = g.diameterHandler.Forward(ctx, req.Payload, req.SessionID)
```

**What happens:** Every DIAMETER-based NSSAA procedure gets an empty `[]byte{}` response. The Biz Pod's EAP engine receives no AAA response. Authentication always fails.

The `proto.AaaForwardRequest.Payload` contains raw EAP bytes, not a DER-encoded Diameter message. So even if `Forward()` were wired to a raw TCP socket, it would send unframed EAP payloads, not DER with proper header and AVPs.

### 2. Server-Initiated Path (ASR): AAA-S → NSSAAF

This path is correctly implemented:

- AAA-S dials AAA Gateway's TCP listener on `:3868`
- `HandleConnection()` reads the 20-byte Diameter header, routes by Command Code
- Command Code 274 (ASR) → `handleServerInitiated()` → `forwardToBiz()` → HTTP POST to Biz Pod
- Command Code 268 (DEA response) → `publishResponse()` → Redis pub/sub

No CER/CEA is needed here because **AAA-S initiates the connection**. The handshake is AAA-S's responsibility. This path is fine.

### 3. `internal/diameter/client.go` — Full CER/CEA Implementation

The `internal/diameter/client.go` package (the Biz Pod / old monolithic binary's Diameter client) has proper CER/CEA and watchdog:

```94:104:internal/diameter/client.go
	c.smClient = &sm.Client{
		Dict:               dict.Default,
		Handler:            c.machine,
		MaxRetransmits:     3,
		RetransmitInterval: 5 * time.Second,
		EnableWatchdog:     true,
		WatchdogInterval:   30 * time.Second,
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP)),
		},
	}
```

The `sm.New(c.settings)` creates a state machine that handles CER/CEA handshake automatically. `PeerMetadata()` (line 316-327) extracts CEA results. This is the correct RFC 6733 implementation.

### 4. The Architecture Barrier

The plan's **Task 1.1** (proto contract) explicitly requires:

> `internal/proto/` must have **zero dependencies** on `internal/radius/`, `internal/diameter/`, `internal/eap/`, `internal/aaa/`, or any external package beyond the Go standard library.

This means `internal/aaa/gateway/` cannot import `internal/diameter/`. The AAA Gateway cannot reuse `diameter.Client` directly.

The plan's **Task 2.3.3** (diameter_handler.go) says:

> go-diameter/v4 is used in `internal/diameter/client.go` (client-initiated path). This server-side handler uses manual header parsing — **no go-diameter/v4 import needed**.

This explicitly opts for manual parsing on the server side (fine for reading incoming messages). But it does **not** address the `Forward()` stub for the client-initiated outgoing path.

## What the Plan Says

The plan (PHASE §2.3.3) describes `diameter_handler.go` with these key statements:

- "Implement Diameter listener supporting both TCP and SCTP" — listener ✓
- "parses CER/CEA handshake via go-diameter/v4 state machine" — **ambiguous**: this is only for the server-side listener, where AAA-S initiates the connection. No CER/CEA needed there.
- "streams messages. Distinguish client-initiated (Diameter-EAR/EAP) from server-initiated (ASR)." — server-initiated ✓, client-initiated ✗
- "The server-side handler uses manual header parsing (no go-diameter/v4 import)" — fine for reading
- The plan never describes what `Forward()` should do, never mentions connection pool management, never describes CER/CEA for the outbound path

The plan's **Task 6.1.2** (Diameter server-initiated handler) has a TODO comment about ASA error codes but does not address the `Forward()` stub.

**No plan section describes:**
- A connection pool to AAA-S
- CER/CEA handshake for the outbound client-initiated path
- DWR/DWA watchdog
- How `Forward()` encodes raw EAP bytes into a proper DER message
- How `Forward()` waits for and routes the DEA response by session ID

## The Gap

| RFC Requirement | Status | Location |
|---|---|---|
| CER/CEA handshake before DER | **MISSING** | `diameter_handler.Forward()` stub |
| DER encoding (Session-Id, Auth-Application-Id, EAP-Payload AVP) | **MISSING** | `Forward()` does nothing |
| Hop-by-hop ID tracking for DEA correlation | **MISSING** | `Forward()` has no pending map |
| DWR/DWA watchdog | **MISSING** | No watchdog in AAA Gateway |
| Connection pool / reconnect | **MISSING** | No connection management |

### Why Is This Missing?

Three compounding causes:

1. **The plan treats `Forward()` as trivial.** The plan says "AAA Gateway forwards raw RADIUS/Diameter transport bytes without modification" (Task 1.1 comment on `AaaForwardRequest.Payload`). This works for RADIUS (stateless UDP), but not for Diameter (stateful, connection-oriented, requires CER/CEA). The plan assumed raw forwarding was sufficient.

2. **The architecture barrier made it invisible.** Because `internal/proto/` cannot import `internal/diameter/`, the plan had no mechanism to describe the AAA Gateway's Diameter client. The planner may have concluded the AAA Gateway just forwards bytes and left `Forward()` as a stub because the real work was assumed to be "in Task 6.1.2" (it wasn't — that task only covers server-initiated).

3. **The server-initiated path worked, masking the gap.** The ASR path is fully implemented (reads messages, routes correctly, sends to Biz Pod). This may have created a false sense of completeness. The client-initiated path (`Forward()`) was never wired to anything real.

## Recommended Fix

The fix requires two decisions:

### Decision A: AAA Gateway Owns the Diameter Client

The AAA Gateway creates its own `diameter.Client` (not importing `internal/diameter/`, but creating a minimal client within `internal/aaa/gateway/`). It maintains a connection pool to AAA-S, performs CER/CEA on startup, and implements watchdog. On each `Forward()` call, it encodes the EAP payload into a DER, sends it, waits for DEA by hop-by-hop ID, and returns the raw bytes.

**Trade-off:** The AAA Gateway becomes Diameter-aware. It must carry the go-diameter/v4 dependency. The `proto.AaaForwardRequest.Payload` must be a fully-encoded DER (the Biz Pod or an intermediate encoder must produce it). Alternatively, the payload contains EAP and the AAA Gateway wraps it in DER AVPs.

**Implementation path:**
1. Add `aaaGateway.diameterServerAddress`, `aaaGateway.diameterRealm`, `aaaGateway.diameterHost` to config
2. Create `internal/aaa/gateway/diameter_client.go` with a dedicated `diameterClient` struct (not importing `internal/diameter/`, but using `go-diameter/v4` directly)
3. On startup, `Connect()` to AAA-S, perform CER/CEA, start watchdog goroutine
4. Implement `Forward()`: build DER message, register hop-by-hop ID → sessionID mapping, send, wait on channel, return DEA bytes
5. On DEA arrival (from the `sm.Client` handler), look up sessionID by hop-by-hop, publish to Redis

### Decision B: Biz Pod Produces Encoded DER, AAA Gateway Does Raw Forwarding

The Biz Pod (or an encoding layer in the Biz Pod) produces a fully-encoded DER message as `AaaForwardRequest.Payload`. The AAA Gateway opens a TCP connection to AAA-S, sends the bytes, reads the response, and returns it.

**Trade-off:** The AAA Gateway stays "dumb" — no go-diameter/v4 dependency, no state machine. But:
- Connection establishment overhead on every request (no connection reuse)
- No CER/CEA (AAA-S may reject the connection if CER is required)
- No watchdog (dead connection not detected)
- The Biz Pod must encode DER correctly

**Trade-off:** RFC 6733 §2.1: "A Diameter peer MUST... send a CER to the peer before sending any other message." If the AAA Gateway sends DER without CER, AAA-S can reject it.

### Recommended: Decision A

RFC 6733 mandates CER/CEA. A connectionless proxy is not possible for Diameter TCP/SCTP. The AAA Gateway must manage the Diameter connection. This means:

1. **Add Diameter config to `AAAgwConfig`** (`DiameterServerAddress`, `DiameterRealm`, `DiameterHost`, `DiameterNetwork`)
2. **Create `diameter_forward.go`** in `internal/aaa/gateway/` — a lightweight wrapper that uses `go-diameter/v4/sm` directly (not `internal/diameter/`, just the library). This avoids the `internal/proto/` zero-dependency rule since the gateway is not proto.
3. **Wire the diameter forwarder into `Gateway.New()`** — initialize on startup, maintain connection, handle watchdog
4. **Update `Forward()`** — encode EAP payload into DER AVPs, send, correlate DEA by hop-by-hop ID
5. **Update the plan** — add this as a deferred item in the Risk Register, since this was a silent gap

### What to Add to the Plan

The plan's **Task 2.3.3** should be updated to include:

```markdown
#### 2.3.3.1 `diameter_forward.go` (Client-Initiated Path)

The AAA Gateway MUST maintain a persistent TCP/SCTP connection to AAA-S for the client-initiated path.
This is distinct from the server-side listener (HandleConnection). The forwarder:

1. On startup: connect to AAA-S, send CER, receive CEA, start watchdog goroutine (DWR/DWA every 30s)
2. On Forward(): build DER from AaaForwardRequest.Payload (EAP bytes → DER with Session-Id, Auth-Application-Id, EAP-Payload AVP), send, wait for DEA by hop-by-hop ID, return raw DEA bytes
3. On connection failure: reconnect with exponential backoff (max 3 retries, 5s interval)

Uses go-diameter/v4/sm directly — NOT internal/diameter/client.go (which is for the old monolithic binary).

Spec: RFC 6733 §2.1 (CER/CEA), RFC 6733 §5.4 (DWR/DWA), RFC 4072 (Diameter EAP DER/DEA)
```

## Root Cause Summary

| Factor | Description |
|---|---|
| **Design assumption** | The plan assumed "raw byte forwarding" works for Diameter like it does for RADIUS UDP. It doesn't. |
| **Architectural constraint** | `internal/proto/` zero-dependency rule made the planner avoid describing a Diameter client in the AAA Gateway. |
| **Scope ambiguity** | Task 2.3.3 covers server-initiated (correctly) but Task 6.1.2 never addressed the `Forward()` stub for client-initiated. |
| **Missing requirement** | The plan never specified that the AAA Gateway needs AAA-S connection parameters in its config. |

The stub `Forward()` was written with a `// TODO` comment, meaning the implementor (or planner) knew it was incomplete but deferred it. It should have been flagged as a blocking gap requiring a design decision (Decision A vs B above).
