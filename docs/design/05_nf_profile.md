---
spec: TS 29.510 v18.11.0
section: §5.2 (Nnrf_NFManagement), §5.3 (Nnrf_NFDiscovery), §5.4 (Nnrf_AccessToken), §6.1-6.3
interface: Nnrf (NSSAAF-NRF)
service: Nnrf-disc, Nnrf-nfm, Nnrf-oauth2
operation: NF Registration, NF Discovery, NF Heartbeat, Token Request
---

# NSSAAF NF Profile & NRF Integration Design

## 1. Overview

NRF (Network Repository Function) is the central NF in 5G SBA managing:
1. **NF Registration** — NSSAAF registers profile with NRF (Nnrf_NFManagement)
2. **NF Discovery** — NSSAAF discovers other NFs via NRF (Nnrf_NFDiscovery)
3. **NF Heartbeat** — NSSAAF sends periodic heartbeat to keep registration alive
4. **OAuth2 Token** — NSSAAF obtains access tokens for SBI calls (Nnrf_AccessToken)

> **Phase R Note:** After the 3-component refactor, NSSAAF is split into HTTP Gateway, Biz Pod, and AAA Gateway. See `docs/design/01_service_model.md` §5.4. NF profile registration is performed by **HTTP Gateway**, which registers its own address as the SBI contact point.

### 1.1 Implementation Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Implementation location | HTTP Gateway | External SBI interface, owns lifecycle |
| NF Profile configuration | YAML file per deployment | Versionable, auditable, git-friendly |
| NF Instance ID | Static UUID in config | Predictable, idempotent registration |
| NRF OAuth2 | Client credentials | Production-grade security (TS 29.510 §5.4) |
| Heartbeat interval | NRF-negotiated | TS 29.510 §5.2.2.3.2 compliant |
| Heartbeat failure | Retry then degrade, re-register | Self-healing, no manual intervention |

### 1.2 Component Responsibilities

```
┌─────────────────────────────────────────────────────────────┐
│                      HTTP Gateway                            │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────┐ │
│  │   NRF Client    │  │  NFProfile Mgr  │  │ Heartbeat   │ │
│  │  - Register     │  │  - Load from    │  │ Manager     │ │
│  │  - Heartbeat    │  │    YAML         │  │ - Negotiate │ │
│  │  - Deregister   │  │  - Validate     │  │ - Retry     │ │
│  │  - OAuth2 token │  │  - Merge env    │  │ - Re-reg    │ │
│  └─────────────────┘  └─────────────────┘  └─────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**Spec Reference:** TS 29.510 V18.11.0 (2026-03)

---

## 2. Configuration

### 2.1 NF Profile YAML Configuration

NFProfile is configured via YAML file per deployment:

```yaml
# config/nf-profile.yaml
nfProfile:
  instanceId: "550e8400-e29b-41d4-a716-446655440000"  # Static UUID, pre-generated

  # Identity
  instanceName: "nssAAF-gw-001"
  fqdn: "nssAAF.operator.com"
  locality: "dc-1"
  nfSetId: "nssAAF-set-001"

  # Network addresses (ipEndPoints take precedence over fqdn)
  ipv4Addresses:
    - "10.0.1.50"
    - "10.0.2.50"

  # PLMN configuration
  plmnList:
    - mcc: "208"
      mnc: "001"
    - mcc: "208"
      mnc: "93"

  # S-NSSAI support
  snssais:
    - sst: 1
      sd: "000001"
    - sst: 2

  # NSSAAF-specific info
  nssaafInfo:
    supiRanges:
      - start: "imsi-208010000000001"
        end: "imsi-208019999999999"
        pattern: "^imsi-20801[0-9]{8}$"
        size: "LARGE"
    internalGroupIdentifiersRanges:
      - start: "group-001"
        end: "group-999"

  # Services offered by this NSSAAF
  nfServices:
    nnssaaf-nssaa:
      serviceInstanceId: "nnssaaf-nssaa-001"
      apiPrefix: "/nnssaaf-nssaa/v1"
      allowedNfTypes: ["AMF"]
      capacity: 1000
      priority: 100
      supportedFeatures: "3GPP-R18-NSSAA-REAUTH-REVOC"
    nnssaaf-aiw:
      serviceInstanceId: "nnssaaf-aiw-001"
      apiPrefix: "/nnssaaf-aiw/v1"
      allowedNfTypes: ["AUSF"]
      capacity: 1000
      priority: 100
      supportedFeatures: "3GPP-R18-AIW"

  # Custom capabilities
  customInfo:
    supportedAaaProtocols: ["RADIUS", "DIAMETER"]
    maxEapRounds: 20
    eapTimeoutSeconds: 30
