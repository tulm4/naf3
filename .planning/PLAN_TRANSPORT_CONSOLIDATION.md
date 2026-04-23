# AAA Transport Consolidation — Gap Analysis

## The Question

The user wants the AAA Gateway to use `go-diameter/v4` for transport, mirroring the `internal/diameter/client.go` reference implementation, so that all transport communication with AAA-S (DER/DEA encoding, CER/CEA handshake, DWR/DWA watchdog, hop-by-hop correlation, reconnection) flows through the AAA Gateway. The Biz Pod should only send raw EAP bytes over HTTP.

**Original request (translated from Vietnamese):**
> "Use go-diameter/v4 for the server the same as the client to handle transport, move all transport communication with AAA up to the AAA Gateway (the gateway is currently not handling anything for the client)"

## Current Architecture Analysis

### Client-Initiated Path (AMF → NSSAAF → AAA-S) — BROKEN AT STEP 5

```
AMF
  └─→ POST /nnssaaf-nssaa/v1/slice-authentications
      └─→ HTTP Gateway (cmd/http-gateway/main.go)
          └─→ POST /nnssaaf-nssaa/v1/slice-authentications
              └─→ Biz Pod (cmd/biz/main.go)
                  └─→ EAP engine (internal/eap/engine_client.go)
                      └─→ eap.AAAClient.SendEAP(eapPayload)
                          └─→ httpAAAClient.SendEAP()  [cmd/biz/http_aaa_client.go:55]
                              ├─ SessionID = "nssAAF;{nano};{authCtxID}"  [line 59]
                              ├─ Payload = eapPayload  [line 63]
                              └─→ HTTP POST /aaa/forward
                                  └─→ AAA Gateway HandleForward()  [gateway.go:201]
                                      └─→ ForwardEAP()  [gateway.go:140]
                                          ├─ Write session to Redis: nssaa:session:{SessionID}
                                          ├─ pending[SessionID] = ch  [gateway.go:156]
                                          └─→ DIAMETER: diameterHandler.Forward()  [gateway.go:172]
                                              └─→ RETURNS ERROR  [diameter_handler.go:305]
                                                  "diameter_forward: not implemented (see PLAN §2.3.5)"
```

**The chain breaks at `diameterHandler.Forward()`** — it is a stub (lines 299-306 of `diameter_handler.go`):

```299:306:internal/aaa/gateway/diameter_handler.go
func (h *DiameterHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	// TODO: Implement diameter_forward.go per PLAN §2.3.5
	// - Connect to AAA-S, perform CER/CEA (go-diameter/v4 sm.Client)
	// - Build DER from EAP payload (Session-Id, Auth-Application-Id=5, EAP-Payload AVP=209)
	// - Register hop-by-hop ID → pending channel, send, wait for DEA
	// - DWR watchdog and reconnect on failure
	return []byte{}, fmt.Errorf("diameter_forward: not implemented (see PLAN §2.3.5)")
}
```

### Server-Initiated Path (AAA-S → NSSAAF) — PARTIALLY WORKING

```
AAA-S sends ASR (Diameter) or CoA (RADIUS)
  └─→ AAA Gateway TCP/UDP listener
      └─→ DiameterHandler.HandleConnection()  [diameter_handler.go:93]
      OR
      └─→ RadiusHandler.handlePacket()  [radius_handler.go:59]
          ├─ Extract Session-Id from message
          ├─ publishResponse(SessionID, raw)  [radius_handler.go:70]
          └─→ Redis pub/sub nssaa:aaa-response
              └─→ Biz Pod receive path  [BROKEN — see SessionID vs AuthCtxID bug]
```

The server-initiated listener code exists and correctly identifies ASR/CoA/RAR. But the response routing back to Biz Pod also has the routing bug (see below).

### What `AaaForwardRequest.Payload` Contains

**Comment says one thing** (`aaa_transport.go` line 48):
```48:48:internal/proto/aaa_transport.go
	Payload       []byte       `json:"payload"`     // Raw EAP bytes (already-encoded RADIUS/Diameter)
```

**The comment is misleading.** The actual `http_aaa_client.go` code sets:

