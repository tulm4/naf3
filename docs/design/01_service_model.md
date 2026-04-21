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

The NSSAAF is structured as a **3-component model** for Kubernetes production deployments:

1. **HTTP Gateway**: Go stdlib `net/http` with TLS 1.3 termination. Each replica binds its own pod IP for external interfaces. Envoy migration is planned for a future phase (see `docs/roadmap/PHASE_Refactor_3Component.md` §7).
2. **AAA Gateway**: Custom Go UDP/TCP server. 2 replicas in active-standby, sharing a VIP via keepalived + Multus CNI bridge VLAN. Forwards raw transport to Business Logic. Envoy migration for AAA proxy is also planned for the future.
3. **Business Logic Pods**: NSSAAF application (EAP engine, session state). No direct external connectivity. Communicates with AAA Gateway via internal HTTP.

```
+=============================================================================================================+
|                                    NSSAAF Service (3-Component Model)                                      |
|                                                                                                            |
|  +======================================================================================================+  |
|  ||                              HTTP Gateway (Deployment, N replicas)                               ||  |
|  ||  +----------------------------------------------------------------------------------------+    ||  |
|  ||  | Go stdlib net/http (TLS 1.3)                                                           |    ||  |
|  ||  |  * TLS 1.3 termination                                                                  |    ||  |
|  ||  |  * Routes N58/N60 to Business Logic pods via ClusterIP                                 |    ||  |
|  ||  |  * Routes to Business Logic pods via ClusterIP                                       |    ||  |
|  ||  |  * Binds to pod IP on external interface                                              |    ||  |
|  ||  +----------------------------------------------------------------------------------------+    ||  |
|  +======================================================================================================+  |
|                                              |                                                                 |
|                                              v                                                                 |
|  +======================================================================================================+  |
|  ||                          Business Logic Pods (Deployment, N replicas)                           ||  |
|  ||  +=========================================================================================+    ||  |
|  ||  | NSSAAF Application                                                                       |    ||  |
|  ||  |  +----------------------+  +----------------------+  +------------------------+  |    ||  |
|  ||  |  | EAP-TLS Handler     |  | EAP-TTLS Handler   |  | EAP-AKA' Handler       |  |    ||  |
|  ||  |  +----------------------+  +----------------------+  +------------------------+  |    ||  |
|  ||  |  +----------------------------------------------------------------------------------------+ |    ||  |
|  ||  |  | Session State Machine (PostgreSQL + Redis)                                    |    ||  |
|  ||  |  | IDLE->INIT->EAP_EXCHANGE->COMPLETING->DONE                                  |    ||  |
|  ||  |  +----------------------------------------------------------------------------------------+ |    ||  |
|  ||  |  | HTTP Client: sends/receives raw AAA transport via internal HTTP                  |    ||  |
|  ||  |  | No direct external connectivity                                                    |    ||  |
|  ||  +=========================================================================================+    ||  |
|  +======================================================================================================+  |
|                                              |                                                                 |
|                                              v                                                                 |
|  +======================================================================================================+  |
|  ||              AAA Gateway (Deployment, 2 replicas: active + standby)                            ||  |
|  ||  +=========================================================================================+    ||  |
|  ||  | Go Custom UDP/TCP Server                                                              |    ||  |
|  ||  |  * RADIUS UDP listener (:1812)                                                           |    ||  |
|  ||  |  * Diameter TCP/SCTP listener (:3868)                                                     |    ||  |
|  ||  |  * FORWARDS raw transport to Business Logic -- no encode/decode                         |    ||  |
|  ||  +=========================================================================================+    ||  |
|  ||  keepalived (VIP on Multus CNI bridge VLAN interface)                                         ||  |
|  ||  1 active + 1 standby  |  VIP = stable source IP for AAA-S                                  ||  |
|  +======================================================================================================+  |
|                                                                                                            |
|  +======================================================================================================+  |
|  ||                               External Integrations                                             ||  |
|  ||  +---------------+  +---------------+  +---------------+  +------------------------+        ||  |
|  ||  | NRF Client    |  | UDM Client    |  | PostgreSQL    |  | Redis (Session Cache)  |        ||  |
|  ||  +---------------+  +---------------+  +---------------+  +------------------------+        ||  |
|  +======================================================================================================+  |
+=============================================================================================================+
```

