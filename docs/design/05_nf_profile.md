---
spec: TS 29.526 v18.7.0 / TS 29.510 v18.6.0
section: §6 (NF Registration), TS 29.510 §6
interface: Nnrf (NSSAAF-NRF)
service: Nnrf-disc, Nnrf-nfm
operation: NF Registration, NF Discovery, NF Heartbeat
---

# NSSAAF NF Profile & NRF Integration Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, NSSAAF is split into HTTP Gateway, Biz Pod, and AAA Gateway. See `docs/design/01_service_model.md` §5.4 for the architecture overview. NF profile registration (heartbeat, load updates) is performed by Biz Pods, which register the HTTP Gateway's address as the SBI contact point.

NRF (Network Repository Function) là NF trung tâm trong 5G SBA quản lý:
1. **NF Registration** — NSSAAF đăng ký profile với NRF
2. **NF Discovery** — NSSAAF discover các NF khác (AMF callback URI, UDM services)
3. **NF Heartbeat** — NSSAAF heartbeat định kỳ để giữ registration alive

Tài liệu này thiết kế chi tiết NRF integration cho NSSAAF.

---

## 2. NF Registration

### 2.1 Registration Sequence

```
NSSAAF                      NRF
  │                          │
  │ 1. POST /nf-instances    │
  │    (NFProfile)           │
  │─────────────────────────►│
  │                          │
  │ 2. 201 Created           │
  │    (NFInstanceId)        │
  │◄─────────────────────────│
  │                          │
  │ 3. Heartbeat every 5min  │
  │    PUT /nf-instances/{id}│
  │    (nfStatus=REGISTERED) │
  │─────────────────────────►│
```

### 2.2 NF Profile Specification

> **Note (Phase R):** The `ipv4Addresses` in NFProfile point to the **HTTP Gateway** pod IPs. The Biz Pod performs NRF registration and heartbeat — it reports the HTTP Gateway's address as the SBI contact point. See `01_service_model.md` §5.4.8 for how HTTP Gateway and Biz Pod coordinate for NRF registration.

