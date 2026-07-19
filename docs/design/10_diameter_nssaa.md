# NSSAAF Diameter Support - Implementation Specification

## 1. Overview

This document specifies the implementation of full Diameter support for the NSSAAF (Network Slice Specific Authentication and Authorization Function) per 3GPP TS 29.561 Chapter 17.

**Spec References:**
- TS 29.561 Ch.17 (Diameter-based NSSAA)
- TS 29.571 §5.4.4.60-61 (Data types)
- RFC 6733 (Diameter Base Protocol)
- RFC 4072 (Diameter EAP Application)

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                          NSSAAF Service                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌──────────────┐     ┌──────────────┐     ┌──────────────────┐  │
│   │  HTTP/REST   │     │  Diameter    │     │   EAP Engine      │  │
│   │  Handlers    │     │  Client      │     │   (EAP-TLS)      │  │
│   └──────┬───────┘     └──────┬───────┘     └────────┬─────────┘  │
│          │                    │                       │             │
│          │            ┌───────┴───────┐               │             │
│          │            │               │               │             │
│          ▼            ▼               ▼               ▼             │
│   ┌─────────────────────────────────────────────────────────────┐  │
│   │              Diameter Message Types (Generated)              │  │
│   │                                                              │  │
│   │  DER/DEA    │    ASR/ASA    │    RAR/RAA    │    STR/STA   │  │
│   └─────────────────────────────────────────────────────────────┘  │
│                              │                                      │
└──────────────────────────────│──────────────────────────────────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │   3GPP AAA Server   │
                    │   (via Diameter)    │
                    └─────────────────────┘
```

## 3. Dictionary Structure

### 3.1 Approach: Base + Extension

Use go-diameter's default base dictionary (RFC 6733) extended with NSSAAF-specific definitions.

### 3.2 Dictionary Files

| File | Purpose | Source |
|------|---------|--------|
| `dict/nssaa_extension.xml` | NSSAAF AVP/Command definitions | Manual (TS 29.561) |
| `dict/autogen.sh` | Code generator | This implementation |
| `internal/diameter/generated/nssaa_codes.go` | AVP/Command constants | Auto-generated |
| `internal/diameter/generated/nssaa_dict.go` | Dictionary parser | Auto-generated |

### 3.3 Application IDs

| App ID | Name | Usage |
|--------|------|-------|
| 0 | Base | ASR/ASA, STR/STA, RAR/RAA |
| 5 | Diameter EAP | DER/DEA |
| 16777251 | 3GPP S6a | (Reference only) |

### 3.4 Command Codes

| Command | Code | Application | Purpose |
|---------|------|-------------|---------|
| DER | 268 | EAP (5) | Authentication request |
| DEA | 268 | EAP (5) | Authentication answer |
| ASR | 274 | Base (0) | Abort session request |
| ASA | 274 | Base (0) | Abort session answer |
| RAR | 258 | Base (0) | Re-auth request |
| RAA | 258 | Base (0) | Re-auth answer |
| STR | 275 | Base (0) | Session termination request |
| STA | 275 | Base (0) | Session termination answer |

### 3.5 AVP Codes

#### 3.5.1 Base AVPs (RFC 6733)

| AVP Name | Code | Type | Flags | Required |
|-----------|------|------|-------|----------|
| Session-Id | 263 | UTF8String | M,P | Yes |
| Origin-Host | 264 | DiameterIdentity | M,P | Yes |
| Origin-Realm | 296 | DiameterIdentity | M,P | Yes |
| Destination-Host | 293 | DiameterIdentity | M,P | No |
| Destination-Realm | 283 | DiameterIdentity | M,P | Yes |
| Auth-Application-Id | 258 | Unsigned32 | M,P | Yes |
| Auth-Request-Type | 383 | Enumerated | M,P | Yes |
| Auth-Session-State | 277 | Enumerated | M,P | Yes |
| Result-Code | 268 | Unsigned32 | M,P | Yes |
| User-Name | 1 | UTF8String | M,P | No |
| EAP-Payload | 209 | OctetString | M,P | No |
| Termination-Cause | 295 | Enumerated | M,P | Yes (STR) |
| Re-Auth-Request-Type | 285 | Enumerated | M,P | Yes (RAR) |

#### 3.5.2 3GPP AVPs (Vendor ID 10415)

| AVP Name | Code | Type | Flags | Required |
|-----------|------|------|-------|----------|
| 3GPP-S-NSSAI | 200 | OctetString | M,V | No |
| NSSAI-Configuration | 3100 | Grouped | M,V | No |
| Configured-NSSAI | 3101 | Grouped | M,V | No |
| Requested-NSSAI | 3102 | Grouped | M,V | No |
| NSSAI-Configuration-Data | 3103 | Grouped | M,V | No |
| PLMN-Id | 1467 | OctetString | M,V | No |
| AAA-Server-Name | 260 | DiameterIdentity | M,V | No |
| NSSAAuthorization-Information | 3104 | Grouped | M,V | No |
| Authorization-Result | 3105 | Enumerated | M,V | No |
| Authorization-Grace-Period | 3106 | Unsigned32 | M,V | No |
| NSSAAF-Server-Name | 3107 | DiameterIdentity | M,V | No |
| Rejected-SNSSAI-List | 3108 | Grouped | M,V | No |
| Rejected-SNSSAI-Cause | 3109 | Enumerated | M,V | No |
| Supported-Features | 628 | Grouped | M,V | No |
| Feature-List-ID | 629 | Unsigned32 | M,V | No |
| Feature-List | 630 | Unsigned32 | M,V | No |
| External-Identifier | 606 | UTF8String | M,V | No |

## 4. Message Flows

### 4.1 NSSAA Procedure (DER/DEA)

```
UE          AMF          NSSAAF         AAA
 │            │             │            │
 │---NAS REQ->│             │            │
 │            │---NSSAA REQ│            │
 │            │           --│---DER------>│
 │            │             │            │
 │            │             │<--DEA-----│
 │            │<-NSSAA RSP--│           │
 │            │             │            │
 │<--NAS RSP--│             │            │
