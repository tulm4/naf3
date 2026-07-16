# NSSAAF Access Token Data Structures

**Source:** TS 29.510 V18.11.0 (2026-03)
**Sections:** §6.3.4.2, §6.1.6.2.28

## Overview

NSSAAF uses OAuth 2.0 Client Credentials flow to obtain access tokens from NRF for:
- Calling other NF services (AMF, AUSF, UDM)
- Service-to-service authentication

---

## 1. AccessTokenRequest

### Content-Type
- `application/x-www-form-urlencoded` (required by RFC 6749)
- Alternative: `application/json` (3GPP extension)

### Request Parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| grant_type | M | Must be "client_credentials" |
| client_id | M | NF Instance ID of NSSAAF (UUID) |
| scope | M | Space-separated list of requested service names |
| requester_nf_type | M | "NSSAAF" |
| target_nf_type | O | Target NF type (e.g., "AMF", "AUSF") |
| target_nf_instance_id | O | Specific target NF instance ID |
| target_nf_set_id | O | Target NF Set ID |
| plmn_id | O | PLMN ID (for roaming) |
| requester_plmn_id | O | Requester PLMN ID (for roaming) |

### Example Request (form-urlencoded)

```
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded

grant_type=client_credentials&client_id=4947a69a-f61b-4bc1-b9da-47c9c5d14b64&scope=nnssaaf-nssaa&requester_nf_type=NSSAAF&target_nf_type=AMF
```

### Example Request (JSON)

```json
POST /oauth2/token
Content-Type: application/json

{
  "grant_type": "client_credentials",
  "client_id": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64",
  "scope": "nnssaaf-nssaa nnssaaf-aiw",
  "requester_nf_type": "NSSAAF",
  "target_nf_type": "AMF"
}
```

---

## 2. AccessTokenResponse

### Success Response (200 OK)

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "scope": "nnssaaf-nssaa"
}
```

### Response Headers

| Header | Value |
|--------|-------|
| Content-Type | application/json |
| Cache-Control | no-store |
| Pragma | no-cache |

### Response Attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| access_token | string | JWT token |
| token_type | string | Always "Bearer" |
| expires_in | integer | Token lifetime in seconds |
| scope | string | Authorized scope (may differ from request) |
| nrf_id | string | NRF identifier (optional) |

---

## 3. AccessTokenErr (Error Response)

### Error Response (400 Bad Request)

```json
{
  "error": "invalid_client",
  "error_description": "Client authentication failed"
}
```

### Error Values

| Error Code | Description |
|------------|-------------|
| invalid_request | Missing or malformed parameter |
| invalid_client | Client authentication failed |
| invalid_grant | Invalid authorization grant |
| unauthorized_client | Client not authorized |
| unsupported_grant_type | Grant type not supported |
| invalid_scope | Invalid scope requested |

---

## 4. JWT Access Token Structure

### Header

```json
{
  "alg": "RS256",
  "typ": "JWT",
  "kid": "key-id-001"
}
```

### Payload Claims

```json
{
  "iss": "http://nrf.operator.com/nnrf-oauth2",
  "sub": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64",
  "aud": "nnssaaf-nssaa",
  "scope": "nnssaaf-nssaa",
  "exp": 1721112000,
  "iat": 1721108400,
  "jti": "unique-token-id",
  "nf_type": "NSSAAF",
  "nf_id": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64",
  "allowed_aud": ["AMF", "AUSF"]
}
```

### Token Validation Claims

| Claim | Description |
|-------|-------------|
| iss | Issuer (NRF URL) |
| sub | Subject (requesting NF instance ID) |
| aud | Audience (requested service) |
| scope | Authorized scopes |
| exp | Expiration time |
| iat | Issued at time |
| jti | JWT ID (for revocation) |
| nf_type | NF type of requester |
| allowed_aud | Allowed target NF types |

---

## 5. Scope Values for NSSAAF

### Scopes NSSAAF Requests

| Scope | Description | When Used |
|-------|-------------|-----------|
| nnssaaf-nssaa | NSSAA Service | Calling AMF for notifications |
| nnssaaf-aiw | AIW Service | Calling AUSF for interworking |
| nnssaa-reauth-notification | Re-auth notification | Sending reauth to AMF |
| nnssaa-revoc-notification | Revocation notification | Sending revocation to AMF |

### Scope Grammar

```
scope       = scope-token *( SP scope-token )
scope-token = 1*( %x21 / %x23-2B / %x2D-5B / %x5D-7E )
            ; Printable ASCII except space and control chars
```

---

## 6. Token Caching Strategy

### Cache Configuration

| Parameter | Value | Description |
|-----------|-------|-------------|
| validityPeriod | 3600 | NRF-specified validity in seconds |
| refreshBuffer | 300 | Refresh 5 minutes before expiry |
| maxCacheSize | 100 | Max cached tokens |

### Token Refresh Flow

```
1. Check token in cache
2. If exists and not expiring soon:
   - Use cached token
3. Else:
   - Request new token from NRF
   - Cache with expiry
   - Use new token
```

---

## 7. Error Handling

### Token Request Errors

| HTTP Status | Error | Action |
|-------------|-------|--------|
| 400 | invalid_request | Fix request parameters |
| 400 | invalid_client | Check client certificate |
| 400 | invalid_grant | Retry with valid grant |
| 400 | invalid_scope | Adjust scope parameters |
| 401 | - | Re-authenticate |
| 403 | - | Check authorization policy |
| 503 | - | Retry with backoff |

### Token Validation Errors

| Error | Description | Action |
|-------|-------------|--------|
| Missing token | No Authorization header | Request token first |
| Expired token | exp claim in past | Refresh token |
| Invalid signature | JWT signature failed | Check NRF public key |
| Invalid issuer | iss mismatch | Verify NRF configuration |
| Invalid audience | aud mismatch | Check target service |
| Insufficient scope | Missing required scope | Request additional scope |

---

## 8. Integration with NSSAAF Flows

### AMF Notification (N58)

When sending NSSAA_ReAuthNotification to AMF:

```bash
# Get token for AMF
POST /oauth2/token
grant_type=client_credentials
client_id={nssaafInstanceId}
scope=nnssaa-reauth-notification
requester_nf_type=NSSAAF
target_nf_type=AMF

# Use token in notification
POST {amfNotificationUri}/nnssaaf-nssaa/v1/reAuthNotifications
Authorization: Bearer {access_token}
```

### AUSF AIW (N60)

When calling AUSF for AIW authentication:

```bash
# Get token for AUSF
POST /oauth2/token
grant_type=client_credentials
client_id={nssaafInstanceId}
scope=nnssaaf-aiw
requester_nf_type=NSSAAF
target_nf_type=AUSF

# Use token in AIW request
GET {ausfUri}/nnssaaf-aiw/v1/authentication-auth-data
Authorization: Bearer {access_token}
```

---

## 9. Implementation Checklist

- [ ] OAuth2 token endpoint client implementation
- [ ] JWT validation library integration
- [ ] Token caching with TTL
- [ ] Automatic token refresh
- [ ] Error handling for 4xx/5xx responses
- [ ] Scope construction for each NF call
- [ ] Certificate-based client auth (mTLS)
- [ ] Token expiry handling
- [ ] Concurrent token requests (avoid thundering herd)