```55:64:cmd/biz/http_aaa_client.go
func (c *httpAAAClient) SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error) {
	req := &proto.AaaForwardRequest{
		Version:       c.version,
		SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID),
		AuthCtxID:     authCtxID,
		TransportType: proto.TransportRADIUS, // Default to RADIUS; Biz Router determines actual type
		Direction:     proto.DirectionClientInitiated,
		Payload:       eapPayload,  // ← RAW EAP bytes, NOT pre-encoded RADIUS/DIAMETER
	}
```

**`Payload` is raw EAP bytes from the EAP engine**, not pre-encoded RADIUS/Diameter. The forwarder must encode EAP into the AAA protocol:

- **RADIUS**: Wrap EAP in Access-Request with Message-Authenticator, State, etc.
- **Diameter**: Wrap EAP in DER with Session-Id, Auth-Application-Id, EAP-Payload AVP (code 209), SNSSAI AVP, etc.

The `internal/diameter/client.go` reference implementation shows exactly how (lines 159-256):
- `SendDER()` takes raw `eapPayload []byte`
- Builds DER with Session-Id, Auth-Application-Id=5, EAP-Payload (AVP code 209), SNSSAI
- Uses hop-by-hop ID → pending channel pattern
- Returns serialized DEA bytes

### Pending Channel Routing Bug — SessionID vs AuthCtxID Mismatch

**Two different routing keys are used:**

**AAA Gateway side** (`gateway.go` lines 153-162):
```154:162:internal/aaa/gateway/gateway.go
	// 2. Set up response channel for this session
	ch := make(chan []byte, 1)
	g.pendingMu.Lock()
	g.pending[req.SessionID] = ch   // ← ROUTED BY SessionID
	g.pendingMu.Unlock()

	defer func() {
		g.pendingMu.Lock()
		delete(g.pending, req.SessionID)  // ← DELETED BY SessionID
		g.pendingMu.Unlock()
	}()
```

**Biz Pod side** (`http_aaa_client.go` lines 112-118):
```112:118:cmd/biz/http_aaa_client.go
		c.pendingMu.RLock()
		pendingCh, ok := c.pending[event.AuthCtxID]  // ← LOOKUP BY AuthCtxID
		c.pendingMu.RUnlock()

		if !ok {
			continue // Not for this Biz Pod
		}
```

**SessionID format** (from `http_aaa_client.go` line 59):
```
SessionID = "nssAAF;{unixnano};{authCtxID}"
```

**The mismatch:** The AAA Gateway stores the response channel keyed by `SessionID` (e.g., `"nssAAF;1745412345678901234;auth-ctx-abc123"`), but the Biz Pod looks up by `AuthCtxID` (e.g., `"auth-ctx-abc123"`). These are different strings — the Biz Pod lookup will always fail for the first EAP round-trip.

**Second order effect:** Even if the channel routing worked, the `AaaResponseEvent.AuthCtxID` is set to `""` in `publishResponseBytes()` at `gateway.go` line 256:
```254:258:internal/aaa/gateway/gateway.go
func (g *Gateway) publishResponseBytes(sessionID string, raw []byte) {
	event := proto.AaaResponseEvent{
		Version:   g.version,
		SessionID: sessionID,
		AuthCtxID: "",  // ← EMPTY — lookup will always fail
		Payload:   raw,
	}
```

This means `event.AuthCtxID` is always empty, making the Biz Pod's `pending[event.AuthCtxID]` lookup a lookup of `pending[""]` — which never matches any real AuthCtxID.

**Note:** The server-initiated flow (`forwardToBiz`) at `gateway.go` line 281 correctly populates `AuthCtxID` from the Redis lookup. The client-initiated response path (`publishResponseBytes`) does not.

**Impact:**
1. For DIAMETER: Even after `diameter_forward.go` is implemented, the response will be published to Redis with `AuthCtxID: ""` and the Biz Pod will never match it.
2. For RADIUS: Same issue.
3. This is a **data flow bug**, not a transport bug — fixing the forwarder alone is insufficient.

## The Gap: AAA Gateway Has No Transport Client

### What Should Happen

