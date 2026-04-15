---
spec: TS 29.526 v18.7.0 / TS 33.501 v18.10.0
section: §7.2, TS 29.501 §16.3
interface: N58 (AMF-NSSAAF)
service: Nnssaaf_NSSAA
operation: Authenticate, Re-AuthenticationNotification, RevocationNotification
eapMethod: EAP-TLS
aaaProtocol: RADIUS
---

# Nnssaaf_NSSAA API Implementation Design

## 1. Overview

Tài liệu này thiết kế chi tiết implementation của Nnssaaf_NSSAA REST API service — service chính của NSSAAF cho Network Slice-Specific Authentication. Dựa trên TS 29.526 v18.7.0 và TS 33.501 v16.3.

---

## 2. API Specification

### 2.1 Base Configuration

```yaml
Base URL: https://{nssAAF_fqdn}/nnssaaf-nssaa/v1
OpenAPI:  3.0.0
Version:  1.2.1

Security:
  - OAuth2 Client Credentials (scope: nnssaaf-nssaa)
  - No security (for testing only)

Headers required:
  Content-Type: application/json
  Authorization: Bearer {oauth2_token}
  X-Request-ID: {uuid}          # Correlation ID
  X-Forwarded-For: {amf_ip}     # For audit
```

### 2.2 Endpoint: POST /slice-authentications

**Operation ID:** `CreateSliceAuthenticationContext`

**Mục đích:** Tạo resource cho slice authentication context, forward EAP Identity Response đến AAA-S.

#### Request

```json
POST /nnssaaf-nssaa/v1/slice-authentications

{
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "eapIdRsp": "AG5nZXQtaWQAdXNlckBleGFtcGxlLmNvbQA=",
  "amfInstanceId": "amf-instance-001",
  "reauthNotifUri": "https://amf1.operator.com:8080/namf-comm/v1/subscriptions",
  "revocNotifUri": "https://amf1.operator.com:8080/namf-comm/v1/subscriptions"
}
```

#### Field Validation

| Field | Validation | Error |
|-------|-----------|-------|
| gpsi | Required, matches `^5[0-9]{8,14}$` or `^5-[0-9]{8,14}$` (with optional dash per TS 23.003) | 400 InvalidGpsi |
| snssai.sst | Required, 0-255 | 400 InvalidSnssaiSst |
| snssai.sd | Optional, 6 hex chars `[A-Fa-f0-9]{6}` | 400 InvalidSnssaiSd |
| eapIdRsp | Required, Base64 encoded, non-empty | 400 MissingEapPayload |
| amfInstanceId | Optional, non-empty if present | 400 InvalidAmfInstanceId |
| reauthNotifUri | Optional, valid URI scheme (https) | 400 InvalidNotificationUri |
| revocNotifUri | Optional, valid URI scheme (https) | 400 InvalidNotificationUri |

#### Processing Logic

```
1. Validate OAuth2 token (NRF-issued, scope: nnssaaf-nssaa)
2. Parse SliceAuthInfo JSON
3. Validate all required fields
4. Extract AMF identity (from cert CN or amfInstanceId)
5. Resolve AAA server config:
   a. Lookup AAA_CONFIG by (snssai.sst, snssai.sd) from config store
   b. If not found by exact match, match by (snssai.sst, sd=*)
   c. If still not found, match by (sst=*, sd=*) as default
   d. If no config found → 404 AaaServerNotConfigured
6. Create authCtxId: UUIDv7 (time-sortable)
7. Create session record in PostgreSQL:
   - authCtxId, gpsi, snssai, nssaaStatus=PENDING
   - amfInstanceId, amfNotifUri{reauth,revoc}
   - aaaServerConfigId, eapSessionState (serialized)
   - createdAt, expiresAt (now + sessionTimeout)
8. Encode EAP Identity Response into AAA protocol:
   a. If AAA protocol = RADIUS:
      - Build Access-Request
      - User-Name: gpsi
      - Calling-Station-Id: gpsi
      - 3GPP-S-NSSAI (VSA #200): sst + sd
      - EAP-Message: eapIdRsp
      - Message-Authenticator
   b. If AAA protocol = Diameter:
      - Build DER
      - User-Name: gpsi
      - Calling-Station-Id: gpsi
      - 3GPP-S-NSSAI AVP
      - EAP-Message AVP
9. Send to AAA-S (direct or via AAA-P)
10. Wait for AAA response (timeout: aaaTimeout)
    - Access-Challenge / DEA → extract EAP message → return
    - Access-Accept → EAP-Success → return
    - Access-Reject → EAP-Failure → return
    - Timeout → 504 AAA_TIMEOUT
    - Error → 502 AAA_UNREACHABLE
11. Store EAP session state in PostgreSQL + Redis
12. Return 201 Created with SliceAuthContext
```