```

### 2.2 NRF Client Configuration

```yaml
nrf:
  # NRF connection
  baseUrl: "https://nrf.operator.com"
  timeoutSeconds: 30

  # OAuth2 client credentials (for NRF lifecycle)
  accessToken:
    enabled: true
    authServer: "https://nrf.operator.com/oauth2/token"
    clientId: "nssAAF-client"
    clientSecret: "${NRF_CLIENT_SECRET}"  # From env/K8s secret
    scope: "nnrf-nfm"

  # Heartbeat configuration
  heartbeat:
    initialIntervalSeconds: 300  # Requested initial interval
    acceptNegotiatedInterval: true  # Accept NRF-adjusted value
    maxConsecutiveFailures: 3  # Failures before re-registration

  # Discovery cache
  discoveryCache:
    enabled: true
    defaultTTLSeconds: 3600
```

### 2.3 Configuration Loading

```go
type Config struct {
    NFProfile NFProfileConfig `yaml:"nfProfile"`
    NRF       NRFConfig       `yaml:"nrf"`
}

type NFProfileConfig struct {
    InstanceID   string              `yaml:"instanceId"`
    InstanceName string              `yaml:"instanceName"`
    FQDN         string              `yaml:"fqdn"`
    // ... other fields
}

type NRFConfig struct {
    BaseURL         string          `yaml:"baseUrl"`
    Timeout         time.Duration   `yaml:"timeoutSeconds"`
    AccessToken     TokenConfig     `yaml:"accessToken"`
    Heartbeat       HeartbeatConfig `yaml:"heartbeat"`
    DiscoveryCache  CacheConfig     `yaml:"discoveryCache"`
}

// LoadConfig reads YAML, merges environment variable overrides
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("reading config: %w", err)
    }

    // Expand environment variables in config
    expanded := os.ExpandEnv(string(data))

    var cfg Config
    if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
        return nil, fmt.Errorf("parsing yaml: %w", err)
    }

    return &cfg, nil
}
```

---

## 3. Nnrf_NFManagement Service

### 2.1 Service Operations

| Operation | HTTP Method | Resource URI | Description |
|-----------|-------------|-------------|-------------|
| NFRegister | PUT | `/nnrf-nfm/v1/nf-instances/{nfInstanceID}` | Register NSSAAF profile |
| NFUpdate | PATCH | `/nnrf-nfm/v1/nf-instances/{nfInstanceID}` | Partial profile update (heartbeat) |
| NFDeregister | DELETE | `/nnrf-nfm/v1/nf-instances/{nfInstanceID}` | Deregister from NRF |
| NFStatusSubscribe | POST | `/nnrf-nfm/v1/subscriptions` | Subscribe to NF status changes |
| NFProfileRetrieval | GET | `/nnrf-nfm/v1/nf-instances/{nfInstanceID}` | Retrieve own profile |

### 3.2 NFRegister Procedure (§5.2.2.2)

**Resource URI:** `{apiRoot}/nnrf-nfm/v1/nf-instances/{nfInstanceID}`

```
PUT /nnrf-nfm/v1/nf-instances/4947a69a-f61b-4bc1-b9da-47c9c5d14b64
```

**Request Headers:**
- `Content-Type: application/json`

**Success Response:** `201 Created`
- `Location` header contains URI of created resource
- `HeartBeat-Interval` header indicates heartbeat interval (seconds)

**Error Responses:**
- `400 Bad Request` — Invalid NFProfile encoding
- `403 Forbidden` — Not authorized
- `500 Internal Server Error` — NRF internal error
- `3xx` — Redirection to another NRF instance

### 3.3 NFProfile Specification (§6.1.6.2.2)

> **Spec Reference:** TS 29.510 §6.1.6.2.2

#### Mandatory Fields (M)

| Field | Type | Description |
|-------|------|-------------|
| nfInstanceId | string (UUIDv4) | Globally unique NF instance ID |
| nfType | NFType | Must be "NSSAAF" |
| nfStatus | NFStatus | REGISTERED, SUSPENDED, UNDISCOVERABLE |

#### NFProfile for NSSAAF (3GPP Compliant)

```json
PUT /nnrf-nfm/v1/nf-instances/4947a69a-f61b-4bc1-b9da-47c9c5d14b64

