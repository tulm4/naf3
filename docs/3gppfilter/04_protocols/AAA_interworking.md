# AAA Protocol Interworking

Extracted from: 3GPP TS 29.561 v18.5.0

Source: `TS29561_NSSAAA_Interworking.md` Ch.16-17

---

## Overview

NSSAAF translates SBI (HTTP/2 JSON) ↔ AAA protocols (RADIUS/Diameter).

```
AMF ←──── SBI (HTTP/2) ──────→ NSSAAF ←──── AAA Protocol ──────→ NSS-AAA Server
                                       │
                                   AAA-P (optional)
                                 (third-party routing)
```

AAA protocol selection: NSSAAF uses whichever protocol the target AAA-S supports. Both RADIUS and Diameter may be supported simultaneously.

---

## RADIUS Interworking — Ch.16

### RFC Standards

- RFC 2865: RADIUS base
- RFC 3162: RADIUS IPv6
- RFC 4818: RADIUS DTLS
- RFC 3579: RADIUS EAP extension (mandatory for NSSAAF)
- RFC 5216: EAP-TLS
- RFC 5281: EAP-TTLS

### RADIUS Client

RADIUS client function resides in NSSAAF.

**Flow:** NSSAAF receives Nnssaaf_NSSAA_Authenticate from AMF → RADIUS client sends Access-Request to NSS-AAA (direct or via AAA-P).

### RADIUS Message Attributes for NSSAA

