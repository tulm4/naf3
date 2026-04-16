---
spec: RFC 2865 / RFC 3162 / RFC 3579 / RFC 4818 / RFC 5176 / TS 29.561 Ch.16
section: Ch.16
interface: N/A (NSSAAF ↔ AAA-S internal)
service: RADIUS Client
operation: N/A (internal)
implementation: Custom (no external library)
---

# NSSAAF RADIUS Client Design

## 0. Implementation Decision: Custom Implementation

**Không dùng `layeh/radius`.** Lý do:

- `layeh/radius` không hỗ trợ DTLS (RFC 4818) — transport security phải implement riêng bất kể
- UDP transport + HMAC-MD5 + 3GPP VSA custom = phần lớn code vẫn tự viết
- Dùng library base protocol cho RADIUS không tiết kiệm nhiều effort
- Custom implementation dễ debug production issues hơn (không có "black box")

## 1. Overview

RADIUS Client trong NSSAAF thực hiện giao tiếp với NSS-AAA Server sử dụng RADIUS protocol (RFC 2865, RFC 3579). NSSAAF encode EAP messages vào RADIUS Access-Request và decode RADIUS Access-Accept/Challenge/Reject responses.

RADIUS được chọn cho low-latency enterprise deployments.

---

## 2. RADIUS Protocol (RFC 2865 + RFC 3579)

### 2.1 RADIUS Packet Format

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Code      |       ID        |            Length             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Vector (16 bytes)                       |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                                                               |
|                         Attributes                             |
|                                                               |
+-+-+-+-+-+-+-+-+                                               |
```

- **Code:** 1=Access-Request, 2=Access-Accept, 3=Access-Reject, 4=Accounting-Request, 11=Access-Challenge, 12=Disconnect-Request, 40=Disconnect-ACK, 41=Disconnect-NAK
- **ID:** Incremented for each packet, used for matching Request/Response
- **Length:** Total packet length (8 + attributes)
- **Vector:** Authenticator (Request) or Response Authenticator
- **Attributes:** TLV format, variable length

### 2.2 RADIUS Attributes for NSSAA

| Attribute | RFC | Code | Type | Description |
|-----------|-----|------|------|-------------|
| User-Name | 2138 | 1 | String | GPSI or EAP identity |
| Calling-Station-Id | 2138 | 31 | String | GPSI |
| Called-Station-Id | 2138 | 30 | String | Network identifier |
| NAS-IP-Address | 2138 | 4 | Address | NSSAAF IP |
| NAS-Identifier | 2138 | 32 | String | NSSAAF identifier |
| NAS-Port-Type | 2138 | 61 | Integer | 19 (Virtual) |
| Service-Type | 2138 | 6 | Integer | 10 (Framed) |
| EAP-Message | 3579 | 79 | String | EAP payload (multiple allowed) |
| Message-Authenticator | 3579 | 80 | String | HMAC-MD5 integrity (16 bytes) |
| 3GPP-S-NSSAI | 3GPP | 200 | VSA | S-NSSAI for NSSAA |
| NAS-Feature-Radius | 3GPP | 26/35 | Integer | RADIUS capabilities |

### 2.3 Message Authenticator (RFC 3579)

```go
// Message-Authenticator = HMAC-MD5(packet + Message-Authenticator:16-bytes-zero)
func ComputeMessageAuthenticator(packet []byte, sharedSecret string) []byte {
    // Copy packet to avoid modifying original
    p := make([]byte, len(packet))
    copy(p, packet)

    // Zero out existing Message-Authenticator value (if present)
    for i := 0; i < len(p); {
        attrType := p[i]
        attrLen := int(p[i+1])
        if attrType == 80 && attrLen == 18 {
            // Zero out the value
            for j := 0; j < 16; j++ {
                p[i+2+j] = 0
            }
        }
        i += attrLen
    }

    // Pad with shared secret
    padded := append(p, []byte(sharedSecret)...)

    // HMAC-MD5
    mac := hmac.New(md5.New, []byte(sharedSecret))
    mac.Write(padded)
    return mac.Sum(nil)[:16]
}