{
  "nfInstanceId": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64",
  "nfType": "NSSAAF",
  "nfStatus": "REGISTERED",
  "nfInstanceName": "nssAAF-operator-001",
  "heartBeatTimer": 30,

  "plmnList": [
    { "mcc": "208", "mnc": "001" }
  ],

  "nssaafInfo": {
    "supiRanges": [
      {
        "start": "imsi-208010000000001",
        "end": "imsi-208019999999999",
        "pattern": "^imsi-20801[0-9]{8}$",
        "size": "LARGE"
      }
    ],
    "internalGroupIdentifiersRanges": [
      {
        "start": "group-001",
        "end": "group-999"
      }
    ]
  },

  "nfServices": [
    {
      "serviceInstanceId": "nnssaaf-nssaa-001",
      "serviceName": "nnssaaf-nssaa",
      "versions": [
        {
          "apiVersion": "v1"
        }
      ],
      "scheme": "https",
      "nfServiceStatus": "REGISTERED",
      "fqdn": "nssAAF.operator.com",
      "apiPrefix": "https://nssAAF.operator.com/nnssaaf-nssaa/v1",
      "ipEndPoints": [
        {
          "ipv4Address": "10.0.1.50",
          "port": 443,
          "transport": "TCP"
        },
        {
          "ipv4Address": "10.0.2.50",
          "port": 443,
          "transport": "TCP"
        }
      ],
      "capacity": 1000,
      "priority": 100,
      "allowedNfTypes": ["AMF"],
      "allowedNfDomains": ["operator.com"],
      "supportedFeatures": "3GPP-R18-NSSAA-REAUTH-REVOC"
    },
    {
      "serviceInstanceId": "nnssaaf-aiw-001",
      "serviceName": "nnssaaf-aiw",
      "versions": [
        {
          "apiVersion": "v1"
        }
      ],
      "scheme": "https",
      "nfServiceStatus": "REGISTERED",
      "fqdn": "nssAAF.operator.com",
      "apiPrefix": "https://nssAAF.operator.com/nnssaaf-aiw/v1",
      "ipEndPoints": [
        {
          "ipv4Address": "10.0.1.50",
          "port": 443,
          "transport": "TCP"
        }
      ],
      "capacity": 1000,
      "priority": 100,
      "allowedNfTypes": ["AUSF"],
      "allowedNfDomains": ["operator.com"],
      "supportedFeatures": "3GPP-R18-AIW"
    }
  ],

  "nfSetId": "nssAAF-set-001",
  "locality": "dc-1",
  "customInfo": {
    "supportedAaaProtocols": ["RADIUS", "DIAMETER"],
    "maxEapRounds": 20,
    "eapTimeoutSeconds": 30
  }
}
```

#### NssaafInfo Type (§6.1.6.2.104)

Per TS 29.510, `nssaafInfo` has only two attributes:

| Attribute | Type | Cardinality | Description |
|-----------|------|-------------|-------------|
| supiRanges | array(SupiRange) | 1..N (O) | Ranges of SUPIs served by NSSAAF |
| internalGroupIdentifiersRanges | array(InternalGroupIdRange) | 1..N (O) | Ranges of internal group IDs served |

> **Note:** `supportedSecurityAlgorithm` is NOT in TS 29.510 NssaafInfo. Security algorithms should be indicated via `supportedFeatures` in each `nfServices` entry if needed.

#### NFService Type (§6.1.6.2.3)

| Attribute | Type | P | Cardinality | Description |
|-----------|------|---|-------------|-------------|
| serviceInstanceId | string | M | 1 | Unique service instance ID |
| serviceName | string | M | 1 | Service name (e.g., "nnssaaf-nssaa") |
| versions | array(NFServiceVersion) | M | 1..N | Supported API versions |
| **scheme** | string | **M** | 1 | http, https (MANDATORY) |
| **nfServiceStatus** | NFServiceStatus | **M** | 1 | Service status (MANDATORY) |
| fqdn | string | O | 0..1 | Fully Qualified Domain Name |
| **ipEndPoints** | array(IpEndPoint) | O | 1..N | IP addresses and ports |
| apiPrefix | string | O | 0..1 | API prefix URI |
| capacity | integer | O | 0..1 | Capacity indicator |
| priority | integer | O | 0..1 | Priority for load balancing |
| supportedFeatures | string | O | 0..1 | Supported feature flags |
| allowedNfTypes | array(NFType) | O | 0..N | Allowed consumer NF types |
| allowedNfDomains | array(string) | O | 0..N | Allowed consumer domains |

> **Note:** `ipEndPoints` takes precedence over `fqdn` when using HTTP scheme. The spec states: "IP addresses provided in ipEndPoints have precedence over IP addresses provided as part of the NFProfile information and, when using the HTTP scheme, over FQDN provided as part of the NFProfile information."

#### IpEndPoint Type (§6.1.6.2.5)

| Attribute | Type | P | Cardinality | Description |
|-----------|------|---|-------------|-------------|
| ipv4Address | string | C | 0..1 | IPv4 address (mutually exclusive with ipv6Address) |
| ipv6Address | string | C | 0..1 | IPv6 address (mutually exclusive with ipv4Address) |
| transport | string | O | 0..1 | Transport protocol |
| port | integer | O | 0..1 | Port number (0-65535). If absent, default HTTP port is used: 80 for http, 443 for https |

> **Note:** At most one of ipv4Address or ipv6Address shall be included. If no port is specified, the NF consumer uses the default HTTP port.

#### Service Names for NSSAAF

| Service Name | Description | Interface |
|--------------|-------------|-----------|
| nnssaaf-nssaa | NSSAA authentication service | N58 |
| nnssaaf-aiw | AUSF interworking service | N60 |

### 3.4 NFUpdate (Heartbeat) Procedure (§5.2.2.3.1B)

Heartbeat is implemented as a **partial update** using PATCH:

**Resource URI:** `{apiRoot}/nnrf-nfm/v1/nf-instances/{nfInstanceID}`

```
PATCH /nnrf-nfm/v1/nf-instances/4947a69a-f61b-4bc1-b9da-47c9c5d14b64
Content-Type: application/json-patch+json
If-Match: "etag-from-registration"
```

**Request Body:** JSON Patch operations

```json
[
  { "op": "replace", "path": "/nfStatus", "value": "REGISTERED" },
  { "op": "replace", "path": "/heartBeatTimer", "value": 30 }
]
```

**Success Response:** `204 No Content`

**Heartbeat Interval:**
- NRF returns `HeartBeat-Interval` header on registration (typically 30-60 seconds)
- NSSAAF must send heartbeat before interval expires
- If missed, NRF marks NSSAAF as SUSPENDED

**Load Update (optional):**
```json
[
  { "op": "replace", "path": "/nfStatus", "value": "REGISTERED" }
]
```

### 3.5 NFDeregister Procedure (§5.2.2.4)

```
DELETE /nnrf-nfm/v1/nf-instances/4947a69a-f61b-4bc1-b9da-47c9c5d14b64
```

**Success Response:** `204 No Content`

---

## 4. Nnrf_NFDiscovery Service

### 3.1 NFDiscover Procedure (§5.3.2.2)

**Resource URI:** `{apiRoot}/nnrf-disc/v1/nf-instances`

```
GET /nnrf-disc/v1/nf-instances?target-nf-type=AMF&requester-nf-type=NSSAAF
```

**Mandatory Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| target-nf-type | NFType | Type of NF to discover |
| requester-nf-type | NFType | Type of requesting NF (NSSAAF) |

**Optional Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| service-names | array(string) | Filter by service names |
| req-snssais | array(Snssai) | Filter by S-NSSAI |
| req-plmn-list | array(Plmn) | Filter by PLMN |
| limit | integer | Max results |

**Success Response:** `200 OK`

```json
{
  "validityPeriod": 3600,
  "nfInstances": [
    {
      "nfInstanceId": "amf-instance-001",
      "nfType": "AMF",
      "nfStatus": "REGISTERED",
      "nfServices": [...]
    }
  ]
}
```

### 3.2 Discovery Use Cases

#### Discover AMF (for notifications)
```
GET /nnrf-disc/v1/nf-instances?target-nf-type=AMF&requester-nf-type=NSSAAF
```

#### Discover AUSF (for AIW)
```
GET /nnrf-disc/v1/nf-instances?target-nf-type=AUSF&requester-nf-type=NSSAAF
```

#### Discover UDM (for AMF ID lookup)
```
GET /nnrf-disc/v1/nf-instances?target-nf-type=UDM&service-names=nudm-uem&requester-nf-type=NSSAAF
```

### 3.3 Discovery Cache

```go
type DiscoveryCache struct {
    mu    sync.RWMutex
    cache map[string]*CacheEntry
    ttl   time.Duration
}