| RADIUS Attribute | Usage in NSSAA |
|-----------------|----------------|
| Calling-Station-Id | GPSI or External-Identifier |
| 3GPP-S-NSSAI (VSA Sub-attr #200) | S-NSSAI in Access-Request |
| EAP-Message | EAP payload (RFC 3579) |
| Message-Authenticator | Integrity (RFC 3579) |

#### 3GPP-S-NSSAI Sub-attribute (VSA #200)

```
Format:
Octets:  1         2        3        4-6
         3GPP Type=200 | Length | SST   | SD (optional)

- 3GPP Type: 200
- Length: 3 (SST only) or 6 (SST + SD)
- SST: Slice/Service Type (0-255)
- SD: 3-octet Slice Differentiator (optional)
```

**Presence:** Conditional — SHALL be included in initial Access-Request.

### Authentication Flow (RADIUS)

```
1.  AMF decides to trigger NSSAA

2.  AMF → UE: NAS Network Slice-Specific Authentication Command (EAP Identity Request)

3.  UE → AMF: NAS Network Slice-Specific Authentication Complete (EAP Identity Response)

4.  AMF → NSSAAF: Nnssaaf_NSSAA_Authenticate Request

5.  NSSAAF → NSS-AAA (or AAA-P → NSS-AAA): RADIUS Access-Request
    (GPSI in Calling-Station-Id, S-NSSAI in 3GPP-S-NSSAI VSA, EAP in EAP-Message)

6.  NSS-AAA → NSSAAF (or AAA-P): RADIUS Access-Challenge (EAP challenge)

7.  NSSAAF → AMF: Nnssaaf_NSSAA_Authenticate Response (EAP message)

8.  AMF → UE: NAS Network Slice-Specific Authentication Command (EAP data)

Steps 6-8 repeat for multi-round EAP methods (e.g., EAP-TLS)

9.  NSS-AAA → NSSAAF: RADIUS Access-Accept (EAP-Success) or Access-Reject (EAP-Failure)

10. NSSAAF → AMF: Nnssaaf_NSSAA_Authenticate Response (final result)

11. AMF → UE: NAS Network Slice-Specific Authentication Result
```

### RADIUS Revocation — §16.2.2

NSS-AAA sends **RADIUS Disconnect-Request** (RFC 5176) to NSSAAF (direct or via AAA-P).

NSSAAF checks authorization (local config of NSS-AAA address per S-NSSAI).

If authorized:
- NSSAAF releases resources
- NSSAAF → UDM: Nudm_UECM_Get (GPSI) → gets AMF ID
- NSSAAF → AMF: Nnssaaf_NSSAA_RevocationNotification
- NSSAAF → NSS-AAA: Disconnect-ACK

If unable to release resources: Disconnect-NAK.

**Note:** NSSAAF may send Disconnect-ACK before receiving AMF/Nudm response.

---

## Diameter Interworking — Ch.17

### RFC Standards

- RFC 7155: Diameter EAP application
- RFC 4072: Diameter EAP (NASREQ + EAP, Vendor-Specific-App-ID with 3GPP Vendor-ID 10415)
- RFC 6733: Diameter base (Capabilities-Exchange-Request/Answer)

### Diameter Client

Diameter client function resides in NSSAAF.

NSSAAF and NSS-AAA advertise support via Capabilities-Exchange-Request:
- Auth-Application-Id: 1 (NASREQ) and 5 (Diameter EAP)
- Vendor-Id: 10415 (3GPP)

### Diameter Message Flow

```
1.  AMF decides to trigger NSSAA

2-4. AMF ↔ UE ↔ AMF: NAS EAP exchange (same as RADIUS flow)

5.  NSSAAF → NSS-AAA (or AAA-P): Diameter DER (Diameter-EAP-Request)
    (GPSI in Calling-Station-Id, S-NSSAI in 3GPP-S-NSSAI AVP)

6.  NSS-AAA → NSSAAF: Diameter DEA (Diameter-EAP-Answer) (EAP challenge)

7-14. Multi-round DER/DEA exchanges mirrored by NAS messages via AMF

15-16. NSS-AAA → NSSAAF: Final DEA (EAP-Success/Failure)

17. NSSAAF → AMF: Nnssaaf_NSSAA_Authenticate Response

18. AMF → UE: NAS Network Slice-Specific Authentication Result
```

### Diameter Re-Authentication (STR/REA)

For AAA-S triggered reauth via Diameter:
- NSSAAF sends DER with Session-Id from original authentication
- NSS-AAA responds with DEA
- Session-Termination-Request (STR) / Session-Termination-Answer (STA) used when NSSAA no longer needed

### Diameter Revocation

NSS-AAA initiated via proprietary AVPs in DEA or via Re-Auth-Request.

---

## AAA-P Path (Third-Party AAA-S)

```
AMF ←──── SBI ─────→ NSSAAF ←──── AAA ─────→ AAA-P
                                                   │
                                                   ↓
                                              NSS-AAA (3rd party)
```

AAA-P is required when:
- AAA-S belongs to a third party
- Operator deploys a proxy toward third parties

NSSAAF and AAA-P may be co-located.

NSSAAF optionally maps S-NSSAI → ENSI (External Network Slice Information) before forwarding to third-party AAA-S. AAA-S identifies UE via EAP-ID + ENSI.

---

## Protocol Comparison

| Aspect | RADIUS | Diameter |
|--------|--------|---------|
| Transport | UDP/TCP (RFC 3579) | SCTP/TCP |
| Reliability | Best-effort with retransmit | Stateful, CER/CEA handshake |
| AVP | VSA Sub-attr #200 for S-NSSAI | Grouped 3GPP-S-NSSAI AVP |
| Application ID | NASREQ (1), EAP (5) | NASREQ (1), EAP (5) |
| Revocation | Disconnect-Request (RFC 5176) | DEA with result-code or REA |
| Reauth | Access-Request with new EAP session | DER with Session-Id |

---

## NSSAAF as AAA Client

NSSAAF implements:
- **RADIUS client:** Access-Request, Access-Challenge, Access-Accept, Access-Reject, Disconnect-Request handling
- **Diameter client:** DER, DEA, STR, STA, CER/CEA capabilities exchange
- **Protocol selection:** Determined by AAA-S capability (local configuration per S-NSSAI)