func VerifyMessageAuthenticator(packet []byte, sharedSecret string) bool {
    // Extract Message-Authenticator from packet
    expected := ExtractMessageAuthenticator(packet)

    // Compute
    computed := ComputeMessageAuthenticator(packet, sharedSecret)

    // Constant-time comparison
    return hmac.Equal(expected, computed)
}
```

---

## 3. 3GPP-S-NSSAI Vendor-Specific Attribute

### 3.1 VSA Format

TS 29.561 §16.3.2 định nghĩa 3GPP Vendor ID = 10415.

```
VSA format (RFC 2865 §5.26):
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Type=26      |    Length     |  Vendor-Id (3 bytes)          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
| Vendor-Id cont| Vendor-Type=200|   Data...                    |
+-+-+-+-+-+-+-+-+                                         +-+-+
|                                                               |
```

- **Type:** 26 (Vendor-Specific)
- **Length:** 9 + data length
- **Vendor-Id:** 10415 (3GPP) — 3 bytes: 0x00, 0x28, 0x9F
- **Vendor-Type:** 200 (3GPP-S-NSSAI)
- **Data:** SST (1 byte) + SD (3 bytes, optional)

### 3.2 S-NSSAI Encoding

```go
type Snssai struct {
    Sst uint8   // Slice/Service Type (0-255)
    Sd  string  // Slice Differentiator (6 hex chars, optional)
}

func EncodeSnssaiVSA(snssai Snssai) []byte {
    var data []byte

    // SST: 1 byte
    data = append(data, snssai.Sst)

    // SD: 3 bytes (if present)
    if snssai.Sd != "" {
        sdBytes, _ := hex.DecodeString(snssai.Sd)
        if len(sdBytes) == 3 {
            data = append(data, sdBytes...)
        }
    }

    vsa := VSA{
        VendorId:    10415,
        VendorType:  200,
        Data:        data,
    }

    return vsa.Encode()
}

// Full VSA packet encoding
func (v *VSA) Encode() []byte {
    // Vendor-Id: 3 bytes (low-order first)
    vendorBytes := []byte{
        byte(v.VendorId),
        byte(v.VendorId >> 8),
        byte(v.VendorId >> 16),
    }

    // Total length = 1 (type) + 1 (len) + 3 (vendor) + 1 (vendor-type) + len(data)
    totalLen := 6 + len(v.Data)

    packet := make([]byte, totalLen)
    packet[0] = 26          // Type = Vendor-Specific
    packet[1] = byte(totalLen)
    packet[2] = vendorBytes[0]
    packet[3] = vendorBytes[1]
    packet[4] = vendorBytes[2]
    packet[5] = byte(v.VendorType)
    copy(packet[6:], v.Data)

    return packet
}
```

---

## 4. RADIUS Client Implementation

### 4.1 Client Architecture

```go
type RadiusClient struct {
    // Configuration
    config *RadiusConfig

    // Connection pool (per AAA server)
    connPool *ConnPool

    // Async I/O
    eventLoop *EventLoop

    // Circuit breaker
    cb *CircuitBreaker

    // Metrics
    metrics *RadiusMetrics
}

type RadiusConfig struct {
    ServerHost     string
    ServerPort     int               // Default: 1812
    SharedSecret   string            // Encrypted at rest
    Timeout        time.Duration     // Default: 10s
    MaxRetries     int               // Default: 3
    ResponseWindow time.Duration      // Default: 10s

    // Transport
    Transport      string            // "UDP" or "DTLS" (RFC 4818)
    LocalBindAddr  string            // Source address (per PLMN)
}

