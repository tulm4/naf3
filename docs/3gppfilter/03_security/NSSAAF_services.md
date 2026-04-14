# NSSAAF Security Requirements & Services

Extracted from: 3GPP TS 33.501 v18.10.0

Source: `TS33501_NSSAAF_Services.md` §5.13, §14.4, §16.3-16.5, Annex B.2

---

## NSSAAF Responsibilities — §5.13

The NSSAAF:

1. Handles NSSAA requests from serving AMF (§16)
2. Sends NSSAA requests to appropriate AAA-S
3. Supports AAA-S triggered Re-authentication and Authorization Revocation
4. Translates AAA protocol (RADIUS/Diameter) ↔ Service Based format (SBI)
5. Translates SBI messages from AMF/AUSF → AAA protocols toward AAA-P/AAA-S

---

## Services Provided by NSSAAF — §14.4

### Nnssaaf_NSSAA — §14.4.1

| Service Operation | Semantics | Consumer |
|-------------------|-----------|----------|
| Authenticate | Request/Response | AMF |
| Re-AuthenticationNotification | Notify | AMF (implicit subscription) |
| RevocationNotification | Notify | AMF (implicit subscription) |

#### Authenticate Operation — §14.4.1.2

**Input Required (initial request):**
- EAP ID Response
- GPSI
- S-NSSAI

**Input Required (subsequent requests):**
- EAP message
- GPSI
- S-NSSAI

**Output Required:**
- EAP message
- GPSI
- S-NSSAI

#### Re-AuthenticationNotification — §14.4.1.3

Notifies AMF to trigger NSSAA reauth for UE + S-NSSAI.

**Input Required:** GPSI, S-NSSAI

**Note:** AMF is implicitly subscribed; no explicit subscription required.

#### RevocationNotification — §14.4.1.4

Notifies AMF to revoke NSSAA authorization for UE + S-NSSAI.

**Input Required:** GPSI, S-NSSAI

**Note:** AMF is implicitly subscribed.

---

### Nnssaaf_AIW — §14.4.2

| Service Operation | Semantics | Consumer |
|-------------------|-----------|----------|
| Authenticate | Request/Response | AUSF |

Used for primary authentication in SNPN with Credentials Holder using AAA Server.

---

## NSSAA Procedure — §16.3

### Architecture Roles

```
UE ←─────── EAP ──────────→ AMF (EAP Authenticator)
                                  │
                                  │ Nnssaaf_NSSAA_Authenticate (SBI)
                                  ▼
                               NSSAAF (EAP authenticator backend)
                                  │
            ┌─────────────────────┼─────────────────────┐
            │                     │                     │
            ▼                     ▼                     ▼
       AAA-P (opt)          AAA-S (H-PLMN)        AAA-S (3rd party)
       (proxy)              (operator)            via AAA-P
```

### Key Requirements

1. **EAP Framework:** RFC 3748 mandatory. SEAF/AMF acts as EAP Authenticator.
2. **Privacy:** Recommend privacy-protecting EAP method if privacy required for EAP ID.
3. **AAA Protocol:** NSSAAF undertakes protocol translation. Multiple EAP methods supported.
4. **Third-Party AAA-S:** NSSAAF contacts via AAA-P. NSSAAF and AAA-P may be co-located.
5. **GPSI:** Mandatory for NSSAA. GPSI stored by AAA-S to enable later reauth/revocation.
6. **GPSI ↔ EAP ID mapping:** AAA-S stores GPSI to associate with EAP ID from response, enabling future revocation or reauth.

### S-NSSAI Routing

NSSAAF routes to AAA-S based on S-NSSAI (local configuration of AAA-S address per S-NSSAI).

**Third-party case:** NSSAAF may optionally map S-NSSAI to External Network Slice Information (ENSI) before forwarding to AAA-S. AAA-S uses EAP-ID + ENSI to identify UE.

### On Failure

If NSSAA cannot be completed (server error, UE unreachable):
- AMF sets status of corresponding S-NSSAI as requiring NSSAA in UE Context
- NSSAA re-executes next time UE requests registration with that S-NSSAI

---

## Re-authentication — §16.4

Triggered by AAA-S via AAA protocol (not by timer). NSSAAF:
1. Authorizes AAA-S request (local config of AAA-S address per S-NSSAI)
2. Looks up serving AMF via Nudm_UECM_Get (GPSI)
3. Notifies AMF via Nnssaaf_NSSAA_Re-authenticationNotification
4. AMF is implicitly subscribed; callback URI via NRF

---

## Authorization Revocation — §16.5

AAA-S requests revocation via AAA protocol. NSSAAF:
1. Authorizes revocation request (same per-S-NSSAI config check)
2. Looks up serving AMF via Nudm_UECM_Get (GPSI)
3. Notifies AMF via Nnssaaf_NSSAA_RevocationNotification
4. AMF updates UE Config; if Allowed NSSAI empty → triggers Deregistration

---

## EAP-TLS for NSSAA — Annex B.2

For NSSAA using EAP-TLS:

1. SEAF/AMF = EAP Authenticator
2. NSSAAF = EAP authenticator backend
3. NSSAAF forwards EAP messages between AMF and AAA-S
4. Key derivation: MSK from TLS as specified in RFC 5216

**EAP-TLS** and **EAP-TTLS** are both specified for NSSAA use.

---

## Key Spec Cross-Reference

| Spec Paragraph | Content |
|----------------|---------|
| §5.13 | NSSAAF responsibilities |
| §14.4.1 | Nnssaaf_NSSAA service operations |
| §14.4.2 | Nnssaaf_AIW service operations |
| §16.3 | NSSAA EAP-based procedure |
| §16.4 | AAA-S triggered reauth |
| §16.5 | AAA-S triggered revocation |
| Annex B.2 | EAP-TLS for NSSAA |
