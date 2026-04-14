---
spec: TS 29.503 / TS 23.502 §4.2.9.3 / TS 29.526
section: §4.2.9.3
interface: N59 (NSSAAF-UDM)
service: Nudm_UECM
operation: Nudm_UECM_Get
---

# NSSAAF UDM Integration Design

## 1. Overview

NSSAAF tích hợp với UDM qua Nudm_UECM_Get service để lấy AMF ID hiện tại của UE khi AAA-S trigger re-authentication hoặc revocation.

**Trigger flows:**
- §4.2.9.3 Step 3a: AAA-S → NSSAAF → UDM: get AMF ID for re-auth
- §4.2.9.4 Step 3a: AAA-S → NSSAAF → UDM: get AMF ID for revocation

---

## 2. Nudm_UECM_Get Service

### 2.1 Service Definition

```yaml
Service: Nudm_UECM_Get
Version: v1
Base URL: https://{udm_fqdn}/nudm-uem/v1
Spec: TS 29.503 §5.3.2
```

### 2.2 Get AMF Registration

```
GET /nudm-uem/v1/{gpsi}/registrations?service-names=namf-comm
```

**Path Parameter:**
- `gpsi`: GPSI của UE (matches `^5[0-9]{8,14}$`)

**Query Parameters:**
- `service-names`: Lọc registration theo service names (e.g., `namf-comm`)

**Response 200:**

```json
{
  "amfInfo": [
    {
      "amfInstanceId": "amf-instance-001",
      "amfSetId": "amf-set-01",
      "amfRegionId": "region-01",
      "guami": {
        "plmnId": {
          "mcc": "208",
          "mnc": "001"
        },
        "amfId": "amf-001"
      },
      "amfUri": "https://amf1.operator.com:8080/namf-comm/v1",
      "regTimestamp": "2025-01-01T10:00:00Z"
    },
    {
      "amfInstanceId": "amf-instance-002",
      "guami": {
        "plmnId": { "mcc": "208", "mnc": "001" },
        "amfId": "amf-002"
      },
      "amfUri": "https://amf2.operator.com:8080/namf-comm/v1"
    }
  ],
  "supportedFeatures": "3GPP-R18-UECM"
}
```

### 2.3 NRF Discovery for UDM

```go
// Discover UDM that exposes nudm-uem service
GET /nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem

// Response:
{
  "nfInstances": [
    {
      "nfInstanceId": "udm-instance-001",
      "nfType": "UDM",
      "nfServices": {
        "nudm-uem": {
          "ipEndPoints": [{ "ipv4Address": "10.0.4.10", "port": 8080 }],
          "fqdn": "udm.operator.com"
        }
      }
    }
  ]
}
```

---

## 3. Error Handling

| HTTP | Cause | Handling |
|------|-------|----------|
| 404 | GPSI not found in UDM | Stop procedure, ACK to AAA-S |
| 503 | UDM unavailable | Retry 3x with backoff, then return 503 to AAA-S |
| 504 | UDM timeout | Stop procedure, ACK to AAA-S |
| 401 | Token invalid | Retry with fresh token |

### 3.1 AMF Not Registered Case

```go
// When AMF is not registered in UDM for this GPSI:
if len(amfInfo) == 0 {
    // Step 3c: Send ACK to AAA-S immediately
    // Procedure stops here — UE may be deregistered
    log.Infof("No AMF registered for GPSI %s, stopping reauth", gpsI)
    return nil  // nil = stop procedure, ACK already sent
}
```

---

## 4. Multi-AMF Handling

```go
// When two AMF addresses are returned (multi-registration case)
if len(amfInfo) > 1 {
    // Option 1: Notify both AMFs
    for _, amf := range amfInfo {
        err := sendNotification(amf, notification)
        if err != nil {
            log.Errorf("Failed to notify AMF %s: %v", amf.InstanceId, err)
        }
    }

    // Option 2: Notify primary AMF first, fallback to secondary
    primary := amfInfo[0]
    err := sendNotification(primary, notification)
    if err != nil {
        log.Warnf("Primary AMF %s failed, trying secondary", primary.InstanceId)
        secondary := amfInfo[1]
        err = sendNotification(secondary, notification)
    }
}
```
