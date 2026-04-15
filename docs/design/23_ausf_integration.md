---
spec: TS 29.526 v18.7.0 / TS 33.501 v18.10.0 / TS 23.501 §I
section: §7.3, TS 29.501 §I.2.2.2
interface: N60 (AUSF-NSSAAF)
service: Nnssaaf_AIW
operation: Authenticate, ConfirmAuthentication
eapMethod: EAP-TLS / EAP-TTLS / EAP-AKA'
aaaProtocol: RADIUS / Diameter
---

# AUSF Integration Design

## 1. Overview

Tài liệu này thiết kế chi tiết integration giữa NSSAAF và AUSF qua **N60 interface** cho SNPN (Standalone Non-Public Network) authentication với Credentials Holder.

AUSF (Authentication Server Function) trong SNPN context đóng vai trò:
- **Consumer**: Gọi NSSAAF Nnssaaf_AIW service để xác thực Credentials Holder
- **Key Deriver**: Derive NAS security keys từ MSK nhận được từ NSSAAF

Khác với Nnssaaf_NSSAA (dùng GPSI, AMF là consumer), Nnssaaf_AIW dùng **SUPI** và AUSF là consumer.

---

## 2. AUSF vs AMF Consumer Comparison

| Aspect | Nnssaaf_NSSAA | Nnssaaf_AIW |
|--------|---------------|-------------|
| **Consumer NF** | AMF | AUSF |
| **Interface** | N58 | N60 |
| **Subscriber ID** | GPSI | SUPI |
| **Authentication Type** | Slice-specific | SNPN primary auth |
| **Trigger** | Registration with NSSAA-required S-NSSAI | SNPN access with Credentials Holder |
| **Key Output** | Not specified | **MSK** (Master Session Key) |
| **PVS Info** | Not used | **Yes** (Privacy Violating Servers) |
| **Re-auth/Revocation** | Yes (AAA-S triggered) | Not in scope |

---

## 3. N60 Interface Specification

### 3.1 Service Definition

```
Service Name: Nnssaaf_AIW
Version: 1.1.0
Base Path: /nnssaaf-aiw/v1
Security: OAuth2 Client Credentials (scope: nnssaaf-aiw)
```

### 3.2 API Endpoints

#### POST /authentications
Tạo authentication context mới.

**Request Headers:**
```
Content-Type: application/json
Authorization: Bearer {oauth2_token}
X-Request-ID: {uuid}
```

**Request Body:**
```json
{
  "supi": "imu-208046000000001",
  "eapIdRsp": "AG5uZXh0LWlkQHVzZXIuZXhhbXBsZS5jb20=",
  "ttlsInnerMethodContainer": null,
  "supportedFeatures": "3GPP-R18-AIW"
}
```

**Response 201 Created:**
```json
{
  "supi": "imu-208046000000001",
  "authCtxId": "01fr5xg2e3p4q5r6s7t8u9v0w2",
  "eapMessage": "AG5nZXQtaWQAdXNlckBleGFtcGxlLmNvbQA=",
  "ttlsInnerMethodContainer": null,
  "supportedFeatures": "3GPP-R18-AIW"
}
```

**Location Header:**
```
Location: https://nssAAF.operator.com/nnssaaf-aiw/v1/authentications/01fr5xg2e3p4q5r6s7t8u9v0w2
```

#### PUT /authentications/{authCtxId}
Confirm/advance authentication.

**Request Body:**
```json
{
  "supi": "imu-208046000000001",
  "eapMessage": "AG5jaGFsbGVuZ2UAdXNlcjEA",
  "supportedFeatures": "3GPP-R18-AIW"
}
```

**Response 200 OK (continuing):**
```json
{
  "supi": "imu-208046000000001",
  "eapMessage": "AG5jaGFsbGVuZ2UAdXNlcjEA",
  "authResult": null,
  "pvsInfo": null,
  "msk": null,
  "supportedFeatures": "3GPP-R18-AIW"
}
```

**Response 200 OK (final success):**
```json
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
```

