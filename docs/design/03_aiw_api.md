---
spec: TS 29.526 v18.7.0 / TS 33.501 v18.10.0 / TS 33.501 §I.2.2.2
section: §7.3, TS 29.501 §I.2.2.2
interface: N60 (AUSF-NSSAAF)
service: Nnssaaf_AIW
operation: Authenticate
eapMethod: EAP-TLS / EAP-TTLS
aaaProtocol: RADIUS / Diameter
---

# Nnssaaf_AIW API Implementation Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, NSSAAF is split into HTTP Gateway, Biz Pod, and AAA Gateway. See `docs/design/01_service_model.md` §5.4 for the architecture overview.

Nnssaaf_AIW là dịch vụ NSSAAF dùng cho SNPN (Standalone Non-Public Network) authentication với Credentials Holder sử dụng AAA Server. Khác với Nnssaaf_NSSAA (dùng GPSI, consumer là AMF), Nnssaaf_AIW dùng **SUPI** và consumer là **AUSF**.

Tài liệu này thiết kế chi tiết implementation của Nnssaaf_AIW theo TS 29.526 v18.7.0 và TS 33.501 §I.2.2.2.

---

## 2. Sự khác biệt giữa Nnssaaf_AIW và Nnssaaf_NSSAA

| Aspect | Nnssaaf_NSSAA | Nnssaaf_AIW |
|--------|---------------|-------------|
| **Consumer NF** | AMF | AUSF |
| **Subscriber ID** | GPSI | SUPI |
| **Authentication** | Slice-specific (per S-NSSAI) | SNPN primary auth (per SUPI) |
| **Interface** | N58 | N60 |
| **Trigger** | Registration with NSSAA-required S-NSSAI | SNPN access with Credentials Holder |
| **GPSI involvement** | Required | Not used |
| **MSK output** | Not specified | **Yes** (RFC 5216) |
| **pvsInfo output** | Not used | **Yes** (Privacy Violating Servers) |
| **Re-auth/Revocation** | Yes (AAA-S triggered) | Not in scope |
| **AAA Protocol** | RADIUS/Diameter | RADIUS/Diameter |

---

## 3. API Specification

### 3.1 Base Configuration

```yaml
Base URL: https://{nssAAF_fqdn}/nnssaaf-aiw/v1
OpenAPI:  3.0.0
Version:  1.1.0

Security: OAuth2 Client Credentials (scope: nnssaaf-aiw)
```

### 3.2 Endpoint: POST /authentications

**Operation ID:** `CreateAuthenticationContext`

**Trigger:** AUSF cần xác thực Credentials Holder user trong SNPN sử dụng AAA Server.

#### Request

```json
POST /nnssaaf-aiw/v1/authentications

{
  "supi": "imu-208046000000001",
  "eapIdRsp": "AG5nZXQtaWQAdXNlckBleGFtcGxlLmNvbQA=",
  "ttlsInnerMethodContainer": null,
  "supportedFeatures": "3GPP-R18-AIW"
}
```

#### Field Validation

| Field | Validation | Error |
|-------|-----------|-------|
| supi | Required, matches `^imu-[0-9]{15}$` | 400 InvalidSupi |
| eapIdRsp | Optional, Base64 encoded if present | 400 InvalidEapPayload |
| ttlsInnerMethodContainer | Optional, Base64 encoded | 400 InvalidPayload |
| supportedFeatures | Optional, non-empty if present | 400 InvalidFeatures |

#### Processing Logic