**Note on development mode:** In single-pod / development mode, all three components (HTTP Gateway, AAA Gateway, Business Logic) run as a single process. The 3-component separation is required for production multi-pod Kubernetes deployments. See §5.4 for full details.

### 5.2 Data Flow per Request Type

#### Flow A: AMF → NSSAAF → AAA-S → NSSAAF → AMF

> **Note (Phase R):** This flow spans three NSSAAF components: HTTP Gateway → Biz Pod → AAA Gateway. Each component handles a distinct responsibility. See §5.4.3 for the per-component responsibility model.

```
AMF                    HTTP GW              Biz Pod              AAA GW (active)           AAA-S
  │                       │                   │                       │                      │
  │ POST /slice-auth      │                   │                       │                      │
  │ (EAP ID Response,    │                   │                       │                      │
  │  GPSI, S-NSSAI)      │                   │                       │                      │
  │──────────────────────►│                   │                       │                      │
  │                       │ HTTP/ClusterIP    │                       │                      │
  │                       │ POST /slice-auth  │                       │                      │
  │                       │───────────────────►│                       │                      │
  │                       │                   │                       │                      │
  │                       │                   │ 1. Validate request   │                      │
  │                       │                   │ 2. Create session in  │                      │
  │                       │                   │    PostgreSQL         │                      │
  │                       │                   │ 3. Select AAA config  │                      │
  │                       │                   │    (by S-NSSAI)       │                      │
  │                       │                   │                       │                      │
  │                       │                   │ 4. Encode EAP into    │                      │
  │                       │                   │    RADIUS Access-Req  │                      │
  │                       │                   │                       │                      │
  │                       │                   │ 5. HTTP POST /aaa/    │                      │
  │                       │                   │    forward (raw bytes)│                      │
  │                       │                   │                       │                      │
  │                       │                   │ HTTP/9090             │ UDP:1812             │
  │                       │                   │──────────────────────►│─────────────────────►│
  │                       │                   │                       │                      │
  │                       │                   │                       │    RADIUS Access-    │
  │                       │                   │                       │    Challenge          │
  │                       │                   │                       │◄────────────────────│
  │                       │                   │                       │                      │
  │                       │                   │ 6. Decode EAP from    │                      │
  │                       │                   │    RADIUS response     │                      │
  │                       │                   │ 7. Advance EAP state   │                      │
  │                       │                   │                       │                      │
  │ 201 Created          │                   │                       │                      │
  │ (EAP Challenge,       │                   │                       │                      │
  │  authCtxId)          │                   │                       │                      │
  │◄──────────────────────│                   │                       │                      │
  │                       │                   │                       │                      │
  │ PUT /slice-auth/{id} │                   │                       │                      │
  │ (EAP Response)       │                   │                       │                      │
  │──────────────────────►│                   │                       │                      │
  │                       │───────────────────►│                       │                      │
  │                       │                   │ 8. Encode EAP into    │                      │
  │                       │                   │    RADIUS Access-Req  │                      │
  │                       │                   │──────────────────────►│─────────────────────►│
  │                       │                   │                       │                      │
  │                       │                   │                       │    RADIUS Access-    │
  │                       │                   │                       │    Accept/Reject      │
  │                       │                   │                       │◄────────────────────│
  │                       │                   │ 9. Decode EAP result │                      │
  │                       │                   │ 10. Update session    │                      │
  │                       │                   │     state in DB        │                      │
  │ 200 OK               │                   │                       │                      │
  │ (EAP Result,         │                   │                       │                      │
  │  authResult)         │                   │                       │                      │
  │◄──────────────────────│                   │                       │                      │
```

**Key separation of concerns in Flow A:**
- **HTTP Gateway:** TLS termination, routing N58 to Biz Pod. No encode/decode, no session state.
- **Biz Pod:** Full EAP encode/decode via `internal/radius/` encode/decode functions. Session state management. Calls AAA Gateway via `httpAAAClient`.
- **AAA Gateway:** Raw RADIUS UDP socket I/O. Writes session correlation to Redis. Publishes responses via Redis pub/sub. No EAP decode/encode.