```json
POST /nnrf-disc/v1/nf-instances

{
  "nfInstanceId": "nssAAF-instance-550e8400-e29b-41d4-a716-446655440000",
  "nfType": "NSSAAF",
  "nfStatus": "REGISTERED",
  "plmnId": {
    "mcc": "208",
    "mnc": "001"
  },
  "plmnList": [
    {
      "plmnId": { "mcc": "208", "mnc": "001" },
      "snssaiList": [
        { "sst": 1, "sd": "000001" },
        { "sst": 2, "sd": "000001" },
        { "sst": 4, "sd": "000001" }
      ]
    }
  ],
  "nsiList": [
    "nsi-001",
    "nsi-002"
  ],
  "fqdn": "nssAAF.operator.com",
  "ipv4Addresses": [
    "10.0.1.50",
    "10.0.2.50",
    "10.0.3.50"
  ],
  "nodeId": {
    "fqdn": "nssAAF-operator-1.operator.com",
    "ipv4Addresses": ["10.0.1.50"],
    "ipv6Addresses": []
  },
  "nffInfo": {
    "priority": 100,
    "capacity": 10000,
    "load": 0
  },
  "nssaaInfo": {
    "supiRanges": [
      {
        "start": "imu-208001000000000",
        "end": "imu-208001099999999",
        "pattern": "^imu-208001[0-9]{8}$"
      }
    ],
    "internalGroupIdentifiersRanges": [
      {
        "start": "group-001",
        "end": "group-999"
      }
    ],
    "supportedSecurityAlgorithm": [
      "EAP-TLS",
      "EAP-TTLS",
      "EAP-AKA_PRIME"
    ]
  },
  "nfServices": {
    "nnssaaf-nssaa": {
      "serviceInstanceId": "nnssaaf-nssaa-001",
      "serviceName": "nnssaaf-nssaa",
      "versions": [
        {
          "apiVersion": "v1",
          "fullVersion": "1.2.1",
          "expiry": "2030-12-31T23:59:59Z"
        }
      ],
      "scheme": "https",
      "fqdn": "nssAAF.operator.com",
      "apiPrefix": "https://nssAAF.operator.com/nnssaaf-nssaa",
      "ipEndPoints": [
        {
          "ipv4Address": "10.0.1.50",
          "port": 443,
          "transport": "TCP",
          "priority": 100,
          "weight": 1
        },
        {
          "ipv4Address": "10.0.2.50",
          "port": 443,
          "transport": "TCP",
          "priority": 100,
          "weight": 1
        },
        {
          "ipv4Address": "10.0.3.50",
          "port": 443,
          "transport": "TCP",
          "priority": 100,
          "weight": 1
        }
      ],
      "supportedFeatures": "3GPP-R18-NSSAA-REAUTH-REVOC",
      "securityMethods": ["TLS 1.3"]
    },
    "nnssaaf-aiw": {
      "serviceInstanceId": "nnssaaf-aiw-001",
      "serviceName": "nnssaaf-aiw",
      "versions": [
        {
          "apiVersion": "v1",
          "fullVersion": "1.1.0",
          "expiry": "2030-12-31T23:59:59Z"
        }
      ],
      "scheme": "https",
      "fqdn": "nssAAF.operator.com",
      "apiPrefix": "https://nssAAF.operator.com/nnssaaf-aiw",
      "ipEndPoints": [
        {
          "ipv4Address": "10.0.1.50",
          "port": 443,
          "transport": "TCP",
          "priority": 100,
          "weight": 1
        }
      ],
      "supportedFeatures": "3GPP-R18-AIW"
    }
  },
  "heartBeatTimer": 300,
  "priority": 100,
  "capacity": 10000,
  "load": 0,
  "kafkaInfo": {},
  "customInfo": {
    "supportedAaaProtocols": ["RADIUS", "DIAMETER"],
    "maxEapRounds": 20,
    "eapTimeoutSeconds": 30,
    "operationalStatus": "ACTIVE"
  }
}
```

### 2.3 Heartbeat Mechanism

```go
// Heartbeat: every 5 minutes (300s)
// Sends PUT to update nfStatus and load
const heartBeatInterval = 5 * time.Minute

type HeartbeatPayload struct {
    NFInstanceID string `json:"nfInstanceId"`
    nfStatus      string `json:"nfStatus"`  // always REGISTERED
    HeartBeatTimer int    `json:"heartBeatTimer"`
    Load          int    `json:"load"`       // current load percentage (0-100)
}

// Load calculation:
// load = (active_sessions / capacity) * 100
// Active session = sessions with nssaa_status = 'PENDING'
```

```go
// Background goroutine: heartbeat loop
func (n *NRFClient) StartHeartbeat(ctx context.Context) {
    ticker := time.NewTicker(heartBeatInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Calculate current load
            load := n.calculateLoad()

            // Send heartbeat
            err := n.PutNFInstance(ctx, &HeartbeatPayload{
                NFInstanceID: n.nfInstanceId,
                nfStatus:      "REGISTERED",
                HeartBeatTimer: 300,
                Load:          load,
            })
            if err != nil {
                log.Errorf("NRF heartbeat failed: %v", err)
                // Retry with exponential backoff
            }
        }
    }
}
```

---

## 3. NF Discovery

### 3.1 Discover AMF Callback URI

NSSAAF cần discover AMF's callback URI để gửi Re-Auth/Revocation notifications:

```go
// NRF Discovery: find AMF instances that expose Nnssaaf_NSSAA_Notification service
GET /nnrf-disc/v1/nf-instances?target-nf-type=AMF&service-names=nnssaaf-nssaa-notif
```

**Problem:** AMF không expose notification service qua NRF discovery. AMF được implicit subscription — NSSAAF dùng `amfInstanceId` trong SliceAuthInfo để xác định AMF.

**Solution:** AMF instance ID được gửi bởi AMF trong `amfInstanceId` field của SliceAuthInfo. NSSAAF:

