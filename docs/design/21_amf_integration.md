---
spec: TS 23.502 §4.2.9.2 / TS 29.526 §7.2
section: §4.2.9
interface: N58 (AMF-NSSAAF)
service: Nnssaaf_NSSAA
operation: Authenticate, Re-AuthenticationNotification, RevocationNotification
---

# NSSAAF AMF Integration Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, the AMF sends requests to the HTTP Gateway (N58), which routes to Biz Pods. Re-auth/revocation notifications are sent by Biz Pods via HTTP. See `docs/design/01_service_model.md` §5.4 for the architecture overview.

AMF là consumer chính của NSSAAF. AMF tích hợp qua N58 interface (Nnssaaf_NSSAA service) và nhận callbacks cho re-auth/revocation notifications.

---

## 2. AMF Callback Endpoint

### 2.1 AMF Exposes Notification Endpoint

AMF phải expose một HTTP endpoint để nhận Re-AuthenticationNotification và RevocationNotification từ NSSAAF.

```
AMF exposes: https://{amf_fqdn}/nnssaaf-callback/v1/notifications
```

**Security:** NSSAAF gọi endpoint này qua mTLS. AMF validate token từ NRF.

### 2.2 Callback Payload

**Re-AuthenticationNotification:**

```json
POST https://amf1.operator.com:8080/nnssaaf-callback/v1/notifications

Headers:
  Content-Type: application/json
  Authorization: Bearer {token}
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

**AMF Response:** `204 No Content`

**AMF Expected Action:** Trigger NSSAA procedure (§4.2.9.2)

---

## 3. AMF → NSSAAF Flows

### 3.1 During Registration

> **Note (Phase R):** In the 3-component model, step 7 (NSSAAF processes) executes in the Biz Pod: it validates the request, creates a session, encodes the EAP message, sends to the AAA Gateway via HTTP, which forwards to AAA-S. See `01_service_model.md` §5.4.6 for the internal communication flow.

```
Registration Request arrives at AMF
         │
         ▼
AMF identifies S-NSSAI requiring NSSAA
         │
         ▼
AMF sends NAS Network Slice-Specific Authentication Command (EAP Identity Request)
         │
         ▼
UE responds with EAP Identity Response in NAS MM Transport
         │
         ▼
AMF → NSSAAF: POST /slice-authentications
(N58 → HTTP Gateway → Biz Pod)
         │
         ▼
Biz Pod: validates request, creates session in PostgreSQL, encodes EAP,
         sends raw AAA bytes to AAA Gateway via HTTP POST /aaa/forward
         │
         ▼
AAA Gateway: receives raw packet, forwards to AAA-S
         │
         ▼
Biz Pod ← AAA Gateway: receives response via Redis pub/sub
         │
         ▼
NSSAAF → AMF: 201 Created (EAP message for UE)
(HTTP Gateway routes Biz Pod response back to AMF)
         │
         ▼
AMF sends NAS Network Slice-Specific Authentication Command to UE
         │
         ▼
... multi-round EAP ...
         │
         ▼
NSSAAF → AMF: 200 OK (EAP Result)
         │
         ▼
AMF sends NAS Network Slice-Specific Authentication Result to UE
         │
         ▼
AMF updates Allowed NSSAI based on result
```

### 3.2 H-PLMN S-NSSAI

AMF luôn gửi **H-PLMN S-NSSAI** (không phải mapped value) trong NSSAA request:

```json
{
  "snssai": {
    "sst": 1,
    "sd": "000001"  // H-PLMN SD, not mapped
  }
}
```

---

## 4. AMF Notification Callback Discovery

AMF callback URI được AMF cung cấp trong request (`reauthNotifUri`, `revocNotifUri`). AMF có thể discover URI của chính nó qua NRF:

```go
// AMF profile in NRF:
GET /nnrf-disc/v1/nf-instances/{amfInstanceId}

{
  "nfInstanceId": "amf-instance-001",
  "nfType": "AMF",
  "nfServices": {
    "nnssaaf-nssaa-notif": {
      "ipEndPoints": [{ "ipv4Address": "10.1.0.5", "port": 8080 }],
      "fqdn": "amf1.operator.com"
    }
  }
}
```

**Alternative (simpler):** AMF tự construct callback URI dựa trên own FQDN/IP và đăng ký với NRF.
