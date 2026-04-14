---
spec: TS 23.501 v18.10.0 / TS 29.526 v18.7.0 / TS 29.510 v18.6.0
section: §5.15.10, §6.3.17, TS 29.510 §6
interface: N58 (AMF-NSSAAF), N60 (AUSF-NSSAAF), N59 (NSSAAF-UDM), Nnrf (NSSAAF-NRF)
service: Nnssaaf_NSSAA, Nnssaaf_AIW
operation: N/A (architecture)
---

# NSSAAF Service Model Design

## 1. Overview

NSSAAF (Network Slice-Specific Authentication and Authorization Function) là một Network Function (NF) trong 5G Service-Based Architecture (SBA), chịu trách nhiệm relay các thông điệp EAP giữa AMF và NSS-AAA Server để thực hiện xác thực và ủy quyền đặc thù cho từng Network Slice (S-NSSAI).

Tài liệu này thiết kế NSSAAF như một microservice tuân thủ 3GPP Release 18, deploy được trên Kubernetes với yêu cầu telecom-grade: high availability, scalability, và deterministic latency.

---

## 2. NSSAAF trong 5G Architecture

### 2.1 Vị trí trong 5G SBA

Theo TS 23.501 §6.3.17 và TS 33.501 §5.13:

```
┌─────────────────────────────────────────────────────────────────────┐
│                         5G Service-Based Architecture                 │
│                                                                      │
│   ┌───────┐                                                        │
│   │  NRF  │◄─── Nnrf ──────────────────────────────────────────┐  │
│   └───────┘                                                      │  │
│                                                                      │
│   ┌───────┐    ┌───────┐    ┌───────┐    ┌───────┐              │  │
│   │  AMF  │◄───│ AUSF  │◄───│ NSSAF │◄───│ UDM   │              │  │
│   └───┬───┘    └───────┘    └───────┘    └───┬───┘              │  │
│       │                                      │                     │  │
│       │ Namf_          Nausf_        Nudm_  │                     │  │
│       │ Nsmf_           Nudm_         Nudf_  │                     │  │
│       │ ...             ...            ...   │                     │  │
│       │                                      │                     │  │
│       │         N58 (Nnssaaf)                 │ N59 (Nudm_UECM)  │  │
│       └──────────────┬──────────────────────────┼─────────────────┘  │
│                      ▼                                           │  │
│              ┌──────────────┐                                    │  │
│              │   NSSAAF     │                                    │  │
│              │ (THIS NF)    │                                    │  │
│              └──────┬───────┘                                    │  │
│                     │                                           │  │
│                     │ Nnssaaf_AIW (N60)                        │  │
│                     │ AUSF is consumer                          │  │
│                     ▼                                           │  │
│              (Nnssaaf_AIW not shown separately)                  │  │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### 2.2 NSSAAF vs các NF khác

| NF | Vai trò | Giao tiếp với NSSAAF | Spec |
|----|---------|----------------------|------|
| **AMF** | EAP Authenticator, chủ động gọi NSSAAF | N58: Nnssaaf_NSSAA_Authenticate | TS 23.502 §4.2.9 |
| **AUSF** | Xác thực người dùng SNPN | N60: Nnssaaf_AIW_Authenticate | TS 33.501 §I.2.2.2 |
| **UDM** | Quản lý subscription, AMF registration | N59: Nudm_UECM_Get | TS 23.502 §4.2.9.3 |
| **NRF** | Service discovery, NF registration | Nnrf: NFProfile registration | TS 29.510 §6 |
| **SMF** | Không tương tác trực tiếp với NSSAAF | — | — |
| **UDM/UDR** | Lưu subscription data về NSSAA | Gián tiếp qua AMF | TS 23.502 §4.2.9 |

### 2.3 NSSAAF Services

NSSAAF cung cấp **hai NF services** (TS 29.526 §7):

#### Nnssaaf_NSSAA

Dịch vụ chính cho Network Slice-Specific Authentication.

| Operation | Semantics | Direction | Consumer |
|-----------|-----------|-----------|----------|
| Authenticate | Request/Response | AMF → NSSAAF | AMF |
| Re-AuthenticationNotification | Notify | NSSAAF → AMF | AMF (implicit) |
| RevocationNotification | Notify | NSSAAF → AMF | AMF (implicit) |

#### Nnssaaf_AIW

Dịch vụ cho SNPN Credentials Holder authentication (TS 33.501 §I.2.2.2).

| Operation | Semantics | Direction | Consumer |
|-----------|-----------|-----------|----------|
| Authenticate | Request/Response | AUSF → NSSAAF | AUSF |

---

## 3. Service Interface Design (SBI)

### 3.1 N58 Interface (AMF ↔ NSSAAF)

**Protocol:** HTTP/2 (SBI, TS 29.500)

**Base URL:** `https://{nssAAF_fqdn}/nnssaaf-nssaa/v1`

