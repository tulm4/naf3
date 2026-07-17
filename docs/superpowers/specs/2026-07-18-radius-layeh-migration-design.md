# RADIUS layeh.com Migration Design

**Date:** 2026-07-18
**Status:** Draft
**Scope:** NSS-AAA RADIUS interface (TS 29.561 Chapter 16)

## 1. Overview

Replace the custom RADIUS codec implementation (`internal/radius/`) with `layeh.com/radius` library, running in parallel with the existing implementation behind a feature flag. The goal is to fix 4 critical bugs in the current implementation while maintaining zero-downtime migration.

### 1.1 Motivation

The current RADIUS implementation has the following critical bugs:

1. **Message-Authenticator computation**: `sendCoAResponse` computes Message-Authenticator over request bytes instead of response bytes (RFC 5176 §3.2 violation)
2. **Response Authenticator**: `sendCoAResponse` recomputes Response Authenticator instead of copying from Request (RFC 5176 §3.2/§3.3 violation)
3. **Endianness**: AVP scanner reads uint16 little-endian, which is wrong for RADIUS TLV
4. **Socket binding**: `Listen` calls `net.ListenUDP("udp", nil)` and ignores the configured `addr` parameter

### 1.2 Decision: Parallel Run with Feature Flag

**Approach:** Phased Parallel Run with Feature Flag

- Create `internal/radius/layeh/` package with layeh-based implementation
- Keep `internal/radius/legacy/` with current implementation
- Feature flag `RADIUS_BACKEND=legacy|layeh` (default: `legacy`)
- Validate layeh implementation with golden file tests + integration tests
- Flip default after 30-day parallel run in staging

**Rationale:**
- Zero-risk migration for production system
- Clean separation allows independent debugging
- Small scope (NSS-AAA only, one VSA) makes parallel run lightweight
- Rollback is flipping an env var

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  internal/aaa/gateway/  (socket I/O layer — unchanged)      │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  RADIUSHandler  ───►  RADIUS_BACKEND feature flag   │   │
│  └─────────────────────────────────────────────────────┘   │
│                       │                                      │
│            ┌──────────┴──────────┐                          │
│            ▼                      ▼                           │
│  ┌─────────────────────┐  ┌─────────────────────┐          │
│  │ internal/radius/    │  │ internal/radius/    │          │
│  │     legacy         │  │     layeh           │          │
│  │  (current impl)    │  │  (new layeh)        │          │
│  └─────────────────────┘  └─────────────────────┘          │
└─────────────────────────────────────────────────────────────┘

Environment: RADIUS_BACKEND=legacy|layeh (default: legacy)
```

## 3. RADIUS Dictionaries

### 3.1 Source Specification

**Spec:** 3GPP TS 29.561 V18.5.0 (2024-09), Chapter 16

**Key points:**
- NSS-AAA uses standard RADIUS attributes (RFC 2865/2866/5176)
- One 3GPP Vendor-Specific Attribute (VSA): `3GPP-S-NSSAI`
- Vendor ID: 10415 (3GPP)
- Sub-type: 200
- Format: Type(1) + Length(1) + SST(1) + SD(3) = 6 bytes
- SST: 0-255
- SD: 3-octet Slice Differentiator (optional)

### 3.2 Dictionary Files

**Location:** `data/dictionaries/`

| File | Purpose |
|------|---------|
| `radius-standard.dict` | Standard RADIUS attributes per RFC 2865/2866/5176 |
| `3gpp-nssaaa.dict` | 3GPP Vendor ID 10415, sub-type 200 |
| `composite.dict` | $INCLUDE combining both |

### 3.3 3GPP-S-NSSAI Format

```
Octets:  | 1      | 1      | 1      | 3
         +--------+--------+--------+--------+
         | Type=200 | Length  | SST    | SD     |
         +--------+--------+--------+--------+