type CacheEntry struct {
    Data       interface{}
    ExpiresAt  time.Time
}

// Cache keys: "amf:{plmnId}", "ausf:{plmnId}", "udm:uem:{plmnId}"
// TTL: Use validityPeriod from NRF response (typically 3600s)
```

---

## 5. Nnrf_AccessToken Service

### 4.1 Access Token Request Procedure (§5.4.2.2)

**Endpoint:** `{nrfApiRoot}/oauth2/token`

```
POST /oauth2/token
Content-Type: application/x-www-form-urlencoded
```

**Request Parameters (form-urlencoded):**

| Parameter | Required | Description |
|-----------|----------|-------------|
| grant_type | M | "client_credentials" |
| client_id | M | NSSAAF instance ID (UUID) |
| scope | M | Requested service names |
| requester_nf_type | M | "NSSAAF" |
| target_nf_type | O | Target NF type |
| target_nf_instance_id | O | Specific target NF instance |

**Example:**
```
grant_type=client_credentials&client_id=4947a69a-f61b-4bc1-b9da-47c9c5d14b64&scope=nnssaaf-nssaa&requester_nf_type=NSSAAF&target_nf_type=AMF
```

**Success Response:** `200 OK`

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "scope": "nnssaaf-nssaa"
}
```

### 4.2 JWT Token Structure