1. Dùng AMF instance ID để lookup AMF profile từ NRF (optional)
2. Hoặc dùng trực tiếp `reauthNotifUri`/`revocNotifUri` được AMF cung cấp trong request

```go
// Discover AMF profile (optional)
amfProfile, err := nrfClient.GetNFInstance(ctx, amfInstanceId)
// Returns: AMF's fqdn, ipEndPoints, services
```

### 3.2 Discover UDM Service

NSSAAF discover UDM's Nudm_UECM service để gọi Nudm_UECM_Get:

```go
// NRF Discovery: find UDM that exposes nudm-uem service
GET /nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem
```

**Response:**
```json
{
  "nfInstances": [
    {
      "nfInstanceId": "udm-instance-001",
      "nfType": "UDM",
      "nfServices": {
        "nudm-uem": {
          "ipEndPoints": [
            {
              "ipv4Address": "10.0.4.10",
              "port": 8080
            }
          ],
          "fqdn": "udm.operator.com"
        }
      }
    }
  ]
}
```

### 3.3 Service Discovery Caching

```go
// NRF cache with TTL
type NRFDiscoveryCache struct {
    mu    sync.RWMutex
    cache map[string]*CacheEntry
    ttl   time.Duration
}

type CacheEntry struct {
    Data      interface{}
    DiscoveredAt time.Time
    ExpiresAt   time.Time
}

// Cache keys:
// - "udm:uem:{plmnId}"  → UDM Nudm_UECM endpoint
// - "amf:{amfId}"       → AMF profile
// - "ausf:auth"         → AUSF endpoint

func (c *NRFDiscoveryCache) Get(ctx context.Context, key string) (interface{}, error) {
    c.mu.RLock()
    entry, exists := c.cache[key]
    c.mu.RUnlock()

    if exists && time.Now().Before(entry.ExpiresAt) {
        return entry.Data, nil
    }

    // Cache miss or expired: query NRF
    data, err := c.queryNRF(ctx, key)
    if err != nil {
        return nil, err
    }

    // Update cache
    c.mu.Lock()
    c.cache[key] = &CacheEntry{
        Data:        data,
        DiscoveredAt: time.Now(),
        ExpiresAt:   time.Now().Add(5 * time.Minute),
    }
    c.mu.Unlock()

    return data, nil
}
```

### 3.4 DNS-based Service Resolution

```go
// NSSAAF FQDN → IP resolution via Kubernetes DNS
// nssAAF.operator.com → LoadBalancer → HTTP Gateway pods

// For multi-pod: Kubernetes Headless Service
// nssAAF.operator.com → all pod IPs (A records)
// Client-side load balancing by NSSAAF HTTP Gateway (not Envoy sidecar)

// Service URL construction:
func BuildServiceURL(baseURL, service, version string) string {
    return fmt.Sprintf("%s/%s/%s", baseURL, service, version)
}

// Example:
udmUemURL := BuildServiceURL("https://udm.operator.com", "nudm-uem", "v1")
// "https://udm.operator.com/nudm-uem/v1"
```

---

## 4. OAuth 2.0 / NRF Token Validation

### 4.1 NRF as OAuth 2.0 Authorization Server

NRF cung cấp OAuth 2.0 token cho SBI communication:

```
Consumer (AMF/AUSF) → NRF OAuth2: POST /oauth2/token
                           │ (client credentials grant)
                           ▼
                     NRF issues JWT
                           │
                           ▼
                     Consumer → NSSAAF: Bearer {JWT}
```

### 4.2 Token Validation in NSSAAF