#### 3.1.1 CreateSliceAuthenticationContext

```
POST /nnssaaf-nssaa/v1/slice-authentications
```

**Trigger:** AMF gửi EAP Identity Response từ UE cho S-NSSAI yêu cầu NSSAA.

**Request Headers:**
```
Content-Type: application/json
Authorization: Bearer {oauth2_token}
X-Request-ID: {uuid}
```

**Request Body:** `SliceAuthInfo`

```json
{
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" },
  "eapIdRsp": "<base64-encoded-eap-identity-response>",
  "amfInstanceId": "af-j1g2h3k4l5",
  "reauthNotifUri": "https://amf1.operator.com:8080/nnsf-nssaaf/v1/notifications",
  "revocNotifUri": "https://amf1.operator.com:8080/nnsf-nssaaf/v1/notifications"
}
```

**Response 201:** `SliceAuthContext`

```json
{
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" },
  "authCtxId": "nssaa-auth-01fr5xg2e3p4q5r6s7",
  "eapMessage": "<base64-encoded-eap-identity-request-to-ue>"
}
```

**Location Header:** `https://nssAAF.example.com/nnssaaf-nssaa/v1/slice-authentications/nssaa-auth-01fr5xg2e3p4q5r6s7`

#### 3.1.2 ConfirmSliceAuthentication

```
PUT /nnssaaf-nssaa/v1/slice-authentications/{authCtxId}
```

**Trigger:** AMF forward EAP response từ UE cho round tiếp theo trong multi-round EAP.

**Request Body:** `SliceAuthConfirmationData`

```json
{
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" },
  "eapMessage": "<base64-encoded-eap-response>"
}
```

**Response 200:** `SliceAuthConfirmationResponse`

```json
{
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" },
  "eapMessage": "<base64-encoded-eap-challenge-or-result>",
  "authResult": "EAP_SUCCESS"
}
```

**Khi nào authResult được trả về:**
- `null` hoặc không có: EAP exchange chưa hoàn tất, tiếp tục round tiếp theo
- `EAP_SUCCESS`: NSSAA thành công, kết thúc
- `EAP_FAILURE`: NSSAA thất bại, kết thúc

#### 3.1.3 Server-Sent Notifications (Callbacks)

AMF cung cấp `reauthNotifUri` và `revocNotifUri` trong SliceAuthInfo. NSSAAF gọi ngược về AMF khi AAA-S trigger.

**Re-Authentication Notification (NSSAAF → AMF):**

```
POST {reauthNotifUri}   (AMF provides this URI in SliceAuthInfo)

{
  "notifType": "SLICE_RE_AUTH",
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" }
}
```

**Revocation Notification (NSSAAF → AMF):**

```
POST {revocNotifUri}

{
  "notifType": "SLICE_REVOCATION",
  "gpsi": "5-208046000000001",
  "snssai": { "sst": 1, "sd": "000001" }
}
```

**Note:** AMF được implicit subscription, không cần explicit subscription với NSSAAF. AMF discover callback URI của chính nó qua NRF (TS 29.501).

### 3.2 N60 Interface (AUSF ↔ NSSAAF)

**Base URL:** `https://{nssAAF_fqdn}/nnssaaf-aiw/v1`

