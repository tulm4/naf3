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
1. Validate OAuth2 token (scope: nnssaaf-aiw)
2. Parse AuthInfo JSON
3. Validate supi (required)
4. Resolve AAA server config:
   a. Lookup AAA_CONFIG by SUPI range or default
   b. If not found → 404 AaaServerNotConfigured
5. Create authCtxId: UUIDv7
6. Create session record:
   - authCtxId, supi, nssaaStatus=PENDING
   - eapIdRsp present → start EAP exchange
   - eapIdRsp absent → return 201 with initial EAP request
7. Encode to AAA protocol (same as NSSAA)
8. Return 201 Created with AuthContext
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
1. Load session by authCtxId
2. Validate supi matches
3. Check session not expired
4. Check not already completed
5. Forward EAP message to AAA-S
6. Return AuthConfirmationResponse
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

```sql
-- AIW sessions: separate from NSSAA sessions
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

### 7.1 SNPN Access with Credentials Holder

```
UE              AUSF              NSSAAF            AAA-S
 │                │                 │                 │
 │ 1. Auth Req   │                 │                 │
 │ (EAP Start)   │                 │                 │
 │───────────────►│                 │                 │
 │                │                 │                 │
 │ 2. AUSF        │                 │                 │
 │    determines  │                 │                 │
 │    needs AAA   │                 │                 │
 │                │                 │                 │
 │                │ 3. POST /aiw   │                 │
 │                │    (supi,      │                 │
 │                │     eapIdRsp)  │                 │
 │                │───────────────►│                 │
 │                │                │                 │
 │                │                │ 4. RADIUS/DIA  │
 │                │                │    Access-Req   │
 │                │                │───────────────►│
 │                │                │                │
 │                │                │◄───────────────│
 │                │                │ 5. RADIUS/DIA  │
 │                │                │    Access-Chal │
 │                │                │                │
 │                │ 6. 201 Created │                 │
 │                │    (eapMsg)    │                 │
 │                │◄───────────────│                 │
 │                │                │                 │
 │ 7. EAP         │                │                 │
 │    Challenge    │                │                 │
 │◄───────────────│                │                 │
 │                │                │                 │
 │ 8. EAP         │                │                 │
 │    Response    │                │                 │
 │───────────────►│                │                 │
 │                │                │                 │
 │                │ 9. PUT /aiw   │                 │
 │                │    (eapMsg)   │                 │
 │                │───────────────►│                 │
 │                │                │                 │
 │                │                │ 10. Final      │
 │                │                │    RADIUS/DIA   │
 │                │                │───────────────►│
 │                │                │                │
 │                │                │◄───────────────│
 │                │                │ 11. Final Resp │
 │                │                │    (EAP result,│
 │                │                │     MSK, pvs) │
 │                │                │                │
 │                │ 12. 200 OK     │                 │
 │                │    (eapMsg,    │                 │
 │                │     authResult,│                 │
 │                │     msk, pvs) │                 │
 │                │◄───────────────│                 │
 │                │                │                 │
 │ 13. Auth Result│                │                 │
 │◄───────────────│                │                 │
 │                │                │                 │
 │                │ 14. AUSF derive│                 │
 │                │    NAS keys    │                 │
 │                │    from MSK    │                 │
```

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
