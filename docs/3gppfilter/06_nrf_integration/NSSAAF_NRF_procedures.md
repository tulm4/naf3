# NSSAAF NRF Integration Guide

**Source:** TS 29.510 V18.11.0 (2026-03) — Network Function Repository Services
**Sections:** §5.2 (Nnrf_NFManagement), §5.3 (Nnrf_NFDiscovery), §5.4 (Nnrf_AccessToken), §6.1 (NFManagement API), §6.2 (NFDiscovery API), §6.3 (AccessToken API)

## Overview

NSSAAF communicates with NRF via the following SBI services:

| NRF Service | Purpose for NSSAAF |
|-------------|-------------------|
| Nnrf_NFManagement | Register, update, deregister NSSAAF NF profile |
| Nnrf_NFDiscovery | Discover other NFs (AMF, AUSF, etc.) |
| Nnrf_AccessToken | Obtain OAuth2 tokens for service-to-service auth |

---

## 1. Nnrf_NFManagement Service (§5.2)

### 1.1 Service Operations

| Operation | HTTP Method | Description |
|-----------|-------------|-------------|
| NFRegister | PUT | Register NSSAAF profile in NRF |
| NFUpdate | PUT/PATCH | Update NSSAAF profile (full replacement or partial) |
| NFDeregister | DELETE | Deregister NSSAAF from NRF |
| NFStatusSubscribe | POST | Subscribe to NF status changes |
| NFStatusNotify | POST | Receive NF status notifications (callback) |
| NFStatusUnsubscribe | DELETE | Unsubscribe from NF status changes |
| NFProfileRetrieval | GET | Retrieve NSSAAF profile |

### 1.2 NFRegister Procedure (§5.2.2.2)

**Resource URI:** `{apiRoot}/nnrf-nfm/v1/nf-instances/{nfInstanceID}`

```
PUT /nnrf-nfm/v1/nf-instances/{nfInstanceID}
```

**Request Body:** NFProfile (see §6.1.6.2.2)

**Success Response:** `201 Created`
- Location header contains URI of created resource
- Body contains NFProfile with NRF-added attributes
- `HeartBeat-Interval` header indicates heartbeat interval (seconds)

**Error Responses:**
- `400 Bad Request` — Invalid NFProfile encoding or unknown Shared Data IDs
- `403 Forbidden` — Not authorized
- `500 Internal Server Error` — NRF internal error
- `3xx` — Redirection to another NRF instance

**Example UUID format:** `4947a69a-f61b-4bc1-b9da-47c9c5d14b64` (UUIDv4, lowercase)

### 1.3 NFUpdate Procedure (§5.2.2.3)

#### Complete Replacement (PUT)
**Resource URI:** `{apiRoot}/nnrf-nfm/v1/nf-instances/{nfInstanceID}`

```
PUT /nnrf-nfm/v1/nf-instances/{nfInstanceID}
```

**Success Response:** `200 OK`

#### Partial Update (PATCH)
**Resource URI:** `{apiRoot}/nnrf-nfm/v1/nf-instances/{nfInstanceID}`

```
PATCH /nnrf-nfm/v1/nf-instances/{nfInstanceID}
Content-Type: application/json-patch+json
```

**Request Body:** JSON Patch operations (add/delete/replace)

**Headers:**
- Include `If-Match` header with latest ETag for conditional update
- NRF returns `412 Precondition Failed` if ETag mismatch

**Success Response:** `204 No Content` (or `200 OK` with full profile)

### 1.4 NFDeregister Procedure (§5.2.2.4)

**Resource URI:** `{apiRoot}/nnrf-nfm/v1/nf-instances/{nfInstanceID}`

```
DELETE /nnrf-nfm/v1/nf-instances/{nfInstanceID}
```

**Success Response:** `204 No Content`

**Error Responses:**
- `403 Forbidden` — Not authorized
- `404 Not Found` — NF instance not found
- `500 Internal Server Error` — NRF internal error

### 1.5 NFStatusSubscribe Procedure (§5.2.2.5)

**Resource URI:** `{apiRoot}/nnrf-nfm/v1/subscriptions`

```
POST /nnrf-nfm/v1/subscriptions
```

**Request Body:** NFStatusSubscription
- `nfStatusUri` — Callback URI for notifications
- `subscriptionId` — Unique subscription ID
- Query conditions (nfType, serviceName, reqSnssais, etc.)

**Success Response:** `201 Created`

### 1.6 NFStatusNotify (Callback) §5.2.2.6