#### Flow B: AAA-S → NSSAAF → AMF (Re-Auth Triggered)

> **Note (Phase R):** Server-initiated messages arrive at the AAA Gateway via RADIUS/Diameter sockets. The AAA Gateway looks up the session correlation in Redis to determine the `authCtxId`, then POSTs to the Biz Pod's `/aaa/server-initiated` endpoint.

```
AAA-S              AAA GW                Biz Pod              UDM                  AMF
  │                   │                    │                    │                    │
  │ RADIUS Re-Auth    │                    │                    │                    │
  │ Request           │                    │                    │                    │
  │──────────────────►│                    │                    │                    │
  │                   │ 1. Lookup session  │                    │                    │
  │                   │    corr in Redis   │                    │                    │
  │                   │                    │                    │                    │
  │                   │ 2. HTTP POST /aaa/│                    │                    │
  │                   │    server-initiated│                    │                    │
  │                   │───────────────────►│                    │                    │
  │                   │                    │ 3. Validate AAA-S  │                    │
  │                   │                    │    authorization   │                    │
  │                   │                    │ 4. Nudm_UECM_Get  │                    │
  │                   │                    │    (GPSI)         │                    │
  │                   │                    │──────────────────►│                    │
  │                   │                    │                    │                    │
  │                   │                    │    AMF ID(s)      │                    │
  │                   │                    │◄──────────────────│                    │
  │                   │                    │                    │                    │
  │                   │                    │ 5. Nnssaaf_NSSAA_│                    │
  │                   │                    │    Re-AuthNotif   │                    │
  │                   │                    │                   │                    │
  │                   │                    │───────────────────────────────────────►│
  │                   │                    │                    │                    │
  │                   │                    │                    │    204 No Content  │
  │                   │                    │◄───────────────────────────────────────│
  │                   │                    │                    │                    │
  │                   │                    │ 6. AMF triggers    │                    │
  │                   │                    │    NSSAA procedure │                    │
  │                   │                    │    (→ Flow A)      │                    │
  │                   │                    │                    │                    │
  │ ACK (immediate)  │                    │                    │                    │
  │◄──────────────────│                    │                    │                    │
```

### 5.3 Thread Model

> **Note (Phase R):** In the 3-component model, threads are distributed across three separate processes. RADIUS Sender and Diameter Sender run in the AAA Gateway, not in the Biz Pod. The Biz Pod's thread model includes only HTTP/2 acceptor and EAP state processing.

**AAA Gateway process threads:**
```
├── RADIUS UDP Listener    (goroutine per listener: 1 thread)
│     └── recv/send loop, non-blocking UDP
│
├── Diameter TCP/SCTP Listener (goroutine per listener: 1 thread)
│     └── accept/recv loop, non-blocking I/O
│
└── Redis Pub/Sub Handler  (goroutine: 1 thread)
      └── response dispatch to pending channels
```

**Biz Pod process threads:**
```
├── SBI HTTP/2 Acceptor     (thread pool: 4-8 threads)
│     └── per-request: route → validate → dispatch
│
├── EAP State Processor     (dedicated thread pool: 16-32 threads)
│     └── per-session: advance EAP state machine
│
└── Background Workers:
      ├── NRF heartbeat       (interval: 5 min)
      ├── AAA health checker   (interval: 30s) — via HTTP to AAA Gateway
      ├── Session timeout      (scanner: every 1 min)
      └── Audit log flusher    (batch: every 5s)
```

### 5.4 Multi-Pod Kubernetes Deployment

#### 5.4.1 The Source-IP Problem

In a multi-pod Kubernetes deployment, each pod has its own ephemeral IP. Consumer NFs and AAA servers need a **single, stable address** to reach NSSAAF regardless of which pod handles a request:

| Protocol | Transport | Consumer perspective | IP stability needed |
|---|---|---|---|
| HTTP/2 (SBI N58/N60) | TCP/443 | AMF/AUSF discover via NRF with FQDN | No — TLS cert bound to FQDN |
| RADIUS | UDP/1812 | AAA-S uses source IP for shared-secret validation | **Yes — stable IP required** |
| Diameter | TCP/SCTP/3868 | AAA-S uses source IP for connection authorization | **Yes — stable IP required** |