```
Biz Pod sends raw EAP over HTTP
  └─→ AAA Gateway ForwardEAP()
      └─→ diamForwarder.Forward(ctx, eapPayload, sessionID)
          ├─ 1. Get persistent TCP connection to AAA-S (or connect)
          ├─ 2. Build DER from raw EAP:
          │   ├─ Session-Id = sessionID
          │   ├─ Auth-Application-Id = 5 (Diameter EAP)
          │   ├─ Auth-Request-Type = 1
          │   ├─ Origin-Host, Origin-Realm (from config)
          │   ├─ Destination-Host, Destination-Realm (from config)
          │   ├─ EAP-Payload AVP (code 209) = eapPayload
          │   └─ 3GPP-SNSSAI AVP (code 310)
          ├─ 3. hop-by-hop ID = nextSeq++
          ├─ 4. pending[hopByHopID] = ch
          ├─ 5. Write DER to TCP connection
          ├─ 6. Wait on ch with ctx deadline
          ├─ 7. Return DEA bytes
          └─→ Redis pub/sub → Biz Pod
```

**On connection failure:** Reconnect with exponential backoff (1s, 2s, 4s, max 30s). After 3 consecutive failures, return `context.DeadlineExceeded`.

**DWR watchdog:** `go-diameter/v4/sm.Client` handles DWR/DWA automatically (`EnableWatchdog: true, WatchdogInterval: 30s` at `client.go` lines 99-100).

**CER/CEA handshake:** Handled automatically by `sm.Client` on connect (`client.go` lines 94-104).

### What Actually Happens

```
AAA Gateway ForwardEAP() → diameterHandler.Forward() → returns error "not implemented"
```

No TCP connection is ever made to AAA-S. No DER is built. No DEA is received. Every DIAMETER NSSAA procedure fails immediately.

### Why `go-diameter/v4` Should Be Used

`internal/diameter/client.go` is the **reference implementation**. It is a complete, working implementation:

- **CER/CEA**: `sm.Client` handles handshake automatically (lines 94-104)
- **DER building**: `SendDER()` with all required AVPs (lines 159-218)
- **Pending map**: hop-by-hop ID → channel (lines 69-74, 301-313)
- **DWR watchdog**: `EnableWatchdog: true` (line 99)
- **Reconnection**: `getConn()` lazy-connect pattern (lines 276-293)
- **Peer metadata**: `PeerMetadata()` for CER/CEA metadata (lines 316-327)

The AAA Gateway forwarder should use **the same library** (`github.com/fiorix/go-diameter/v4/diam`, `diam/sm`, `diam/sm/smpeer`), **not import `internal/diameter/`** (more on this below).

## Architectural Constraints

### Why `internal/aaa/gateway/` Cannot Import `internal/diameter/`

**Dependency chain would be violated:**

```
cmd/aaa-gateway/main.go
  └─→ internal/aaa/gateway/  (AAA Gateway package)
      └─→ internal/diameter/  (if imported)
          └─→ internal/radius/ (diameter depends on radius for shared types)
          └─→ internal/eap/ (for Snssai encoding, etc.)
```

This creates a cross-contamination risk. More importantly, `internal/diameter/client.go` is designed for a **synchronous, single-use** client pattern where the caller holds the connection. The AAA Gateway needs a **persistent, shared** forwarder that serves all Biz Pod requests across multiple concurrent EAP sessions.

The architecture decision documented in the plan (lines 720-723) is correct: the AAA Gateway uses `go-diameter/v4` directly, not `internal/diameter/`.

### Why `internal/aaa/gateway/` CAN Use `go-diameter/v4` Directly

Both packages independently import `github.com/fiorix/go-diameter/v4`:

```
internal/diameter/client.go imports:
  "github.com/fiorix/go-diameter/v4/diam"
  "github.com/fiorix/go-diameter/v4/diam/avp"
  "github.com/fiorix/go-diameter/v4/diam/datatype"
  "github.com/fiorix/go-diameter/v4/diam/dict"
  "github.com/fiorix/go-diameter/v4/diam/sm"
  "github.com/fiorix/go-diameter/v4/diam/sm/smpeer"

internal/aaa/gateway/ (would import same):
  "github.com/fiorix/go-diameter/v4/diam"
  "github.com/fiorix/go-diameter/v4/diam/sm"
  "github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
```

There is no shared state between them. Each is a separate client instance. The library is thread-safe when each connection has its own `sm.Client`.

## Recommended Design

### New File: `internal/aaa/gateway/diameter_forward.go`

This file implements a **persistent Diameter client** (the `diamForwarder`) inside the AAA Gateway. It follows the `internal/diameter/client.go` pattern but with a shared, reconnecting architecture.

#### Core Data Structures