```
1. Validate OAuth2 token (NRF-issued, scope: nnssaaf-aiw)
   - Reject if token expired or missing scope
2. Parse AuthInfo JSON
3. Validate all required fields:
   - supi: regex ^imu-[0-9]{15}$
   - eapIdRsp: if present, valid Base64, decodes to valid EAP Response packet
   - ttlsInnerMethodContainer: if present, valid Base64
   - supportedFeatures: if present, non-empty string
4. Check circuit breaker for AAA target:
   - If OPEN → 503 Service Unavailable immediately
   - If HALF_OPEN → allow 1 probe request
5. Check rate limit (per-SUPI: 10 req/min, per-AUSF: 1000 req/sec)
   - If exceeded → 429 Too Many Requests
6. Resolve AAA server config:
   a. Lookup AAA_CONFIG by SUPI range (operator-configured mapping)
   b. If not found by range → use default AAA_CONFIG
   c. If no default configured → 404 AaaServerNotConfigured
7. Create authCtxId: UUIDv7 (time-sortable, monotonic within node)
8. Create session record in PostgreSQL:
   - authCtxId, supi, nssaaStatus=PENDING
   - aaaServerConfigId, eapSessionState (serialized JSON)
   - createdAt, expiresAt (now + sessionTimeout)
   - supportedFeatures (echoed back in response)
9. Determine whether initial EAP-Request or EAP exchange is needed:
   a. If eapIdRsp is null: build EAP Identity Request internally
      → return 201 with eapMessage = Base64(EAP-Identity-Request)
   b. If eapIdRsp is present: encode to AAA protocol and send to AAA-S
10. Encode to AAA protocol (RADIUS or Diameter):
    a. If AAA protocol = RADIUS:
       - Build Access-Request (Code=1)
       - User-Name: supi
       - Message-Authenticator: HMAC-MD5(secret, packet) — RFC 3579 §3.2
       - EAP-Message: eapIdRsp bytes
       - (3GPP-S-NSSAI AVP: not used for AIW; per-SUPI not per-slice)
    b. If AAA protocol = Diameter:
       - Build DER (Command-Code=268, Application-Id=5)
       - Session-Id, Auth-Application-Id, Auth-Request-Type
       - User-Name: supi
       - EAP-Payload AVP
11. Send to AAA-S via HTTP to AAA Gateway:
    - Biz Pod sends raw AAA bytes to AAA Gateway via HTTP POST /aaa/forward
    - AAA Gateway receives raw RADIUS/Diameter packet and forwards to AAA-S
    - Apply retry with exponential backoff: 0ms, 100ms, 200ms
    - After 3 retries exhausted → 504 AAA_TIMEOUT
12. Receive AAA response via HTTP from AAA Gateway:
    a. RADIUS Access-Challenge → extract EAP-Message → return eapMessage
    b. RADIUS Access-Accept → EAP-Success
       - Extract MSK from MSK VSA (Vendor-Id=VSA, Vendor-Type=TBD)
       - Extract pvsInfo if present
    c. RADIUS Access-Reject → EAP-Failure
    d. Diameter DEA result-code=2001 → EAP_SUCCESS (extract KeyingMaterial AVP = MSK)
    e. Diameter DEA result-code=4001 → EAP_FAILURE
13. On terminal result (Success/Failure):
    - Update PostgreSQL: nssaaStatus = EAP_SUCCESS | EAP_FAILURE
    - Delete from Redis (no active session)
    - If Success: return msk, pvsInfo in response
14. On continue (EAP Challenge):
    - Store EAP session state in PostgreSQL + Redis (TTL = sessionTTL)
    - Set nssaaStatus = PENDING
15. Return 201 Created with AuthContext:
    - authResult: null (multi-round in progress)
    - eapMessage: Base64(EAP-Request from AAA-S) if Challenge
    - ttlsInnerMethodContainer: echoed from request if present
    - supportedFeatures: echoed from request
```

#### Response 201 Created

```json
HTTP/1.1 201 Created
Location: https://nssAAF.operator.com/nnssaaf-aiw/v1/authentications/01fr5xg2e3p4q5r6s7t8u9v0w2

{
  "supi": "imu-208046000000001",
  "authCtxId": "01fr5xg2e3p4q5r6s7t8u9v0w2",
  "eapMessage": "AG5uZXh0LWlkQHVzZXIuZXhhbXBsZS5jb20=",
  "ttlsInnerMethodContainer": null,
  "supportedFeatures": "3GPP-R18-AIW"
}
```

**Note:** Nếu `eapIdRsp` không được gửi (đầu tiên), `eapMessage` là initial EAP Identity Request để AUSF forward đến UE.

### 3.3 Endpoint: PUT /authentications/{authCtxId}

**Operation ID:** `ConfirmAuthentication`

**Trigger:** AUSF forward EAP response từ UE.

#### Request

```json
PUT /nnssaaf-aiw/v1/authentications/01fr5xg2e3p4q5r6s7t8u9v0w2

{
  "supi": "imu-208046000000001",
  "eapMessage": "AG5uZXh0LWlkQHVzZXIuZXhhbXBsZS5jb20=",
  "supportedFeatures": "3GPP-R18-AIW"
}
```

#### Processing Logic