Khác với N58, N60 dùng **SUPI** thay vì GPSI và **không có notifications** (one-shot auth cho SNPN primary authentication).

```
POST /nnssaaf-aiw/v1/authentications
PUT  /nnssaaf-aiw/v1/authentications/{authCtxId}
```

### 3.3 N59 Interface (NSSAAF → UDM)

**Purpose:** NSSAAF truy vấn AMF ID hiện tại của UE qua GPSI.

**Service:** Nudm_UECM_Get (TS 29.503 §5.3.2.2)

**Khi nào cần:**
- AAA-S trigger re-authentication (§4.2.9.3 Step 3a)
- AAA-S trigger revocation (§4.2.9.4 Step 3a)

**Request:**
```
GET /nudm-uem/v1/{gpsi}/registrations?service-names=nsmf-pdusession
```

**Response:**
```json
{
  "amfInfo": [
    {
      "amfInstanceId": "amf-instance-001",
      "amfUri": "https://amf1.operator.com:8080/namf-comm/v1",
      "guami": { "plmnId": {...}, "amfId": "amf-001" }
    }
  ]
}
```

### 3.4 Nnrf Interface (NSSAAF ↔ NRF)

**Purpose:** NSSAAF đăng ký với NRF và discover các NF khác.

**NF Registration (NSSAAF → NRF):**

```
POST /nnrf-disc/v1/nf-instances
```

**NF Profile:**

```json
{
  "nfInstanceId": "nssAAF-instance-001",
  "nfType": "NSSAAF",
  "nfStatus": "REGISTERED",
  "plmnList": [{ "mcc": "208", "mnc": "001" }],
  "sNssais": [{ "sst": 1, "sd": "000001" }],
  "nsiList": ["nsi-001", "nsi-002"],
  "nff信息": [...],
  "nssaaInfo": {
    "supiRanges": [{ "start": "imu-208001000000000", "end": "imu-208001000099999" }],
    "supportedSecurityAlgorithm": ["EAP-TLS", "EAP-TTLS"]
  },
  "nfServices": [
    {
      "serviceName": "nnssaaf-nssaa",
      "versions": [{ "apiVersion": "v1", "fullVersion": "1.2.1" }],
      "scheme": "https",
      "fqdn": "nssAAF.operator.com",
      "ipEndPoints": [{ "ipv4Address": "10.0.1.50", "port": 8080 }],
      "supportedFeatures": "3GPP-R18-NSSAA"
    },
    {
      "serviceName": "nnssaaf-aiw",
      "versions": [{ "apiVersion": "v1", "fullVersion": "1.1.0" }],
      "scheme": "https",
      "fqdn": "nssAAF.operator.com",
      "ipEndPoints": [{ "ipv4Address": "10.0.1.50", "port": 8080 }]
    }
  ],
  "heartBeatTimer": 300,
  "priority": 100,
  "capacity": 10000
}
```

---

## 4. Multi-Tenancy Architecture

### 4.1 PLMN Isolation

NSSAAF hỗ trợ multiple PLMN trên cùng một deployment:

```
┌─────────────────────────────────────────────────────────┐
│                    NSSAAF Cluster                        │
│                                                          │
│  ┌──────────────────┐  ┌──────────────────┐           │
│  │    PLMN #208001   │  │    PLMN #310410   │           │
│  │  ┌─────────────┐  │  │  ┌─────────────┐  │           │
│  │  │ S-NSSAI     │  │  │  │ S-NSSAI     │  │           │
│  │  │ sst=1, sd=x │  │  │  │ sst=2, sd=y │  │           │
│  │  └─────────────┘  │  │  └─────────────┘  │           │
│  │  ┌─────────────┐  │  │  ┌─────────────┐  │           │
│  │  │ AAA Config  │  │  │  │ AAA Config  │  │           │
│  │  │ per S-NSSAI │  │  │  │ per S-NSSAI │  │           │
│  │  └─────────────┘  │  │  └─────────────┘  │           │
│  └──────────────────┘  └──────────────────┘           │
│                                                          │
│  Shared infrastructure: PostgreSQL, Redis, Istio mesh     │
└─────────────────────────────────────────────────────────┘
```

