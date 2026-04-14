# NSSAAF API Operations

Extracted from: 3GPP TS 29.526 v18.7.0

Source files: `TS29526_Nnssaaf_NSSAA.yaml`, `TS29526_Nnssaaf_AIW.yaml`

---

## Nnssaaf_NSSAA Service Operations

### POST /slice-authentications — CreateSliceAuthenticationContext

Creates a new slice authentication context resource on NSSAAF.

**Consumer:** AMF

**Service Operation:** Nnssaaf_NSSAA_Authenticate (Request/Response)

**Request Schema:** `SliceAuthInfo`

```
POST {apiRoot}/nnssaaf-nssaa/v1/slice-authentications
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| gpsi | Gpsi | M | Generic Public Subscriber Identifier |
| snssai | Snssai | M | Single-NSSAI requiring auth |
| eapIdRsp | EapMessage | M | EAP Identity Response from UE |
| amfInstanceId | NfInstanceId | O | AMF instance ID |
| reauthNotifUri | Uri | O | Callback URI for reauth notifications |
| revocNotifUri | Uri | O | Callback URI for revocation notifications |

**Response 201 Created:** `SliceAuthContext`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| gpsi | Gpsi | M | — |
| snssai | Snssai | M | — |
| authCtxId | SliceAuthCtxId | M | Resource ID for this context |
| eapMessage | EapMessage | M | Next EAP message to forward to UE |

**Response Headers:** `Location: {apiRoot}/nnssaaf-nssaa/v1/slice-authentications/{authCtxId}`

**Error Codes:** 400, 403, 404, 502, 503, 504

---

### PUT /slice-authentications/{authCtxId} — ConfirmSliceAuthentication

Confirms the result of a slice authentication round. Used for multi-step EAP.

**Consumer:** AMF

**Request:** `SliceAuthConfirmationData`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| gpsi | Gpsi | M | — |
| snssai | Snssai | M | — |
| eapMessage | EapMessage | M | EAP response from UE |

**Response 200:** `SliceAuthConfirmationResponse`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| gpsi | Gpsi | M | — |
| snssai | Snssai | M | — |
| eapMessage | EapMessage | M | Next EAP message (or final result) |
| authResult | AuthStatus | O | Final result: EAP_SUCCESS or EAP_FAILURE |

---

### Callbacks (Server-Sent)

#### reauthenticationNotification

AMF provides `reauthNotifUri` in SliceAuthInfo. NSSAAF calls back when AAA-S triggers reauth.

**Notification Schema:** `SliceAuthReauthNotification`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| notifType | SliceAuthNotificationType | M | Always `SLICE_RE_AUTH` |
| gpsi | Gpsi | M | — |
| snssai | Snssai | M | — |
| supi | Supi | O | Subscription Permanent Identifier |

#### revocationNotification

AMF provides `revocNotifUri` in SliceAuthInfo. NSSAAF calls back when AAA-S revokes auth.

**Notification Schema:** `SliceAuthRevocNotification`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| notifType | SliceAuthNotificationType | M | Always `SLICE_REVOCATION` |
| gpsi | Gpsi | M | — |
| snssai | Snssai | M | — |
| supi | Supi | O | — |

---

## Nnssaaf_AIW Service Operations

Used by AUSF for SNPN authentication with Credentials Holder via AAA Server.

### POST /authentications — CreateAuthenticationContext

**Consumer:** AUSF

**Request Schema:** `AuthInfo`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| supi | Supi | M | Subscription Permanent Identifier |
| eapIdRsp | EapMessage | O | EAP Identity Response |
| ttlsInnerMethodContainer | EapMessage | O | For EAP-TTLS |
| supportedFeatures | SupportedFeatures | O | Feature flags |

**Response 201:** `AuthContext`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| supi | Supi | M | — |
| authCtxId | AuthCtxId | M | Resource ID |
| eapMessage | EapMessage | O | Next EAP message |
| ttlsInnerMethodContainer | EapMessage | O | — |
| supportedFeatures | SupportedFeatures | O | — |

---

### PUT /authentications/{authCtxId} — ConfirmAuthentication

**Request:** `AuthConfirmationData`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| supi | Supi | M | — |
| eapMessage | EapMessage | M | EAP response |
| supportedFeatures | SupportedFeatures | O | — |

**Response 200:** `AuthConfirmationResponse`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| supi | Supi | M | — |
| eapMessage | EapMessage | M | — |
| authResult | AuthStatus | O | Final result |
| pvsInfo | ServerAddressingInfo[] | O | Privacy-violating servers info |
| msk | Msk | O | Master Session Key (from EAP-TLS) |
| supportedFeatures | SupportedFeatures | O | — |

---

## Notification Type Enum

```
SliceAuthNotificationType:
  enum:
    - SLICE_RE_AUTH      # AAA-S triggered re-authentication
    - SLICE_REVOCATION   # AAA-S triggered authorization revocation
```

## Simple Types

```
SliceAuthCtxId: string          # Resource ID for NSSAA context
AuthCtxId: string               # Resource ID for AIW context
EapMessage: string (byte)       # Base64-encoded EAP packet, nullable
```
