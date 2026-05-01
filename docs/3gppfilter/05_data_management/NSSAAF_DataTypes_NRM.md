# NSSAAF Data Types & NRM

## Data Types — TS 29.571 v18.2.0

Source: `TS29571_NSSAAF_DataTypes.md` §5.4.4.60-61

---

### NssaaStatus — §5.4.4.60

Represents the NSSAA authentication status for a specific S-NSSAI.

| Attribute | Type | P | Description |
|-----------|------|---|-------------|
| snssai | Snssai | M | Subscribed S-NSSAI |
| status | AuthStatus | M | NSSAA status flag |

**Enum: AuthStatus**

| Value | Meaning |
|-------|---------|
| `NOT_EXECUTED` | NSSAA not yet executed for this S-NSSAI |
| `PENDING` | NSSAA procedure is ongoing |
| `EAP_SUCCESS` | EAP authentication succeeded |
| `EAP_FAILURE` | EAP authentication failed |

**Usage:**
- Stored by AMF in UE Context per S-NSSAI
- Part of NSSAISubscription data from UDM
- If S-NSSAI subject to NSSAA is rejected by Network Slice Admission Control (max UEs reached), NssaaStatus in UE Context is **not** impacted

---

### NssaaStatusRm — §5.4.4.61

Same as NssaaStatus but with `nullable: true` on the `status` field (OpenAPI nullable property). Used in resource management APIs where the status may be absent.

---

## Common Identifier Types

### Snssai (S-NSSAI)

Network slice identifier used in all NSSAAF operations.

```yaml
Snssai:
  type: object
  properties:
    sst:           # Slice/Service Type
      type: integer
      minimum: 0
      maximum: 255
    sd:            # Slice Differentiator
      type: string
      pattern: '^[A-Fa-f0-9]{6}$'  # 3-byte hex
```

**Note:** AMF sends H-PLMN S-NSSAI (not mapped value) in NSSAA procedures.

### Gpsi (Generic Public Subscriber Identifier)

External identifier enabling AAA-S to identify the subscriber.

```yaml
Gpsi:
  type: string
  # Pattern from TS 29.571 §5.2.2:
  # '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
  pattern: '^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$'
  description: |
    String identifying a Gpsi shall contain either an External Id or an MSISDN.
    - MSISDN-based: "msisdn-" + 5-15 decimal digits
    - External Identifier-based: "extid-" + <ext-id> + "@" + <realm>
    - Any other string (catch-all for backwards compatibility)
```

**Mandatory:** TS23502 §4.2.9.1 explicitly requires GPSI for NSSAA.

### Supi (Subscription Permanent Identifier)

Primary 5G subscription identifier.

```yaml
Supi:
  type: string
  pattern: '^imu-[0-9]{15}$'
  description: Permanent identifier assigned to the subscription
```

**Used in:** Nnssaaf_AIW service (AUSF side, where subscription identity is primary).

### NfInstanceId

AMF instance identifier for routing NSSAAF→AMF notifications.

```yaml
NfInstanceId: string
```

---

## NSSAAF NRM — TS 28.541 v18.3.0

Source: `TS28541_NSSAAF_NRM.md` §5.3.145-148

---

### NSSAAFFunction IOC — §5.3.145

Managed Object Class for NSSAAF network function.

**Attributes (inherited from ManagedFunction + additional):**

| Attribute | S | isReadable | isWritable | isNotifyable | Description |
|-----------|---|-----------|-----------|-------------|-------------|
| pLMNInfoList | M | T | T | T | PLMN IDs served by this NSSAAF |
| sBIFQDN | M | T | T | T | SBI FQDN for service exposure |
| cNSIIdList | O | T | T | T | Network Slice Instance IDs |
| managedNFProfile | M | T | T | T | NF profile (TS 29.510) |
| commModelList | M | T | T | T | Communication models supported |
| nssaafInfo | O | T | T | T | NSSAAF-specific info |

---

### NssaafInfo DataType — §5.3.146

NSSAAF-specific information associated with NSSAAFFunction.

| Attribute | S | Description |
|-----------|---|-------------|
| supiRanges | O | SUPI ranges served by this NSSAAF instance |
| internalGroupIdentifiersRanges | O | Internal Group ID ranges served |

If `internalGroupIdentifiersRanges` not provided → does **not** imply all internal groups are supported.

---

### EP_N58 — §5.3.147

Endpoint IOC for N58 interface (NSSAAF ↔ AMF).

Inherits from `EP_RP` (generic service endpoint, TS 28.622).

| Attribute | S | Description |
|-----------|---|-------------|
| localAddress | O | Local endpoint address |
| remoteAddress | O | Remote endpoint address (AMF side) |

---

### EP_N59 — §5.3.148

Endpoint IOC for N59 interface (NSSAAF ↔ UDM).

Inherits from `EP_RP`.

| Attribute | S | Description |
|-----------|---|-------------|
| localAddress | O | Local endpoint address |
| remoteAddress | O | Remote endpoint address (UDM side) |

---

## NRM Diagram Hierarchy

```
NSSAAFFunction (IOC)
├── nssaafInfo: NssaafInfo (optional)
├── EP_N58 (NSSAAF-AMF endpoint)
├── EP_N59 (NSSAAF-UDM endpoint)
└── (inherits from ManagedFunction IOC)
```

---

## Notification Types

### NSSAAFSubscribedEventNotification

Generic event notification structure (TS 28.541 §5.5) for NSSAAF:

- **NSSAA_REAUTH_NOTIFICATION**: AAA-S triggered reauth
- **NSSAA_REVOC_NOTIFICATION**: AAA-S triggered revocation
- **Notification URI**: Callback endpoint
- **Event Data**: GPSI, S-NSSAI, notification-specific payload

---

## ProblemDetails (Error Responses) — TS 29.501

Standard error format for all NSSAAF API error responses:

```json
{
  "type": "https://nssAAF.5gc.npn.org/problem/...",
  "title": "EAP Authentication Failed",
  "status": 400,
  "detail": "AAA server rejected the EAP response",
  "cause": "AUTHENTICATION_FAILURE",
  "invalidParams": [
    { "param": "eapPayload", "reason": "Invalid EAP method" }
  ]
}
```

---

## State Transition Diagram

```
                    ┌──────────────┐
                    │ NOT_EXECUTED │
                    └──────┬───────┘
                           │ AMF triggers NSSAA
                           ▼
                    ┌──────────────┐
                    │   PENDING    │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            │            ▼
      ┌──────────────┐     │    ┌──────────────┐
      │ EAP_SUCCESS  │     │    │  EAP_FAILURE  │
      └──────────────┘     │    └──────────────┘
                           │
                           │ timeout / error
                           ▼
                    ┌──────────────┐
                    │ NOT_EXECUTED │  (retry next registration)
                    └──────────────┘

Note: PENDING may persist if UE is CM-IDLE on non-3GPP access only.
      Reauth/Revocation can be triggered by AAA-S at any time from EAP_SUCCESS state.
```