Access tokens are JWTs (RFC 7519) signed by NRF.

**Header:**
```json
{
  "alg": "RS256",
  "typ": "JWT"
}
```

**Claims:**
```json
{
  "iss": "http://nrf.operator.com/nnrf-oauth2",
  "sub": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64",
  "aud": "nnssaaf-nssaa",
  "scope": "nnssaaf-nssaa",
  "exp": 1721112000,
  "iat": 1721108400,
  "nf_type": "NSSAAF",
  "nf_id": "4947a69a-f61b-4bc1-b9da-47c9c5d14b64"
}
```

### 4.3 Token Caching

```go
type TokenCache struct {
    mu        sync.RWMutex
    tokens    map[string]*CachedToken
    nrfClient *NRFClient
}

type CachedToken struct {
    AccessToken string
    ExpiresAt  time.Time
}

// Refresh when remaining life < 5 minutes
func (c *TokenCache) GetToken(ctx context.Context, scope string) (string, error) {
    // Check cache...
    // If expired or expiring soon, request new token
}
```

### 4.4 Scopes for NSSAAF

| Scope | Description | Used For |
|-------|-------------|----------|
| nnssaaf-nssaa | NSSAA service | Calling AMF notifications |
| nnssaaf-aiw | AIW service | Calling AUSF |
| nnssaa-reauth-notification | Reauth notifications | Sending to AMF |
| nnssaa-revoc-notification | Revocation notifications | Sending to AMF |