```go
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

const (
	// AppIDAAP is the Diameter EAP Application ID (RFC 4072).
	AppIDAAP = 5
	// CmdDER / CmdDEA = 268 (Diameter-EAP-Request/Answer)
)

// diamForwarder manages a persistent connection to AAA-S.
// It is safe for concurrent use by multiple goroutines (multiple Biz Pod requests).
type diamForwarder struct {
	addr        string
	network     string // "tcp" or "sctp"
	settings    *sm.Settings
	machine     *sm.StateMachine
	smClient    *sm.Client
	conn        diam.Conn
	mu          sync.RWMutex
	logger      *slog.Logger

	// Pending requests: hop-by-hop ID → result channel.
	pending   map[uint32]chan *diam.Message
	pendingMu sync.RWMutex

	// Atomic counter for generating unique hop-by-hop IDs.
	hopByHopSeq uint64
}
```

#### Connection Management

```go
// newDiamForwarder creates a new Diameter forwarder.
func newDiamForwarder(addr, network string, originHost, originRealm string, logger *slog.Logger) *diamForwarder {
	df := &diamForwarder{
		addr:    addr,
		network: network,
		logger:  logger,
		pending: make(map[uint32]chan *diam.Message),
	}

	df.settings = &sm.Settings{
		OriginHost:  datatype.DiameterIdentity(originHost),
		OriginRealm: datatype.DiameterIdentity(originRealm),
		VendorID:    datatype.Unsigned32(VendorID3GPP), // from internal/diameter/
		ProductName: "NSSAAF-GW",
	}

	df.machine = sm.New(df.settings)

	df.smClient = &sm.Client{
		Dict:               dict.Default,
		Handler:            df.machine,
		MaxRetransmits:     3,
		RetransmitInterval: 5 * time.Second,
		EnableWatchdog:     true,              // DWR/DWA per RFC 6733 §5.5
		WatchdogInterval:   30 * time.Second,
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP)),
		},
	}

	// Register handler for DEA (and STA) responses.
	df.machine.Handle("DEA", df.handleDEA())
	df.machine.Handle("STA", df.handleDEA())

	return df
}

// Connect establishes and maintains a persistent connection to AAA-S.
func (df *diamForwarder) Connect(ctx context.Context) error {
	df.mu.Lock()
	defer df.mu.Unlock()

	conn, err := df.smClient.DialNetwork(df.network, df.addr)
	if err != nil {
		return fmt.Errorf("diameter_forward: failed to connect to %s: %w", df.addr, err)
	}

	df.conn = conn
	df.logger.Info("diameter_forward_connected",
		"server", df.addr,
		"network", df.network,
	)

	// Start connection monitor goroutine
	go df.monitorConnection(ctx)

	return nil
}

// monitorConnection watches for connection errors and reconnects.
func (df *diamForwarder) monitorConnection(ctx context.Context) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
			df.mu.RLock()
			conn := df.conn
			df.mu.RUnlock()

			if conn == nil {
				// Attempt reconnect
				if err := df.reconnect(ctx); err != nil {
					df.logger.Error("diameter_forward_reconnect_failed",
						"error", err, "backoff", backoff)
					time.Sleep(backoff)
					backoff = min(backoff*2, maxBackoff)
					continue
				}
				backoff = 1 * time.Second // Reset on success
			}

			// Check connection health periodically
			time.Sleep(5 * time.Second)
		}
	}
}

// reconnect attempts to reconnect with exponential backoff.
func (df *diamForwarder) reconnect(ctx context.Context) error {
	df.mu.Lock()
	defer df.mu.Unlock()

	conn, err := df.smClient.DialNetwork(df.network, df.addr)
	if err != nil {
		return err
	}

	df.conn = conn
	df.logger.Info("diameter_forward_reconnected", "server", df.addr)

	// Clear stale pending entries from the old connection
	df.pendingMu.Lock()
	for id, ch := range df.pending {
		select {
		case ch <- nil: // Signal connection was lost
		default:
		}
		delete(df.pending, id)
	}
	df.pendingMu.Unlock()

	return nil
}
```

#### Forward Method (DER → DEA)