```
1. Validate authCtxId format (UUIDv7 regex)
2. Load session from Redis cache first:
   - If found in Redis → use cached state
   - If not found → load from PostgreSQL → populate Redis (TTL = sessionTTL)
3. If session not found in either → 404 AuthContextNotFound
4. If session expired (now > expiresAt) → 410 AuthContextExpired
5. If session status = EAP_SUCCESS or EAP_FAILURE → 409 AuthAlreadyCompleted
6. Validate supi matches session.supi field
7. Validate eapMessage: non-empty, valid Base64, decodes to valid EAP Response packet
8. Check circuit breaker for AAA target → if OPEN → 503 Service Unavailable
9. Idempotent retry detection:
   - Compute sha256(eapMsgPayload)
   - If hash == session.LastNonce AND session.CachedResponse != nil:
     → Return cached response immediately (no AAA call)
10. Encode EAP message to AAA protocol (same as POST step 10)
11. Send to AAA-S via HTTP to AAA Gateway:
    - Biz Pod sends raw AAA bytes to AAA Gateway via HTTP POST /aaa/forward
    - AAA Gateway forwards raw packet to AAA-S
    - Update session.ExpectedId = eapMsg.Id + 1
    - Update session.Rounds++
12. Receive AAA response via HTTP from AAA Gateway:
    a. RADIUS Access-Challenge → extract EAP-Message → return eapMessage, authResult=null
    b. RADIUS Access-Accept → EAP-Success
       - Extract MSK from MSK VSA
       - Extract pvsInfo array if present
    c. RADIUS Access-Reject → EAP-Failure
    d. Diameter DEA result-code=2001 → EAP_SUCCESS (extract KeyingMaterial AVP = MSK)
    e. Diameter DEA result-code=4001 → EAP_FAILURE
13. Update session state:
    - LastActivity = now
    - LastNonce = sha256(eapMsgPayload)
    - CachedResponse = AAA response bytes
14. On terminal result (Success/Failure):
    - Transition state: SessionStateEapExchange → SessionStateDone | SessionStateFailed
    - Update PostgreSQL: nssaaStatus = EAP_SUCCESS | EAP_FAILURE
    - Delete from Redis
    - Return authResult set, eapMessage=null, msk/pvsInfo populated on Success
15. On continue (EAP Challenge):
    - Transition state: SessionStateEapExchange → SessionStateCompleting
    - Update PostgreSQL + Redis
    - Return authResult=null, eapMessage=Base64(EAP-Request from AAA-S)
16. On timeout (no AAA response within roundTimeout):
    - Transition state → SessionStateTimeout
    - Return 504 Gateway Timeout
```

#### Response 200 OK

```json
// Multi-round continues
{
  "supi": "imu-208046000000001",
  "eapMessage": "AG5jaGFsbGVuZ2UAdXNlcjEA",
  "authResult": null,
  "pvsInfo": null,
  "msk": null,
  "supportedFeatures": "3GPP-R18-AIW"
}

// Final: EAP Success (with MSK)
{
  "supi": "imu-208046000000001",
  "eapMessage": "AG5zZXNzaW9uLWtleS1kYXRh",
  "authResult": "EAP_SUCCESS",
  "pvsInfo": [
    {
      "serverType": "PROSE",
      "serverId": "pvs-001"
    }
  ],
  "msk": "SGVsbG8gV29ybGRfTVNLIEtFWQ==",
  "supportedFeatures": "3GPP-R18-AIW"
}

// Final: EAP Failure
{
  "supi": "imu-208046000000001",
  "eapMessage": null,
  "authResult": "EAP_FAILURE",
  "pvsInfo": null,
  "msk": null,
  "supportedFeatures": "3GPP-R18-AIW"
}
```

---

## 4. MSK Handling (RFC 5216)

### 4.1 What is MSK?

MSK (Master Session Key) là key material được derive từ EAP-TLS handshake, được trả về cho AUSF để tạo security context cho SNPN access.

Theo RFC 5216 §2.1.4:
- MSK là 64-octet key material
- Derived từ TLS session's master secret
- Lower 32 octets = EMSK (Extended MSK)
- AUSF dùng MSK để derive NAS security keys

### 4.2 MSK in AuthConfirmationResponse

```go
type AuthConfirmationResponse struct {
    Supi         string   `json:"supi"`
    EapMessage   string   `json:"eapMessage"`   // base64
    AuthResult   string   `json:"authResult"`   // EAP_SUCCESS/FAILURE
    PvsInfo      []PvsInfo `json:"pvsInfo,omitempty"`
    Msk          string   `json:"msk,omitempty"` // base64-encoded MSK (64 bytes)
    SupportedFeatures string `json:"supportedFeatures,omitempty"`
}
```

**Security:** MSK cần được truyền qua TLS-protected channel. Không bao giờ log MSK. AUSF phải xử lý MSK ngay sau khi nhận.

### 4.3 AUSF Usage of MSK

