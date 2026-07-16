# NSSAAF NRF Integration Index

Quick reference for NSSAAF ↔ NRF communication.

## Decision Tree

| Need | Read This |
|------|-----------|
| How NSSAAF registers with NRF | `NSSAAF_NRF_procedures.md` §1.2 |
| How NSSAAF sends heartbeat | `NSSAAF_NRF_procedures.md` §7 |
| How NSSAAF discovers AMF/AUSF | `NSSAAF_NRF_procedures.md` §2.2, §9 |
| What to put in NFProfile | `NSSAAF_NFProfile.md` |
| NssaafInfo structure | `NSSAAF_NFProfile.md` §3 |
| How to get OAuth tokens | `NSSAAF_AccessToken.md` |
| JWT token format | `NSSAAF_AccessToken.md` §4 |

## File Overview

| File | Content | Lines |
|------|---------|-------|
| `README.md` | This index | ~50 |
| `NSSAAF_NRF_procedures.md` | Nnrf_NFManagement, Nnrf_NFDiscovery, Nnrf_AccessToken procedures | ~400 |
| `NSSAAF_NFProfile.md` | NFProfile data structure, NssaafInfo, NFService | ~400 |
| `NSSAAF_AccessToken.md` | OAuth2 token request/response, JWT structure | ~300 |

## Quick Reference

### NRF API Endpoints

| Service | Base URL |
|---------|----------|
| Nnrf_NFManagement | `{nrfApiRoot}/nnrf-nfm/v1` |
| Nnrf_NFDiscovery | `{nrfApiRoot}/nnrf-disc/v1` |
| Nnrf_AccessToken | `{nrfApiRoot}/oauth2/token` |

### Key URIs

| Operation | URI |
|-----------|-----|
| Register NSSAAF | `PUT /nnrf-nfm/v1/nf-instances/{nfInstanceId}` |
| Update profile | `PATCH /nnrf-nfm/v1/nf-instances/{nfInstanceId}` |
| Deregister | `DELETE /nnrf-nfm/v1/nf-instances/{nfInstanceId}` |
| Heartbeat | `PATCH /nnrf-nfm/v1/nf-instances/{nfInstanceId}` |
| Discover AMF | `GET /nnrf-disc/v1/nf-instances?target-nf-type=AMF` |
| Discover AUSF | `GET /nnrf-disc/v1/nf-instances?target-nf-type=AUSF` |
| Get token | `POST /oauth2/token` |

### NFProfile Required Fields for NSSAAF

```json
{
  "nfInstanceId": "<UUIDv4>",
  "nfType": "NSSAAF",
  "nfStatus": "REGISTERED",
  "nssaafInfo": {
    "supiRanges": [...],
    "internalGroupIdentifiersRanges": [...]
  },
  "nfServices": [
    {
      "serviceName": "nnssaaf-nssaa",
      "versions": [{"apiVersion": "v1"}]
    },
    {
      "serviceName": "nnssaaf-aiw",
      "versions": [{"apiVersion": "v1"}]
    }
  ]
}
```

### OAuth Token Request

```
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded

grant_type=client_credentials
&client_id={nssaafInstanceId}
&scope=nnssaaf-nssaa
&requester_nf_type=NSSAAF
&target_nf_type=AMF
```

### Heartbeat Interval

- Default: 30-60 seconds
- NRF returns `HeartBeat-Interval` header on registration
- Send PATCH with `nfStatus: REGISTERED` to refresh
- If missed: NRF marks as SUSPENDED

## Related Documents

| Document | Description |
|----------|-------------|
| `../01_api_specs/NSSAA_API_operations.md` | NSSAA API endpoints (TS 29.526) |
| `../02_procedures/NSSAA_flow_AMF.md` | NSSAA procedure flows |
| `../03_security/NSSAAF_services.md` | Security requirements |
| `../05_data_management/NSSAAF_DataTypes_NRM.md` | Data types (NssaaStatus, etc.) |
