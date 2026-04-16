---
spec: RFC 6733 / RFC 4072 / RFC 7155 / TS 29.561 Ch.17
section: Ch.17
interface: N/A (NSSAAF ↔ AAA-S internal)
service: Diameter Client
operation: N/A (internal)
implementation: github.com/fiorix/go-diameter/v4 (base stack) + custom 3GPP AVPs
---

# NSSAAF Diameter Client Design

## 0. Library Decision: `fiorix/go-diameter/v4`

**Dùng `github.com/fiorix/go-diameter/v4` làm base stack.** Lý do:

- Diameter RFC 6733 base protocol phức tạp: CER/CEA handshake, DWR/DWA state machine, hop-by-hop/end-to-end IDs
- go-diameter cung cấp SCTP + TLS transport, CER/CEA capabilities exchange, dictionary-based AVP encoding
- Tiết kiệm ~40% effort cho base protocol
- 3GPP-specific AVPs (S-NSSAI, EAP-Payload) vẫn phải custom thêm

**Điều không có sẵn trong go-diameter:**
- 3GPP-S-NSSAI AVP (code 310) — tự viết
- EAP-Payload AVP (code 209) — tự viết
- Transport security (DTLS/IPSec) — tự viết

## 1. Overview

Diameter Client trong NSSAAF thực hiện giao tiếp với NSS-AAA Server sử dụng Diameter protocol, được chọn cho carrier-grade deployments với độ tin cậy cao hơn RADIUS.

**RFC standards:**
- RFC 6733: Diameter Base Protocol
- RFC 4072: Diameter EAP Application
- RFC 7155: Diameter NASREQ Application

**3GPP-specific:**
- Vendor-Id: 10415
- NASREQ Application-Id: 1
- Diameter EAP Application-Id: 5

---

## 2. Diameter Protocol Basics

### 2.1 Diameter Message Format

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         Version                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                   Message Length                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    0    |               Command Flags                        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                      Command-Code                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Application-Id                        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-++
|                      Hop-by-Hop Identifier                    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                      End-to-End Identifier                    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                    AVP's (Variable)                          |
+-------------------------------------------------------------+
```

### 2.2 Command Codes for NSSAA

| Command | Code | Application | Direction |
|---------|------|------------|-----------|
| DER | 268 | Diameter EAP (5) | NSSAAF → AAA-S |
| DEA | 268 | Diameter EAP (5) | AAA-S → NSSAAF |
| STR | 275 | Diameter Base (0) | NSSAAF → AAA-S |
| STA | 275 | Diameter Base (0) | AAA-S → NSSAAF |
| CER | 257 | Diameter Base (0) | Both |
| CEA | 257 | Diameter Base (0) | Both |
| DWR | 280 | Diameter Base (0) | Both |
| DWA | 280 | Diameter Base (0) | Both |

---

## 3. AVP Specification for NSSAA

### 3.1 Key AVPs

| AVP | Code | Type | M/O | Description |
|-----|------|------|-----|-------------|
| User-Name | 1 | OctetString | M | GPSI |
| Calling-Station-Id | 31 | OctetString | M | GPSI |
| EAP-Payload | 209 | OctetString | M | EAP message |
| EAP-Replied-NAI | 209 | UTF8String | O | EAP identity |
| 3GPP-S-NSSAI | 310 | Grouped | M | Slice identifier |
| Auth-Application-Id | 258 | Unsigned32 | M | 5 (Diameter EAP) |
| Auth-Request-Type | 274 | Enumerated | M | AUTHORIZE_AUTHENTICATE (1) |
| Auth-Session-State | 277 | Enumerated | M | 1 (NO_STATE_MAINTAINED) |
| Result-Code | 268 | Unsigned32 | M | RFC 6733 |
| Experimental-Result | 297 | Grouped | O | 3GPP-specific errors |
| Session-Id | 263 | UTF8String | M | Unique session identifier |

### 3.2 3GPP-S-NSSAI AVP

```go
// 3GPP-S-NSSAI AVP (AVP Code: 310, Vendor: 10415)
//
// 3GPP-S-NSSAI ::= <AVP Header: 310, Vendor: 10415>
//                  { Slice/Service Type }
//                  [ Slice Differentiator ]
//                  [ Mapped HPLMN SNSSAI ]