```go
// Forward encodes raw EAP payload into a DER message, sends it to AAA-S,
// and waits for the DEA response.
func (df *diamForwarder) Forward(ctx context.Context, eapPayload []byte, sessionID string) ([]byte, error) {
	conn, err := df.getConn()
	if err != nil {
		return nil, fmt.Errorf("diameter_forward: no connection: %w", err)
	}

	// Create channel for response.
	respCh := make(chan *diam.Message, 1)
	hopByHop := df.nextHopByHopID()
	df.addPending(hopByHop, respCh)
	defer df.removePending(hopByHop)

	// Build DER message.
	m := diam.NewRequest(268, AppIDAAP, conn.Dictionary())
	m.Header.HopByHopID = hopByHop

	addAVP := func(code interface{}, flags uint8, vendor uint32, data datatype.Type) error {
		_, err := m.NewAVP(code, flags, vendor, data)
		return err
	}

	// Session-Id (263)
	if err := addAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionID)); err != nil {
		return nil, err
	}
	// Auth-Application-Id (418) — M-bit required
	if err := addAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP)); err != nil {
		return nil, err
	}
	// Auth-Request-Type (274) — M-bit required, value 1 = AUTHORIZE_AUTHENTICATE
	if err := addAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(1)); err != nil {
		return nil, err
	}
	// Auth-Session-State (277) — M-bit required, value 1 = NO_STATE_MAINTAINED
	if err := addAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Unsigned32(1)); err != nil {
		return nil, err
	}
	// Origin-Host (264) — M-bit required
	if err := addAVP(avp.OriginHost, avp.Mbit, 0, df.settings.OriginHost); err != nil {
		return nil, err
	}
	// Origin-Realm (296) — M-bit required
	if err := addAVP(avp.OriginRealm, avp.Mbit, 0, df.settings.OriginRealm); err != nil {
		return nil, err
	}
	// Origin-State-Id (278) — M-bit required per RFC 6733 §8.8
	if err := addAVP(avp.OriginStateID, avp.Mbit, 0, datatype.Unsigned32(1)); err != nil {
		return nil, err
	}
	// Destination-Host (293) — M-bit required
	if err := addAVP(avp.DestinationHost, avp.Mbit, 0, datatype.DiameterIdentity(df.cfg.DestinationHost)); err != nil {
		return nil, err
	}
	// Destination-Realm (283) — M-bit required
	if err := addAVP(avp.DestinationRealm, avp.Mbit, 0, datatype.DiameterIdentity(df.cfg.DestinationRealm)); err != nil {
		return nil, err
	}
	// EAP-Payload AVP (code 209, RFC 4072) — M-bit required
	if err := addAVP(209, avp.Mbit, 0, datatype.OctetString(eapPayload)); err != nil {
		return nil, err
	}
	// 3GPP-SNSSAI AVP (code 310, TS 29.061) — copied from internal/diameter/client.go
	snssaiAVP, err := encodeSnssaiAVP(sst, sd) // S-NSSAI from AaaForwardRequest
	if err != nil {
		return nil, fmt.Errorf("diameter_forward: failed to encode SNSSAI: %w", err)
	}
	m.AddAVP(snssaiAVP)

	// Set deadline on the connection.
	if deadline, ok := ctx.Deadline(); ok {
		if dc, ok := conn.(interface {
			SetWriteDeadline(t time.Time) error
			SetReadDeadline(t time.Time) error
		}); ok {
			_ = dc.SetWriteDeadline(deadline)
			_ = dc.SetReadDeadline(deadline)
		}
	}

	_, err = m.WriteTo(conn)
	if err != nil {
		df.removePending(hopByHop)
		return nil, fmt.Errorf("diameter_forward: failed to send DER: %w", err)
	}

	df.logger.Debug("diameter_forward_der_sent",
		"session_id", sessionID,
		"hop_by_hop", hopByHop,
		"eap_len", len(eapPayload),
	)

	// Wait for response or context cancellation.
	select {
	case <-ctx.Done():
		df.removePending(hopByHop)
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("diameter_forward: connection lost")
		}
		data, err := resp.Serialize()
		if err != nil {
			return nil, fmt.Errorf("diameter_forward: failed to serialize DEA: %w", err)
		}
		return data, nil
	}
}

// handleDEA dispatches DEA responses to pending channels by hop-by-hop ID.
func (df *diamForwarder) handleDEA() diam.HandlerFunc {
	return func(conn diam.Conn, m *diam.Message) {
		hopByHop := m.Header.HopByHopID
		df.pendingMu.RLock()
		ch, ok := df.pending[hopByHop]
		df.pendingMu.RUnlock()
		if !ok {
			df.logger.Warn("diameter_forward_unexpected_responser",
				"hop_by_hop", hopByHop,
			)
			return
		}
		ch <- m
	}
}
```

