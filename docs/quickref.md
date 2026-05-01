# NSSAAF Quick Reference

## Architecture

NSSAAF uses a **3-component model** for production Kubernetes deployments:

```
AMF/AUSF → HTTP Gateway (N replicas) → Biz Pods (N replicas) → AAA Gateway (2 replicas, active-standby) → AAA-S
```

| Component | Replicas | Protocol |
|---|---|---|
| HTTP Gateway | N (stateless) | HTTPS/443 → HTTP/ClusterIP |
| Biz Pod | N (stateless) | HTTP/9090 → AAA Gateway |
| AAA Gateway | 2 (active-standby) | RADIUS UDP :1812 / Diameter TCP :3868 |

See `docs/design/01_service_model.md` §5.4 for full details.

## Critical Facts (MUST KNOW)

### Key Data Types

```go
// GPSI: Generic Public Subscriber Identifier
// Pattern: ^(msisdn-[0-9]{5,15}|extid-[^@]+@[^@]+|.+)$ (required for NSSAA)
// Example: "5-208046000000001"
type Gpsi string

// SUPI: Subscription Permanent Identifier
// Pattern: ^imsi-[0-9]{5,15}$ (used in AIW)
// Example: "imsi-208046000000001"
type Supi string

// S-NSSAI: Single Network Slice Selection Assistance Information
// SST: 0-255 (Slice/Service Type)
// SD: 6 hex chars (optional, Slice Differentiator)
// Example: sst=1, sd="000001"
type Snssai struct {
    Sst uint8   `json:"sst"`
    Sd  string `json:"sd,omitempty"`  // 6 hex chars
}

// NssaaStatus: NSSAA authentication result
// Values: NOT_EXECUTED → PENDING → EAP_SUCCESS | EAP_FAILURE
type NssaaStatus string

// EapMessage: Base64-encoded EAP payload, nullable
type EapMessage *string
```

### Key API Endpoints

```
POST /nnssaaf-nssaa/v1/slice-authentications
  → 201 Created (SliceAuthContext)
  → 400 (invalid), 403 (rejected), 404 (no AAA config), 502/503/504 (AAA errors)

PUT /nnssaaf-nssaa/v1/slice-authentications/{authCtxId}
  → 200 OK (SliceAuthConfirmationResponse)
  → authResult: null (continue), "EAP_SUCCESS", "EAP_FAILURE"

POST /nnssaaf-aiw/v1/authentications
  → 201 Created (AuthContext)
  → Uses SUPI instead of GPSI

PUT /nnssaaf-aiw/v1/authentications/{authCtxId}
  → 200 OK (AuthConfirmationResponse)
  → Returns MSK on EAP_SUCCESS
```

### Key HTTP Headers

```
Content-Type: application/json
Authorization: Bearer {oauth2_token}  (NRF-issued)
X-Request-ID: {uuid}                (Correlation ID)
```

### Key Error Codes

| HTTP | Cause | Trigger |
|------|-------|---------|
| 400 | BAD_REQUEST | Invalid EAP payload, missing GPSI |
| 403 | AUTHENTICATION_REJECTED | AAA-S rejects auth |
| 404 | USER_NOT_FOUND | GPSI not found |
| 502 | BAD_GATEWAY | AAA-S unreachable |
| 503 | SERVICE_UNAVAILABLE | AAA-S overloaded |
| 504 | GATEWAY_TIMEOUT | AAA-S timeout |

### Key NssaaStatus State Machine

```
NOT_EXECUTED ──(POST /slice-auth)──→ PENDING
PENDING ──(EAP-Success)──→ EAP_SUCCESS
PENDING ──(EAP-Failure)──→ EAP_FAILURE
PENDING ──(timeout 30s)──→ NOT_EXECUTED (retry next registration)
EAP_SUCCESS ──(AAA-S Re-Auth)──→ PENDING
EAP_SUCCESS ──(AAA-S Revoke)──→ removed from Allowed NSSAI
```

### Key RADIUS AVPs

| AVP | Code | Usage |
|-----|------|-------|
| User-Name | 1 | GPSI |
| Calling-Station-Id | 31 | GPSI |
| EAP-Message | 79 | EAP payload |
| Message-Authenticator | 80 | HMAC-MD5 (RFC 3579) |
| 3GPP-S-NSSAI | 200 | VSA, 3 bytes SST + 3 bytes SD |

### Key Diameter AVPs

| AVP | Code | Usage |
|-----|------|-------|
| User-Name | 1 | GPSI |
| EAP-Payload | 209 | EAP message |
| 3GPP-S-NSSAI | 310 | Grouped AVP |
| Auth-Application-Id | 258 | 5 (Diameter EAP) |
| Auth-Request-Type | 274 | 1 (AUTHORIZE_AUTHENTICATE) |

### Key Configuration

```yaml
# EAP
eap:
  maxRounds: 20           # Max EAP rounds
  roundTimeoutSeconds: 30  # Per-round timeout
  sessionTtlSeconds: 300 # 5 minutes

# AAA
aaa:
  responseTimeoutMs: 10000  # 10 seconds
  maxRetries: 3

# Internal communication (Phase R: 3-component)
biz:
  aaaGatewayUrl: "http://svc-nssaa-aaa:9090"  # Biz Pod → AAA Gateway
aaaGateway:
  listenRadius: ":1812"
  listenDiameter: ":3868"
  bizServiceUrl: "http://svc-nssaa-biz:8080"   # AAA Gateway → Biz Pod

# Rate Limiting
rateLimit:
  perGpsiPerMin: 10       # Per subscriber
  perAmfPerSec: 1000      # Per AMF
  globalPerSec: 100000    # Global
```

### Key Spec Paragraphs

| Spec | Section | Content |
|------|---------|---------|
| TS 29.526 | §7.2 | Nnssaaf_NSSAA API |
| TS 29.526 | §7.3 | Nnssaaf_AIW API |
| TS 23.502 | §4.2.9.2 | AMF-triggered NSSAA flow |
| TS 23.502 | §4.2.9.3 | AAA-S triggered reauth |
| TS 23.502 | §4.2.9.4 | AAA-S triggered revocation |
| TS 33.501 | §5.13 | NSSAAF responsibilities |
| TS 33.501 | §16.3 | NSSAA EAP procedure |
| TS 29.561 | §16 | RADIUS interworking |
| TS 29.561 | §17 | Diameter interworking |

### Key Performance Targets

| Metric | Target |
|--------|--------|
| Concurrent sessions | 50,000 / instance |
| Requests per second | 10,000 / instance |
| P99 latency | <500ms |
| RADIUS transaction rate | >50,000 / sec |
| Session setup rate | >5,000 / sec |
| Availability | 99.999% |

### Key AAA Server Routing

```go
// 3-level fallback lookup for AAA config:
1. Exact match: (snssai.sst, snssai.sd)
2. SST-only:    (snssai.sst, sd=*)
3. Default:     (sst=*, sd=*)
```

### Key Notification Types

```
Re-AuthenticationNotification: POST {reauthNotifUri}
  { "notifType": "SLICE_RE_AUTH", "gpsi": "...", "snssai": {...} }

RevocationNotification: POST {revocNotifUri}
  { "notifType": "SLICE_REVOCATION", "gpsi": "...", "snssai": {...} }
```