#### Response 201 Created

```json
HTTP/1.1 201 Created
Location: https://nssAAF.operator.com/nnssaaf-nssaa/v1/slice-authentications/01fr5xg2e3p4q5r6s7t8u9v0w1
Content-Type: application/json

{
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "authCtxId": "01fr5xg2e3p4q5r6s7t8u9v0w1",
  "eapMessage": "AG5nZXQtaWQAdXNlckBleGFtcGxlLmNvbQA="
}
```

**Note:** `eapMessage` trong response là message tiếp theo từ AAA-S để AMF forward đến UE. Nếu là EAP Identity Request đầu tiên, AMF đã nhận trực tiếp từ UE rồi.

#### Error Responses

```json
400 Bad Request:
{
  "type": "https://nssAAF.5gc.npn.org/problem/bad-request",
  "title": "Invalid EAP payload",
  "status": 400,
  "detail": "EAP message is not valid RFC 3748 format",
  "cause": "INVALID_EAP_MESSAGE",
  "invalidParams": [
    { "param": "eapIdRsp", "reason": "Invalid EAP code in message" }
  ]
}

403 Forbidden:
{
  "type": "https://nssAAF.5gc.npn.org/problem/forbidden",
  "title": "Authentication rejected by AAA server",
  "status": 403,
  "detail": "AAA server rejected the initial authentication request",
  "cause": "AAA_AUTH_REJECTED"
}

404 Not Found:
{
  "type": "https://nssAAF.5gc.npn.org/problem/not-found",
  "title": "AAA server not configured for this S-NSSAI",
  "status": 404,
  "detail": "No AAA server configuration found for S-NSSAI sst=1, sd=000001",
  "cause": "AAA_SERVER_NOT_CONFIGURED"
}

502 Bad Gateway:
{
  "type": "https://nssAAF.5gc.npn.org/problem/bad-gateway",
  "title": "Cannot reach AAA server",
  "status": 502,
  "detail": "AAA server at 192.168.1.100:1812 is unreachable",
  "cause": "AAA_UNREACHABLE"
}

503 Service Unavailable:
{
  "type": "https://nssAAF.5gc.npn.org/problem/service-unavailable",
  "title": "AAA server temporarily unavailable",
  "status": 503,
  "detail": "AAA server responded with overload or maintenance",
  "cause": "AAA_UNAVAILABLE"
}

504 Gateway Timeout:
{
  "type": "https://nssAAF.5gc.npn.org/problem/gateway-timeout",
  "title": "AAA server response timeout",
  "status": 504,
  "detail": "AAA server did not respond within 10 seconds",
  "cause": "AAA_TIMEOUT"
}
```

### 2.3 Endpoint: PUT /slice-authentications/{authCtxId}

**Operation ID:** `ConfirmSliceAuthentication`

**Mục đích:** Advance EAP round. AMF forward EAP response từ UE.

#### Request

```json
PUT /nnssaaf-nssaa/v1/slice-authentications/01fr5xg2e3p4q5r6s7t8u9v0w1

{
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "eapMessage": "AG5uZXh0LWlkQHVzZXIuZXhhbXBsZS5jb20="
}
```

#### Processing Logic

```
1. Validate authCtxId format
2. Load session from PostgreSQL (or Redis cache):
   - If not found → 404 Not Found
   - If expired (now > expiresAt) → 410 Gone
   - If status = EAP_SUCCESS or EAP_FAILURE → 409 Conflict (already completed)
3. Validate gpsi, snssai match session
4. Increment eapRound counter:
   - If eapRound >= maxEapRounds (20) → 400 MaxEapRoundsExceeded
5. Forward EAP message to AAA-S (same as POST step 8-10)
6. Update session state in PostgreSQL + Redis
7. Return SliceAuthConfirmationResponse
```