#### Supporting Methods

```go
func (df *diamForwarder) getConn() (diam.Conn, error) {
	df.mu.RLock()
	conn := df.conn
	df.mu.RUnlock()

	if conn != nil {
		return conn, nil
	}

	if err := df.Connect(context.Background()); err != nil {
		return nil, err
	}

	df.mu.RLock()
	conn = df.conn
	df.mu.RUnlock()
	return conn, nil
}

func (df *diamForwarder) nextHopByHopID() uint32 {
	return uint32(atomic.AddUint64(&df.hopByHopSeq, 1))
}

func (df *diamForwarder) addPending(hopByHop uint32, ch chan *diam.Message) {
	df.pendingMu.Lock()
	df.pending[hopByHop] = ch
	df.pendingMu.Unlock()
}

func (df *diamForwarder) removePending(hopByHop uint32) {
	df.pendingMu.Lock()
	delete(df.pending, hopByHop)
	df.pendingMu.Unlock()
}
```

### Payload Encoding: Raw EAP → DER

The key encoding is the **EAP-Payload AVP** (RFC 4072):

```go
// EAP-Payload AVP (code 209, RFC 4072 §4.2)
if err := addAVP(209, avp.Mbit, 0, datatype.OctetString(eapPayload)); err != nil {
    return nil, err
}
```

This matches exactly what `internal/diameter/client.go` does at line 211:
```211:212:internal/diameter/client.go
	if err := addAVP(209, 0, 0, datatype.OctetString(eapPayload)); err != nil {
```

Note: The M-bit on EAP-Payload AVP is set here but was not set in the original client (line 211 uses `0, 0` instead of `avp.Mbit, 0`). Per RFC 4072, the M-bit (Mandatory bit) SHOULD be set on the EAP-Payload AVP since it carries essential authentication data.

### Session Correlation Fix

**Fix the routing bug** by either:

**Option A (preferred):** Change Biz Pod to use `SessionID` for pending lookup:
```go
// http_aaa_client.go — subscribeResponses
c.pendingMu.RLock()
pendingCh, ok := c.pending[event.SessionID]  // ← LOOKUP BY SessionID
c.pendingMu.RUnlock()
```

**Option B:** Change AAA Gateway to populate `AuthCtxID` in `publishResponseBytes`:
```go
// gateway.go — ForwardEAP, store AuthCtxID alongside the pending channel
g.pendingMu.Lock()
g.pending[req.SessionID] = pendingEntry{sessionID: req.SessionID, authCtxID: req.AuthCtxID, ch: ch}
g.pendingMu.Unlock()

// publishResponseBytes: use the stored authCtxID
event := proto.AaaResponseEvent{
    Version:   g.version,
    SessionID: sessionID,
    AuthCtxID: entry.authCtxID,  // ← Populate from stored entry
    Payload:   raw,
}
```

Option A is simpler and matches the existing `SessionID`-based routing in the gateway.

### RADIUS Forward Path

The RADIUS `Forward()` method is also a stub (lines 83-87 of `radius_handler.go`):

```83:87:internal/aaa/gateway/radius_handler.go
func (h *RadiusHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
	// TODO: Implement actual RADIUS forwarding to AAA-S server
	// For now, return a placeholder RAR-Nak response
	return []byte{2, 0, 0, 12}, nil
}
```

**RADIUS is stateless (UDP)**, so the implementation is simpler than Diameter:
1. Build an Access-Request packet from raw EAP (add Message-Authenticator, State, User-Name, NAS-IP-Address, etc.)
2. Send to AAA-S UDP address
3. Set a deadline on the read
4. Wait for Access-Accept/Reject/Challenge
5. Return raw response bytes

A RADIUS client library (or direct encoding) is needed. The existing `internal/radius/` package may be usable here if the architectural split allows it — but the gateway should not depend on `internal/radius/` if it can be avoided. The plan's constraint is that `internal/aaa/gateway/` should not import `internal/radius/` or `internal/diameter/`.

**Recommendation:** Create a minimal RADIUS encoder in `internal/aaa/gateway/radius_forward.go` or use the raw encoding approach (similar to how `sendDWA()` manually builds Diameter messages). Since RADIUS is simpler, a direct encoding is feasible.

### Wire into Gateway