type ConnPool struct {
    mu      sync.Mutex
    conns   map[string]*RadiusConn  // server → connection
    maxConn int                     // Default: 10 per server
}
```

### 4.2 Access-Request Encoding

```go
func (c *RadiusClient) BuildAccessRequest(
    ctx *EapContext,
    eapPayload []byte,
    snssai Snssai,
    gpsi string,
) (*RadiusPacket, error) {

    // Code = 1 (Access-Request)
    code := 1

    // ID: pseudo-random, incremented per unique request
    id := c.nextPacketId()

    // Authenticator: 16 bytes random
    authenticator := make([]byte, 16)
    rand.Read(authenticator)

    // Build attributes
    attrs := []RadiusAttribute{
        // User-Name = GPSI
        {Type: 1, Value: []byte(gpsi)},

        // Calling-Station-Id = GPSI
        {Type: 31, Value: []byte(gpsi)},

        // NAS-IP-Address = NSSAAF local IP
        {Type: 4, Value: c.localIP.To4()},

        // Service-Type = Framed (10)
        {Type: 6, Value: []byte{0, 0, 0, 10}},

        // NAS-Port-Type = Virtual (19)
        {Type: 61, Value: []byte{0, 0, 0, 19}},

        // 3GPP-S-NSSAI (VSA #200)
        EncodeSnssaiAttribute(snssai),
    }

    // EAP-Message: may need fragmentation
    eapAttrs := c.FragmentEapMessage(eapPayload)
    for _, eapFrag := range eapAttrs {
        attrs = append(attrs, RadiusAttribute{
            Type:  79,
            Value: eapFrag,
        })
    }

    // Build packet without Message-Authenticator first
    packet := c.BuildPacket(code, id, authenticator, attrs)

    // Compute Message-Authenticator (RFC 3579 §3.2)
    // MA = HMAC-MD5(Code+ID+Length+RequestAuth+Attributes+SharedSecret)
    ma := c.ComputeMessageAuthenticator(packet, c.config.SharedSecret)
    attrs = append(attrs, RadiusAttribute{Type: 80, Value: ma})

    // Rebuild packet with Message-Authenticator
    packet = c.BuildPacket(code, id, authenticator, attrs)

    return packet, nil
}
```

### 4.3 Access-Challenge / Access-Accept Decoding

```go
func (c *RadiusClient) DecodeResponse(packet []byte) (*RadiusResponse, error) {
    if len(packet) < 20 {
        return nil, ErrInvalidPacket
    }

    code := packet[0]
    id := packet[1]
    length := binary.BigEndian.Uint16(packet[2:4])
    responseAuth := packet[4:20]

    // Validate response authenticator
    // ResponseAuth = MD5(Code+ID+Length+ResponseAuth+Attributes+SharedSecret)
    // For Access-Challenge: uses Request Authenticator
    expectedAuth := c.ComputeResponseAuth(packet, responseAuth)
    if !hmac.Equal(responseAuth, expectedAuth) {
        return nil, ErrInvalidAuthenticator
    }

    // Parse attributes
    attrs, err := ParseAttributes(packet[20:])
    if err != nil {
        return nil, err
    }

    resp := &RadiusResponse{
        Code: code,
        Id:   id,
    }

    // Extract EAP-Message(s)
    var eapPayload []byte
    for _, attr := range attrs {
        switch attr.Type {
        case 79: // EAP-Message
            eapPayload = append(eapPayload, attr.Value...)
        case 24: // State (for Challenge)
            resp.State = attr.Value
        case 80: // Message-Authenticator
            // Validate MA
            if !c.VerifyMessageAuthenticator(packet) {
                return nil, ErrInvalidMessageAuthenticator
            }
        case 18: // Reply-Message (human-readable)
            resp.ReplyMessage = string(attr.Value)
        }
    }

    resp.EapPayload = eapPayload

    // Map RADIUS code to result
    switch code {
    case 11: // Access-Challenge
        resp.Result = RESULT_CONTINUE
    case 2: // Access-Accept
        resp.Result = RESULT_SUCCESS
    case 3: // Access-Reject
        resp.Result = RESULT_FAILURE
    default:
        resp.Result = RESULT_UNKNOWN
    }

    return resp, nil
}
```

### 4.4 Async Send/Receive

```go
// Non-blocking send with callback
func (c *RadiusClient) SendAsync(
    ctx context.Context,
    packet *RadiusPacket,
    callback func(*RadiusResponse, error),
) error {

    // Encode to wire format
    wireData, err := packet.Encode()
    if err != nil {
        return err
    }

    // Get connection from pool
    conn, err := c.connPool.Get(ctx, c.config.ServerHost)
    if err != nil {
        return err
    }

    // Register pending request for response matching
    pending := &PendingRequest{
        Id:        packet.Id,
        Packet:    packet,
        SendAt:    time.Now(),
        Timeout:   c.config.ResponseWindow,
        Callback:  callback,
        Ctx:       ctx,
    }
    c.pending.Store(packet.Id, pending)

    // Set timeout
    timer := time.AfterFunc(c.config.ResponseWindow, func() {
        c.handleTimeout(packet.Id)
    })
    pending.Timer = timer

    // Async send (non-blocking UDP)
    return c.eventLoop.SendUDPMsg(conn.Fd, wireData, c.config.ServerAddr)
}

// Event loop handles incoming responses
func (c *RadiusClient) HandleIncomingResponse(data []byte, from net.Addr) {
    // Parse response
    resp, err := c.DecodeResponse(data)
    if err != nil {
        log.Errorf("RADIUS decode error: %v", err)
        return
    }

    // Match pending request
    pending, ok := c.pending.LoadAndDelete(resp.Id)
    if !ok {
        log.Warnf("Unexpected RADIUS response ID: %d", resp.Id)
        return
    }

    pending.Timer.Stop()

    // Check circuit breaker
    if resp.Result == RESULT_SUCCESS {
        c.cb.RecordSuccess(c.config.ServerHost)
    } else {
        c.cb.RecordFailure(c.config.ServerHost)
    }

    // Invoke callback
    pending.Callback(resp, nil)
}