#### Response 200 OK

```json
// Multi-round: EAP challenge continues
{
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" },
  "eapMessage": "AG5leHQtaWQAdXNlckBleGFtcGxlLmNvbQA=",
  "authResult": null
}

// Final: EAP Success
{
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" },
  "eapMessage": "AG5zZXNzaW9uLWtleS1kYXRh",
  "authResult": "EAP_SUCCESS"
}

// Final: EAP Failure
{
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" },
  "eapMessage": null,
  "authResult": "EAP_FAILURE"
}
```

### 2.4 Idempotency

**POST /slice-authentications** không idempotent theo nghĩa truyền thống vì mỗi request tạo một session mới. Tuy nhiên, AMF có thể retry nếu response bị mất (không nhận được Location header).

**PUT /slice-authentications/{authCtxId}** idempotent vì AMF có thể retry cùng authCtxId với cùng EAP message. Implementation phải detect duplicate request (same authCtxId + same eapMessage hash) và trả về cached response.

### 2.5 State Persistence

**PostgreSQL** (persistent, durable):
```sql
CREATE TABLE slice_auth_sessions (
    auth_ctx_id    VARCHAR(64) PRIMARY KEY,
    gpsi           VARCHAR(32) NOT NULL,
    snssai_sst     INTEGER NOT NULL,
    snssai_sd      VARCHAR(8),
    amf_instance_id VARCHAR(64),
    reauth_notif_uri TEXT,
    revoc_notif_uri  TEXT,
    aaa_config_id   UUID NOT NULL,
    eap_session_state BYTEA NOT NULL,  -- serialized
    eap_rounds      INTEGER DEFAULT 0,
    nssaa_status    VARCHAR(20) DEFAULT 'PENDING',
    auth_result     VARCHAR(20),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_sessions_gpsi_snssai ON slice_auth_sessions(gpsi, snssai_sst, snssai_sd);
CREATE INDEX idx_sessions_nssaa_status ON slice_auth_sessions(nssaa_status) WHERE nssaa_status = 'PENDING';
CREATE INDEX idx_sessions_expires_at ON slice_auth_sessions(expires_at);
```

**Redis** (hot cache, TTL 5 min):
```
Key: nssaa:session:{authCtxId}
Value: { eapSessionState, nssaaStatus, aaaConfigId }
TTL: 300s
```

### 2.6 NssaaStatus State Machine

```
                    ┌──────────────────┐
                    │   NOT_EXECUTED   │
                    │  (before POST)   │
                    └────────┬─────────┘
                             │ POST /slice-auth
                             ▼
                    ┌──────────────────┐
                    │     PENDING      │◄──────────────┐
                    │  (after POST,    │               │
                    │   before final   │               │
                    │   EAP result)    │               │
                    └────────┬─────────┘               │
                             │                        │
              ┌──────────────┼──────────────┐         │
              │              │              │         │
              ▼              │              ▼         │
    ┌──────────────────┐   │   ┌──────────────────┐ │
    │    EAP_SUCCESS    │   │   │   EAP_FAILURE    │ │
    │   (terminal)      │   │   │   (terminal)     │ │
    └──────────────────┘   │   └──────────────────┘ │
                            │                        │
                            │ timeout / retry       │
                            └────────────────────────┘
                             (back to NOT_EXECUTED,
                              AMF stores in UE Context)
```

**State transitions triggered by:**
- `NOT_EXECUTED → PENDING`: POST /slice-authentications
- `PENDING → EAP_SUCCESS`: AAA-S returns Access-Accept / DEA with EAP-Success
- `PENDING → EAP_FAILURE`: AAA-S returns Access-Reject / DEA with EAP-Failure
- `PENDING → NOT_EXECUTED`: Session timeout, AMF stores result in UE Context

### 2.7 Callback Notifications (Server-Sent)

#### Re-AuthenticationNotification

NSSAAF POST đến `reauthNotifUri` khi AAA-S trigger reauth (TS 33.501 §16.4):