type SnssaiAVP struct {
    SliceServiceType uint8     // SST (0-255)
    SliceDifferentiator string // SD (6 hex chars, optional)
    MappedHplmnSnssai *Snssai  // Optional
}

func EncodeSnssaiAVP(snssai Snssai) []byte {
    grouped := GroupedAVP{}

    // SST
    grouped.Add(&AVP{
        Code:       259,  // Slice/Service Type
        VendorId:   10415,
        Data:       []byte{snssai.Sst},
        DataLength: 1,
        Flags:      AVP_FLAG_VENDOR,
    })

    // SD (if present)
    if snssai.Sd != "" {
        sdBytes, _ := hex.DecodeString(snssai.Sd)
        grouped.Add(&AVP{
            Code:       260,  // Slice Differentiator
            VendorId:   10415,
            Data:       sdBytes,
            DataLength: 3,
            Flags:      AVP_FLAG_VENDOR,
        })
    }

    return grouped.Encode()
}
```

### 3.3 EAP-Payload AVP

```go
// EAP-Payload AVP (AVP Code: 209)
// Contains the EAP message, similar to RADIUS EAP-Message

func EncodeEapPayload(eapPayload []byte) *AVP {
    return &AVP{
        Code:       209,
        VendorId:   0,  // Standard AVP, no vendor
        Data:       eapPayload,
        DataLength: uint32(len(eapPayload)),
        Flags:      0,
    }
}
```

---

## 4. DER/DEA Message Flow

### 4.1 DER (Diameter-EAP-Request)

```go
func (c *DiameterClient) BuildDER(ctx *EapContext, eapPayload []byte) (*DiameterMessage, error) {
    // Session-Id: unique per authentication attempt
    sessionId := buildSessionId(c.config.OriginHost, c.config.Realm, ctx.AuthCtxId)

    avps := []AVP{
        // Session-Id (required first)
        MakeAVP(263, 0, sessionId, FLAG_MANDATORY),

        // Auth-Application-Id = 5 (Diameter EAP)
        MakeAVP(258, 0, uint32(5), FLAG_MANDATORY),

        // Auth-Request-Type = AUTHORIZE_AUTHENTICATE (1)
        MakeAVP(274, 0, uint32(1), FLAG_MANDATORY),

        // Auth-Session-State = NO_STATE_MAINTAINED (1)
        // NSSAAF does not maintain state between requests
        MakeAVP(277, 0, uint32(1), FLAG_MANDATORY),

        // Origin-Host, Origin-Realm (routing)
        MakeAVP(264, 0, c.config.OriginHost, FLAG_MANDATORY),
        MakeAVP(296, 0, c.config.OriginRealm, FLAG_MANDATORY),

        // Destination-Host, Destination-Realm
        MakeAVP(293, 0, c.config.DestHost, FLAG_MANDATORY),
        MakeAVP(283, 0, c.config.DestRealm, FLAG_MANDATORY),

        // User-Name = GPSI
        MakeAVP(1, 0, []byte(ctx.Gpsi), FLAG_MANDATORY),

        // Calling-Station-Id = GPSI
        MakeAVP(31, 0, []byte(ctx.Gpsi), FLAG_MANDATORY),

        // EAP-Payload
        MakeAVP(209, 0, eapPayload, FLAG_MANDATORY),

        // 3GPP-S-NSSAI
        EncodeSnssaiAVP(ctx.Snssai),
    }

    // Hop-by-Hop ID: random 32-bit
    hopByHop := rand.Uint32()

    // End-to-End ID: random 32-bit
    endToEnd := rand.Uint32()

    msg := &DiameterMessage{
        Version:       1,
        CommandCode:    268, // DER
        ApplicationId:  5,  // Diameter EAP
        HopByHopId:    hopByHop,
        EndToEndId:    endToEnd,
        Avps:          avps,
    }

    // Set flags
    msg.Flags.Request = true

    return msg, nil
}
```

### 4.2 CER/CEA Capabilities Exchange

```go
// On connection establishment, exchange capabilities
func (c *DiameterClient) BuildCER() (*DiameterMessage, error) {
    avps := []AVP{
        MakeAVP(264, 0, c.config.OriginHost, FLAG_MANDATORY),
        MakeAVP(296, 0, c.config.OriginRealm, FLAG_MANDATORY),
        MakeAVP(266, 0, c.config.ProductName, 0), // Product-Name

        // Host-IP-Address
        MakeAVP(257, 0, c.localIP.To4(), FLAG_MANDATORY),

        // Vendor-Id = 10415 (3GPP)
        MakeAVP(266, 10415, nil, FLAG_VENDOR), // Vendor-Id

        // Supported-Vendor-Id = 10415
        MakeAVP(265, 0, uint32(10415), FLAG_MANDATORY),

        // Auth-Application-Id: NASREQ (1) + EAP (5)
        MakeAVP(258, 0, uint32(1), FLAG_MANDATORY),
        MakeAVP(258, 0, uint32(5), FLAG_MANDATORY),

        // Inband-Security-Id (for IPSec)
        MakeAVP(299, 0, uint32(1), FLAG_MANDATORY), // IPSec
    }

    return &DiameterMessage{
        Version:      1,
        CommandCode:   257, // CER
        ApplicationId: 0,   // Diameter Base
        Avps:         avps,
    }, nil
}
```

### 4.3 SCTP Transport

```go
// SCTP preferred for ordered, reliable delivery
// TCP fallback