NRF calls this endpoint when:
- New NF instance registered
- NF instance profile changed
- NF instance deregistered
- NF instance status changed (REGISTERED → SUSPENDED)

**Request Body:** NotificationData
- `event` — NFProfileChange, NFRegistered, NFDeregistered
- `nfProfile` — Changed NF profile (or delta)

### 1.7 NFProfileRetrieval Procedure (§5.2.2.9)

**Resource URI:** `{apiRoot}/nnrf-nfm/v1/nf-instances/{nfInstanceID}`

```
GET /nnrf-nfm/v1/nf-instances/{nfInstanceID}
```

**Success Response:** `200 OK` with NFProfile

---

## 2. Nnrf_NFDiscovery Service (§5.3)

### 2.1 Service Operations

| Operation | HTTP Method | Description |
|-----------|-------------|-------------|
| NFDiscover | GET | Discover NF instances by query parameters |

### 2.2 NFDiscover Procedure (§5.3.2.2)

**Resource URI:** `{apiRoot}/nnrf-disc/v1/nf-instances`

```
GET /nnrf-disc/v1/nf-instances?target-nf-type={type}&requester-nf-type={type}
```

**Mandatory Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| target-nf-type | NFType | Type of NF to discover (e.g., AMF, AUSF) |
| requester-nf-type | NFType | Type of requesting NF (NSSAAF) |

**Optional Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| service-names | array(ServiceName) | Filter by service names |
| req-snssais | array(Snssai) | Filter by S-NSSAI |
| req-plmn-list | array(Plmn) | Filter by PLMN |
| limit | integer | Max results |
| page-number, page-size | integer | Pagination |

**Success Response:** `200 OK`
```json
{
  "validityPeriod": 3600,
  "nfInstances": [
    {
      "nfInstanceId": "...",
      "nfType": "AMF",
      "nfStatus": "REGISTERED",
      "nfServices": [...]
    }
  ]
}
```

**Error Responses:**
- `400 Bad Request` — Invalid query parameters
- `403 Forbidden` — Not allowed to discover
- `404 Not Found` — No matching NF found
- `500 Internal Server Error` — NRF internal error
- `3xx` — Redirection

---

## 3. Nnrf_AccessToken Service (§5.4)

### 3.1 Access Token Request Procedure

**Endpoint:** `{nrfApiRoot}/oauth2/token`

```
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded
```

**Request Body (form-urlencoded):**
| Parameter | Value | Description |
|-----------|-------|-------------|
| grant_type | client_credentials | OAuth2 grant type |
| client_id | {nfInstanceId} | NSSAAF instance ID |
| scope | nnssaaf-nssaa nnssaaf-aiw | Requested service names |
| requester-nf-type | NSSAAF | NF type of requester |
| target-nf-type | AMF | NF type of target producer |
| target-nf-instance-id | {nfInstanceId} | Optional: specific target |