```

- **Type:** 200 (fixed)
- **Length:** 3 (SST-only) or 6 (SST+SD)
- **SST:** Slice/Service Type (0-255)
- **SD:** Slice Differentiator (present if Length=6)

## 4. File Structure

```
data/dictionaries/
├── radius-standard.dict      # Standard RADIUS attributes
├── 3gpp-nssaaa.dict         # 3GPP vendor-specific
└── composite.dict           # $INCLUDE composite

internal/radius/
├── legacy/                    # Current implementation (unchanged during Phase 1)
│   ├── packet.go
│   ├── attribute.go
│   ├── vsa.go
│   ├── message_auth.go
│   └── client.go
│
├── layeh/                    # New layeh-based implementation
│   ├── gen/
│   │   ├── dict.go           # Generated from dictionaries
│   │   └── vendor_3gpp.go   # 3GPP NSSAI TLV handler
│   ├── client.go             # layeh-based RADIUS client
│   ├── server.go             # layeh-based RADIUS server
│   ├── pool.go               # Client connection pool
│   ├── dict_test.go          # Golden file tests
│   └── integration_test.go   # E2E tests
│
└── factory.go               # Feature-flagged factory
```

## 5. Code Generation

### 5.1 Tool

**Tool:** `github.com/layeh/radius/tools/radius-dict-gen`

Install locally:
```bash
go install github.com/layeh/radius/tools/radius-dict-gen@latest
```

### 5.2 Makefile Target

```makefile
RADIUS_DICT_GEN := ./bin/radius-dict-gen
RADIUS_DICTS := data/dictionaries/radius-standard.dict \
                data/dictionaries/3gpp-nssaaa.dict

$(RADIUS_DICT_GEN):
	go install github.com/layeh/radius/tools/radius-dict-gen@latest

.PHONY: gen-radius-dict
gen-radius-dict: $(RADIUS_DICT_GEN)
	$(RADIUS_DICT_GEN) -dict data/dictionaries/composite.dict \
	  -package gen -o internal/radius/layeh/gen/dict.go
```

### 5.3 Manual 3GPP Handler

The generated code does not handle 3GPP-specific TLV sub-encoding. A manual handler is required:

**File:** `internal/radius/layeh/gen/vendor_3gpp.go`

```go
// NSSAI represents a Single Network Slice Selection Assistance Information.
// Spec: TS 29.561 §16.3.2
type NSSAI struct {
    SST uint8   // Slice/Service Type (0-255)
    SD  [3]byte // Slice Differentiator (zero if not present)
}

// Pack encodes NSSAI into VSA sub-TLV format.
func (n *NSSAI) Pack() []byte { ... }

// Unpack decodes NSSAI from VSA sub-TLV format.
func (n *NSSAI) Unpack(b []byte) error { ... }

// AddNSSAIAttribute adds 3GPP-S-NSSAI to a RADIUS packet.
func AddNSSAIAttribute(p *radius.Packet, nssai NSSAI) error { ... }

// GetNSSAIAttributes extracts all 3GPP-S-NSSAI from a RADIUS packet.
func GetNSSAIAttributes(p *radius.Packet) ([]NSSAI, error) { ... }
```

## 6. Feature Flag Factory

**File:** `internal/radius/factory.go`

```go
package radius

import (
    "context"
    "os"
)

type Backend string

const (
    BackendLegacy Backend = "legacy"
    BackendLayeh  Backend = "layeh"
)

type RADIUSClient interface {
    AccessRequest(ctx context.Context, req *AccessRequest) (*AccessResponse, error)
    Close() error
}

func getBackend() Backend {
    switch os.Getenv("RADIUS_BACKEND") {
    case "layeh":
        return BackendLayeh
    default:
        return BackendLegacy
    }
}

