---
spec: RFC 3748 / RFC 5216 / RFC 5281 / RFC 5448 / TS 33.501 §16.3, Annex B.2
section: §16.3, Annex B.2
interface: Internal (AMF ↔ NSSAAF ↔ AAA-S)
service: EAP Relay Engine
operation: N/A (internal)
---

# NSSAAF EAP Engine Design

## 1. Overview

NSSAAF thực hiện vai trò **EAP authenticator backend** — relay các thông điệp EAP giữa AMF (với tư cách EAP Authenticator) và AAA-S. EAP Engine xử lý tất cả EAP methods (EAP-TLS, EAP-TTLS, EAP-AKA'), quản lý session state, và serialize/deserialze EAP messages.

---

## 2. EAP Framework (RFC 3748)

### 2.1 EAP Packet Format

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Code      |       ID        |            Length             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Data                                |
+-+-+-+-+-+-+-+-+                                               |
|     ...                                                      |
```

- **Code:** 1 (Request), 2 (Response), 3 (Success), 4 (Failure), 5 (Initiate), 6 (Acknowledgement)
- **ID:** Sequence number for matching Request/Response
- **Length:** Total packet length (includes header)
- **Type:** Present in Request/Response packets (1 byte)

### 2.2 EAP Types Supported

| Type | Name | RFC | Notes |
|------|------|-----|-------|
| 13 | EAP-TLS | RFC 5216 | Primary for enterprise slices |
| 21 | EAP-TTLS | RFC 5281 | Tunneled TLS, legacy support |
| 23 | EAP-AKA' | RFC 5448 | 3GPP native, AKA' variant |
| 49 | EAP-SIM | RFC 4186 | SIM-based (optional) |
| 50 | EAP-AKA | RFC 4187 | AKA original (deprecated) |

### 2.3 NSSAAF Role

```
┌─────────────────────────────────────────────────────────────────┐
│                    EAP Authentication Flow                        │
│                                                                  │
│   UE ←───────────── EAP ───────────→ AMF (EAP Authenticator)   │
│                                               │                  │
│                                               │ N58 (SBI)        │
│                                               ▼                  │
│                                          NSSAAF                 │
│                                    (EAP Authenticator Backend)    │
│                                               │                  │
│                                               │ AAA Protocol      │
│                                               ▼                  │
│                                         AAA-S Server              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**AMF:** Thực hiện vai trò EAP Authenticator theo nghĩa RFC 3748. Gửi EAP Request và nhận EAP Response từ UE qua NAS messages.

**NSSAAF:** EAP authenticator backend — tiếp nhận EAP Response từ AMF, chuyển đổi sang AAA protocol (RADIUS/Diameter), gửi đến AAA-S, nhận response, chuyển đổi ngược và gửi về AMF.

### 2.4 AAA Client Interface (Phase R)

> **Note (Phase R):** After the 3-component refactor, `eap.AAAClient` is satisfied by `httpAAAClient` in `cmd/biz/http_aaa_client.go`, not by a direct RADIUS/Diameter socket client. The interface remains unchanged — only the implementation differs.

The `eap.AAAClient` interface defines how the EAP engine communicates with AAA-S:

```go
type AAAClient interface {
    SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error)
}
```

In the 3-component model, `httpAAAClient` forwards EAP payloads to the AAA Gateway via HTTP POST `/aaa/forward` (see `internal/proto/aaa_transport.go`). The AAA Gateway then sends raw RADIUS/Diameter over the wire. This indirection is why the Biz Pod has no direct external connectivity.

The Biz Pod's routing layer (`internal/biz/router.go`) determines the transport type (RADIUS or DIAMETER) based on S-NSSAI configuration, but does not send directly — it produces a `proto.AaaForwardRequest` that `httpAAAClient` forwards to the AAA Gateway.

---

## 3. EAP Session State Machine

### 3.1 State Definitions

```go
type EapSessionState struct {
    AuthCtxId       string

    // Current state
    State           EapState

    // Method
    Method          EapMethod    // EAP-TLS, EAP-TTLS, EAP-AKA_PRIME

    // Counters
    Rounds          int
    MaxRounds       int           // Default: 20

    // Sequence
    ExpectedId      uint8         // Next expected EAP ID

    // Method-specific state
    MethodState     interface{}   // EapTlsState, EapTtlsState, etc.

    // Timing
    CreatedAt       time.Time
    LastActivity    time.Time
    Timeout         time.Duration // Default: 30s per round
}

type EapState int
const (
    EAP_STATE_IDLE         EapState = iota
    EAP_STATE_INIT
    EAP_STATE_EAP_EXCHANGE // Multi-round EAP
    EAP_STATE_COMPLETING   // Final response from AAA-S
    EAP_STATE_DONE
    EAP_STATE_FAILED
    EAP_STATE_TIMEOUT
)

type EapMethod string
const (
    METHOD_EAP_TLS       EapMethod = "EAP-TLS"
    METHOD_EAP_TTLS      EapMethod = "EAP-TTLS"
    METHOD_EAP_AKA_PRIME EapMethod = "EAP-AKA'"
)
```

### 3.2 State Transition Diagram

```
┌─────────────┐
│    IDLE     │ ──── POST /slice-auth ────► INIT
└─────────────┘
                     │
                     ▼
            ┌─────────────────┐
            │   EAP_EXCHANGE   │◄────────────────────────┐
            │ (multi-round)    │                         │
            └────────┬────────┘                         │
                     │                                   │
        ┌────────────┼────────────┐                     │
        │            │            │                     │
        ▼            │            ▼                     │
┌─────────────┐     │     ┌─────────────────┐        │
│   SUCCESS    │     │     │   FAILURE       │        │
│ EAP-Success  │     │     │   EAP-Failure   │        │
│  received    │     │     │   received      │        │
└──────┬──────┘     │     └────────┬────────┘        │
       │            │              │                   │
       │            │              │                   │
       ▼            ▼              ▼                   │
  ┌─────────────────────────────────────┐             │
  │              DONE                    │             │
  └─────────────────────────────────────┘             │
                                                     │
       ┌──────────────────────────────────┘
       │
       ▼
┌─────────────────────────────────────┐
│            TIMEOUT                   │ ──► AMF retries same round
│     No response within 30s           │     (PUT with same EAP message)
└─────────────────────────────────────┘

Note: PUT /slice-auth/{id} with same EAP message + same nonce
      = retry, returns cached response
```

### 3.3 State Transition Logic

```go
func (e *EapEngine) AdvanceState(ctx *EapContext, incoming *EapMessage) (*EapMessage, EapResult, error) {
    switch e.State {
    case EAP_STATE_INIT:
        // First message from AMF (EAP Identity Response)
        e.State = EAP_STATE_EAP_EXCHANGE
        e.Rounds = 1
        e.ExpectedId = incoming.Id + 1

        // Determine EAP method from identity or AAA config
        method := e.detectEapMethod(incoming)
        e.Method = method

        // Forward to AAA-S
        return e.forwardToAaa(ctx, incoming)

    case EAP_STATE_EAP_EXCHANGE:
        // Validate ID matches expected
        if incoming.Id != e.ExpectedId {
            return nil, RESULT_IGNORED, ErrEapIdMismatch
        }

        // Check round limit
        if e.Rounds >= e.MaxRounds {
            return nil, RESULT_FAILED, ErrMaxRoundsExceeded
        }

        e.Rounds++
        e.ExpectedId = incoming.Id + 1

        // Forward to AAA-S
        response, err := e.forwardToAaa(ctx, incoming)
        if err != nil {
            return nil, RESULT_FAILED, err
        }

        // Check if final result
        if response.Code == EAP_CODE_SUCCESS {
            e.State = EAP_STATE_DONE
            return response, RESULT_SUCCESS, nil
        }
        if response.Code == EAP_CODE_FAILURE {
            e.State = EAP_STATE_DONE
            return response, RESULT_FAILED, nil
        }

        // Continue exchange
        e.State = EAP_STATE_EAP_EXCHANGE
        return response, RESULT_CONTINUE, nil

    case EAP_STATE_DONE:
        return nil, RESULT_IGNORED, ErrSessionAlreadyCompleted
    }
}

// Idempotent retry handling
func (e *EapEngine) HandleRetry(ctx *EapContext, incoming *EapMessage) (*EapMessage, EapResult, error) {
    // Check if same message hash was already processed
    msgHash := sha256(incoming.Payload)

    if ctx.LastNonce == msgHash {
        // Duplicate: return cached response
        return ctx.CachedResponse, ctx.CachedResult, nil
    }

    // Different message: process normally
    return e.AdvanceState(ctx, incoming)
}
```

---

## 4. EAP-TLS Implementation (RFC 5216)

### 4.1 EAP-TLS Overview

EAP-TLS sử dụng TLS handshake làm EAP method. Cả client (AAA-S) và server (AAA-S, vai trò TLS server) có certificate.

```
TLS Handshake (within EAP):
 UE ←─────── TLS ClientHello ──────────► AAA-S
 UE ◄─────── TLS ServerHello, Cert ──────► AAA-S
 UE ←─────── TLS ClientKeyExchange ─────► AAA-S
 UE ◄─────── TLS Finished ──────────────► AAA-S
    │
    ├─ MSK derived from TLS master secret
    └─ EAP-Success/Failure
```

### 4.2 EAP-TLS State

```go
type EapTlsState struct {
    // TLS version
    Version uint16  // 0x0303 (TLS 1.2), 0x0304 (TLS 1.3)

    // Handshake state
    HandshakeComplete bool

    // Client state
    ClientRandom []byte  // 32 bytes
    ClientSessionId []byte

    // Server state
    ServerRandom []byte  // 32 bytes
    ServerCertificate []byte
    ServerCertificateVerified bool

    // Key material
    PreMasterSecret []byte  // Encrypted at rest
    MasterSecret    []byte  // Encrypted at rest

    // Flags
    CertificateRequested bool

    // MSK (RFC 5216 §2.1.4)
    // MSK = first 64 bytes of TLS exporter output
    Msk []byte  // Encrypted at rest

    // AKA' specific (if using EAP-AKA')
    ResyncCounter uint8
    Autn         []byte
    Ik           []byte
    Ck           []byte
}
```

### 4.3 MSK Derivation (RFC 5216)

```go
// RFC 5216 §2.1.4: MSK Derivation from TLS
//
// The Master Session Key (MSK) is derived from the TLS master secret
// using the TLS exporter interface.
//
// MSK = TLS-Exporter("EAP-TLS MSK", 64)
//
// Where TLS-Exporter is defined in RFC 5705.
//
// For TLS 1.3: use TLS 1.3 exporter with label "EAP-TLS MSK"
func deriveMSK(tls *tls.ConnectionState, label string) ([]byte, error) {
    // TLS 1.3
    if tls.Version == tls.VersionTLS13 {
        // TLS 1.3 uses early_data with exporter
        masterSecret := tls.HandshakeSecrets.MasterSecret
        return tls13Exporter(masterSecret, label, 64)
    }

    // TLS 1.2
    return tls12Exporter(tls, label, 64)
}

// MSK structure (RFC 5216):
// MSK[0..31]  = MSK Part 1 (EMSK in TLS 1.2)
// MSK[32..63] = MSK Part 2
//
// EMSK (Extended MSK, RFC 5295):
// EMSK[0..31] = EMSK Part 1
// EMSK[32..63] = EMSK Part 2
```

### 4.4 EAP-TLS Flags

EAP-TLS sử dụng flags trong Type-Data field:

```go
const (
    EAP_TLS_FLAGS_START       = 0x80  // Initiate TLS handshake
    EAP_TLS_FLAGS_MORE_FRAGS  = 0x40  // More fragments follow
    EAP_TLS_FLAGS_LENGTH       = 0x20  // Length field present
    EAP_TLS_FLAGS_RESERVED    = 0x1F  // Reserved bits
)

type EapTlsPacket struct {
    Code       uint8
    Id         uint8
    Length     uint16  // Network byte order
    Type       uint8   // Always 13 (EAP-TLS)
    Flags      uint8
    TLSData    []byte  // TLS record (may be fragmented)
}
```

---

## 5. Multi-Round Handling

### 5.1 Fragmentation

EAP messages có thể lớn hơn MTU. NSSAAF hỗ trợ fragment/reassemble:

```go
const (
    MAX_EAP_FRAGMENT_SIZE = 4096  // bytes
)

type FragmentBuffer struct {
    AuthCtxId    string
    FragmentId   uint8
    TotalLength  uint32
    Received     uint32
    Fragments    map[uint16][]byte  // fragment number → data
    Complete     bool
}

func (f *FragmentBuffer) AddFragment(seq uint16, data []byte, moreFrags bool) error {
    f.Fragments[seq] = data
    f.Received += uint32(len(data))

    if !moreFrags {
        f.Complete = true
        return nil
    }

    if f.TotalLength > 0 && f.Received >= f.TotalLength {
        f.Complete = true
    }

    return nil
}

func (f *FragmentBuffer) Reassemble() ([]byte, error) {
    if !f.Complete {
        return nil, ErrFragmentIncomplete
    }

    result := make([]byte, 0, f.TotalLength)
    for seq := 0; ; seq++ {
        frag, ok := f.Fragments[uint16(seq)]
        if !ok {
            break
        }
        result = append(result, frag...)
    }
    return result, nil
}
```

### 5.2 Concurrent Session Management

```go
// Thread-safe session manager
type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]*EapSessionState
    cache    *redis.Client
    ttl      time.Duration
}

func (m *SessionManager) Get(ctxId string) (*EapSessionState, error) {
    m.mu.RLock()
    state, ok := m.sessions[ctxId]
    m.mu.RUnlock()

    if ok {
        return state, nil
    }

    // Load from Redis
    cached, err := m.cache.Get(ctx, "nssaa:session:"+ctxId)
    if err != nil {
        return nil, ErrSessionNotFound
    }

    state = deserialize(cached)
    if state == nil {
        return nil, ErrSessionNotFound
    }

    // Populate in-memory cache
    m.mu.Lock()
    m.sessions[ctxId] = state
    m.mu.Unlock()

    return state, nil
}

func (m *SessionManager) Update(state *EapSessionState) error {
    // Update in-memory
    m.mu.Lock()
    m.sessions[state.AuthCtxId] = state
    m.mu.Unlock()

    // Async write to Redis
    go func() {
        m.cache.Set(ctx, "nssaa:session:"+state.AuthCtxId, serialize(state), m.ttl)
    }()

    // Async write to PostgreSQL (primary)
    go func() {
        pg.UpdateSession(ctx, state)
    }()

    return nil
}
```

---

## 6. Performance Optimization

### 6.1 Zero-Copy EAP Forwarding

```go
// Zero-copy: reuse byte slices, avoid unnecessary copies
func (e *EapEngine) ForwardEapMessage(ctx *EapContext, msg *EapMessage) error {
    // Parse and validate EAP packet (no copy)
    packet, err := eap.Parse(msg.Payload)  // Zero-copy parsing
    if err != nil {
        return err
    }

    // Encode to AAA protocol (new allocation only for wire format)
    aaaPacket := e.aaaEncoder.Encode(packet)

    // Send to AAA-S (zero-copy send)
    return e.aaaClient.Send(ctx.AuthCtxId, aaaPacket)
}

// TLS session resumption for performance
type TlsSessionCache struct {
    mu      sync.RWMutex
    sessions map[string]*tls.SessionState  // session ID → session state
    maxSize int
    lru     *lru.Cache
}

func (c *TlsSessionCache) Get(sessionId string) (*tls.SessionState, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.sessions[sessionId]
}
```

### 6.2 Async I/O Model

```go
// Async EAP processing with goroutines
func (e *EapEngine) ProcessAsync(ctx *EapContext, incoming *EapMessage) (<-chan EapResult, error) {
    resultCh := make(chan EapResult, 1)

    go func() {
        defer close(resultCh)

        // Get current state
        state, err := e.sessionManager.Get(ctx.AuthCtxId)
        if err != nil {
            resultCh <- EapResult{Err: err}
            return
        }

        // Advance state machine
        response, result, err := e.advanceState(state, incoming)
        if err != nil {
            resultCh <- EapResult{Err: err}
            return
        }

        // Update state (async)
        go e.sessionManager.Update(state)

        resultCh <- EapResult{
            Response: response,
            Result:   result,
        }
    }()

    return resultCh, nil
}
```

---

## 7. Acceptance Criteria

> **Note (Phase R):** In the 3-component model, the EAP engine runs in the Biz Pod. RADIUS encode/decode is in `internal/radius/`; Diameter encode/decode is in `internal/diameter/`. Raw socket I/O (RADIUS UDP, Diameter TCP/SCTP) runs in the AAA Gateway (`internal/aaa/gateway/`). The EAP engine communicates with AAA-S indirectly via `httpAAAClient` → AAA Gateway → AAA-S.

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | EAP-TLS RFC 5216 compliant | MSK derivation, flags handling |
| AC2 | Multi-round support (up to 20 rounds) | EAP_STATE_EAP_EXCHANGE loop |
| AC3 | EAP fragmentation/reassembly | MAX_EAP_FRAGMENT_SIZE = 4096 |
| AC4 | MSK derivation from TLS master secret | TLS-Exporter("EAP-TLS MSK", 64) |
| AC5 | Session timeout: 30s per round | Timeout field in EapSessionState |
| AC6 | Idempotent retry: same msg hash → cached response | nonce tracking |
| AC7 | EAP-TTLS tunneled method support | EapTtlsState struct |
| AC8 | Zero-copy EAP forwarding | Parse without allocation |