type DiameterTransport struct {
    // Primary: SCTP multi-streaming
    sctpConn *sctp.SCTPConn

    // Fallback: TCP
    tcpConn *net.TCPConn

    // Connection state
    state     TransportState
    reconnect chan struct{}
}

type TransportState int
const (
    TRANSPORT_INIT TransportState = iota
    TRANSPORT_CONNECTED
    TRANSPORT_RECONNECTING
    TRANSPORT_FAILED
)

func (t *DiameterTransport) Connect(addr string) error {
    // Try SCTP first
    sctpAddr, err := sctp.ResolveSCTPAddr("sctp4", addr)
    if err == nil {
        conn, err := sctp.Dial(
            "sctp",
            "",         // local addr
            sctpAddr,   // remote addr
            &sctp.Config{
                InitMsg: sctp.InitMsg{
                    NumOutstreams: 5,  // outgoing streams
                    MaxInstreams:  10,  // incoming streams
                },
            },
        )
        if err == nil {
            t.sctpConn = conn
            t.state = TRANSPORT_CONNECTED
            return nil
        }
    }

    // Fallback to TCP
    tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
    if err != nil {
        return err
    }

    conn, err := net.DialTCP("tcp", nil, tcpAddr)
    if err != nil {
        return err
    }

    t.tcpConn = conn
    t.state = TRANSPORT_CONNECTED
    return nil
}

// SCTP advantages over TCP for Diameter:
// - Multi-streaming: ordered delivery within stream
// - Multi-homing: redundant paths
// - No head-of-line blocking
```

---

## 5. Session Management

### 5.1 Session-Id Format

```go
// Session-Id format (RFC 6733 §3):
// <DiameterIdentity>;<high-int>;<low-int>[@<low-int>];<very-low-int>[...]
//
// Example: "nssAAF.operator.com;12345;67890@3232235780;1"

func buildSessionId(originHost, realm, authCtxId string) string {
    timestamp := time.Now().UnixNano()
    high := timestamp >> 32
    low := timestamp & 0xFFFFFFFF
    return fmt.Sprintf("%s;%d;%d@%s;1",
        originHost, high, low, authCtxId)
}
```

### 5.2 Stateful vs Stateless

NSSAAF sử dụng **FULL** state model (Auth-Session-State = 0) để maintain session với AAA-S:

```go
// Session state maintained by NSSAAF
type DiameterSession struct {
    SessionId      string
    AuthCtxId      string
    ServerHost     string
    State          SessionState

    // AAA-S response tracking
    ServerCapabilities *Capabilities
    LastResultCode    uint32
    LastHopByHop     uint32

    // For re-auth
    OriginalSessionId string
    OriginalAuthAppId uint32

    CreatedAt  time.Time
    ExpiresAt time.Time
}

