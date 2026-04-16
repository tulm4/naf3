# NSSAAF 3GPP Documentation Index

## Reading Guide

**Principle:** Never load all 3GPP docs at once. Pick the right chunk based on your question.

| Question | File to Read |
|----------|-------------|
| "Which API endpoints does NSSAAF expose?" | 01_api_specs/NSSAA_API_operations.md |
| "How does AMF trigger NSSAA?" | 02_procedures/NSSAA_flow_AMF.md |
| "How does AAA-S trigger reauth?" | 02_procedures/NSSAA_flow_AMF.md |
| "How does AAA-S revoke authorization?" | 02_procedures/NSSAA_flow_AMF.md |
| "How does AUSF trigger AIW auth?" | 02_procedures/NSSAA_flow_AIW.md |
| "What services does NSSAAF provide?" | 03_security/NSSAAF_services.md |
| "What are the EAP method requirements?" | 03_security/NSSAAF_services.md |
| "How does NSSAAF talk to RADIUS/Diameter?" | 04_protocols/AAA_interworking.md |
| "What data types does NSSAAF use?" | 05_data_management/NSSAAF_DataTypes_NRM.md |
| "How is NSSAAF managed (NRM)?" | 05_data_management/NSSAAF_DataTypes_NRM.md |

---

## Spec Cross-Reference

### Same content across multiple specs

| Topic | Primary Spec | Also Covers |
|-------|-------------|------------|
| NSSAA Flow (AMF-triggered) | TS 23.502 §4.2.9.2 | TS 33.501 §16.3 |
| Reauth Flow (AAA-S-triggered) | TS 23.502 §4.2.9.3 | TS 33.501 §16.4 |
| Revocation Flow | TS 23.502 §4.2.9.4 | TS 33.501 §16.5 |
| AIW Authentication Flow | TS 29.526 §7.3 | TS 33.501 §I.2.2.2 |
| Nnssaaf_NSSAA Service | TS 29.526 §7.2 | TS 33.501 §14.4.1, TS 23.502 §5.2.20 |
| Nnssaaf_AIW Service | TS 29.526 §7.3 | TS 33.501 §14.4.2 |
| NssaaStatus Type | TS 29.571 §5.4.4.60 | TS 29.526 (UE Context) |
| EAP-TLS for NSSAA | TS 33.501 Annex B.2 | RFC 5216 |

### Spec versions

| Spec | Version | File |
|------|---------|------|
| TS 23.502 | v18.4.0 | TS23502_NSSAA_Procedures.md |
| TS 29.526 | v18.7.0 | TS29526_Nnssaaf_NSSAA.yaml |
| TS 29.561 | v18.5.0 | TS29561_NSSAAA_Interworking.md |
| TS 29.571 | v18.2.0 | TS29571_NSSAAF_DataTypes.md |
| TS 28.541 | v18.3.0 | TS28541_NSSAAF_NRM.md |
| TS 33.501 | v18.10.0 | TS33501_NSSAAF_Services.md |

---

## Data Type Quick Reference

### NssaaStatus

```
snssai: Snssai (M)
status: AuthStatus (M)
  enum: NOT_EXECUTED | PENDING | EAP_SUCCESS | EAP_FAILURE
```

### Snssai

```
sst: integer (0-255)     # Slice/Service Type
sd:  string (6 hex chars) # Slice Differentiator (optional)
```

### EapMessage

```
type: string (byte)
description: Base64-encoded EAP packet, nullable
```

### SliceAuthCtxId / AuthCtxId

```
type: string
description: Opaque resource ID for authentication context
```

---

## Interface Summary

| Interface | From | To | Protocol | Purpose |
|-----------|------|----|----------|---------|
| N58 | AMF | NSSAAF | SBI (HTTP/2) | NSSAA authentication |
| N58 | NSSAAF | NSS-AAA | RADIUS/Diameter | AAA protocol |
| N60 | AUSF | NSSAAF | SBI (HTTP/2) | SNPN AIW auth |
| N59 | NSSAAF | UDM | SBI (HTTP/2) | AMF ID lookup (Nudm_UECM_Get) |
| Nnrf | NSSAAF | NRF | SBI (HTTP/2) | Service discovery |

---

## Error Codes (from TS 29.526)

| HTTP | Cause | When |
|------|-------|------|
| 400 | Bad Request | Invalid EAP payload, missing required fields |
| 403 | Forbidden | Slice authentication rejected by AAA-S |
| 404 | User not found | GPSI does not exist |
| 502 | Bad Gateway | NSSAAF cannot reach AAA-S |
| 503 | Service Unavailable | AAA-S temporarily unavailable |
| 504 | Gateway Timeout | Timeout between NSSAAF and AAA-S |

---

## State Machine Reference

```
NOT_EXECUTED ──(AMF triggers)──→ PENDING
PENDING ──(EAP Success)──→ EAP_SUCCESS
PENDING ──(EAP Failure)──→ EAP_FAILURE
PENDING ──(timeout/error)──→ NOT_EXECUTED (retry next registration)
EAP_SUCCESS ──(AAA-S Reauth Request)──→ PENDING
EAP_SUCCESS ──(AAA-S Revocation)──→ removed from Allowed NSSAI
```

---

## Directory Structure

```
docs/3gppfilter/
├── INDEX.md                                    ← You are here
├── README.md                                   ← Original overview
├── SOURCE_MAPPING.md                           ← Source-to-chunk mapping
├── 01_api_specs/
│   └── NSSAA_API_operations.md                 ← TS 29.526 API operations
├── 02_procedures/
│   ├── NSSAA_flow_AMF.md              ← TS 23.502 §4.2.9.2/3/4 (AMF-triggered)
│   └── NSSAA_flow_AIW.md              ← TS 29.526 §7.3 (AUSF-triggered)
├── 03_security/
│   └── NSSAAF_services.md                      ← TS 33.501 §5.13, §14.4, §16.3-5
├── 04_protocols/
│   └── AAA_interworking.md                     ← TS 29.561 Ch.16-17 (RADIUS/Diameter)
└── 05_data_management/
    └── NSSAAF_DataTypes_NRM.md                ← TS 29.571 + TS 28.541

# Reference only (do not read directly):
├── TS29526_Nnssaaf_NSSAA.yaml                  ← Full OpenAPI spec
├── TS29526_Nnssaaf_AIW.yaml                    ← Full OpenAPI spec
├── TS29571_CommonData.yaml                     ← Shared schemas (6740 lines)
```
