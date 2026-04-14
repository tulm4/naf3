---
spec: TS 29.526 v18.7.0 / TS 23.502 v18.4.0 / TS 33.501 v18.10.0
section: §14.4.1.2, §4.2.9.2
interface: N58 (AMF-NSSAAF)
service: Nnssaaf_NSSAA
operation: Authenticate
eapMethod: EAP-TLS
aaaProtocol: RADIUS
---

# [Feature Name] — Design Document

## 1. Overview

[Một đoạn: feature này làm gì, tại sao cần, ngữ cảnh trong kiến trúc NSSAAF]

## 2. Prerequisites

- [ ] UE đã đăng ký với AMF và có GPSI
- [ ] AMF đã có AMF ID lưu trong UDM
- [ ] S-NSSAI yêu cầu NSSAA trong subscription data
- [ ] AAA-S đã được cấu hình trong NSSAAF (per-S-NSSAI mapping)

## 3. API Design

### Endpoint

```
POST /nnssaaf-nssaa/v1/slice-authentications
PUT  /nnssaaf-nssaa/v1/slice-authentications/{authCtxId}
```

### Request Schema (SliceAuthInfo)

| Field | Type | Required | Source |
|-------|------|----------|--------|
| gpsi | Gpsi | M | TS29571 |
| snssai | Snssai | M | TS29571 |
| eapIdRsp | EapMessage | M | TS29526 |
| amfInstanceId | NfInstanceId | O | TS29526 |
| reauthNotifUri | Uri | O | TS29526 |
| revocNotifUri | Uri | O | TS29526 |

### Response Schema (SliceAuthContext)

| Field | Type | Required | Source |
|-------|------|----------|--------|
| gpsi | Gpsi | M | TS29526 |
| snssai | Snssai | M | TS29526 |
| authCtxId | SliceAuthCtxId | M | TS29526 |
| eapMessage | EapMessage | M | TS29526 |

### Error Handling

| HTTP | cause | Triggered When |
|------|-------|----------------|
| 400 | BAD_REQUEST | Invalid EAP payload |
| 403 | AUTHENTICATION_REJECTED | AAA-S rejects auth |
| 404 | USER_NOT_FOUND | GPSI not found |
| 502 | AAA_UNREACHABLE | Cannot reach AAA-S |
| 503 | AAA_UNAVAILABLE | AAA-S temporarily down |
| 504 | AAA_TIMEOUT | Timeout |

## 4. Procedure Flow

**Spec reference:** TS 23.502 §4.2.9.2 / TS 33.501 §16.3

```
Step 1:  [Who] → [Action]
         [Spec citation]

Step 2:  [Who] → [Action]
         [Spec citation]
...
```

### 4.1 Happy Path

### 4.2 Failure Cases

- [ ] EAP-Failure from AAA-S → step 17 trả về EAP_FAILURE
- [ ] AAA-S unreachable → 502 error
- [ ] Timeout → 504 error, AMF giữ status PENDING
- [ ] UE unreachable trong multi-round → AMF giữ NOT_EXECUTED

## 5. State Machine

```
                    ┌──────────────┐
                    │ NOT_EXECUTED │
                    └──────┬───────┘
                           │ [trigger]
                           ▼
                    ┌──────────────┐
                    │   PENDING    │ ← intermediate state bắt buộc
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            │            ▼
      ┌──────────────┐     │    ┌──────────────┐
      │ EAP_SUCCESS  │     │    │  EAP_FAILURE  │
      └──────────────┘     │    └──────────────┘
                           │
                           │ [timeout]
                           ▼
                    ┌──────────────┐
                    │ NOT_EXECUTED │ ← retry next registration
                    └──────────────┘
```

## 6. AAA Protocol Mapping

**Spec reference:** TS 29.561 Ch.16 (RADIUS) / Ch.17 (Diameter)

### 6.1 RADIUS

```
SBI message          → RADIUS message
─────────────────────────────────────
POST /slice-auth     → RADIUS Access-Request
PUT  /slice-auth     → RADIUS Access-Challenge
SliceAuthContext     → RADIUS Access-Accept/Reject
```

### 6.2 Key RADIUS AVPs

| AVP | Value | Source |
|-----|-------|--------|
| Calling-Station-Id | GPSI | TS29561 §16.3 |
| 3GPP-S-NSSAI (#200) | Snssai (SST + SD) | TS29561 §16.3.2 |
| EAP-Message | EapMessage | RFC 3579 |

## 7. Security Considerations

**Spec reference:** TS 33.501 §16.3, Annex B.2 (EAP-TLS)

- EAP method: [EAP-TLS / EAP-AKA' / ...]
- NSSAAF role: [EAP authenticator backend]
- SEAF/AMF role: [EAP Authenticator]
- Key derivation: MSK từ TLS (RFC 5216)
- Privacy: [Có/Không] yêu cầu privacy-protecting EAP method

## 8. NRM Impact

**Spec reference:** TS 28.541 §5.3.145

- NSSAAFFunction attributes affected: [danh sách]
- Endpoint impacts: [EP_N58 / EP_N59]

## 9. Notification Handling

### 9.1 Re-AuthenticationNotification (AAA-S → NSSAAF → AMF)

**Spec reference:** TS 29.526 Callback / TS 33.501 §16.4

### 9.2 RevocationNotification

**Spec reference:** TS 29.526 Callback / TS 33.501 §16.5

## 10. Acceptance Criteria

| # | Criteria | Spec Paragraph |
|---|----------|---------------|
| AC1 | AMF có thể tạo slice authentication context qua POST | TS 29.526 §7.2.2 |
| AC2 | NSSAAF forward EAP messages giữa AMF và AAA-S | TS 33.501 §16.3 |
| AC3 | NssaaStatus chuyển NOT_EXECUTED → PENDING → EAP_SUCCESS/EAP_FAILURE | TS 29.571 §5.4.4.60 |
| AC4 | GPSI bắt buộc trong mọi NSSAA request | TS 23.502 §4.2.9.1 |
| AC5 | AAA-P được support cho third-party AAA-S | TS 29.561 §16.1.1 |
| AC6 | RADIUS Access-Request chứa 3GPP-S-NSSAI VSA #200 | TS 29.561 §16.3.2 |