```

### 4.2 Session Abort (ASR/ASA)

```
AAA          NSSAAF         AMF
 │              │             │
 │<--ASR-------│             │
 │             │             │
 │---ASA------>│             │
 │              │             │
```

### 4.3 Re-Authentication (RAR/RAA)

```
AAA          NSSAAF         AMF          UE
 │              │             │            │
 │<--RAR-------|             │            │
 │             │             │<--NAS REQ--│
 │             │<--NSSAA RSP-│            │
 │             │             │<--NAS RSP--│
 │---RAA------>│             │            │
```

### 4.4 Session Termination (STR/STA)

```
AMF          NSSAAF         AAA
 │              │             │
 │<--STR-------|             │
 │              │<--STR------│
 │              │             │
 │              │---STA----->│
 │---STA------>│             │
```

## 5. Data Structures

### 5.1 DER (Diameter-EAP-Request)

```go
type DER struct {
    SessionID             string
    OriginHost            string
    OriginRealm          string
    DestinationRealm      string
    DestinationHost       string  // Optional
    AuthApplicationID     uint32  // 5 (EAP)
    AuthRequestType       AuthRequestType
    AuthSessionState      AuthSessionState
    UserName              string  // Optional
    EAPPayload            []byte  // Optional
    ThreeGPPSNSSAI        []ThreeGPPSNSSAI  // Optional
    NSSAIConfiguration    *NSSAIConfiguration  // Optional
    CallingStationID      string  // Optional (GPSI)
    ExternalIdentifier    string  // Optional
    AAAServerName         string  // Optional
    RouteRecord           []string  // Optional
}
```

### 5.2 DEA (Diameter-EAP-Answer)

```go
type DEA struct {
    SessionID                 string
    OriginHost                string
    OriginRealm               string
    AuthApplicationID         uint32  // 5 (EAP)
    AuthRequestType           AuthRequestType
    AuthSessionState          AuthSessionState
    ResultCode                uint32
    ExperimentalResult        *ExperimentalResult  // Optional
    UserName                  string  // Optional
    EAPPayload                []byte  // Optional
    NSSAAuthorizationInfo     []NSSAAuthorizationInformation  // Optional
    ErrorMessage              string  // Optional
    FailedAVP                 []AVP  // Optional
}
```

### 5.3 Supporting Types

```go
// ThreeGPPSNSSAI encodes S-NSSAI per TS 29.571 §5.4.4.60
// Format: SST(1 octet) + SD(3 octets, optional)
type ThreeGPPSNSSAI struct {
    SST uint8    // Slice Service Type (0-255)
    SD  [3]byte  // Slice Differentiator (optional)
    HasSD bool
}