---

## 6. Token Validation (Incoming Requests)

NSSAAF validates tokens from consumers (AMF, AUSF) using NRF's public key.

### 5.1 Validation Flow

```
AMF/AUSF → NRF: POST /oauth2/token
                ← JWT access_token

AMF/AUSF → NSSAAF: Authorization: Bearer {JWT}
NSSAAF:
  1. Verify JWT signature using NRF's public key
  2. Validate claims (iss, aud, exp, scope)
  3. Check scope matches requested operation
```

### 5.2 Validation Code

```go
type TokenClaims struct {
    jwt.RegisteredClaims
    Scope  string `json:"scope"`
    NfType string `json:"nf_type"`
    NfId   string `json:"nf_id"`
}

func ValidateToken(tokenString string, requiredScope string) (*TokenClaims, error) {
    // 1. Get NRF public key (cached)
    publicKey, err := nrfClient.GetNRFPublicKey(ctx)
    if err != nil {
        return nil, err
    }

    // 2. Parse and verify JWT
    token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{},
        func(token *jwt.Token) (interface{}, error) {
            if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
                return nil, fmt.Errorf("unexpected signing method")
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

    // 3. Check scope
    if !hasScope(claims.Scope, requiredScope) {
        return nil, ErrInsufficientScope
    }

    // 4. Check expiry
    if claims.ExpiresAt.Before(time.Now()) {
        return nil, ErrTokenExpired
    }

    return claims, nil
}

// Required scopes
const (
    ScopeNnssaafNssaa = "nnssaaf-nssaa"
    ScopeNnssaafAiw   = "nnssaaf-aiw"
)
```

---

## 7. Graceful Shutdown

```go
func GracefulShutdown(ctx context.Context, nrf *NRFClient, instanceId string) {
    // 1. Stop accepting new requests
    listener.Close()

    // 2. Wait for in-flight requests (with timeout)
    waitGroup.WaitWithTimeout(30 * time.Second)

    // 3. Deregister from NRF
    err := nrf.DeleteNFInstance(ctx, instanceId)
    if err != nil {
        log.Errorf("NRF deregistration failed: %v", err)
    }

    log.Info("NSSAAF shutdown complete")
}
```

---

## 8. Heartbeat Manager

The heartbeat manager implements self-healing registration with NRF-negotiated intervals.

### 8.1 State Machine

