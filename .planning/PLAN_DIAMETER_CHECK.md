# PLAN_DIAMETER_CHECK.md

## Finding: Diameter Protocol Discrepancies in Phase Refactor Plan

**Date:** 2026-04-22
**Phase:** PHASE_Refactor_3Component
**Source File:** `.planning/PLAN_PHASE_REFACTOR_3COMPONENT.md`

---

## Executive Summary

The plan contains two critical issues regarding Diameter protocol implementation:

1. **Wrong command code 280 used for DER/DEA** (should be 268 per RFC 4072)
2. **Misleading description of go-diameter/v4 usage** on the server-side handler

Both issues appear in both the plan AND the actual implementation in `internal/aaa/gateway/diameter_handler.go`.

---

## Issue 1: Wrong Command Code for DER/DEA

### Problematic Text (Plan lines 619-621)

```go
// HandleConnection processes an incoming Diameter connection from AAA-S.
// It reads messages, determines the type, and routes to the appropriate handler.
// Spec: RFC 6733 App H (SCTP), Command Code 280 (DER/DEA), Command Code 274 (ASR/ASA).
```

### Correct Reference

**RFC 4072 (Diameter EAP Application)** defines:
- **DER (Diameter-EAP-Request): Command Code 268**
- **DEA (Diameter-EAP-Answer): Command Code 268**

Command code 268 is shared for both request and answer (distinguished by R-bit in flags).

### Actual Implementation (`diameter_handler.go` lines 129-133)

```go
// Route based on Command Code
// 280 = Experimental-Result (DER/DEA — client-initiated)
// 274 = Abort-Session-Request (ASR — server-initiated)
switch commandCode {
case 280:
    // Client-initiated: response to our DER
    sessionID := extractDiameterSessionID(header)
    h.publishResponse(sessionID, header)
```

### Correct Implementation (`internal/diameter/client.go` lines 28-30)

```go
// Command codes for NSSAA.
const (
    CmdDER = 268 // Diameter-EAP-Request
    CmdDEA = 268 // Diameter-EAP-Answer
    CmdSTR = 275 // Session-Termination-Request
    CmdSTA = 275 // Session-Termation-Answer
)
```

### Impact

- The `diameter_handler.go` implementation uses command code 280, which will **never match** DER/DEA responses from the AAA-S server (AAA-S sends 268)
- This means DEA responses received on the server-side listener will be silently dropped
- The plan incorrectly documents the command code

### Proposed Fix for Plan

**OLD TEXT (lines 619-621):**
```go
// HandleConnection processes an incoming Diameter connection from AAA-S.
// It reads messages, determines the type, and routes to the appropriate handler.
// Spec: RFC 6733 App H (SCTP), Command Code 280 (DER/DEA), Command Code 274 (ASR/ASA).
```

**NEW TEXT:**
```go
// HandleConnection processes an incoming Diameter connection from AAA-S.
// It reads messages, determines the type, and routes to the appropriate handler.
// Spec: RFC 6733 App H (SCTP), RFC 4072 (Diameter EAP)
// Command Code 268 = DER/DEA (distinguished by R-bit)
// Command Code 274 = ASR/ASA (Abort-Session-Request/Answer)
```

---

## Issue 2: Misleading go-diameter/v4 Usage Description

### Problematic Text (Plan line 619)

```go
// Use go-diameter/v4 to decode message header and determine message type.
```

### Actual Implementation

`internal/aaa/gateway/diameter_handler.go` does **NOT** import or use `go-diameter/v4`:

```go
import (
    "context"
    "encoding/binary"
    "fmt"
    "io"
    "log/slog"
    "net"
    "net/http"
)
```

The server-side handler does **manual header parsing**:
```go
header := make([]byte, 20)
if _, err := io.ReadFull(conn, header); err != nil { ... }
commandCode := binary.BigEndian.Uint32(header[1:4]) >> 8
```

### Where go-diameter/v4 IS Used