Embedding RADIUS/Diameter clients directly in each NSSAAF app pod fails because AAA-S would see different source IPs from different pods, breaking shared-secret validation and connection authorization.

#### 5.4.2 Architecture: 3-Component Model

The production deployment follows a **3-component model**, each deployed as a separate Kubernetes Deployment:

1. **HTTP Gateway**: Go stdlib `net/http` with TLS 1.3. Each replica binds its own pod IP for external interfaces. Envoy migration planned (see `docs/roadmap/PHASE_Refactor_3Component.md` §7).
2. **AAA Gateway**: Custom Go UDP/TCP server. **2 replicas: 1 active + 1 standby**, sharing a **Virtual IP (VIP)** via **keepalived**. The VIP floats over a **Multus CNI bridge VLAN** interface, giving AAA-S a single consistent IP regardless of which replica is active.
3. **Business Logic Pods**: NSSAAF application code. No direct external connectivity. Receives and sends AAA traffic via HTTP through the AAA Gateway.

**Implementation:** Initial deployment uses Go stdlib for HTTP Gateway and a custom Go UDP/TCP server for AAA Gateway.

```
+=============================================================================================================+
||                                       Operator Network                                                     ||
||                                                                                                            ||
||   +---------+  +---------+            +---------------------------------------+      +---------------+      ||
||   |   AMF   |  |  AUSF   |            |             AAA-S Server             |      |     NRF       |      ||
||   | (N58)  |  |  (N60)  |            |  * RADIUS: shared-secret by IP     |      |               |      ||
||   +----+----+  +----+----+            |  * Diameter: CER/CEA by source IP   |      +-------+-------+      ||
||        |            |                            +----------------+----------------+            |             ||
||        | HTTPS/443 | HTTPS/443                  | UDP:1812 / TCP:3868            |            |             ||
||        v            v                              +--------------------------------v-+          |             ||
||   +====================================+              |                            |          |             ||
||   ||  HTTP Gateway (N replicas)       ||              |                            |          |             ||
||   ||  Go stdlib net/http (TLS 1.3)       ||              |                            |          |             ||
||   ||  * TLS 1.3 termination          ||              |                            |          |             ||
||   ||  * Routes N58/N60 to Biz pods   ||              |                            |          |             ||
||   ||  * Stable FQDN: nssaa-gw.operator.com        ||              |                            |          |             ||
||   ||  * Binds to pod IP on external iface ||              |                            |          |             ||
||   +====================================+              |                            |          |             ||
||                 | ClusterIP                            |                            |          |             ||
||   +=============+====================================+                            |          |             ||
||   ||                         Business Logic Pods (N replicas)                  ||                            |             ||
||   ||  +===========================================================+           ||                            |             ||
||   ||  | NSSAAF Business Logic                                  |           ||                            |             ||
||   ||  |  * SBI Handlers (N58/N60/N59)                        |           ||                            |             ||
||   ||  |  * EAP Engine                                         |           ||                            |             ||
||   ||  |  * Session State (PostgreSQL + Redis)                 |           ||                            |             ||
||   ||  |  * No direct external connectivity                     |           ||                            |             ||
||   ||  |  * Receives/transmits AAA via HTTP to AAA Gateway    |           ||                            |             ||
||   ||  +===========================================================+           ||                            |             ||
||   +=================================================================+                            |             ||
||                            | HTTP/8080                              |                            |             ||
||   +========================+========================================+                            |             ||
||   ||                    AAA Gateway (2 replicas: 1 active + 1 standby)         ||                            |             ||
||   ||  +=========================================================+             ||                            |             ||
||   ||  | Go Custom UDP/TCP Server                            |             ||                            |             ||
||   ||  |  * RADIUS UDP listener (:1812)                              |             ||                            |             ||
||   ||  |  * Diameter TCP/SCTP listener (:3868)                        |             ||                            |             ||
||   ||  |  * FORWARDS raw transport to/from Business Logic via HTTP   |             ||                            |             ||
||   ||  |  * No encode/decode -- pass-through                        |             ||                            |             ||
||   ||  +=========================================================+             ||                            |             ||
||   ||  Multus CNI Bridge VLAN Interface                              |             ||                            |             ||
||   ||  +=========================================================+             ||                            |             ||
||   ||  | keepalived: 1 active + 1 standby                          |             ||                            |             ||
||   ||  | Virtual IP (VIP) = stable AAA-S facing IP                 |             ||                            |             ||
||   ||  +=========================================================+             ||                            |             ||
||   +==================================================================+                            |             ||
||                                  | VIP (keepalived)                                              |             ||
||                                  +---------------------------------------------------------------v-------------+
+=============================================================================================================+
```