#### Changes to `gateway.go`

```go
// Add diamForwarder field to Gateway struct
type Gateway struct {
    cfg Config

    redis         *redis.Client
    bizHTTPClient *http.Client
    version       string
    logger        *slog.Logger

    radiusHandler   *RadiusHandler
    diameterHandler *DiameterHandler
    diamForwarder   *diamForwarder  // ← NEW: persistent Diameter client

    pending   map[string]chan []byte
    pendingMu sync.RWMutex

    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

// In New():
func New(cfg Config) *Gateway {
    g := &Gateway{
        cfg:     cfg,
        version: cfg.Version,
        logger:  cfg.Logger,
        pending: make(map[string]chan []byte),
    }

    g.redis = newRedisClient(cfg.RedisAddr, cfg.RedisMode)
    g.bizHTTPClient = &http.Client{Timeout: 30 * time.Second}

    g.radiusHandler = &RadiusHandler{
        logger:          cfg.Logger,
        publishResponse: g.publishResponseBytes,
        forwardToBiz:    g.forwardToBiz,
    }

    // Create the Diameter forwarder
    g.diamForwarder = newDiamForwarder(
        cfg.DiameterServerAddress,
        cfg.DiameterProtocol,
        cfg.DiameterHost,
        cfg.DiameterRealm,
        cfg.Logger,
    )

    g.diameterHandler = &DiameterHandler{
        logger:          cfg.Logger,
        publishResponse: g.publishResponseBytes,
        forwardToBiz:   g.forwardToBiz,
        version:        cfg.Version,
        bizURL:        cfg.BizServiceURL,
        httpClient:     g.bizHTTPClient,
        diamForwarder:  g.diamForwarder,  // ← Pass forwarder to handler
    }

    return g
}

// In Start():
func (g *Gateway) Start(ctx context.Context) error {
    g.ctx, g.cancel = context.WithCancel(ctx)

    // Connect Diameter forwarder to AAA-S
    if g.diamForwarder != nil {
        g.wg.Add(1)
        go func() {
            defer g.wg.Done()
            if err := g.diamForwarder.Connect(g.ctx); err != nil {
                g.logger.Error("diameter_forward_connect_failed", "error", err)
                // Do not fail startup — AAA-S may be temporarily unavailable
            }
        }()
    }

    // ... existing listeners ...
}

// In ForwardEAP(): Replace diameterHandler.Forward() with diamForwarder.Forward()
switch req.TransportType {
case proto.TransportRADIUS:
    response, err = g.radiusHandler.Forward(ctx, req.Payload, req.SessionID)
case proto.TransportDIAMETER:
    response, err = g.diamForwarder.Forward(ctx, req.Payload, req.SessionID)  // ← Changed
default:
    return nil, fmt.Errorf("aaa-gateway: unknown transport type: %s", req.TransportType)
}
```

#### Changes to `diameter_handler.go`

Remove the `Forward()` method (it becomes unused after the forwarder is moved to `diameter_forward.go`), or make it delegate:

```go
// Forward delegates to the diamForwarder (if configured).
func (h *DiameterHandler) Forward(ctx context.Context, payload []byte, sessionID string) ([]byte, error) {
    if h.diamForwarder == nil {
        return nil, fmt.Errorf("diameter_forward: no forwarder configured")
    }
    return h.diamForwarder.Forward(ctx, payload, sessionID)
}
```

The `DiameterHandler` struct gains a `diamForwarder` field for the delegation pattern.

### Fix the Session Routing Bug

**In `gateway.go`**, store both `sessionID` and `authCtxID` together:

```go
type pendingEntry struct {
    sessionID string
    authCtxID string
    ch       chan []byte
}

// In ForwardEAP():
entry := &pendingEntry{
    sessionID: req.SessionID,
    authCtxID: req.AuthCtxID,
    ch:       ch,
}
g.pendingMu.Lock()
g.pending[req.SessionID] = entry
g.pendingMu.Unlock()

// In publishResponseBytes():
func (g *Gateway) publishResponseBytes(sessionID string, raw []byte) {
    g.pendingMu.RLock()
    p, ok := g.pending[sessionID]
    g.pendingMu.RUnlock()

    var authCtxID string
    if ok {
        authCtxID = p.authCtxID
    }

    event := proto.AaaResponseEvent{
        Version:   g.version,
        SessionID: sessionID,
        AuthCtxID: authCtxID,  // ← Populated from stored entry
        Payload:   raw,
    }
    if err := g.publishResponse(g.ctx, &event); err != nil {
        g.logger.Error("failed to publish response bytes", "error", err, "session_id", sessionID)
    }
}
```