`internal/diameter/client.go` (client-side, NSSAAF → AAA-S):
```go
import (
    "github.com/fiorix/go-diameter/v4/diam"
    "github.com/fiorix/go-diameter/v4/diam/avp"
    "github.com/fiorix/go-diameter/v4/diam/datatype"
    "github.com/fiorix/go-diameter/v4/diam/dict"
    "github.com/fiorix/go-diameter/v4/diam/sm"
    "github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)
```

### Architectural Clarification

The plan is architecturally correct that NSSAAF acts as a **client** (not server) for AAA communication:
- `internal/diameter/client.go` is the correct client implementation using go-diameter/v4
- `internal/aaa/gateway/diameter_handler.go` handles server-initiated messages (ASR), not the primary DER/DEA flow

However, the plan text incorrectly implies the server-side handler uses go-diameter/v4 for CER/CEA handshake:

> "parses CER/CEA handshake via `go-diameter/v4` state machine"

**Fact:** CER/CEA handshake is handled by `go-diameter/v4` only in the client (`sm.New`, `sm.Client`), not in the server handler.

### Proposed Fix for Plan (line 619)

**OLD TEXT:**
```go
// Use go-diameter/v4 to decode message header and determine message type.
// Command Code 280 = Experimental-Result (used for DER/DEA)
// Command Code 274 = Abort-Session-Request (ASR) / Abort-Session-Answer (ASA)
// Route based on Command Code.
```

**NEW TEXT:**
```go
// Parse Diameter message header manually (no go-diameter/v4 import).
// Command Code 268 = DER/DEA (distinguished by R-bit in header flags)
// Command Code 274 = ASR/ASA (Abort-Session-Request/Answer)
// Route based on Command Code.
```

---

## Issue 3: Misleading SCTP Note

### Problematic Text (Plan line 626)

> "The `go-diameter/v4` library handles the Diameter message framing on top of the SCTP byte stream — no application-level changes needed beyond the listener type."

### Correction

This note is misleading because:
1. `diameter_handler.go` does NOT use go-diameter/v4 at all
2. Manual message parsing handles SCTP framing
3. The note should be removed or rewritten for accuracy

### Proposed Fix (line 626)

**OLD TEXT:**
> **Note on SCTP:** The standard library `net.Listen("sctp", addr)` requires the `net` package to be built with SCTP support. SCTP support is available in Linux kernels since 2.6.27 and Go 1.17+. If SCTP is not available at runtime, the server logs a fatal error and exits. The `go-diameter/v4` library handles the Diameter message framing on top of the SCTP byte stream — no application-level changes needed beyond the listener type.

**NEW TEXT:**
> **Note on SCTP:** The standard library `net.Listen("sctp", addr)` requires the `net` package to be built with SCTP support. SCTP support is available in Linux kernels since 2.6.27 and Go 1.17+. If SCTP is not available at runtime, the server falls back to TCP (see implementation). Diameter message framing on SCTP is handled manually by reading the 20-byte header and determining message length, matching the TCP behavior.

---

## Summary of Required Fixes

| Location | Issue | Severity |
|----------|-------|----------|
| Plan line 620 | Command Code 280 should be 268 for DER/DEA | BLOCKER |
| Plan line 619 | go-diameter/v4 not used in server handler | WARNING |
| Plan line 626 | go-diameter/v4 SCTP note is misleading | WARNING |
| `diameter_handler.go` line 130 | Command Code 280 should be 268 | BLOCKER (implementation bug) |

---

## Spec References

| Spec | Section | Content |
|------|---------|---------|
| RFC 4072 | §3.1 | DER/DEA Command Code = 268 |
| RFC 6733 | §3 | Diameter Base Protocol |
| TS 29.561 | §17.1.2 | Diameter EAP Application for NSSAA |
| TS 29.561 | §17.2.1 | DER/DEA message flow |

---

## Verification

To verify the correct command code is used:

```bash
# Check client.go uses 268
grep -n "CmdDER = 268" internal/diameter/client.go

# Check handler.go incorrectly uses 280
grep -n "case 280:" internal/aaa/gateway/diameter_handler.go
```

Expected output:
- `client.go`: `CmdDER = 268` (correct)
- `handler.go`: Should use 268, but currently shows 280 (bug)