```go
type TokenClaims struct {
    jwt.RegisteredClaims
    Scope    string `json:"scope"`
    NfType   string `json:"nf_type"`
    NfId     string `json:"nf_id"`
    PlmnId   string `json:"plmn_id"`
    Exp      int64  `json:"exp"`
}

func ValidateToken(tokenString string, requiredScope string) (*TokenClaims, error) {
    // 1. Verify JWT signature using NRF's public key (cached)
    publicKey, err := nrfClient.GetNRFPublicKey(ctx)
    if err != nil {
        return nil, err
    }

    // 2. Parse and validate
    token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{},
        func(token *jwt.Token) (interface{}, error) {
            if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
                return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
            }
            return publicKey, nil
        })

    if err != nil {
        return nil, err
    }

    claims, ok := token.Claims.(*TokenClaims)
    if !ok {
        return nil, ErrInvalidToken
    }

    // 3. Validate scope
    if !hasScope(claims.Scope, requiredScope) {
        return nil, ErrInsufficientScope
    }

    // 4. Validate expiry
    if claims.Exp < time.Now().Unix() {
        return nil, ErrTokenExpired
    }

    return claims, nil
}

// Required scopes:
const (
    ScopeNnssaafNssaa = "nnssaaf-nssaa"   // For N58 API
    ScopeNnssaafAiw   = "nnssaaf-aiw"     // For N60 API
)
```

### 4.3 Token Caching

```go
// NRF token caching (reuse tokens until near expiry)
// Token TTL typically: 3600s (1 hour)
// Refresh when: remaining_life < 300s (5 min buffer)

type TokenCache struct {
    mu          sync.RWMutex
    tokens      map[string]*CachedToken
    nrfClient   *NRFClient
}

type CachedToken struct {
    AccessToken string
    ExpiresAt  time.Time
    AcquiredAt time.Time
}

func (c *TokenCache) GetToken(ctx context.Context, consumerNFType, scope string) (string, error) {
    key := fmt.Sprintf("%s:%s", consumerNFType, scope)

    c.mu.RLock()
    cached := c.tokens[key]
    c.mu.RUnlock()

    // Use cached token if still valid (>5 min remaining)
    if cached != nil && time.Until(cached.ExpiresAt) > 5*time.Minute {
        return cached.AccessToken, nil
    }

    // Acquire new token from NRF
    newToken, err := c.nrfClient.RequestToken(ctx, consumerNFType, scope)
    if err != nil {
        return "", err
    }

    c.mu.Lock()
    c.tokens[key] = newToken
    c.mu.Unlock()

    return newToken.AccessToken, nil
}
```

---

## 5. NF Deregistration

### 5.1 Graceful Shutdown Sequence

```go
// On SIGTERM (Kubernetes termination):
// 1. Stop accepting new requests
// 2. Wait for in-flight requests to complete (30s max)
// 3. Deregister from NRF
// 4. Exit

func GracefulShutdown(ctx context.Context, nrf *NRFClient, instanceId string) {
    // Step 1: Stop accepting new requests
    listener.Close()

    // Step 2: Wait for in-flight requests (with timeout)
    waitGroup.WaitWithTimeout(30 * time.Second)

    // Step 3: Deregister from NRF
    deregErr := nrf.DeleteNFInstance(ctx, instanceId)
    if deregErr != nil {
        log.Errorf("NRF deregistration failed: %v", deregErr)
    }

    log.Info("NSSAAF shutdown complete")
}
```

### 5.2 NRF Deregistration Request

```go
DELETE /nnrf-disc/v1/nf-instances/{nfInstanceId}

// Expected: 204 No Content
```

---

## 6. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | NSSAAF đăng ký NFProfile với NRF tại startup | POST /nf-instances on boot |
| AC2 | Heartbeat gửi mỗi 5 phút | Background goroutine, PUT /nf-instances |
| AC3 | Load được tính và cập nhật trong heartbeat | active_sessions / capacity * 100 |
| AC4 | UDM service discovery qua NRF | GET /nf-instances?target-nf-type=UDM&service-names=nudm-uem |
| AC5 | NRF discovery cache với TTL 5 min | NRFDiscoveryCache struct |
| AC6 | JWT token validation với scope check | ValidateToken() với requiredScope |
| AC7 | Token caching (reuse cho đến khi <5 min expiry) | TokenCache struct |
| AC8 | Deregister khi graceful shutdown | DELETE /nf-instances/{id} |
