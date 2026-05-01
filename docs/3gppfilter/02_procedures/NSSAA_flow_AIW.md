# NSSAA AIW Procedure Flows (SNPN Authentication with Credentials Holder)

Extracted from: 3GPP TS 29.526 v18.7.0 §7.3, TS 33.501 v18.10.0 §I.2.2.2

Source: `TS29526_Nnssaaf_AIW.yaml`, `TS33501_NSSAAF_Services.md` §14.4.2, §I.2.2.2

---

## Overview

Nnssaaf_AIW is the service used by AUSF for SNPN (Standalone Non-Public Network) primary authentication with Credentials Holder via AAA Server. This is distinct from Nnssaaf_NSSAA (§4.2.9.2) which is AMF-triggered per-slice authentication.

**Key differences from NSSAA:**

| Aspect | Nnssaaf_NSSAA (§4.2.9.2) | Nnssaaf_AIW (§I.2.2.2) |
|--------|--------------------------|-------------------------|
| Consumer NF | AMF | AUSF |
| Subscriber ID | GPSI | SUPI (imuscheme) |
| Scope | Per S-NSSAI (slice-specific) | Per SUPI (primary auth) |
| Interface | N58 | N60 (AUSF-NSSAAF) |
| Trigger | Registration with NSSAA-required S-NSSAI | SNPN access with Credentials Holder |
| Re-auth/Revocation | Yes (AAA-S → NSSAAF → AMF) | Not applicable |
| MSK output | Not specified | **Required** (RFC 5216) |
| pvsInfo output | Not used | **Required** (Privacy Violating Servers) |
| EAP methods | EAP-TLS, EAP-AKA', EAP-TTLS | EAP-TLS, EAP-TTLS |
| AAA Protocol | RADIUS/Diameter | RADIUS/Diameter |

---

## AIW Authentication Flow (AUSF-triggered) — TS 33.501 §I.2.2.2

**Trigger conditions:**
- UE attempts SNPN access with Credentials Holder identity
- AUSF determines that AAA-based authentication is required (vs 5G-AKA)
- UE does not have 3GPP subscription credentials; uses enterprise credentials instead

**Precondition:** UE has SUPI (imsi-scheme). AUSF holds SUPI from SIDF or UE context.

### Step-by-Step Flow

```
Step 1:  AUSF receives authentication data request from SEAF (during
         primary authentication). SEAF indicates AAA-based authentication
         is needed for SNPN access with Credentials Holder.

Step 2:  AUSF selects authentication method = "EAP" (AAA-based).
         AUSF → NSSAAF: Nnssaaf_AIW_Authenticate_Request
         (SUPI, eapMessage=null)

         NOTE: eapMessage is null on initial request (AUSF does not
         have an EAP Identity Response yet).

Step 3:  NSSAAF creates authentication context:
         - Generates authCtxId (UUIDv7)
         - Stores SUPI, status=PENDING, timestamp
         - Resolves AAA server config by SUPI range or default

Step 4:  NSSAAF → AUSF: Nnssaaf_AIW_Authenticate_Response
         (authCtxId, eapMessage=<initial EAP-Request>)
         eapMessage contains EAP Identity Request from NSSAAF/AAA-S
         to be forwarded to the UE.

Step 5:  AUSF forwards EAP Identity Request to SEAF → AMF → UE.
         (via Nausf_UEAuthentication_Authorize or EAP passthrough)

Step 6:  UE responds with EAP Identity Response containing
         enterprise user identity.

Step 7:  AUSF → NSSAAF: Nnssaaf_AIW_Authenticate_Request
         (authCtxId, SUPI, eapMessage=EAP Identity Response)

Step 8:  NSSAAF forwards EAP Identity Response to AAA-S
         (via RADIUS Access-Request or Diameter DER).
         AAA-S validates credentials, initiates EAP method
         (e.g., EAP-TLS or EAP-TTLS handshake).

Steps 9-N: EAP method rounds exchanged:
         - AAA-S → NSSAAF: EAP-Request (TLS data, challenge, etc.)
         - NSSAAF → AUSF: Nnssaaf_AIW_Authenticate_Response
         - AUSF → UE: EAP-Request (forwarded)
         - UE → AUSF: EAP-Response
         - AUSF → NSSAAF: Nnssaaf_AIW_Authenticate_Request
         - NSSAAF → AAA-S: EAP-Response (forwarded)

         Multi-round continues until EAP completes.

Step N+1: EAP authentication completes.
         AAA-S → NSSAAF: EAP-Success or EAP-Failure
         (via RADIUS Access-Accept/Reject or Diameter DEA).

Step N+2: If EAP-Success:
         - AAA-S derives MSK (RFC 5216) from TLS handshake
         - MSK included in RADIUS Access-Accept (MSK VSA) or
           Diameter DEA (KeyingMaterial AVP)

Step N+3: NSSAAF → AUSF: Nnssaaf_AIW_Authenticate_Response
         (SUPI, authResult=EAP_SUCCESS|EAP_FAILURE,
          eapMessage=null, msk=<Base64>,
          pvsInfo=[...])

Step N+4: If EAP-Success:
         - AUSF derives master key from MSK for SNPN access
         - AUSF creates SEAF security context with SEAF
         - SEAF creates NAS security context for UE
         - UE proceeds with SNPN access

         If EAP-Failure:
         - AUSF returns authentication failure to SEAF
         - SEAF rejects UE authentication attempt
```

### Key Points