#### 5.4.3 Component Responsibilities

**HTTP Gateway (N replicas, Go stdlib):**
- Terminates TLS 1.3 for AMF/AUSF SBI traffic
- Routes N58 and N60 requests to Business Logic pods via internal ClusterIP
- Applies rate limiting, circuit breaking, observability
- NRF registration: the HTTP Gateway's FQDN/IP is the SBI contact address. NRF registration is performed by Biz Pod, not the HTTP Gateway itself (see §5.4.8)
- Each replica binds its own pod IP on the external interface — no VIP needed for HTTP

**AAA Gateway (2 replicas: active + standby, Go UDP/TCP + keepalived):**
- Terminates RADIUS (UDP/1812) and Diameter (TCP/SCTP/3868) from AAA-S
- **No encode/decode** — forwards raw RADIUS/Diameter transport messages to the Business Logic pod via internal HTTP
- Forwards raw response messages from Business Logic back to AAA-S
- The Virtual IP (VIP) from keepalived is the **single stable source IP** seen by AAA-S
- **Active-standby failover**: if the active pod dies, keepalived migrates the VIP to the standby within seconds; AAA-S sees no address change
- Wire protocol: see `internal/proto/aaa_transport.go` (`AaaForwardRequest`/`AaaForwardResponse`) and `internal/proto/biz_callback.go` (`AaaResponseEvent`, `SessionCorrEntry`)

**Business Logic Pods (N replicas, NSSAAF app):**
- Receives raw AAA transport from AAA Gateway via internal HTTP
- Performs full EAP encode/decode, EAP state machine, session management
- Sends encoded EAP responses back to AAA Gateway for transmission to AAA-S
- No direct external connectivity — purely internal HTTP communication
- Wire protocol: uses `httpAAAClient` (`cmd/biz/http_aaa_client.go`) which implements `eap.AAAClient` and forwards via `proto.AaaForwardRequest`

**Why no encode/decode in AAA Gateway?**
- Separation of concerns: AAA Gateway handles only transport; Business Logic handles EAP semantics
- AAA Gateway can be updated (protocol tweaks, TLS cert rotation) without affecting EAP session state
- RADIUS/Diameter library changes do not require redeploying the Business Logic pod
- Protocol translation (e.g., RADIUS <-> Diameter bridging) can be added in the Business Logic layer, not the gateway

#### 5.4.4 HTTP Gateway: Pod IP Binding

Each HTTP Gateway replica binds its **own pod IP** on the external interface. AMF/AUSF discover NSSAAF via NRF using an FQDN, which resolves to the LoadBalancer IP or the individual pod IPs. The Go stdlib HTTP server handles TLS termination and routes HTTP/2 requests to Business Logic pods.

Key deployment parameters:

```yaml
# http-gateway/deployment.yaml
spec:
  replicas: 3                    # scale horizontally
  template:
    spec:
      containers:
        - name: http-gw
          env:
            - name: BIND_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          args:
            - "$(BIND_IP)"      # Go HTTP server binds to pod IP
```

**Why bind pod IP for HTTP?** AMF via NRF uses FQDN-based discovery — no hardcoded IP needed. TLS certificates are bound to the FQDN, not the pod IP. The LoadBalancer routes traffic to the pod IPs automatically. This eliminates the need for keepalived on the HTTP side.

#### 5.4.5 AAA Gateway: HA with Keepalived + Multus CNI

The AAA Gateway is deployed with **2 replicas** in an **active-standby** configuration using keepalived to manage a shared Virtual IP (VIP). This ensures HA without requiring the AAA-S to re-establish connections on failover.