### 4.2 Slice Isolation

Per S-NSSAI:
- Separate AAA server configuration
- Separate AAA-P routing rules
- Separate rate limiting buckets
- Separate audit log streams

### 4.3 Tenant Aware Processing

```
Request Flow:

AMF → NSSAAF SBI Gateway
  │
  ▼
Tenant Resolver
  ├─ Extract PLMN from request (via AMF cert CN / header)
  ├─ Extract S-NSSAI from payload
  └─ Resolve tenant context
  │
  ▼
Tenant-Routed Handler
  ├─ Route to correct DB schema (multi-tenant)
  ├─ Select AAA config for this S-NSSAI
  ├─ Apply rate limits for this PLMN
  └─ Tag audit log with tenant ID
```

---

## 5. Microservice Architecture

### 5.1 Component Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                      NSSAAF Service                          │
│                                                               │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                    SBI Gateway                          │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌────────────┐ │  │
│  │  │ N58 Handler  │  │ N60 Handler  │  │ N59 Client │ │  │
│  │  │ (Nnssaaf_    │  │ (Nnssaaf_    │  │ (UDM UECM) │ │  │
│  │  │  NSSAA)      │  │  AIW)        │  │            │ │  │
│  │  └──────────────┘  └──────────────┘  └────────────┘ │  │
│  └────────────────────────┬─────────────────────────────────┘  │
│                           │                                    │
│  ┌────────────────────────▼─────────────────────────────────┐  │
│  │                    EAP Engine                             │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │  │
│  │  │ EAP-TLS      │  │ EAP-TTLS     │  │ EAP-AKA'     │  │  │
│  │  │ Handler      │  │ Handler      │  │ Handler      │  │  │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  │  │
│  │                                                          │  │
│  │  ┌──────────────────────────────────────────────────┐   │  │
│  │  │ Session State Machine                             │   │  │
│  │  │ IDLE→INIT→EAP_EXCHANGE→COMPLETING→DONE          │   │  │
│  │  └──────────────────────────────────────────────────┘   │  │
│  └────────────────────────┬─────────────────────────────────┘  │
│                           │                                    │
│  ┌────────────────────────▼─────────────────────────────────┐  │
│  │                   AAA Protocol Layer                      │  │
│  │  ┌──────────────────────┐  ┌──────────────────────────┐  │  │
│  │  │    RADIUS Client     │  │    Diameter Client       │  │  │
│  │  │    (RFC 2865)        │  │    (RFC 7155)           │  │  │
│  │  │    • Access-Request  │  │    • DER/DEA             │  │  │
│  │  │    • Access-Challenge│  │    • STR/STA             │  │  │
│  │  │    • Access-Accept   │  │    • CER/CEA             │  │  │
│  │  └──────────────────────┘  └──────────────────────────┘  │  │
│  │                                                          │  │
│  │  ┌──────────────────────────────────────────────────┐   │  │
│  │  │ AAA Proxy Client (optional)                      │   │  │
│  │  │ • Protocol passthrough                          │   │  │
│  │  │ • S-NSSAI → ENSI translation                   │   │  │
│  │  └──────────────────────────────────────────────────┘   │  │
│  └────────────────────────┬─────────────────────────────────┘  │
│                           │                                    │
│  ┌────────────────────────▼─────────────────────────────────┐  │
│  │                External Integrations                      │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │  │
│  │  │ NRF Client   │  │ UDM Client   │  │ Config Store │  │  │
│  │  └──────────────┘  └──────────────┘  └──────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

### 5.2 Data Flow per Request Type

#### Flow A: AMF → NSSAAF → AAA-S → NSSAAF → AMF