```json
POST https://amf1.operator.com:8080/namf-comm/v1/subscriptions

Headers:
  Content-Type: application/json
  X-Request-ID: {uuid}

{
  "notifType": "SLICE_RE_AUTH",
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "supi": "imu-208046000000001"
}
```

**Trigger:** NSSAAF nhận RADIUS Disconnect-Request hoặc Diameter ASR từ AAA-S, validated thành công.

**AMF expected response:** `204 No Content`

**AMF expected action:** Trigger NSSAA procedure (§4.2.9.2) cho UE + S-NSSAI này.

#### RevocationNotification

NSSAAF POST đến `revocNotifUri` khi AAA-S revoke authorization (TS 33.501 §16.5):

```json
POST https://amf1.operator.com:8080/namf-comm/v1/subscriptions

{
  "notifType": "SLICE_REVOCATION",
  "gpsi": "5-208046000000001",
  "snssai": {
    "sst": 1,
    "sd": "000001"
  },
  "supi": "imu-208046000000001"
}
```

**AMF expected response:** `204 No Content`

**AMF expected action:**
- Remove S-NSSAI from Allowed NSSAI
- If no S-NSSAI left → UE Deregistration
- PDU Session Release cho S-NSSAI bị revoke

### 2.8 Timeout & Retry Configuration

| Parameter | Value | Source |
|-----------|-------|--------|
| EAP Round Timeout | 30s | Config: `eap.round_timeout_seconds` |
| Max EAP Rounds | 20 | Config: `eap.max_rounds` |
| Session TTL | 5 min | Config: `session.ttl_seconds` |
| AAA Response Timeout | 10s | Config: `aaa.response_timeout_ms` |
| Notification Timeout | 5s | Config: `notification.timeout_ms` |
| Notification Retry | 3 attempts | Config: `notification.max_retries` |

---

## 3. Implementation Architecture

### 3.1 Request Handling Pipeline

```
HTTP/2 Connection (TLS 1.3 terminated at Istio sidecar)
         │
         ▼
Istio Sidecar (mTLS, rate limiting, observability)
         │
         ▼
NSSAAF SBI Gateway (Java/Go/Rust)
  │
  ├─ Request Validation Layer
  │    ├─ OAuth2 token validation (JWT)
  │    ├─ JSON schema validation
  │    └─ Business rule validation
  │
  ├─ Routing Layer
  │    ├─ POST /slice-authentications → CreateSliceAuthHandler
  │    └─ PUT  /slice-authentications/{id} → ConfirmSliceAuthHandler
  │
  ├─ Business Logic Layer
  │    ├─ AaaConfigResolver
  │    ├─ EapSessionManager
  │    ├─ AaaProtocolEncoder
  │    └─ NotificationDispatcher
  │
  └─ Persistence Layer
       ├─ PostgresSessionRepository (async write)
       └─ RedisSessionCache (async write)

         │
         ▼
Async: EapRoundProcessor (background)
  │
  ├─ AaaClient (RADIUS/Diameter)
  │    ├─ RADIUS: async UDP
  │    └─ Diameter: async SCTP/TCP
  │
  └─ SessionStateUpdater
       ├─ Write to PostgreSQL
       └─ Update Redis cache
```

### 3.2 Database Schema (PostgreSQL)