```
                                    +------------------+
  AAA-S ──────────────────────────► |   VIP (floating) |  <-- keepalived managed
                                    +--------+---------+
                                             |
                            +----------------+--------------------+
                            |                                     |
                       [ACTIVE]                              [STANDBY]
                       pod-1                                   pod-2
                     :1812 UDP                               :1812 UDP
                     :3868 TCP/SCTP                         :3868 TCP/SCTP
                     pod IP: 10.244.1.10                  pod IP: 10.244.2.10
                            |                                     |
                     Multus Bridge VLAN                   Multus Bridge VLAN
                     net0: vlan-100                        net0: vlan-100
                     IP: 10.1.100.x                        IP: 10.1.100.x
                            |                                     |
                     Physical NIC                           Physical NIC
                            +-------------------------------------+
                                             |
                                       To AAA-S
```

**Multus CNI Bridge VLAN:**
- Each AAA Gateway pod attaches to a secondary interface via Multus CNI
- The interface is a **bridge VLAN** (`vlan-100`) that routes traffic over the physical network
- Both replicas have interfaces on the same VLAN subnet
- keepalived binds the VIP to this interface; the standby monitors the active via VRRP
- When the active pod dies, keepalived on the standby promotes its interface to take the VIP

```yaml
# aaa-gateway/deployment.yaml
spec:
  replicas: 2                      # 1 active + 1 standby; NEVER scale beyond 2
  strategy:
    type: Recreate                # prevents two active pods during rolling update
  template:
    spec:
      containers:
        - name: aaa-gw
          securityContext:
            capabilities:
              add: ["NET_ADMIN"]  # needed for keepalived to manage VIP
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
      # Multus CNI: secondary interface on bridge VLAN
      annotations:
        k8s.v1.cni.cncf.io/networks: |
          [{
            "name": "aaa-bridge-vlan",
            "interface": "net0",
            "ips": ["$(POD_IP)/24"],
            "gateway": ["10.1.100.1"]
          }]
```

**keepalived configuration:**

```ini
# /etc/keepalived/keepalived.conf (in ConfigMap)
vrrp_instance vi_aaa {
    state BACKUP           # both start as BACKUP; one wins priority election
    interface net0         # Multus CNI interface, not the default eth0
    virtual_router_id 60   # must be unique per cluster
    priority 100           # active = 100, standby = 90
    advert_int 1
    unicast_peer {
        10.1.100.11        # standby pod IP (injected via downward API)
    }
    virtual_ipaddress {
        10.1.100.200/24    # VIP: stable IP seen by AAA-S
    }
    track_script {
        chk_aaa_gw          # check aaa-gw container process is healthy
    }
}
```

**Critical constraints:**
- `replicas: 2` is a hard maximum — never scale beyond 2. Diameter (RFC 6733) and RADIUS source-IP secrets require exactly one active connection at a time.
- `strategy: Recreate` prevents rolling updates that could briefly create two actives
- The Multus bridge VLAN must be configured on the switch/physical network to allow both nodes to send traffic with the same VIP as source IP
- Both nodes must be on the same L2 broadcast domain for VRRP to work (same VLAN, same subnet)

#### 5.4.6 Internal Communication

```
AMF                    HTTP GW              Biz Pod              AAA GW (active)           AAA-S
  |                      |                   |                       |                      |
  | HTTPS/443           |                   |                       |                      |
  | POST /slice-auth    |                   |                       |                      |
  |─────────────────────►│                   |                       |                      |
  |                      │ HTTP/8080         |                       |                      |
  |                      │ POST /slice-auth  |                       |                      |
  |                      │───────────────────►│                       |                      |
  |                      |                   |                       |                      |
  |                      |                   | 1. Create session     |                      |
  |                      |                   | 2. Encode EAP         |                      |
  |                      |                   |                       |                      |
  |                      |                   | HTTP/9090             | UDP:1812             |
  |                      |                   | POST /aaa/forward     │ UDP/TCP payload      |
  |                      |                   |──────────────────────►│─────────────────────►│
  |                      |                   |                       |                      |
  |                      |                   |                       |    RADIUS/Diameter   |
  |                      |                   |                       │◄────────────────────│
  |                      |                   |                       |                      |
  |                      |                   | HTTP/9090             |                      |
  |                      |                   │◄──────────────────────│                      |
  |                      |                   │ 200 OK (raw response)  |                      |
  |                      |                   │                       |                      |
  |                      │                   │ 3. Decode EAP         |                      |
  |                      │                   │ 4. Advance state       |                      |
  |                      │                   │                       |                      |
  |                      │                   │ 201 Created           |                      |
  |                      │                   │ (EAP message, authCtxId)                      |
  |                      │ HTTP/8080         │                       |                      |
  | 201 Created         │◄──────────────────│                       |                      |
  │ (EAP message,       │                   |                       |                      |
  │  authCtxId)         │                   |                       |                      |
  │◄────────────────────│                   |                       |                      |
```