```
                    ┌─────────────────────────────────────┐
                    │            START                    │
                    └──────────────┬──────────────────────┘
                                   ▼
              ┌─────────────────────────────────────────┐
              │          REGISTER (PUT)                │
              │  - Load NFProfile from YAML            │
              │  - Send PUT /nnrf-nfm/v1/nf-instances │
              │  - Parse HeartBeat-Interval header      │
              └──────────────┬──────────────────────────┘
                             ▼
              ┌─────────────────────────────────────────┐
              │          HEARTBEATING (loop)            │
              │  - Wait for negotiated interval        │
              │  - Send PATCH with nfStatus=REGISTERED  │
              │  - On failure: increment retry counter  │
              └──────────────┬──────────────────────────┘
                             ▼
              ┌─────────────────────────────────────────┐
              │     Check consecutive failures          │
              └──────────────┬──────────────────────────┘
                             ▼
              ┌─────────────────────────────────────────┐
              │  failures >= maxConsecutiveFailures ?    │
              │                                         │
              │  NO ───► Continue heartbeating          │
              │                                         │
              │  YES ──► Mark registered=false         │
              └──────────────┬──────────────────────────┘
                             ▼
              ┌─────────────────────────────────────────┐
              │          RE-REGISTER (async)             │
              │  - Go Register()                        │
              │  - On success: resume heartbeating       │
              │  - On failure: retry with backoff        │
              └─────────────────────────────────────────┘
```

### 8.2 Heartbeat Manager Implementation

```go
type HeartbeatManager struct {
    nrfClient      *NRFClient
    profileManager *ProfileManager
    instanceID     string
    etag           string  // From registration response

    // Configuration
    initialInterval   time.Duration
    acceptNegotiated  bool
    maxFailures       int

    // Runtime state
    mu               sync.RWMutex
    registered       bool
    heartbeatTimer   time.Duration
    consecutiveFailures int

    // Control
    stopCh chan struct{}
    wg     sync.WaitGroup
}

func (m *HeartbeatManager) Start(ctx context.Context) error {
    // 1. Register with NRF
    if err := m.register(ctx); err != nil {
        return fmt.Errorf("initial registration: %w", err)
    }

    // 2. Start heartbeat loop
    m.wg.Add(1)
    go m.heartbeatLoop(ctx)

    return nil
}

func (m *HeartbeatManager) heartbeatLoop(ctx context.Context) {
    defer m.wg.Done()

    ticker := time.NewTicker(m.heartbeatTimer)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            m.deregister(ctx)
            return

        case <-m.stopCh:
            m.deregister(ctx)
            return

        case <-ticker.C:
            if err := m.heartbeat(ctx); err != nil {
                m.handleHeartbeatFailure(ctx, err)
            } else {
                m.consecutiveFailures = 0
                // Update ticker if interval changed
                ticker.Reset(m.heartbeatTimer)
            }
        }
    }
}

func (m *HeartbeatManager) heartbeat(ctx context.Context) error {
    m.mu.RLock()
    etag := m.etag
    m.mu.RUnlock()

    // Send PATCH with JSON Merge Patch (RFC 7396)
    patch := fmt.Sprintf(`{"nfStatus":"%s","heartBeatTimer":%d}`,
        NFStatusRegistered, int(m.heartbeatTimer.Seconds()))

    newEtag, err := m.nrfClient.UpdateNFInstance(ctx, m.instanceID, etag, patch)
    if err != nil {
        return err
    }

    // Update ETag for next heartbeat
    m.mu.Lock()
    m.etag = newEtag
    m.mu.Unlock()

    return nil
}

func (m *HeartbeatManager) handleHeartbeatFailure(ctx context.Context, err error) {
    m.mu.Lock()
    m.consecutiveFailures++
    failures := m.consecutiveFailures
    m.mu.Unlock()

    log.Warnf("NRF heartbeat failed (attempt %d/%d): %v",
        failures, m.maxFailures, err)

    if failures >= m.maxFailures {
        log.Errorf("NRF heartbeat degraded, initiating re-registration")

        m.mu.Lock()
        m.registered = false
        m.mu.Unlock()

        // Re-register asynchronously
        go func() {
            for {
                if err := m.register(context.Background()); err != nil {
                    log.Warnf("Re-registration failed, retrying: %v", err)
                    time.Sleep(exponentialBackoff(failures))
                } else {
                    log.Info("Re-registration successful, resuming heartbeat")
                    return
                }
            }
        }()
    }
}
```