**Response 200 OK (final failure):**
```json
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

## 4. AUSF Integration Flow

### 4.1 SNPN Access with Credentials Holder

```
UE              AUSF              NSSAAF            AAA-S
 │                │                 │                 │
 │ 1. Auth Req  │                 │                 │
 │    (EAP Start)│                 │                 │
 │───────────────►│                 │                 │
 │                │                 │                 │
 │ 2. AUSF        │                 │                 │
 │    determines   │                 │                 │
 │    needs AAA   │                 │                 │
 │                │                 │                 │
 │                │ 3. POST /aiw   │                 │
 │                │    (supi,      │                 │
 │                │     eapIdRsp)  │                 │
 │                │────────────────►│                 │
 │                │                 │                 │
 │                │                 │ 4. RADIUS/DIA  │
 │                │                 │    Access-Req   │
 │                │                 │─────────────────►│
 │                │                 │                 │
 │                │                 │◄─────────────────│
 │                │                 │ 5. RADIUS/DIA  │
 │                │                 │    Access-Chal │
 │                │                 │                 │
 │                │ 6. 201 Created │                 │
 │                │    (eapMsg)    │                 │
 │                │◄────────────────│                 │
 │                │                 │                 │
 │ 7. EAP         │                │                 │
 │    Challenge   │                │                 │
 │◄───────────────│                │                 │
 │                │                 │                 │
 │ 8. EAP         │                │                 │
 │    Response    │                │                 │
 │───────────────►│                │                 │
 │                │                 │                 │
 │                │ 9. PUT /aiw   │                 │
 │                │    (eapMsg)   │                 │
 │                │────────────────►│                 │
 │                │                 │                 │
 │                │                 │ 10. Final      │
 │                │                 │    RADIUS/DIA   │
 │                │                 │─────────────────►│
 │                │                 │                 │
 │                │                 │◄─────────────────│
 │                │                 │ 11. Final Resp  │
 │                │                 │    (EAP result, │
 │                │                 │     MSK, pvs)   │
 │                │                 │                 │
 │                │ 12. 200 OK     │                 │
 │                │    (eapMsg,    │                 │
 │                │     authResult,│                 │
 │                │     msk, pvs)  │                 │
 │                │◄────────────────│                 │
 │                │                 │                 │
 │ 13. Auth Result│                │                 │
 │◄───────────────│                 │                 │
 │                │                 │                 │
 │                │ 14. AUSF derive │                 │
 │                │    NAS keys    │                 │
 │                │    from MSK    │                 │
 │                │                 │                 │
 │                │ 15. Namf_Comm  │                 │
 │                │    UEContextUpdate              │
 │                │────────────────►│                 │
 │ 16. NAS Security│                │                 │
 │    Context Est. │                │                 │
 │◄───────────────│                 │                 │
```

### 4.2 AUSF Processing Steps

#### Step 3: POST /nnssaaf-aiw/v1/authentications

**AUSF Actions:**
1. Validate OAuth2 token (scope: `nnssaaf-aiw`)
2. Parse request body
3. Extract SUPI from request
4. Validate SUPI format (`^imu-[0-9]{15}$`)
5. Forward to NSSAAF

**NSSAAF Actions:**
1. Generate authCtxId (UUIDv7)
2. Lookup AAA server config for this SUPI/range
3. Create session in PostgreSQL
4. Forward EAP Identity Response to AAA-S
5. Return authCtxId + EAP challenge

#### Step 9: PUT /nnssaaf-aiw/v1/authentications/{authCtxId}

**AUSF Actions:**
1. Load session state
2. Forward EAP response to NSSAAF
3. Wait for final response

**NSSAAF Actions:**
1. Load session by authCtxId
2. Validate SUPI matches
3. Check session not expired
4. Forward EAP to AAA-S
5. On final response:
   - Extract MSK if EAP_SUCCESS
   - Extract PVS info if present
   - Update session status
   - Return result to AUSF

#### Step 14: AUSF Derives NAS Keys from MSK

```
MSK received from NSSAAF (64 bytes)
         │
         ▼
┌─────────────────────────────────────┐
│  AUSF Key Derivation (TS 33.501)   │
├─────────────────────────────────────┤
│  K_NAF = PRF(MSK, "NAS priority") │
│  NAS Encryption Key (NEA)          │
│  NAS Integrity Key (NIA)            │
│  NAS Sequence Number                │
└─────────────────────────────────────┘
         │
         ▼