**In `http_aaa_client.go`**, change the lookup key to `SessionID`:

```go
// subscribeResponses:
c.pendingMu.RLock()
pendingCh, ok := c.pending[event.SessionID]  // ← Changed from event.AuthCtxID
c.pendingMu.RUnlock()
```

This ensures the pending channel lookup matches how the gateway stores it.

## Files to Modify

| File | Change |
|------|--------|
| `internal/aaa/gateway/diameter_forward.go` | **NEW** — persistent Diameter forwarder |
| `internal/aaa/gateway/gateway.go` | Add `diamForwarder` field, wire in `New()`, connect in `Start()`, use in `ForwardEAP()` |
| `internal/aaa/gateway/diameter_handler.go` | Remove or delegate `Forward()`, add `diamForwarder` field |
| `internal/aaa/gateway/radius_handler.go` | Implement real `Forward()` (RADIUS UDP forwarding) |
| `cmd/biz/http_aaa_client.go` | Fix pending lookup to use `event.SessionID` |
| `internal/config/config.go` | `AAAgwConfig` already has `DiameterServerAddress`, `DiameterRealm`, `DiameterHost` — pass to `gateway.Config` in `cmd/aaa-gateway/main.go` |
| `cmd/aaa-gateway/main.go` | Pass `DiameterServerAddress`, `DiameterRealm`, `DiameterHost` to `gateway.Config` |
| `compose/configs/aaa-gateway.yaml` | Already has all required config fields (lines 23-26) |

## Plan Updates Needed

Update `PLAN_PHASE_REFACTOR_3COMPONENT.md`:

### Section 2.3.5 — Upgrade from "Deferred" to "Implementation Task"

The current plan labels §2.3.5 as a **BLOCKER (Deferred)** with placeholder code. It needs to become a concrete implementation task with specific file structure, based on the design above.

**Current text** (lines 716-820):
> **BLOCKER (Deferred):** This section was missing from the original plan. The `diameter_handler.Forward()` method is currently a stub returning `[]byte{}`, which silently breaks every DIAMETER-based NSSAA procedure.

**Replace with:** Full implementation task specification using the design in this analysis.

### Add Routing Bug Fix to Task 2.3 or New Task

The `SessionID` vs `AuthCtxID` routing bug is a data flow bug that will silently break all NSSAA procedures after the forwarder is implemented. It should be fixed alongside the forwarder implementation, either as part of Task 2.3 or as a new sub-task.

### Task 2.3.7 — RADIUS Forward Implementation (Currently Missing)

The RADIUS `Forward()` stub (line 83-87 of `radius_handler.go`) should also be implemented. Currently the plan only addresses the DIAMETER gap. The RADIUS gap is equally critical — the current stub returns a hardcoded `[]byte{2, 0, 0, 12}` which will corrupt every RADIUS-based NSSAA session.

## Summary

| Gap | Severity | Root Cause | Fix |
|-----|----------|------------|-----|
| `diameterHandler.Forward()` is a stub | **BLOCKER** | Planned but not implemented | Create `diameter_forward.go` with `go-diameter/v4` |
| `radiusHandler.Forward()` returns garbage | **BLOCKER** | Planned but not implemented | Implement real RADIUS UDP forwarding |
| SessionID vs AuthCtxID routing mismatch | **BLOCKER** | Design oversight | Store both in pending entry; populate `AuthCtxID` in response events |
| `diamForwarder` not wired into `Gateway` | **BLOCKER** | Planned but not implemented | Add field, connect in `Start()`, call from `ForwardEAP()` |
| Config fields exist but not wired | **WARNING** | `AAAgwConfig` has fields, `gateway.Config` passes them, but `diamForwarder` never receives them | Wire `DiameterServerAddress/Realm/Host` to forwarder |

The user is correct: the AAA Gateway currently handles the **server-initiated** path (listening for ASR/CoA) but has **no transport client** for the **client-initiated** path (sending DER to AAA-S). The `go-diameter/v4` library is available, the reference implementation exists, and the config infrastructure is in place — the only missing piece is the `diameter_forward.go` implementation and the wiring.