```
AMF                    NSSAAF                  AAA-S
  │                       │                       │
  │ POST /slice-auth      │                       │
  │ (EAP ID Response,    │                       │
  │  GPSI, S-NSSAI)      │                       │
  │──────────────────────►│                       │
  │                       │                       │
  │                       │ 1. Validate request   │
  │                       │ 2. Create session in  │
  │                       │    PostgreSQL         │
  │                       │ 3. Select AAA config  │
  │                       │    (by S-NSSAI)       │
  │                       │                       │
  │                       │ 4. Encode RADIUS      │
  │                       │    Access-Request     │
  │                       │                       │
  │                       │───────────────────────►
  │                       │                       │
  │                       │    RADIUS Access-     │
  │                       │    Challenge          │
  │                       │◄──────────────────────│
  │                       │                       │
  │                       │ 5. Decode RADIUS      │
  │                       │ 6. Encode EAP to AMF  │
  │                       │                       │
  │ 201 Created          │                       │
  │ (EAP Challenge,       │                       │
  │  authCtxId)          │                       │
  │◄──────────────────────│                       │
  │                       │                       │
  │ PUT /slice-auth/{id} │                       │
  │ (EAP Response)       │                       │
  │──────────────────────►│                       │
  │                       │                       │
  │                       │ 7. RADIUS Access-     │
  │                       │    Request            │
  │                       │───────────────────────►│
  │                       │                       │
  │                       │    RADIUS Access-     │
  │                       │    Accept/Reject      │
  │                       │◄──────────────────────│
  │                       │                       │
  │                       │ 8. Update session     │
  │                       │    state in DB        │
  │                       │                       │
  │ 200 OK               │                       │
  │ (EAP Result,         │                       │
  │  authResult)         │                       │
  │◄──────────────────────│                       │
```

#### Flow B: AAA-S → NSSAAF → AMF (Re-Auth Triggered)

```
AAA-S              NSSAAF                UDM                  AMF
  │                   │                    │                    │
  │ Re-Auth Request   │                    │                    │
  │ (GPSI, S-NSSAI)  │                    │                    │
  │──────────────────►│                    │                    │
  │                   │                    │                    │
  │                   │ 1. Validate AAA-S  │                    │
  │                   │    authorization   │                    │
  │                   │ 2. Nudm_UECM_Get  │                    │
  │                   │    (GPSI)         │                    │
  │                   │──────────────────►│                    │
  │                   │                    │                    │
  │                   │    AMF ID(s)      │                    │
  │                   │◄──────────────────│                    │
  │                   │                    │                    │
  │ ACK (immediate)  │                    │                    │
  │◄──────────────────│                    │                    │
  │                   │                    │                    │
  │                   │ 3. Nnssaaf_NSSAA_ │                    │
  │                   │    Re-AuthNotif   │                    │
  │                   │                   │                    │
  │                   │───────────────────────────────────────►│
  │                   │                    │                    │
  │                   │                    │    204 No Content  │
  │                   │◄───────────────────────────────────────│
  │                   │                    │                    │
  │                   │ 4. AMF triggers    │                    │
  │                   │    NSSAA procedure │                    │
  │                   │    (→ Flow A)      │                    │
```

### 5.3 Thread Model

```
Event-driven, async I/O (io_uring / epoll):

Main Event Loop:
  ├── SBI HTTP/2 Acceptor     (thread pool: 4-8 threads)
  │     └── per-request: route → validate → dispatch
  │
  ├── EAP State Processor     (dedicated thread pool: 16-32 threads)
  │     └── per-session: advance EAP state machine
  │
  ├── RADIUS Sender           (dedicated thread pool: 8-16 threads)
  │     └── async send/recv, non-blocking UDP
  │
  ├── Diameter Sender          (dedicated thread pool: 4-8 threads)
  │     └── SCTP/TCP async, connection pool
  │
  ├── Notification Dispatcher  (dedicated thread pool: 4 threads)
  │     └── HTTP POST to AMF callbacks
  │
  └── Background Workers:
        ├── NRF heartbeat       (interval: 5 min)
        ├── AAA health checker   (interval: 30s)
        ├── Session timeout      (scanner: every 1 min)
        └── Audit log flusher    (batch: every 5s)
```

---

## 6. NF Profile Specification

### 6.1 NF Instance Registration