AMF receives via Namf_Communication_UEContextUpdate
```

---

## 5. MSK Handling (RFC 5216)

### 5.1 MSK Specification

MSK (Master Session Key) là 64-octet key material derived từ EAP-TLS handshake.

```
MSK Structure (64 bytes):
├── MSK (lower 32 bytes) - Used for key derivation
└── EMSK (upper 32 bytes) - Extended MSK (reserved)
```

### 5.2 MSK in Response

**When included:**
```json
{
  "authResult": "EAP_SUCCESS",
  "msk": "<64-byte MSK as Base64>"
}
```

**Security requirements:**
- MSK phải được truyền qua TLS-protected channel
- NSSAAF không bao giờ log MSK
- AUSF phải xử lý MSK ngay sau khi nhận
- MSK không được stored trong NSSAAF (chỉ trả về)

### 5.3 AUSF MSK Processing

```go
// AUSF pseudocode for MSK handling
func (a *AUSF) HandleAuthResponse(mskBase64 string) error {
    // 1. Decode MSK
    msk, err := base64.StdEncoding.DecodeString(mskBase64)
    if err != nil {
        return fmt.Errorf("invalid MSK encoding: %w", err)
    }
    
    // 2. Validate MSK length (64 bytes)
    if len(msk) != 64 {
        return fmt.Errorf("invalid MSK length: %d", len(msk))
    }
    
    // 3. Derive NAS keys (per TS 33.501)
    kNaf := deriveKNaf(msk)
    
    // 4. Store in security context (temporary)
    // 5. Forward to AMF via Namf_Communication
    return nil
}
```

---

## 6. PVS Info (Privacy Violating Servers)

### 6.1 Definition

`pvsInfo` chứa danh sách các server mà UE đã expose identity trong quá trình EAP authentication.

### 6.2 Data Structure

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

### 6.3 Server Types

| Type | Description | Privacy Impact |
|------|-------------|----------------|
| PROSE | Device-to-device discovery | UE identity exposed to ProSe function |
| LOCATION | Location services | UE location revealed |
| OTHER | Other privacy-violating services | Custom impact |

### 6.4 AUSF Handling of PVS Info

```
AUSF receives PVS Info
         │
         ▼
┌─────────────────────────────────────┐
│  1. Log PVS exposure event          │
│  2. Update UE privacy profile       │
│  3. Notify OCS/PCF if required      │
│  4. May reject SNPN access if PVS  │
│     exceeds operator policy         │
└─────────────────────────────────────┘
```

---

## 7. OAuth2 Scope & Security

### 7.1 AUSF Authorization

**Required Scope:** `nnssaaf-aiw`

**Token Validation:**
1. Validate JWT signature
2. Check token not expired
3. Verify scope contains `nnssaaf-aiw`
4. Validate AUSF identity in token claims

### 7.2 NSSAAF Client Registration

```yaml
OAuth2 Client:
  client_id: ausf-{plmnId}
  scope: nnssaaf-aiw
  grant_types: client_credentials
  token_endpoint_auth_method: tls_client_auth
```

### 7.3 TLS Mutual Authentication

```
AUSF ──────── mTLS ────────► NSSAAF
   │                            │
   │• Client certificate        │• Server certificate
   │• AUSF identity (CN)        │• NSSAAF FQDN
   │• PLMN ID validation        │• CA chain validation