```
AUSF nhận MSK từ NSSAAF
         │
         ▼
AUSF derive NAS security keys cho SNPN
         │
         ▼
AUSF trả về security context cho AMF
(via Namf_Communication_UEContextUpdate)
```

---

## 5. PVS Info (Privacy Violating Servers)

### 5.1 Definition

`pvsInfo` (Privacy Violating Servers Information) chứa danh sách các server mà UE đã expose identity của nó trong quá trình EAP authentication, vi phạm privacy.

### 5.2 PVS Data Structure

```json
"pvsInfo": [
  {
    "serverType": "PROSE",
    "serverId": "pvs-001"
  },
  {
    "serverType": "LOCATION",
    "serverId": "loc-server-001",
    "locationInfo": {
      "cellId": "0x1234ABCD",
      "tac": 100
    }
  }
]
```

### 5.3 Server Type Enum

```go
type ServerType string
const (
    ServerType_PROSE    ServerType = "PROSE"     // Device-to-device discovery
    ServerType_LOCATION ServerType = "LOCATION"  // Location services
    ServerType_OTHER    ServerType = "OTHER"
)
```

---

## 6. Database Schema (AIW-specific)

> **Note (Phase R):** Session state is managed by Biz Pods. The `eap_session_state` column stores encrypted EAP session data. The AAA Gateway does not access the database directly — it uses Redis for session correlation (`nssaa:session:{sessionId}`) to map raw RADIUS/Diameter transaction IDs to `authCtxId`. See `docs/design/12_redis_ha.md` §4.6 for cross-component Redis keys.

```sql
-- AIW sessions: separate from NSSAA sessions
-- Managed by Biz Pods (see 01_service_model.md §5.4)
CREATE TABLE aiw_auth_sessions (
    auth_ctx_id          VARCHAR(64) PRIMARY KEY,
    supi                 VARCHAR(32) NOT NULL,
    amf_instance_id      VARCHAR(64),
    aaa_config_id        UUID NOT NULL REFERENCES aaa_server_configs(id),
    eap_session_state    BYTEA NOT NULL,
    eap_rounds           INTEGER NOT NULL DEFAULT 0,
    max_eap_rounds       INTEGER NOT NULL DEFAULT 20,
    nssaa_status         nssaa_status NOT NULL DEFAULT 'NOT_EXECUTED',
    auth_result          nssaa_status,
    msk_encrypted        BYTEA,     -- encrypted MSK
    pvs_info             JSONB,     -- Privacy Violating Servers info
    failure_reason       TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at           TIMESTAMPTZ NOT NULL,
    completed_at         TIMESTAMPTZ
);

CREATE INDEX idx_aiw_sessions_supi ON aiw_auth_sessions(supi);
CREATE INDEX idx_aiw_sessions_status
    ON aiw_auth_sessions(nssaa_status)
    WHERE nssaa_status IN ('PENDING', 'NOT_EXECUTED');

-- Audit for AIW (separate from NSSAA audit)
CREATE TABLE aiw_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    auth_ctx_id     VARCHAR(64),
    supi_hash       VARCHAR(64) NOT NULL,   -- SHA-256 hashed
    action          VARCHAR(30) NOT NULL,
    nssaa_status    VARCHAR(20),
    msk_returned    BOOLEAN NOT NULL DEFAULT FALSE,  -- was MSK included in response
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_aiw_audit_supi ON aiw_audit_log(supi_hash, created_at DESC);
```

---

## 7. AUSF Integration Flow

> **Detailed AUSF integration flow available in:** `23_ausf_integration.md`

For complete AUSF integration details including:
- MSK handling and key derivation (TS 33.501)
- PVS Info data structures
- OAuth2 scope and mTLS security
- AUSF error recovery flows
- AIW session database schema

See: `docs/design/23_ausf_integration.md`

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | Nnssaaf_AIW dùng SUPI thay vì GPSI | AuthInfo.supi required |
| AC2 | AUSF là consumer, không phải AMF | N60 interface, scope nnssaaf-aiw |
| AC3 | MSK returned on EAP_SUCCESS | AuthConfirmationResponse.msk (64-byte, base64) |
| AC4 | pvsInfo returned when applicable | AuthConfirmationResponse.pvsInfo (optional) |
| AC5 | Separate session table từ NSSAA | aiw_auth_sessions table |
| AC6 | MSK encrypted at rest | msk_encrypted BYTEA column |
| AC7 | SUPI hashed in audit log | supi_hash column |
| AC8 | No re-auth/revocation notifications | Not in Nnssaaf_AIW scope |