func (c *RadiusClient) handleTimeout(packetId uint8) {
    pending, ok := c.pending.LoadAndDelete(packetId)
    if !ok {
        return
    }

    // Check retry count
    if pending.Retries < c.config.MaxRetries {
        pending.Retries++
        // Exponential backoff: 1s, 2s, 4s
        backoff := time.Duration(math.Pow(2, float64(pending.Retries-1))) * time.Second
        time.AfterFunc(backoff, func() {
            c.retryRequest(pending)
        })
        return
    }

    // Max retries exceeded
    pending.Callback(nil, ErrRadiusTimeout)
    c.cb.RecordFailure(c.config.ServerHost)
}
```

---

## 5. Circuit Breaker

```go
// Per-AAA-server circuit breaker
type CircuitBreaker struct {
    mu         sync.RWMutex
    states     map[string]*ServerState

    // Thresholds
    FailureThreshold int           // Default: 5 consecutive failures
    FailureWindow    time.Duration // Default: 10s
    HalfOpenMax     int           // Default: 3 requests in half-open
    RecoveryTimeout  time.Duration // Default: 30s
}

type ServerState struct {
    State        CircuitState  // CLOSED, OPEN, HALF_OPEN
    Failures     int
    Consecutive  int
    LastFailure  time.Time
    HalfOpenReqs int
}

func (cb *CircuitBreaker) RecordSuccess(server string) {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    state := cb.states[server]
    state.Consecutive = 0
    state.State = CLOSED
}

func (cb *CircuitBreaker) RecordFailure(server string) {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    state := cb.states[server]
    state.Consecutive++
    state.LastFailure = time.Now()

    if state.Consecutive >= cb.FailureThreshold {
        state.State = OPEN
        // Schedule recovery attempt
        go func() {
            time.Sleep(cb.RecoveryTimeout)
            cb.mu.Lock()
            state.State = HALF_OPEN
            state.HalfOpenReqs = 0
            cb.mu.Unlock()
        }()
    }
}

func (cb *CircuitBreaker) AllowRequest(server string) bool {
    cb.mu.RLock()
    defer cb.mu.RUnlock()

    state := cb.states[server]

    switch state.State {
    case CLOSED:
        return true
    case HALF_OPEN:
        if state.HalfOpenReqs < cb.HalfOpenMax {
            state.HalfOpenReqs++
            return true
        }
        return false
    case OPEN:
        return false
    }
    return false
}
```

---

## 6. RADIUS Disconnect (RFC 5176)

Used for AAA-S triggered authorization revocation:

```go
// Disconnect-Request: AAA-S → NSSAAF
func (c *RadiusClient) DecodeDisconnectRequest(packet []byte) (*DisconnectRequest, error) {
    // Validate Message-Authenticator
    if !c.VerifyMessageAuthenticator(packet) {
        return nil, ErrInvalidMessageAuthenticator
    }

    attrs, _ := ParseAttributes(packet[20:])

    req := &DisconnectRequest{}
    for _, attr := range attrs {
        switch attr.Type {
        case 1: // User-Name = GPSI
            req.UserName = string(attr.Value)
        case 31: // Calling-Station-Id = GPSI
            req.CallingStationId = string(attr.Value)
        case 200: // 3GPP-S-NSSAI VSA
            req.Snssai = DecodeSnssaiVSA(attr.Value)
        case 24: // State
            req.State = attr.Value
        case 45: // Acct-Session-Id
            req.AcctSessionId = string(attr.Value)
        }
    }

    return req, nil
}

// Disconnect-ACK: NSSAAF → AAA-S
func (c *RadiusClient) BuildDisconnectAck(req *DisconnectRequest) (*RadiusPacket, error) {
    // Response authenticator: MD5(Code+ID+Length+RequestAuth+Attributes+Secret)
    packet := c.BuildPacket(40, req.Id, req.RequestAuth, nil)
    return packet, nil
}
```

---

## 7. Performance Targets

| Metric | Target | Implementation |
|--------|--------|----------------|
| Access-Request rate | >50,000/sec per instance | Async UDP, connection pool |
| P99 latency (NSSAAF → AAA-S) | <5ms | io_uring UDP, zero-copy |
| Concurrent pending requests | >10,000 | In-memory map + TTL cleanup |
| Packet ID space | 256 (wrapping) | 8-bit counter |
| Duplicate detection | By Request Authenticator | MD5-based |

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | RFC 2865 compliant Access-Request/Accept/Reject/Challegne | Full packet encoding/decoding |
| AC2 | RFC 3579 EAP-Message fragmentation | Multiple attributes, reassembly |
| AC3 | Message-Authenticator HMAC-MD5 integrity | RFC 3579 §3.2 |
| AC4 | 3GPP-S-NSSAI VSA #200 encoding | SST + SD, 3GPP Vendor-Id 10415 |
| AC5 | Circuit breaker per AAA server | CLOSED/OPEN/HALF_OPEN states |
| AC6 | Exponential backoff retry | 1s, 2s, 4s, max 3 retries |
| AC7 | DTLS transport support (RFC 4818) | For untrusted networks |
| AC8 | Disconnect-Request handling (RFC 5176) | For revocation |