### 8.3 Exponential Backoff

```go
func exponentialBackoff(attempt int) time.Duration {
    base := 5 * time.Second
    max := 5 * time.Minute

    delay := base * time.Duration(1<<uint(attempt))
    if delay > max {
        delay = max
    }

    // Add jitter (±10%)
    jitter := time.Duration(rand.Int63n(int64(delay / 5)))

    return delay + jitter
}
```

---

## 9. Error Handling

```json
{
  "type": "https://example.com/problem/...",
  "title": "Bad Request",
  "status": 400,
  "detail": "NFProfile encoding error",
  "cause": "INVALID_NF_PROFILE",
  "invalidParams": [...]
}
```

### Error Codes

| HTTP | Cause | Description |
|------|-------|-------------|
| 400 | INVALID_NF_PROFILE | NFProfile encoding error |
| 400 | SHARED_DATA_ID_UNKNOWN | Unknown shared data ID |
| 403 | FORBIDDEN | Not authorized |
| 404 | NOT_FOUND | NF instance not found |
| 412 | PRECONDITION_FAILED | ETag mismatch |
| 500 | INTERNAL_ERROR | NRF internal error |
| 503 | SERVICE_UNAVAILABLE | NRF unavailable |

---

## 10. HTTP Requirements

- **HTTP/2** shall be used (TS 29.500 §5)
- **Content-Type:** `application/json`
- **Problem Details:** `application/problem+json`
- **JSON Patch:** `application/json-patch+json`

### Required Headers

| Header | Usage |
|--------|-------|
| Content-Type | Request body format |
| Accept | Response format |
| Location | Created resource URI (201 responses) |
| ETag | Cache validation for PATCH |
| If-Match | Conditional PATCH with ETag |
| X-Request-ID | Correlation ID |

---

## 11. Acceptance Criteria

| # | Criteria | Spec Reference |
|---|----------|----------------|
| AC1 | NSSAAF registers with NRF using PUT `/nnrf-nfm/v1/nf-instances/{id}` | TS 29.510 §5.2.2.2 |
| AC2 | NFProfile contains mandatory fields: nfInstanceId, nfType, nfStatus | TS 29.510 §6.1.6.2.2 |
| AC3 | nfServices is an array with versions, serviceName, fqdn | TS 29.510 §6.1.6.2.3 |
| AC4 | nssaafInfo contains supiRanges and internalGroupIdentifiersRanges | TS 29.510 §6.1.6.2.104 |
| AC5 | Heartbeat uses PATCH with nfStatus=REGISTERED | TS 29.510 §5.2.2.3.1B |
| AC6 | NFDiscovery discovers AMF, AUSF, UDM | TS 29.510 §5.3.2.2 |
| AC7 | Token request to `/oauth2/token` with client_credentials | TS 29.510 §5.4.2.2 |
| AC8 | JWT validation with scope check for incoming requests | TS 29.510 §5.4 |
| AC9 | Deregister on graceful shutdown using DELETE | TS 29.510 §5.2.2.4 |
| AC10 | Handle 3xx redirects from NRF | TS 29.510 §5.2.2.2 |
| AC11 | NFProfile loaded from YAML config file | Internal requirement |
| AC12 | Heartbeat interval negotiated via HeartBeat-Interval header | TS 29.510 §5.2.2.3.2 |
| AC13 | Auto re-registration after maxConsecutiveFailures | Self-healing requirement |
| AC14 | OAuth2 client credentials for NRF authentication | TS 29.510 §5.4.2.2 |
