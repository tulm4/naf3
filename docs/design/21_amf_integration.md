---
spec: TS 23.502 §4.2.9.2 / TS 29.526 §7.2
section: §4.2.9
interface: N58 (AMF-NSSAAF)
service: Nnssaaf_NSSAA
operation: Authenticate, Re-AuthenticationNotification, RevocationNotification
---

# NSSAAF AMF Integration Design

## 1. Overview

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
(EAP Identity Response, GPSI, S-NSSAI, amfInstanceId, reauthNotifUri, revocNotifUri)
         │
         ▼
NSSAAF processes → RADIUS/Diameter → AAA-S
         │
         ▼
NSSAAF → AMF: 201 Created (EAP message for UE)
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