// NSSAAuthorizationInformation per TS 29.571
type NSSAAuthorizationInformation struct {
    SNSSAI           ThreeGPPSNSSAI
    AuthorizationResult AuthorizationResult
    GracePeriod      uint32  // Optional
}

// AuthorizationResult values
const (
    AuthorizationResultSliceAuthorized   AuthorizationResult = 0
    AuthorizationResultSliceNotAuthorized AuthorizationResult = 1
)
```

## 6. API Design

### 6.1 Diameter Client Interface

```go
// Client represents a Diameter client for NSSAAF communication.
type Client interface {
    // DER sends a Diameter-EAP-Request and waits for DEA.
    DER(ctx context.Context, req *DER) (*DEA, error)

    // ASR sends an Abort-Session-Request.
    ASR(ctx context.Context, req *ASR) (*ASA, error)

    // RAR sends a Re-Auth-Request.
    RAR(ctx context.Context, req *RAR) (*RAA, error)

    // STR sends a Session-Termination-Request.
    STR(ctx context.Context, req *STR) (*STA, error)

    // Close closes the client connection.
    Close() error
}
```

### 6.2 Message Builder Interface

```go
// MessageBuilder builds Diameter messages using generated types.
type MessageBuilder interface {
    // NewDER creates a new DER message.
    NewDER() *DER

    // NewDEA creates a new DEA message.
    NewDEA() *DEA

    // Encode encodes a message to wire format.
    Encode(msg interface{}) ([]byte, error)

    // Decode decodes a message from wire format.
    Decode(data []byte) (interface{}, error)
}
```

## 7. File Structure

```
dict/
├── nssaa_extension.xml      # Dictionary extension (manual)
└── autogen.sh               # Code generator (manual)

internal/diameter/
├── dict.go                  # Base dictionary (existing)
├── client.go                # Diameter client (existing, refactor)
├── generated/
│   ├── nssaa_codes.go       # AVP/Command constants (auto-generated)
│   └── nssaa_dict.go        # Dictionary parser (auto-generated)
├── messages/
│   ├── der.go               # DER message implementation
│   ├── dea.go               # DEA message implementation
│   ├── asr.go               # ASR message implementation
│   ├── asa.go               # ASA message implementation
│   ├── rar.go               # RAR message implementation
│   ├── raa.go               # RAA message implementation
│   ├── str.go               # STR message implementation
│   └── sta.go               # STA message implementation
├── builder.go               # Message builder
├── encoder.go               # Wire format encoder
└── decoder.go               # Wire format decoder

internal/aaa/gateway/
└── diameter_forward.go       # AAA Gateway integration (existing, update)
```

## 8. Implementation Phases

### Phase 1: Dictionary Generation
- [ ] Create `dict/nssaa_extension.xml` with all AVPs and commands
- [ ] Create `dict/autogen.sh` to generate Go code
- [ ] Run autogen to create `generated/nssaa_codes.go` and `generated/nssaa_dict.go`

### Phase 2: Message Types
- [ ] Create DER message type and builder
- [ ] Create DEA message type and builder
- [ ] Create ASR/ASA message types
- [ ] Create RAR/RAA message types
- [ ] Create STR/STA message types

### Phase 3: Encoder/Decoder
- [ ] Implement wire-format encoder using go-diameter
- [ ] Implement wire-format decoder using go-diameter
- [ ] Add message validation

### Phase 4: Client Integration
- [ ] Refactor `internal/diameter/client.go` to use generated types
- [ ] Update AAA Gateway integration in `internal/aaa/gateway/diameter_forward.go`
- [ ] Add connection management

### Phase 5: Testing
- [ ] Unit tests for message types
- [ ] Unit tests for encoder/decoder
- [ ] Integration tests for client

## 9. Validation Checklist

- [x] `make generate-dict` produces valid Go code
- [x] All AVP codes match TS 29.571 Table 17.4-1 (3GPP-S-NSSAI = **200**)
- [x] All command codes match RFC 6733/RFC 4072
- [x] DER/DEA round-trip encoding/decoding works
- [x] SNSSAI encoding uses raw octets (not grouped AVP)
- [x] Build compiles without errors
- [x] Unit tests pass for diameter package

## 10. References

- TS 29.561: 5G System; Network Slice Specific Authentication and Authorization Service
- TS 29.571: Common Data
- RFC 6733: Diameter Base Protocol
- RFC 4072: Diameter EAP Application
- RFC 3588: Diameter Base Protocol (obsolete, reference)
