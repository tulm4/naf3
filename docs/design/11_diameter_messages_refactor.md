# NSSAAF Diameter Message Types - Full Refactor Spec

## Context

The current `diameter_forward.go` builds DER messages using raw `diam.NewRequest()` calls with hardcoded AVP codes. This approach is error-prone and doesn't leverage the auto-generated dictionary types.

## Problem

1. **Wrong AVP code**: `snssai_avp.go` uses AVP code **310** for 3GPP-S-NSSAI, but TS 29.561 Table 17.4-1 specifies **code 200**.
2. **Non-typed message building**: DER messages are built with raw `m.NewAVP(code, ...)` calls instead of strongly-typed structs.
3. **Manual encoding**: The `encodeSnssaiAVP()` function manually constructs the SNSSAI bytes instead of using dictionary-driven encoding.

## Solution

### 1. Fix AVP Code (snssai_avp.go)

**Current (wrong)**:
```go
const (
    AVP3GPP_S_NSSAI = 310  // WRONG
)
```

**Correct per TS 29.561 Table 17.4-1**:
```go
const (
    AVP3GPP_S_NSSAI = 200  // Correct per TS 29.561 §17.4.1
)
```

Also fix `diameter_forward.go` where the same wrong code may be used.

### 2. Create Strongly-Typed Message Structs

Create `internal/diameter/messages/` package with:

```
internal/diameter/messages/
├── der.go      # DER (Diameter-EAP-Request)
├── dea.go      # DEA (Diameter-EAP-Answer)
├── asr.go      # ASR (Abort-Session-Request)
├── asa.go      # ASA (Abort-Session-Answer)
├── rar.go      # RAR (Re-Auth-Request)
├── raa.go      # RAA (Re-Auth-Answer)
├── str.go      # STR (Session-Termination-Request)
├── sta.go      # STA (Session-Termination-Answer)
├── snssai.go   # 3GPP-S-NSSAI encoding
├── nssai.go    # NSSAI-Configuration types
└── builder.go  # Message builder using dictionary
```

### 3. Message Struct Definitions

#### DER (Diameter-EAP-Request)

```go
// DER represents a Diameter-EAP-Request (Command Code 268, AppID 5).
// Spec: RFC 4072, TS 29.561 §17.2.1
type DER struct {
    // Session identification
    SessionID         string
    OriginHost        string
    OriginRealm       string
    DestinationHost   string  // Optional
    DestinationRealm  string

    // Authentication
    AuthApplicationID uint32  // 5 (Diameter EAP)
    AuthRequestType   uint32  // 1=Authenticate, 2=Authorize-Authenticate
    AuthSessionState  uint32  // 1=NoStateMaintained
    OriginStateID     uint64  // Per RFC 6733 §8.8

    // Subscriber identification
    UserName           string  // Optional
    CallingStationID   string  // Optional (GPSI)
    ExternalIdentifier string  // Optional

    // EAP
    EAPPayload []byte  // Optional

    // 3GPP NSSAA
    ThreeGPPSNSSAI *SNSSAI  // Optional
    NSSAIConfiguration *NSSAIConfiguration  // Optional
    AAAServerName string  // Optional
}

// Encode serializes DER to wire format using dictionary.
// Spec: RFC 4072, TS 29.561 §17
func (d *DER) Encode(dict *dict.Parser) ([]byte, error)
```

#### DEA (Diameter-EAP-Answer)

```go
// DEA represents a Diameter-EAP-Answer (Command Code 268, AppID 5).
// Spec: RFC 4072, TS 29.561 §17.2.2
type DEA struct {
    SessionID          string
    OriginHost         string
    OriginRealm        string
    AuthApplicationID  uint32
    AuthRequestType    uint32
    ResultCode         uint32
    ExperimentalResult *ExperimentalResult  // Optional
    AuthSessionState   uint32
    OriginStateID      uint64  // Optional

    UserName  string  // Optional
    EAPPayload []byte  // Optional

    // 3GPP NSSAA response
    NSSAAuthorizationInfo []NSSAAuthorizationInfo  // Optional

    ErrorMessage  string  // Optional
    FailedAVP     []byte  // Optional
}

// Decode deserializes DEA from wire format.
func (d *DEA) Decode(dict *dict.Parser, data []byte) error
```

### 4. SNSSAI Encoding

```go
// SNSSAI represents S-NSSAI per TS 29.571 §5.4.4.60
// Format: SST(1 octet) + SD(3 octets, optional)
type SNSSAI struct {
    SST uint8    // 0-255
    SD  [3]byte
    HasSD bool
}

// MarshalBinary encodes to wire format.
// Spec: TS 29.571 §5.4.4.60
func (s *SNSSAI) MarshalBinary() ([]byte, error)

// UnmarshalBinary decodes from wire format.
func (s *SNSSAI) UnmarshalBinary(data []byte) error

// NewSNSSAI creates SNSSAI from SST and optional SD string.
// sd should be 6 hex chars (e.g., "1A2B3C") or empty.
func NewSNSSAI(sst uint8, sd string) (*SNSSAI, error)
```

### 5. Message Builder

```go
// Builder creates Diameter messages using dictionary-driven encoding.
// Spec: RFC 6733, go-diameter dictionary
type Builder struct {
    dict       *dict.Parser
    originHost string
    originRealm string
}

// NewBuilder creates a new message builder.
func NewBuilder(dict *dict.Parser, originHost, originRealm string) *Builder

// BuildDER creates a DER message.
func (b *Builder) BuildDER(req *DER) (*diam.Message, error)

// BuildDEA creates a DEA message.
func (b *Builder) BuildDEA(req *DEA) (*diam.Message, error)

// ParseDEA parses DEA from received message.
func (b *Builder) ParseDEA(m *diam.Message) (*DEA, error)
```

### 6. Integration with diameter_forward.go

Refactor `buildDERMessage()` to use the new typed structs:

```go
// OLD (current):
func (df *diamForwarder) buildDERMessage(conn diam.Conn, hopByHop uint32, sessionID string, eapPayload []byte, sst uint8, sd string) (*diam.Message, error) {
    m := diam.NewRequest(268, df.cfg.AuthApplicationID, conn.Dictionary())
    // ... raw AVP building ...
}

// NEW (refactored):
func (df *diamForwarder) buildDERMessage(conn diam.Conn, hopByHop uint32, req *DERRequest) (*diam.Message, error) {
    builder := messages.NewBuilder(conn.Dictionary(), df.originHost, df.originRealm)
    return builder.BuildDER(req)
}
```

## File Changes

| File | Change |
|------|--------|
| `internal/diameter/snssai_avp.go` | Fix AVP code 310 → 200 |
| `internal/diameter/messages/snssai.go` | New: SNSSAI type with MarshalBinary |
| `internal/diameter/messages/der.go` | New: DER struct and Encode |
| `internal/diameter/messages/dea.go` | New: DEA struct and Decode |
| `internal/diameter/messages/builder.go` | New: Dictionary-driven builder |
| `internal/aaa/gateway/diameter_forward.go` | Refactor to use typed messages |

## Verification

- [ ] `make build` compiles without errors
- [ ] `make test` passes
- [ ] All AVP codes match TS 29.561 Table 17.4-1
- [ ] DER/DEA round-trip works correctly