```sql
-- AAA Server Configuration (static, loaded at startup)
CREATE TABLE aaa_server_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snssai_sst      INTEGER NOT NULL,
    snssai_sd       VARCHAR(8),  -- NULL means wildcard
    protocol        VARCHAR(10) NOT NULL CHECK (protocol IN ('RADIUS', 'DIAMETER')),
    aaa_server_host VARCHAR(255) NOT NULL,
    aaa_server_port INTEGER NOT NULL,
    aaa_proxy_host  VARCHAR(255),  -- NULL if no proxy
    aaa_proxy_port  INTEGER,
    shared_secret   TEXT NOT NULL,  -- encrypted
    priority        INTEGER DEFAULT 100,
    weight          INTEGER DEFAULT 1,
    enabled         BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (snssai_sst, snssai_sd)
);

CREATE INDEX idx_aaa_config_snssai ON aaa_server_configs(snssai_sst, snssai_sd);

-- Session State (high-volume, partitioned by month)
CREATE TABLE slice_auth_sessions (
    auth_ctx_id        VARCHAR(64) PRIMARY KEY,
    gpsi               VARCHAR(32) NOT NULL,
    snssai_sst         INTEGER NOT NULL,
    snssai_sd          VARCHAR(8),
    amf_instance_id    VARCHAR(64),
    amf_ip             INET,
    reauth_notif_uri   TEXT,
    revoc_notif_uri    TEXT,
    aaa_config_id      UUID NOT NULL REFERENCES aaa_server_configs(id),
    eap_session_state  BYTEA NOT NULL,
    eap_rounds         INTEGER DEFAULT 0,
    eap_last_nonce     VARCHAR(64),  -- for duplicate detection
    nssaa_status       VARCHAR(20) NOT NULL,
    auth_result        VARCHAR(20),
    failure_reason     TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at         TIMESTAMPTZ NOT NULL,
    completed_at       TIMESTAMPTZ
) PARTITION BY RANGE (created_at);

-- Create monthly partitions
CREATE TABLE slice_auth_sessions_2025_01 PARTITION OF slice_auth_sessions
    FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');

-- Audit Log (append-only, immutable)
CREATE TABLE nssaa_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    auth_ctx_id     VARCHAR(64),
    gpsi_hash       VARCHAR(64) NOT NULL,  -- SHA-256 of GPSI (privacy)
    snssai_sst      INTEGER,
    snssai_sd       VARCHAR(8),
    amf_instance_id VARCHAR(64),
    action          VARCHAR(30) NOT NULL,  -- POST, PUT, NOTIF_SENT, NOTIF_ACK
    nssaa_status    VARCHAR(20),
    error_code      INTEGER,
    client_ip       INET,
    request_id      VARCHAR(64),
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_audit_gpsi ON nssaa_audit_log(gpsi_hash, created_at DESC);
CREATE INDEX idx_audit_ctx ON nssaa_audit_log(auth_ctx_id, created_at DESC);
```

### 3.3 Redis Cache Structure

```
# Session hot cache
nssaa:session:{authCtxId}
  → Hash: {
      status: "PENDING",
      gpsi: "5-208046000000001",
      snssai: "1:000001",
      aaaConfigId: "uuid",
      eapRounds: 2,
      updatedAt: "2025-01-01T12:00:00Z"
    }
  → TTL: 300s

# Idempotency cache
nssaa:idempotency:{authCtxId}:{eapMessageHash}
  → String: {cachedResponseJson}
  → TTL: 3600s

# AAA server health
nssaa:aaa:health:{aaaServerId}
  → String: "UP" | "DOWN"
  → TTL: 30s

# Rate limiting
nssaa:ratelimit:amf:{amfInstanceId}
  → Counter (sliding window 1s)
  → TTL: 5s

nssaa:ratelimit:gpsi:{gpsiHash}
  → Counter
  → TTL: 60s
```

---

## 4. Acceptance Criteria

| # | Criteria | Implementation Note |
|---|----------|-------------------|
| AC1 | POST tạo SliceAuthContext với authCtxId UUIDv7 | UUIDv7 for time-sortability |
| AC2 | PUT advance EAP round, trả về authResult khi done | State machine validation |
| AC3 | GPSI validation theo pattern `^5[0-9]{8,14}$` | Required field, 400 on invalid |
| AC4 | S-NSSAI routing đến đúng AAA config | 3-level match: exact, sst-only, default |
| AC5 | RADIUS Access-Request chứa 3GPP-S-NSSAI VSA #200 | sst=3 bytes, sd=3 bytes (optional) |
| AC6 | Idempotent PUT: duplicate request → cached response | Hash(eapMessage) as idempotency key |
| AC7 | Session timeout: PENDING session expired sau TTL | Background scanner, cleanup job |
| AC8 | Re-AuthNotification POST đến AMF callback URI | Retry 3x, async non-blocking |
| AC9 | RevocationNotification POST đến AMF callback URI | Retry 3x, async non-blocking |
| AC10 | Audit log ghi mọi operation, GPSI hashed | Immutable append-only table |
