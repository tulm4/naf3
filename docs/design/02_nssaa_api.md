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
| gpsi | Required, matches `^5[0-9]{8,14}$` (TS 29.571 §5.4.4.61) | 400 InvalidGpsi |
| snssai.sst | Required, 0-255 | 400 InvalidSnssaiSst |
| snssai.sd | Optional, 6 hex chars `[A-Fa-f0-9]{6}` | 400 InvalidSnssaiSd |
| eapIdRsp | Required, Base64 encoded, non-empty | 400 MissingEapPayload |
| amfInstanceId | Optional, non-empty if present | 400 InvalidAmfInstanceId |
| reauthNotifUri | Optional, valid URI scheme (https) | 400 InvalidNotificationUri |
| revocNotifUri | Optional, valid URI scheme (https) | 400 InvalidNotificationUri |

#### Processing Logic

```
1. Validate OAuth2 token (NRF-issued, scope: nnssaaf-nssaa)
   - Reject if token expired or missing scope
2. Parse SliceAuthInfo JSON
3. Validate all required fields:
   - gpsi: regex ^5[0-9]{8,14}$ (TS 29.571 §5.4.4.61)
   - snssai.sst: 0-255
   - snssai.sd: 6 hex chars or omitted
   - eapIdRsp: non-empty, valid Base64, decodes to valid EAP Response packet
   - reauthNotifUri / revocNotifUri: valid https:// URI if present, null if absent
4. Extract AMF identity (from client cert CN or amfInstanceId header)
5. Check circuit breaker for AAA target:
   - If OPEN → 503 Service Unavailable immediately
   - If HALF_OPEN → allow 1 probe request
6. Check rate limit (per-GPSI: 10 req/min, per-AMF: 1000 req/sec)
   - If exceeded → 429 Too Many Requests
7. Resolve AAA server config:
   a. Lookup AAA_CONFIG by (snssai.sst, snssai.sd) from config store
   b. If not found by exact match, match by (snssai.sst, sd=*)
   c. If still not found, match by (sst=*, sd=*) as default
   d. If no config found → 404 AaaServerNotConfigured
8. Create authCtxId: UUIDv7 (time-sortable, monotonic within node)
9. Create session record in PostgreSQL:
   - authCtxId, gpsi, snssai, nssaaStatus=PENDING
   - amfInstanceId, amfNotifUri{reauth,revoc} (may be null)
   - aaaServerConfigId, eapSessionState (serialized JSON)
   - createdAt, expiresAt (now + sessionTimeout)
10. Encode EAP Identity Response into AAA protocol:
    a. If AAA protocol = RADIUS:
       - Build Access-Request (Code=1)
       - Message-Authenticator: zeroed initially, computed after all attrs
       - User-Name: gpsi
       - Calling-Station-Id: gpsi
       - 3GPP-S-NSSAI (VSA #26, Vendor-Id 10415, Vendor-Type 200): sst + sd
       - EAP-Message: eapIdRsp (raw EAP Response bytes)
       - NAS-IP-Address, NAS-Port-Type: filled from config
       - Message-Authenticator: HMAC-MD5(secret, packet) — RFC 3579 §3.2
    b. If AAA protocol = Diameter:
       - Build DER (Command-Code=268, Application-Id=5)
       - Session-Id AVP
       - Auth-Application-Id: 5 (Diameter EAP)
       - Auth-Request-Type: 1 (AUTHORIZE_AUTHENTICATE)
       - User-Name: gpsi
       - 3GPP-S-NSSAI AVP (code 310): sst + sd
       - EAP-Payload AVP (code 209): eapIdRsp bytes
11. Send to AAA-S (direct or via AAA-P proxy):
    - Apply retry with exponential backoff: 0ms, 100ms, 200ms
    - After 3 retries exhausted → 504 AAA_TIMEOUT
12. Decode AAA response:
    a. RADIUS Access-Challenge → extract first EAP-Message attr → return eapMessage
    b. RADIUS Access-Accept → nssaaStatus=EAP_SUCCESS, no eapMessage
    c. RADIUS Access-Reject → nssaaStatus=EAP_FAILURE, no eapMessage
    d. Diameter DEA result-code=2001 → EAP_SUCCESS
    e. Diameter DEA result-code=4001 → EAP_FAILURE
    f. Any AAA error / timeout → circuit breaker record failure
13. On terminal result (Success/Failure):
    - Update PostgreSQL: nssaaStatus = EAP_SUCCESS | EAP_FAILURE
    - Delete from Redis (no active session)
14. On continue (EAP Challenge):
    - Store EAP session state in PostgreSQL + Redis (TTL = sessionTTL)
    - Set nssaaStatus = PENDING
15. Return 201 Created with SliceAuthContext:
    - authResult: null (multi-round in progress)
    - eapMessage: Base64(EAP-Request from AAA-S) if Challenge
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
1. Validate authCtxId format (UUIDv7 regex)
2. Load session from Redis cache first:
   - If found in Redis → use cached state
   - If not found → load from PostgreSQL → populate Redis (TTL = sessionTTL)
3. If session not found in either → 404 Not Found
4. If session expired (now > expiresAt) → 410 Gone
5. If session status = EAP_SUCCESS or EAP_FAILURE → 409 Conflict
6. Validate gpsi and snssai match session fields
7. Check circuit breaker for AAA target → if OPEN → 503 Service Unavailable
8. Idempotent retry detection:
   - Compute sha256(eapMsgPayload)
   - If hash == session.LastNonce AND session.CachedResponse != nil:
     → Return cached response immediately (no AAA call)
9. Validate EAP payload (same rules as POST step 3)
10. Increment eapRound counter:
    - If eapRound >= maxEapRounds (20) → 400 MaxEapRoundsExceeded
11. Forward EAP message to AAA-S (same encoding as POST step 10):
    - Update session.ExpectedId = eapMsg.Id + 1
    - Update session.Rounds++
12. Decode AAA response (same as POST step 12)
13. Update session state:
    - LastActivity = now
    - LastNonce = sha256(eapMsgPayload)
    - CachedResponse = AAA response bytes
14. On terminal result (Success/Failure):
    - Transition state: SessionStateEapExchange → SessionStateDone | SessionStateFailed
    - Update PostgreSQL: nssaaStatus = EAP_SUCCESS | EAP_FAILURE
    - Delete from Redis
    - Return authResult set, eapMessage null
15. On continue (EAP Challenge):
    - Transition state: SessionStateEapExchange → SessionStateCompleting
    - Update PostgreSQL + Redis
    - Return authResult = null, eapMessage = Base64(EAP-Request from AAA-S)
16. On timeout (no AAA response within roundTimeout):
    - Transition state → SessionStateTimeout
    - Return 504 Gateway Timeout
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
    │   (terminal for  │   │   │   (terminal)     │ │
    │    initial auth) │   │   └──────────────────┘ │
    └────────┬─────────┘   │                        │
             │              │                        │
             │ AAA-S Reauth │                        │
             │    Request   │                        │
             ▼              │                        │
    ┌──────────────────┐   │                        │
    │     PENDING      │───┘                        │
    │  (re-auth in    │                             │
    │   progress)      │                             │
    └──────────────────┘                             │
                                                    │
             timeout / retry (retry next reg) ◄───────┘
             (back to NOT_EXECUTED,
              AMF stores in UE Context)
```

