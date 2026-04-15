# Phase 2: Protocol — EAP & AAA Clients

## Overview

Phase 2 xây dựng protocol handling layer: EAP engine và AAA protocol clients (RADIUS, Diameter).

## Modules to Implement

### 1. `internal/eap/` — EAP Engine

**Priority:** P0
**Dependencies:** `internal/types/`
**Design Doc:** `docs/design/06_eap_engine.md`

**Deliverables:**
- [ ] `engine.go` — EAP session state machine
- [ ] `session.go` — EAP session state struct
- [ ] `state.go` — State constants and transitions
- [ ] `tls.go` — EAP-TLS handling, MSK derivation (RFC 5216)
- [ ] `fragment.go` — EAP fragmentation/reassembly
- [ ] `codec.go` — EAP packet encoding/decoding (RFC 3748)
- [ ] `engine_test.go` — Unit tests

**EAP Types Supported:**
```go
const (
    EAP_TYPE_IDENTITY    = 1
    EAP_TYPE_NOTIFICATION = 2
    EAP_TYPE_NAK        = 3
    EAP_TYPE_TLS        = 13  // RFC 5216
    EAP_TYPE_TTLS       = 21  // RFC 5281
    EAP_TYPE_AKA_PRIME  = 23  // RFC 5448
)

type EapState int
const (
    EAP_STATE_IDLE         EapState = iota
    EAP_STATE_INIT
    EAP_STATE_EAP_EXCHANGE
    EAP_STATE_COMPLETING
    EAP_STATE_DONE
    EAP_STATE_FAILED
    EAP_STATE_TIMEOUT
)
```

**Key MSK Derivation (RFC 5216):**
```go
// MSK = TLS-Exporter("EAP-TLS MSK", 64)
// EMSK = extended MSK (first 32 bytes)
// MSK = last 32 bytes of exporter output
func deriveMSK(tls *tls.ConnectionState) ([]byte, error)
```

### 2. `internal/radius/` — RADIUS Client

**Priority:** P0
**Dependencies:** `internal/eap/`
**Design Doc:** `docs/design/07_radius_client.md`

**Deliverables:**
- [ ] `client.go` — RADIUS client interface
- [ ] `packet.go` — RADIUS packet encoding/decoding
- [ ] `attribute.go` — RADIUS attribute handling
- [ ] `vsa.go` — 3GPP-S-NSSAI VSA encoding (code 200)
- [ ] `message_auth.go` — HMAC-MD5 Message-Authenticator (RFC 3579)
- [ ] `client_udp.go` — UDP transport
- [ ] `client_test.go` — Unit tests

**RADIUS Packet Codes:**
```go
const (
    RADIUS_CODE_ACCESS_REQUEST    = 1
    RADIUS_CODE_ACCESS_ACCEPT     = 2
    RADIUS_CODE_ACCESS_REJECT     = 3
    RADIUS_CODE_ACCESS_CHALLENGE   = 11
    RADIUS_CODE_DISCONNECT_REQUEST = 40
    RADIUS_CODE_DISCONNECT_ACK     = 41
    RADIUS_CODE_DISCONNECT_NAK    = 42
)
```

**3GPP-S-NSSAI VSA Format:**
```go
// Type: 26 (Vendor-Specific)
// Vendor-Id: 10415 (3GPP)
// Vendor-Type: 200
// Data: SST (1 byte) + SD (3 bytes, optional)

func EncodeSnssaiVSA(snssai Snssai) []byte
func DecodeSnssaiVSA(data []byte) (Snssai, error)
```

### 3. `internal/diameter/` — Diameter Client

**Priority:** P0
**Dependencies:** `internal/eap/`
**Design Doc:** `docs/design/08_diameter_client.md`

**Deliverables:**
- [ ] `client.go` — Diameter client interface
- [ ] `message.go` — Diameter message encoding/decoding
- [ ] `avp.go` — AVP handling
- [ ] `snssai_avp.go` — 3GPP-S-NSSAI AVP (code 310)
- [ ] `transport.go` — SCTP/TCP transport
- [ ] `cer.go` — CER/CEA capabilities exchange
- [ ] `client_test.go` — Unit tests

**Key Command Codes:**
```go
const (
    DIAMETER_CMD_CER  = 257  // Capabilities-Exchange-Request/Answer
    DIAMETER_CMD_DWR  = 280  // Device-Watchdog-Request/Answer
    DIAMETER_CMD_DER  = 268  // DEtach-Request (Diameter-EAP-Request)
    DIAMETER_CMD_DEA  = 268  // Diameter-EAP-Answer
    DIAMETER_CMD_STR  = 275  // Session-Termination-Request
    DIAMETER_CMD_STA  = 275  // Session-Termination-Answer
)
```

### 4. `internal/aaa/` — AAA Proxy & Router

**Priority:** P1
**Dependencies:** `internal/radius/`, `internal/diameter/`
**Design Doc:** `docs/design/09_aaa_proxy.md`

**Deliverables:**
- [ ] `router.go` — Route decision (Direct vs Proxy mode)
- [ ] `config.go` — AAA server configuration
- [ ] `metrics.go` — AAA client metrics

**Routing Logic:**
```go
// 3-level AAA config lookup:
// 1. Exact: (snssai.sst, snssai.sd)
// 2. SST-only: (snssai.sst, sd=*)
// 3. Default: (sst=*, sd=*)

type RouteDecision struct {
    Mode       RoutingMode  // DIRECT or PROXY
    Protocol   string       // RADIUS or DIAMETER
    TargetHost string
    TargetPort int
    Timeout   time.Duration
}
```

## Validation Checklist

- [ ] EAP packet encoding/decoding matches RFC 3748
- [ ] EAP-TLS MSK derivation matches RFC 5216
- [ ] RADIUS Access-Request contains: User-Name, Calling-Station-Id, EAP-Message, Message-Authenticator, 3GPP-S-NSSAI VSA
- [ ] Message-Authenticator HMAC-MD5 computation matches RFC 3579
- [ ] Diameter DER/DEA contains: Session-Id, Auth-Application-Id, EAP-Payload, 3GPP-S-NSSAI
- [ ] Circuit breaker: CLOSED → OPEN (5 failures) → HALF_OPEN (30s) → CLOSED
- [ ] Unit test coverage >80%

## Spec References

- RFC 3748 — EAP
- RFC 5216 — EAP-TLS
- RFC 5281 — EAP-TTLS
- RFC 5448 — EAP-AKA'
- RFC 2865 — RADIUS
- RFC 3579 — RADIUS EAP Extension
- RFC 6733 — Diameter Base
- RFC 4072 — Diameter EAP Application
- TS 29.561 §16 — RADIUS Interworking
- TS 29.561 §17 — Diameter Interworking