```

---

## 8. Error Handling

### 8.1 NSSAAF Error Responses

| HTTP Status | Cause | Description |
|-------------|-------|-------------|
| 400 | VALIDATION_ERROR | Invalid request parameters |
| 401 | UNAUTHORIZED | Invalid/missing OAuth2 token |
| 403 | FORBIDDEN | AUSF not authorized for this SUPI |
| 404 | AUTH_CTX_NOT_FOUND | Authentication context not found |
| 404 | AAA_SERVER_NOT_CONFIGURED | No AAA server for this SUPI |
| 409 | SESSION_ALREADY_COMPLETED | Authentication already finished |
| 410 | SESSION_EXPIRED | Authentication context expired |
| 502 | AAA_TIMEOUT | AAA server not responding |
| 503 | SERVICE_UNAVAILABLE | NSSAAF temporarily unavailable |

### 8.2 AUSF Error Recovery

```go
// AUSF error handling strategy
switch cause {
case "AAA_TIMEOUT":
    // Retry with exponential backoff (max 3 attempts)
    // If still fails, return 503 to UE
case "AUTH_CTX_NOT_FOUND":
    // Reject - session state lost
case "SESSION_EXPIRED":
    // Create new authentication context
case "SERVICE_UNAVAILABLE":
    // NSSAAF overloaded - return 503, trigger alarm
}
```

---

## 9. Database Schema for AUSF Sessions

### 9.1 AIW Session Table

```sql
CREATE TABLE aiw_auth_sessions (
    auth_ctx_id          VARCHAR(64) PRIMARY KEY,
    supi                 VARCHAR(32) NOT NULL,
    ausf_instance_id     VARCHAR(64),
    aaa_config_id        UUID NOT NULL REFERENCES aaa_server_configs(id),
    eap_session_state    BYTEA NOT NULL,
    eap_rounds           INTEGER NOT NULL DEFAULT 0,
    max_eap_rounds       INTEGER NOT NULL DEFAULT 20,
    nssaa_status         nssaa_status NOT NULL DEFAULT 'NOT_EXECUTED',
    auth_result          nssaa_status,
    msk_encrypted        BYTEA,     -- Encrypted MSK (not stored permanently)
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
```

### 9.2 AIW Audit Table

```sql
CREATE TABLE aiw_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    auth_ctx_id     VARCHAR(64),
    supi_hash       VARCHAR(64) NOT NULL,   -- SHA-256 hashed for privacy
    ausf_id         VARCHAR(64),
    action          VARCHAR(30) NOT NULL,
    nssaa_status    VARCHAR(20),
    msk_returned    BOOLEAN NOT NULL DEFAULT FALSE,
    pvs_exposed     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_aiw_audit_supi ON aiw_audit_log(supi_hash, created_at DESC);
```

---

## 10. Performance Requirements

| Metric | Target | Notes |
|--------|--------|-------|
| AUSF → NSSAAF latency (P99) | <50ms | Within same DC |
| Authentication completion | <2s | Including AAA exchange |
| Concurrent AIW sessions | 50,000 | Per NSSAAF instance |
| MSK derivation time | <5ms | AUSF processing |

---

## 11. Monitoring & Metrics

### 11.1 AUSF-Specific Metrics

```yaml
# Prometheus metrics for AUSF integration
nssAAF_aiw_requests_total{ausf_id, result}
nssAAF_aiw_request_duration_seconds{ausf_id, quantile}
nssAAF_aiw_msk_returned_total{ausf_id}
nssAAF_aiw_pvs_exposed_total{ausf_id, server_type}
nssAAF_aiw_session_active{ausf_id}
```

### 11.2 Logging

```json
{
  "timestamp": "2026-04-15T10:30:00Z",
  "level": "INFO",
  "service": "nssAAF",
  "interface": "N60",
  "event": "AuthSuccess",
  "authCtxId": "01fr5xg2e3p4q5r6s7t8u9v0w2",
  "supiHash": "a1b2c3d4...",
  "ausfId": "ausf-plmn-001",
  "eapRounds": 4,
  "mskIncluded": true,
  "pvsInfoCount": 0
}
```

---

## 12. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | AUSF là consumer cho Nnssaaf_AIW | N60 interface, scope nnssaaf-aiw |
| AC2 | SUPI used instead of GPSI | AuthInfo.supi required field |
| AC3 | MSK returned on EAP_SUCCESS | AuthConfirmationResponse.msk (64-byte Base64) |
| AC4 | PVS info captured and returned | AuthConfirmationResponse.pvsInfo |
| AC5 | AUSF derives NAS keys from MSK | TS 33.501 key derivation |
| AC6 | OAuth2 scope validation | scope: nnssaaf-aiw |
| AC7 | mTLS between AUSF and NSSAAF | TLS 1.3 mutual auth |
| AC8 | Separate session table from NSSAA | aiw_auth_sessions table |
| AC9 | SUPI hashed in audit log | supi_hash column |
| AC10 | PVS logged for privacy audit | pvs_exposed in audit log |

---

## 13. Related Documents

- `03_aiw_api.md` - Nnssaaf_AIW API specification
- `06_eap_engine.md` - EAP framework
- `07_radius_client.md` - RADIUS client
- `08_diameter_client.md` - Diameter client
- `16_aaa_security.md` - AAA protocol security