**State transitions triggered by:**
- `NOT_EXECUTED → PENDING`: POST /slice-authentications (initial NSSAA)
- `PENDING → EAP_SUCCESS`: AAA-S returns Access-Accept / DEA with EAP-Success
- `PENDING → EAP_FAILURE`: AAA-S returns Access-Reject / DEA with EAP-Failure
- `PENDING → NOT_EXECUTED`: Session timeout, AMF stores result in UE Context
- `EAP_SUCCESS → PENDING`: AAA-S Reauth Request (TS 23.502 §4.2.9.3)

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

**Processing Logic:**
```
1. NSSAAF receives Disconnect-Request (RADIUS CoA) or ASR (Diameter) from AAA-S
2. Validate message authenticator / signature
3. Look up active slice-auth session by (gpsi, snssai) in Redis
3a. NSSAAF → UDM: Nudm_UECM_Get(GPSI) → get AMF ID(s)
    NOTE: If AMF not registered → procedure stops here (log warning, return 204)
4. If session found:
   a. Transition NssaaStatus from EAP_SUCCESS → PENDING
   b. POST to reauthNotifUri (from SliceAuthContext)
   c. AMF acknowledges with 204, then triggers new NSSAA via POST /slice-authentications
   d. NSSAAF processes new EAP exchange, resolves to SUCCESS or FAILURE
   e. Update NssaaStatus in DB: PENDING → EAP_SUCCESS | EAP_FAILURE
5. If no active session found:
   a. Log warning: "reauth for inactive session"
   b. Return 204 (idempotent — AMF may have already cleaned up)
6. Retry up to 3 times on 5xx, backoff 1s/2s/4s
7. On persistent failure: log error, emit metric nssaa.notification.reauth.failed
```

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