```yaml
nfInstanceId: "<uuid-v7>"
nfType: NSSAAF
nfStatus: REGISTERED

# Identity
nodeId:
 Fqdn: "nssAAF-operator-1.operator.com"
 nodeIpList:
    - "10.0.1.50"    # AZ1
    - "10.0.2.50"    # AZ2
    - "10.0.3.50"    # AZ3

# PLMN Coverage
plmnList:
  - plmnId:
      mcc: "208"
      mnc: "001"
    snssaiList:
      - sst: 1
        sd: "000001"    # eMBB slice
      - sst: 2
        sd: "000001"    # URLLC slice

# Services
nfServices:
  nnssaaf-nssaa:
    version: "v1"
    fqdn: "nssAAF.operator.com"
    apiPrefix: "https://nssAAF.operator.com/nnssaaf-nssaa"
    ipEndPoints:
      - ipv4Address: "10.0.1.50"
        port: 443
        transport: "TCP"
    securityMethods: ["TLS 1.3"]
    supportedFeatures: "NSSAA-REAUTH|NSSAA-REVOC|EAP-TLS|EAP-TTLS"

  nnssaaf-aiw:
    version: "v1"
    fqdn: "nssAAF.operator.com"
    apiPrefix: "https://nssAAF.operator.com/nnssaaf-aiw"
    ipEndPoints:
      - ipv4Address: "10.0.1.50"
        port: 443

# NSSAAF-specific info (per TS 28.541 §5.3.146)
nssaaInfo:
  supiRanges:
    - start: "imu-208001000000000"
      end:   "imu-208001099999999"
      pattern: "^imu-208001[0-9]{8}$"
  internalGroupIdentifiersRanges:
    - start: "group-001"
      end:   "group-999"
  supportedSecurityAlgorithm: ["EAP-TLS", "EAP-TTLS", "EAP-AKA'"]

# Capacity & Priority
capacity: 10000           # concurrent sessions
priority: 100             # for load balancing
load: 0                   # updated by NSSAAF periodically

# Operational
heartBeatTimer: 300       # seconds
snssais: [...]            # same as plmnList.snssaiList
nsiList: ["nsi-001", "nsi-002"]

# Locally-defined info
customInfo:
  supportedAaaProtocols: ["RADIUS", "DIAMETER"]
  maxEapRounds: 20
  eapTimeoutSeconds: 30
```

---

## 7. Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| Concurrent sessions per instance | 50,000 | EAP session state |
| Requests per second (cluster) | 100,000 | N58 + N60 combined |
| P99 latency (end-to-end) | < 100ms | AMF → NSSAAF → AAA-S → NSSAAF → AMF |
| P99 latency (NSSAAF processing) | < 20ms | Excluding AAA-S round-trip |
| Session setup rate | > 5,000/sec per instance | New NSSAA session/s |
| RADIUS transaction rate | > 50,000/sec per instance | Access-Request/s |
| Database write latency | < 5ms (P99) | PostgreSQL |
| Cache hit ratio | > 95% | Redis for session state |
| Availability | 99.999% (5 nines) | Per AZ, per cluster |
| MTTR | < 30 seconds | Automatic failover |
| RPO | 0 seconds | Synchronous replication |

---

## 8. Acceptance Criteria

| # | Criteria | Spec Reference |
|---|----------|----------------|
| AC1 | NSSAAF đăng ký NFProfile với NRF, heartbeat mỗi 5 phút | TS 29.510 §6.2 |
| AC2 | NSSAAF discover AMF callback URI qua NRF | TS 29.501 §5.2.4 |
| AC3 | NSSAAF discover UDM Nudm_UECM_Get service qua NRF | TS 29.503 §5.3 |
| AC4 | AMF implicit subscribe Re-AuthNotification và RevocationNotification | TS 33.501 §14.4.1.3 |
| AC5 | N58 dùng HTTP/2, TLS 1.3, OAuth2 authentication | TS 29.500 §5 |
| AC6 | N60 dùng SUPI thay vì GPSI | TS 29.526 §7.3 |
| AC7 | Multi-PLMN isolation qua PLMN ID trong NFProfile | TS 29.510 §6.1 |
| AC8 | Per-S-NSSAI AAA routing via local config | TS 29.561 §16.1.1 |