const (
    SESSION_STATE_OPEN    = iota
    SESSION_STATE_PENDING  // Waiting for DEA
    SESSION_STATE_AUTHORIZED
    SESSION_STATE_DISCONNECTED
)
```

---

## 6. Connection Management

### 6.1 Peer Management

```go
type PeerManager struct {
    mu    sync.RWMutex
    peers map[string]*Peer

    config *PeerConfig
}

type Peer struct {
    Host             string
    Port             int
    Realm            string

    // Connection
    transport   *DiameterTransport
    state       PeerState
    reconnectAt time.Time

    // Capabilities
    capabilities *Capabilities

    // Statistics
    stats PeerStats
}

type PeerState int
const (
    PEER_STATE_DOWN     PeerState = iota
    PEER_STATE_CONNECTING
    PEER_STATE_OPEN
    PEER_STATE_WATCHDOG  // DWR/DWA pending
)

// Watchdog: CCR/CCA or DWR/DWA
func (p *Peer) StartWatchdog(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Send Device-Watchdog-Request
            if err := p.sendDWR(); err != nil {
                p.handleWatchdogTimeout()
            }
        }
    }
}

func (p *Peer) sendDWR() error {
    dwr := &DiameterMessage{
        Version:      1,
        CommandCode:  280,
        ApplicationId: 0,
        Flags:        FLAG_REQUEST,
        HopByHopId:  rand.Uint32(),
        EndToEndId:  rand.Uint32(),
        Avps: []AVP{
            MakeAVP(264, 0, p.myHost, FLAG_MANDATORY),
            MakeAVP(296, 0, p.myRealm, FLAG_MANDATORY),
        },
    }

    return p.transport.Send(dwr)
}
```

### 6.2 Failover

```go
// If primary peer fails, try failover to secondary
func (c *DiameterClient) sendWithFailover(msg *DiameterMessage) (*DiameterMessage, error) {
    peers := c.peerManager.GetPeersByPriority()

    var lastErr error
    for _, peer := range peers {
        if peer.State != PEER_STATE_OPEN {
            continue
        }

        resp, err := peer.Send(msg)
        if err == nil {
            return resp, nil
        }

        lastErr = err
        peer.MarkFailed()
        log.Warnf("Diameter peer %s failed: %v, trying next", peer.Host, err)
    }

    return nil, fmt.Errorf("all diameter peers failed: %w", lastErr)
}
```

---

## 7. Performance Targets

| Metric | Target | Implementation |
|--------|--------|----------------|
| DER/DEA rate | >20,000/sec per instance | SCTP async I/O |
| P99 latency | <10ms | Connection pool, watchdog |
| SCTP streams | 5 outbound, 10 inbound | Per connection |
| Connection pool | 10 per peer | Reuse SCTP associations |
| Watchdog interval | 30s | DWR/DWA keepalive |

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | RFC 6733 Diameter Base compliant | Full CER/CEA, DWR/DWA |
| AC2 | RFC 4072 Diameter EAP Application | DER/DEA with AVP 209 |
| AC3 | 3GPP Vendor-Id 10415 in capabilities | CER includes Vendor-Id |
| AC4 | Auth-Application-Id: 1 (NASREQ) + 5 (EAP) | CER advertised |
| AC5 | 3GPP-S-NSSAI AVP encoding | Grouped AVP 310 |
| AC6 | SCTP transport với multi-streaming | sctp.Dial with InitMsg |
| AC7 | TCP fallback khi SCTP fails | Fallback logic |
| AC8 | Peer watchdog (30s interval) | DWR/DWA keepalive |