**Service discovery for internal communication:**
- HTTP Gateway → Business Logic: `svc-nssaa-biz:8080` (ClusterIP)
- Business Logic → AAA Gateway: `svc-nssaa-aaa:9090` (ClusterIP, routes to whichever pod holds the VIP)
- The ClusterIP for AAA Gateway resolves to both replicas; the active pod handles the traffic

**Session routing:** Business Logic pods store EAP session state in Redis. When the AAA Gateway forwards a response from AAA-S, it publishes an `AaaResponseEvent` to the Redis channel `nssaa:aaa-response`. All Biz Pods receive every event; each discards events not matching its in-flight sessions. Session correlation entries (mapping `sessionId` → `authCtxId`) are stored at Redis keys `nssaa:session:{sessionId}` by the AAA Gateway before forwarding to AAA-S. See `internal/proto/biz_callback.go` for wire protocol types.

#### 5.4.7 Component Configuration Structure

**Kubernetes manifests** (planned for production, see `docs/roadmap/`):

```
nssAAF/
├── kustomization.yaml
├── http-gateway/
│   ├── deployment.yaml          # Go stdlib HTTP gateway (Deployment, N replicas)
│   ├── service.yaml             # LoadBalancer / ClusterIP
│   └── configmap.yaml           # Go server config (no Envoy bootstrap needed)
├── biz/
│   ├── deployment.yaml          # NSSAAF Business Logic (Deployment, N replicas)
│   ├── service.yaml             # ClusterIP (:8080) for internal SBI
│   └── configmap.yaml           # NSSAAF config
└── aaa-gateway/
    ├── deployment.yaml          # AAA Gateway (Deployment, replicas=2, strategy=Recreate)
    ├── service.yaml             # ClusterIP (:9090) for biz pods
    ├── configmap.yaml           # Go stdlib + custom UDP/TCP server bootstrap
    ├── keepalived.conf          # ConfigMap: keepalived VRRP configuration
    └── network-attachments.yaml  # Multus CNI CRD for bridge VLAN interface
```

**Development configuration** (see `compose/`):

```
compose/
├── dev.yaml                    # docker-compose for all 3 components
└── configs/
    ├── http-gateway.yaml       # HTTP Gateway config
    ├── biz.yaml                # Biz Pod config
    └── aaa-gateway.yaml        # AAA Gateway config
```

#### 5.4.8 NF Profile Registration (NRF)

Biz Pod registers the HTTP Gateway's FQDN/IP with NRF. The AAA Gateway VIP is **not** registered in NRF — it is an internal address used only for NSSAAF-to-AAA-S communication:

```json
{
  "nfInstanceId": "nssAAF-instance-001",
  "nfType": "NSSAAF",
  "nfServices": [
    {
      "serviceName": "nnssaaf-nssaa",
      "fqdn": "nssaa-gw.operator.com",
      "ipEndPoints": [
        { "ipv4Address": "203.0.113.10", "port": 443, "transport": "TCP" }
      ]
    },
    {
      "serviceName": "nnssaaf-aiw",
      "fqdn": "nssaa-gw.operator.com",
      "ipEndPoints": [
        { "ipv4Address": "203.0.113.10", "port": 443, "transport": "TCP" }
      ]
    }
  ],
  "customInfo": {
    "aaaGateway": {
      "replicas": 2,
      "haMode": "active-standby",
      "vip": "10.1.100.200",
      "interface": "net0",
      "virtualRouterId": 60,
      "aaaProtocolBinding": {
        "RADIUS": { "port": 1812, "transport": "UDP", "vipRef": "aaa-gw-vip" },
        "Diameter": { "port": 3868, "transport": "SCTP", "vipRef": "aaa-gw-vip" }
      }
    }
  }
}
```