- AUSF is the EAP passthrough peer; NSSAAF is the EAP authenticator backend
- SUPI (imsi-scheme) is mandatory for AIW, not GPSI
- MSK derivation: `MSK = TLS-Exporter("EAP-TLS MSK", 64)` (RFC 5216 §2.1.4)
- MSK is forwarded via Nnssaaf_AIW response to AUSF (unique to AIW; not done in NSSAA)
- pvsInfo (Privacy-Violating Servers) is returned only on EAP-Success (TS 29.526 §7.3.3)
- No re-authentication or revocation for AIW (scope is primary auth only)
- ttlsInnerMethodContainer field supports EAP-TTLS inner method (TS 29.526 §7.3.2)

---

## AUSF-NSSAAF (N60) Interface Mapping

| Nnssaaf_AIW Operation | HTTP Method | Resource | TS 29.526 § |
|------------------------|-------------|----------|-------------|
| CreateAuthenticationContext | POST | /authentications | §7.3.2 |
| ConfirmAuthentication | PUT | /authentications/{authCtxId} | §7.3.3 |

### Request Validation (POST /authentications)

| Field | Required | Validation | 3GPP Cause |
|-------|----------|-----------|-----------|
| supi | M | `^imsi-[0-9]{15}$` | INVALID_SUPI |
| eapIdRsp | O | Base64-encoded EAP Response | INVALID_EAP_MESSAGE |
| ttlsInnerMethodContainer | O | Base64-encoded | INVALID_PAYLOAD |
| supportedFeatures | O | Non-empty string | INVALID_FEATURES |

### Response Fields (200 OK / 201 Created)

| Field | Type | Present When |
|-------|------|-------------|
| supi | Supi | Always |
| authCtxId | AuthCtxId | Always |
| eapMessage | EapMessage | EAP-Challenge in progress, or initial request |
| ttlsInnerMethodContainer | EapMessage | EAP-TTLS inner method exchange |
| authResult | AuthStatus | Terminal result only (EAP_SUCCESS/EAP_FAILURE) |
| msk | Msk | EAP-Success only (EAP-TLS MSK, Base64) |
| pvsInfo | PvsInfo[] | EAP-Success only |
| supportedFeatures | SupportedFeatures | Always if provided |

---

## Error Codes

| HTTP | Cause | Description |
|------|-------|-------------|
| 400 | INVALID_SUPI | SUPI format invalid |
| 400 | INVALID_EAP_MESSAGE | EAP payload malformed |
| 404 | AUTH_CONTEXT_NOT_FOUND | authCtxId does not exist |
| 404 | AAA_SERVER_NOT_CONFIGURED | No AAA config for this SUPI range |
| 409 | AUTH_ALREADY_COMPLETED | Context already in terminal state |
| 410 | AUTH_CONTEXT_EXPIRED | Session TTL exceeded |
| 502 | AAA_UNREACHABLE | Cannot contact AAA-S |
| 503 | AAA_UNAVAILABLE | AAA-S overloaded or maintenance |
| 504 | AAA_TIMEOUT | No response from AAA-S within timeout |

---

## Idempotency

**POST /authentications** is not idempotent; each request creates a new context.

**PUT /authentications/{authCtxId}** is idempotent. If AUSF retries with the same eapMessage (same nonce), NSSAAF returns the cached response without re-forwarding to AAA-S.

---

## State Machine

```
AUTH_IDLE → AUTH_PENDING (POST /authentications, eapMessage=null)
  → AUTH_EAP_EXCHANGE (POST with eapMessage, or PUT)
  → AUTH_SUCCESS (AAA-S → EAP-Success, MSK derived)
  → AUTH_FAILURE (AAA-S → EAP-Failure)
  → AUTH_TIMEOUT (AAA response timeout)
```

**NssaaStatus mapping:**

| State | authResult in response | eapMessage in response |
|-------|----------------------|----------------------|
| PENDING | null | EAP-Request from AAA-S |
| EAP_SUCCESS | "EAP_SUCCESS" | null |
| EAP_FAILURE | "EAP_FAILURE" | null |

---

## Reference: TS 29.526 §7.3.2 CreateAuthenticationContext

```json
POST {apiRoot}/nnssaaf-aiw/v1/authentications

Request:
{
  "supi": "imsi-208046000000001",
  "eapIdRsp": null,          // null on initial request
  "ttlsInnerMethodContainer": null,
  "supportedFeatures": "3GPP-R18-AIW"
}

Response 201 Created:
{
  "supi": "imsi-208046000000001",
  "authCtxId": "01fr5xg2e3p4q5r6s7t8u9v0w2",
  "eapMessage": "<Base64 EAP Identity Request>",
  "ttlsInnerMethodContainer": null,
  "supportedFeatures": "3GPP-R18-AIW"
}
```

## Reference: TS 29.526 §7.3.3 ConfirmAuthentication

```json
PUT {apiRoot}/nnssaaf-aiw/v1/authentications/01fr5xg2e3p4q5r6s7t8u9v0w2

// Multi-round continues:
Request:
{ "supi": "...", "eapMessage": "<Base64 EAP Response>", "supportedFeatures": "..." }
Response 200 OK:
{ "supi": "...", "eapMessage": "<Base64 EAP Request>", "authResult": null, "msk": null, "pvsInfo": null }

// Final success:
Response 200 OK:
{
  "supi": "...",
  "eapMessage": null,
  "authResult": "EAP_SUCCESS",
  "msk": "<Base64 MSK (64 bytes)>",
  "pvsInfo": [{ "serverType": "PROSE", "serverId": "pvs-001" }],
  "supportedFeatures": "3GPP-R18-AIW"
}

// Final failure:
Response 200 OK:
{ "supi": "...", "eapMessage": null, "authResult": "EAP_FAILURE", "msk": null, "pvsInfo": null }
```