**Success Response:** `200 OK`
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "scope": "nnssaaf-nssaa nnssaaf-aiw"
}
```

**Error Responses:**
- `400 Bad Request` — invalid_request, invalid_client, invalid_grant, invalid_scope
- `401 Unauthorized` — Authentication failed
- `403 Forbidden` — Not authorized for requested scope

### 3.2 Token Validation

Access tokens are JWTs (RFC 7519) signed by NRF using JWS (RFC 7515).

Token claims must include:
- `iss` — NRF issuer
- `sub` — NF instance ID
- `aud` — Target service(s)
- `scope` — Authorized scopes
- `exp` — Expiration time

---

## 4. API Resources Summary (§6.1, §6.2, §6.3)

### 6.1 Nnrf_NFManagement API

| Resource | URI | Methods |
|----------|-----|---------|
| nf-instances | /nnrf-nfm/v1/nf-instances | GET |
| nf-instance | /nnrf-nfm/v1/nf-instances/{nfInstanceID} | GET, PUT, PATCH, DELETE |
| subscriptions | /nnrf-nfm/v1/subscriptions | POST |
| subscription | /nnrf-nfm/v1/subscriptions/{subscriptionID} | PATCH, DELETE |

### 6.2 Nnrf_NFDiscovery API

| Resource | URI | Methods |
|----------|-----|---------|
| nf-instances | /nnrf-disc/v1/nf-instances | GET |

### 6.3 Nnrf_AccessToken API

| Operation | URI | Method |
|-----------|-----|--------|
| Get Access Token | /oauth2/token | POST |

---

## 5. HTTP Requirements

### General
- HTTP/2 shall be used (TS 29.500 §5)
- JSON content type: `application/json`
- Problem Details: `application/problem+json`
- JSON Patch: `application/json-patch+json`

### Headers
| Header | Usage |
|--------|-------|
| Content-Type | Request body format |
| Accept | Response format |
| Location | Created resource URI (201 responses) |
| ETag | Cache validation |
| If-Match | Conditional PATCH/PUT |
| If-None-Match | Conditional GET |
| Cache-Control | Response caching (NFDiscovery) |
| X-Request-ID | Correlation ID |

---

## 6. Error Handling

All errors return ProblemDetails (RFC 7807):
```json
{
  "type": "https://example.com/problem/...",
  "title": "Bad Request",
  "status": 400,
  "detail": "NFProfile encoding error",
  "cause": "INVALID_NF_PROFILE",
  "invalidParams": [...]
}
```

### Error Codes

| HTTP Status | Cause | Description |
|-------------|-------|-------------|
| 400 | INVALID_NF_PROFILE | NFProfile encoding error |
| 400 | SHARED_DATA_ID_UNKNOWN | Unknown shared data ID |
| 400 | SHARED_DATA_NOT_CONFIGURED | Shared data not configured |
| 400 | INVALID_CLIENT | Client verification failed |
| 403 | FORBIDDEN | Not authorized |
| 404 | NOT_FOUND | Resource not found |
| 412 | PRECONDITION_FAILED | ETag mismatch |
| 500 | INTERNAL_ERROR | NRF internal error |
| 503 | SERVICE_UNAVAILABLE | NRF unavailable |

---

## 7. Heartbeat Mechanism

NRF expects periodic heartbeat to maintain NSSAAF status as REGISTERED.

### Heartbeat Interval
- NRF returns `HeartBeat-Interval` header on registration
- Default: typically 30-60 seconds
- NSSAAF sends PATCH with `nfStatus: REGISTERED` to refresh

### Timeout Behavior
- If heartbeat not received within configured timeout, NRF marks NF as SUSPENDED
- SUSPENDED NFs are not discoverable
- NRF sends NFStatusNotify to subscribers

---

## 8. NFProfile for NSSAAF

See `NSSAAF_NFProfile.md` for complete NFProfile structure with NssaafInfo.

### Key Attributes for NSSAAF

```json
{
  "nfInstanceId": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64",
  "nfType": "NSSAAF",
  "nfStatus": "REGISTERED",
  "nssaafInfo": {
    "supiRanges": [
      { "start": "imsi-001010000000001", "end": "imsi-001010099999999" }
    ],
    "internalGroupIdentifiersRanges": [
      { "start": "group-001", "end": "group-099" }
    ]
  },
  "nfServices": [
    {
      "serviceInstanceId": "nnssaaf-nssaa-1",
      "serviceName": "nnssaaf-nssaa",
      "versions": [{ "apiVersion": "v1" }],
      "scheme": "http",
      "nfServiceStatus": "REGISTERED"
    },
    {
      "serviceInstanceId": "nnssaaf-aiw-1",
      "serviceName": "nnssaaf-aiw",
      "versions": [{ "apiVersion": "v1" }],
      "scheme": "http",
      "nfServiceStatus": "REGISTERED"
    }
  ],
  "plmnList": [{ "mcc": "001", "mnc": "01" }],
  "allowedNfTypes": ["AMF", "AUSF"]
}
```

### NF Services Exposed by NSSAAF

| Service Name | API Version | Description |
|--------------|------------|-------------|
| nnssaaf-nssaa | v1 | NSSAA authentication service (N58) |
| nnssaaf-aiw | v1 | AUSF interworking service (N60) |

---

## 9. Discovery Use Cases

### Discover AMF for NSSAA notification
```
GET /nnrf-disc/v1/nf-instances?target-nf-type=AMF&requester-nf-type=NSSAAF
```

### Discover AUSF for AIW
```
GET /nnrf-disc/v1/nf-instances?target-nf-type=AUSF&requester-nf-type=NSSAAF
```

### Discover NSSAAF (for other NFs)
```
GET /nnrf-disc/v1/nf-instances?target-nf-type=NSSAAF&requester-nf-type=AMF
```

---

## 10. Implementation Checklist

- [ ] NFRegistration on startup with full NFProfile
- [ ] Heartbeat timer with PATCH updates
- [ ] NFDeregister on graceful shutdown
- [ ] Token request before calling other NFs
- [ ] NFDiscovery for AMF, AUSF, UDM
- [ ] Error handling with ProblemDetails
- [ ] Redirection handling (3xx responses)
- [ ] ETag handling for conditional updates