**Processing Logic:**
```
1. NSSAAF receives Revocation-Request (RADIUS DM) or STR (Diameter) from AAA-S
2. Validate message authenticator / signature
3. Look up all active slice-auth sessions for (gpsi, snssai) in Redis/DB
3a. NSSAAF → UDM: Nudm_UECM_Get(GPSI) → get AMF ID(s)
    NOTE: If AMF not registered → procedure stops here (log warning, return 204)
    NOTE: NSSAAF may send ACK to AAA-S before receiving AMF/Nudm response (per TS 23.502)
4. For each matching session:
   a. Transition NssaaStatus from EAP_SUCCESS → EAP_FAILURE
   b. POST to revocNotifUri (from SliceAuthContext)
   c. AMF acknowledges with 204, removes S-NSSAI from Allowed NSSAI
   d. Delete session from Redis; mark EAP_SUCCESS → revoked in DB
5. If no active sessions found:
   a. Log info: "revocation for already-cleaned session"
   b. Return 204 (idempotent)
6. Retry up to 3 times on 5xx, backoff 1s/2s/4s
7. On persistent failure:
   a. Log error: "revocation notification failed after retries"
   b. Emit metric nssaa.notification.revocation.failed
   c. Store in dead-letter queue (DLQ) for manual intervention
```

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
HTTP/2 Connection (TLS 1.3 terminated at HTTP Gateway — separate Deployment)
         │
         ▼
NSSAAF HTTP Gateway (Envoy, separate Deployment, N replicas)
  · TLS 1.3 termination
  · Routes N58/N60 to Biz Pods via ClusterIP
  · Rate limiting, circuit breaking, observability
         │
         ▼
NSSAAF Biz Pod (Go, N replicas — stateless)
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
  │    ├─ AaaProtocolEncoder (RADIUS/Diameter — encode/decode here)
  │    └─ NotificationDispatcher
  │
  └─ Persistence Layer
       ├─ PostgresSessionRepository (async write)
       └─ RedisSessionCache (async write)

         │
         ▼
Async: EapRoundProcessor (background)
  │
  ├─ AaaClient (HTTP to AAA Gateway)
  │    └─ POST /aaa/forward → AAA Gateway → RADIUS/Diameter to AAA-S
  │
  └─ SessionStateUpdater
       ├─ Write to PostgreSQL
       └─ Update Redis cache

**Note:** After Phase R (3-Component Refactor), the "AaaClient" is an HTTP client that forwards raw transport bytes to the AAA Gateway. The AAA Gateway (separate Deployment, 2 replicas, active-standby) handles the raw socket I/O to AAA-S. See `docs/design/01_service_model.md` §5.4 and `docs/roadmap/PHASE_Refactor_3Component.md`.
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
