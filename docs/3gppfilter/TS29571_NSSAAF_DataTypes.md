# 3GPP TS 29.571 - NSSAAF Common Data Types
## Extracted from: 3GPP TS 29.571 V18.2.0 (2025-03)

**Source:** 3GPP TS 29.571 - 5G System; Common Data Types for Service Based Interfaces

---

## 5.4.4.60 Type: NssaaStatus

### Definition

The NssaaStatus data type represents the status of Network Slice-Specific Authentication and Authorization for a specific S-NSSAI.

**Table 5.4.4.60-1: Definition of type NssaaStatus**

| Attribute name | Data type | P | Cardinality | Description |
|---------------|-----------|---|-------------|-------------|
| snssai | Snssai | M | 1 | Subscribed S-NSSAI |
| status | AuthStatus | M | 1 | This flag when present shall indicate the NSSAA status of the related Snssai. |

### NssaaStatus Enum Values (AuthStatus)

```yaml
NssaaStatus:
  enum:
    - "NOT_EXECUTED"      # NSSAA not yet executed
    - "EAP_SUCCESS"        # The NSSAA status is EAP-Success.
    - "EAP_FAILURE"        # The NSSAA status is EAP-Failure.
    - "PENDING"            # The NSSAA status is Pending, i.e. the NSSAA procedure is ongoing.
```

### Usage in NSSAAF Context

The `NssaaStatus` type is used in:

1. **UE Context Management:** Stores NSSAA result for each S-NSSAI in AMF
2. **Registration Procedure:** Indicates pending/authenticated/failed status for slice authentication
3. **Subscription Data:** Part of NSSAISubscription data from UDM

---

## 5.4.4.61 Type: NssaaStatusRm

### Definition

This data type is defined in the same way as the "NssaaStatus" data type, but with the OpenAPI "nullable: true" property.

**Table 5.4.4.61-1: Definition of type NssaaStatusRm**

| Attribute name | Data type | P | Cardinality | Description |
|---------------|-----------|---|-------------|-------------|
| snssai | Snssai | M | 1 | Subscribed S-NSSAI |
| status | AuthStatusRm | M | 1 | NSSAA status with nullable support |

---

## Related Common Data Types for NSSAAF

### Snssai (S-NSSAI)

Used in NssaaStatus to identify the network slice:

```yaml
Snssai:
  type: object
  properties:
    sst: 
      description: Slice/Service Type (0-255)
      type: integer
    sd:
      description: Slice Differentiator (3-byte hex string)
      type: string
```

### AuthStatus

Enum values for authentication status:

| Value | Description |
|-------|-------------|
| NOT_EXECUTED | Authentication not yet performed |
| EAP_SUCCESS | EAP authentication successful |
| EAP_FAILURE | EAP authentication failed |
| PENDING | Authentication in progress |

### Gpsi (Generic Public Subscriber Identifier)

Used for AAA server routing and UE identification:

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

### Supi (Subscription Permanent Identifier)

Primary subscription identifier:

```yaml
Supi:
  type: string
  pattern: '^imu-[0-9]{15}$'  # Example pattern for 5G subscription
  description: Permanent identifier assigned to the subscription
```

---

## NSSAA Context Flow with Common Data Types

### 1. NSSAA Request from AMF

```json
{
  "gpsi": "msisdn-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "eapPayload": "..." // Base64 encoded EAP message
}
```

### 2. NSSAA Response to AMF

```json
{
  "eapPayload": "...", // Base64 encoded EAP message
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "nssaaStatus": "EAP_SUCCESS" // or EAP_FAILURE
}
```

### 3. NSSAA Re-authentication Notification

```json
{
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "notificationType": "RE_AUTH"
}
```

### 4. NSSAA Revocation Notification

```json
{
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "notificationType": "REVOKE"
}
```

---

## ProblemDetails for NSSAAF Errors

Standard error response format per TS 29.501:

```json
{
  "type": "https://nssAAF.5gc.npn.org/problem/...",
  "title": "EAP Authentication Failed",
  "status": 400,
  "detail": "AAA server rejected the EAP response",
  "cause": "AUTHENTICATION_FAILURE",
  "invalidParams": [
    {
      "param": "eapPayload",
      "reason": "Invalid EAP method"
    }
  ]
}
```

---

**End of NSSAAF Common Data Types from TS 29.571**