func NewClient(ctx context.Context) (RADIUSClient, error) {
    switch getBackend() {
    case BackendLayeh:
        return layeh.NewClient(ctx)
    default:
        return legacy.NewClient(ctx)
    }
}
```

## 7. Testing Strategy

### 7.1 Golden File Tests

Validate codec correctness with known inputs/outputs:

```
internal/radius/layeh/testdata/golden/
├── access-request-nssai.packet      # Raw encoded bytes
├── access-accept-eap.packet
└── coa-request-nssai.packet
```

**Test cases:**
1. Encode Access-Request with NSSAI → compare to golden
2. Decode Access-Request from golden → verify NSSAI extraction
3. Round-trip: encode → decode → verify idempotency
4. Error cases: malformed NSSAI, wrong vendor ID, etc.

### 7.2 Integration Tests

E2E with mock AAA server:

```go
func TestE2E_AccessRequestResponse(t *testing.T) {
    server, addr := startMockAAAServer(t)
    defer server.Close()

    client := layeh.NewClient(layeh.Config{
        ServerAddr: addr.String(),
        Secret:     []byte("testing123"),
    })

    resp, err := client.AccessRequest(ctx, &layeh.AccessRequest{
        UserName: "user@example.com",
        NSSAI:    layeh.NSSAI{SST: 1, SD: [3]byte{0x00, 0x01, 0x02}},
    })
    // verify resp.Code == radius.CodeAccessAccept
}
```

## 8. Implementation Waves

### Wave 1: Dictionaries & Generation
- [ ] Create `data/dictionaries/radius-standard.dict`
- [ ] Create `data/dictionaries/3gpp-nssaaa.dict`
- [ ] Create `data/dictionaries/composite.dict`
- [ ] Add `gen-radius-dict` Makefile target
- [ ] Vendor `radius-dict-gen` tool
- [ ] Generate `internal/radius/layeh/gen/dict.go`

### Wave 2: layeh Client
- [ ] Create `internal/radius/layeh/gen/vendor_3gpp.go`
- [ ] Create `internal/radius/layeh/client.go`
- [ ] Write golden file tests
- [ ] Verify codec correctness

### Wave 3: Integration
- [ ] Add testcontainers for mock AAA server
- [ ] Write E2E integration tests
- [ ] Validate RFC 5176 compliance (Message-Authenticator, Response Authenticator)

### Wave 4: Factory & Cutover
- [ ] Create `internal/radius/factory.go`
- [ ] Add `RADIUS_BACKEND` to config
- [ ] Run parallel mode in staging (30 days)
- [ ] Flip default to `layeh` after validation
- [ ] Remove `legacy` package after 90-day deprecation window

## 9. Bug Fixes Addressed

| Bug | Root Cause | Fix in layeh |
|-----|------------|--------------|
| Message-Authenticator wrong bytes | Custom implementation | layeh uses RFC 3579-compliant implementation |
| Response Authenticator recomputed | Custom implementation | layeh copies Request Authenticator per RFC 5176 |
| Little-endian AVP read | Custom implementation | layeh uses correct big-endian encoding |
| Listen ignores addr | `net.ListenUDP("udp", nil)` | layeh server binds to configured address |

## 10. Out of Scope

- DN-AAA RADIUS (Chapter 11) — future phase
- DTLS transport — remains custom (layeh has no DTLS support)
- Other 3GPP VSAs beyond 3GPP-S-NSSAI
- RADIUS Accounting (NSS-AAA does not use Accounting per TS 29.561 §16.3.1)

## 11. Verification Checklist

- [ ] `make gen-radius-dict` generates valid Go code
- [ ] `make test` passes for both legacy and layeh packages
- [ ] Golden file tests validate NSSAI encode/decode
- [ ] Integration tests validate E2E flow
- [ ] RFC 5176 Message-Authenticator compliance verified
- [ ] `RADIUS_BACKEND=layeh` works in staging
- [ ] No regression in existing RADIUS flows

---

**Author:** AI Agent (brainstorming session 2026-07-18)
**Reviewers:** TBD
**Spec Reference:** TS 29.561 §16.3, RFC 2865, RFC 3579, RFC 5176