#### 5.4.9 Deployment Scale Tiers

| Tier | HTTP GW | Biz Pods | AAA Gateway | Use Case |
|------|---------|----------|-------------|----------|
| **Development** | Embedded | Embedded | Embedded | Single-node |
| **Small** | 2 replicas | 2 replicas | 2 replicas (active-standby) | Multi-node, single AZ |
| **Production** | 3+ replicas | 5+ replicas | 2 replicas (active-standby) | Multi-AZ, HA |
| **Carrier-grade** | 5+ replicas | 10+ replicas | 2 replicas per AAA-S cluster | Full redundancy, multi-PLMN |

**Key scaling principle:** HTTP Gateway and Business Logic pods scale horizontally independently. The AAA Gateway **never exceeds 2 replicas** — this is a hard constraint: Diameter requires a single active connection, and RADIUS source-IP secrets require a single active IP. Adding a second AAA-S (for HA or per-PLMN) means adding a **second pair of AAA Gateway replicas** with their own VIP, not scaling the existing pair.

## 6. NF Profile Specification

### 6.1 NF Instance Registration

```yaml
nfInstanceId: "<uuid-v7>"
nfType: NSSAAF
nfStatus: REGISTERED

# Identity
# In multi-pod deployments, these are HTTP Gateway IPs (see §5.4)
# The HTTP Gateway uses a stable IP/FQDN that NRF and consumers reference.
nodeId:
  Fqdn: "nssaa-gw.operator.com"
  nodeIpList:
    - "203.0.113.10"    # AZ1 HTTP gateway
    - "203.0.113.11"    # AZ2 HTTP gateway (HA pair)
    - "203.0.113.12"    # AZ3 HTTP gateway (HA pair)

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
# All services point to the HTTP Gateway (see §5.4.2), which routes to NSSAAF app pods internally.
nfServices:
  nnssaaf-nssaa:
    version: "v1"
    fqdn: "nssaa-gw.operator.com"
    apiPrefix: "https://nssaa-gw.operator.com/nnssaaf-nssaa"
    ipEndPoints:
      - ipv4Address: "203.0.113.10"
        port: 443
        transport: "TCP"
      - ipv4Address: "203.0.113.11"
        port: 443
        transport: "TCP"
    securityMethods: ["TLS 1.3"]
    supportedFeatures: "NSSAA-REAUTH|NSSAA-REVOC|EAP-TLS|EAP-TTLS"

  nnssaaf-aiw:
    version: "v1"
    fqdn: "nssaa-gw.operator.com"
    apiPrefix: "https://nssaa-gw.operator.com/nnssaaf-aiw"
    ipEndPoints:
      - ipv4Address: "203.0.113.10"
        port: 443
        transport: "TCP"
      - ipv4Address: "203.0.113.11"
        port: 443
        transport: "TCP"

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
  # AAA Gateway configuration (see §5.4)
  # These are NOT registered in NRF — used internally for NSSAAF-to-AAA-S communication.
  aaaGateway:
    replicas: 2                       # 1 active + 1 standby via keepalived
    haMode: "active-standby"        # keepalived VRRP, VIP floats between replicas
    vip: "10.1.100.200"             # Virtual IP seen by AAA-S (Multus CNI bridge VLAN)
    interface: "net0"                # Multus CNI secondary interface on bridge VLAN
    virtualRouterId: 60              # VRRP group ID (unique per cluster)
    radiusGateway:
      port: 1812
      transport: "UDP"
      forwardOnly: true              # No encode/decode in gateway -- raw transport forwarded to Biz
    diameterGateway:
      port: 3868
      transport: "SCTP"
      forwardOnly: true              # No encode/decode in gateway -- raw transport forwarded to Biz
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
