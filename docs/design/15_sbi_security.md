---
spec: RFC 8446 / RFC 5246 / RFC 5216 / TS 29.500 §5 / TS 33.310
section: §5
interface: N58, N60, N59, Nnrf (all SBI)
service: Security
operation: N/A
---

# NSSAAF SBI Security Design

## 1. Overview

Thiết kế bảo mật cho tất cả Service-Based Interfaces của NSSAAF. Áp dụng TLS 1.3 mandatory, OAuth 2.0 / NRF-based authentication, và mTLS cho inter-NF communication.

---

## 2. TLS Configuration

### 2.1 TLS 1.3 Mandatory

```yaml
# TLS server configuration for all SBI endpoints
tls:
  version: "1.3"  # Mandatory minimum
  cipher_suites:
    - TLS_AES_256_GCM_SHA384
    - TLS_AES_128_GCM_SHA256
    - TLS_CHACHA20_POLY1305_SHA256
  ecc_curves:
    - X25519
    - secp384r1
    - secp256r1
  session_tickets: true
  session_resumption: true
  ocsp_stapling: true
  # TLS 1.2 fallback chỉ cho legacy interoperability (configurable)
  tls_1_2_fallback: false
```

### 2.2 Certificate Management

```go
// Certificate structure for NSSAAF SBI
type CertificateSpec struct {
    // Subject
    CN         string   // "nssAAF.operator.com"
    O          string   // "Operator Name"
    C          string   // "FR"

    // SAN (Subject Alternative Names)
    DNSNames   []string
    IPAddresses []string

    // Validity
    NotBefore  time.Time
    NotAfter   time.Time

    // Key
    KeyAlgorithm string   // "ECDSA-P-384" or "RSA-4096"
}

// NSSAAF SBI certificates:
// - nssAAF.operator.com (primary FQDN)
// - 10.0.1.50, 10.0.2.50, 10.0.3.50 (AZ IPs)
// - *.nssAAF.operator.com (wildcard for Istio)
```

```yaml
# cert-manager Certificate resource
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nssAAF-tls-cert
  namespace: nssAAF
spec:
  secretName: nssAAF-tls-secret
  issuerRef:
    name: operator-ca
    kind: ClusterIssuer
  commonName: nssAAF.operator.com
  dnsNames:
    - nssAAF.operator.com
    - "*.nssAAF.operator.com"
  ipAddresses:
    - 10.0.1.50
    - 10.0.2.50
    - 10.0.3.50
  duration: 2160h  # 90 days
  renewBefore: 360h  # 15 days before expiry
  privateKey:
    algorithm: ECDSA
    size: 384
```

---

## 3. OAuth 2.0 / NRF Authentication

### 3.1 Token Request Flow

```
AMF/AUSF                                      NRF (OAuth2 Server)
  │                                              │
  │ 1. POST /oauth2/token                        │
  │    grant_type=client_credentials             │
  │    client_id=amf-001                         │
  │    client_secret=***                         │
  │    scope=nnssaaf-nssaa                      │
  │────────────────────────────────────────────►│
  │                                              │
  │ 2. 200 OK                                   │
  │    access_token: {JWT}                       │
  │    token_type: Bearer                        │
  │    expires_in: 3600                          │
  │    scope: nnssaaf-nssaa                      │
  │◄────────────────────────────────────────────│
  │                                              │
  │ 3. POST /nnssaaf-nssaa/v1/slice-auth...     │
  │    Authorization: Bearer {JWT}              │
  │──────────────────────────────────────────► NSSAAF
```

### 3.2 NSSAAF Token Validation

```go
// Token validation: JWT verification using NRF's public key
type TokenValidator struct {
    nrfPublicKey *rsa.PublicKey
    cache        *TokenCache
    clock        *clock.Clock
}

type TokenClaims struct {
    jwt.RegisteredClaims
    Scope    string `json:"scope"`
    ClientId string `json:"client_id"`
    NfType   string `json:"nf_type"`
    NfId     string `json:"nf_id"`
    CN       string `json:"cn"`  // from client cert
}

func (v *TokenValidator) Validate(
    ctx context.Context,
    tokenString string,
    requiredScope string,
) (*TokenClaims, error) {

    // 1. Parse JWT
    token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{},
        func(t *jwt.Token) (interface{}, error) {
            // Verify signing algorithm
            if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
                if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
                    return nil, fmt.Errorf("unexpected signing method")
                }
            }
            return v.nrfPublicKey, nil
        })

    if err != nil {
        return nil, fmt.Errorf("invalid token: %w", err)
    }

    claims, ok := token.Claims.(*TokenClaims)
    if !ok {
        return nil, ErrInvalidClaims
    }

    // 2. Validate expiry
    if claims.ExpiresAt.Before(v.clock.Now()) {
        return nil, ErrTokenExpired
    }

    // 3. Validate issuer (NRF)
    if claims.Issuer != "https://nrf.operator.com" {
        return nil, ErrInvalidIssuer
    }

    // 4. Validate audience (NSSAAF)
    validAudience := false
    for _, aud := range claims.Audience {
        if aud == "nnssaaf-nssaa" || aud == "nnssaaf-aiw" {
            validAudience = true
            break
        }
    }
    if !validAudience {
        return nil, ErrInvalidAudience
    }

    // 5. Validate scope
    scopes := strings.Split(claims.Scope, " ")
    if !contains(scopes, requiredScope) {
        return nil, ErrInsufficientScope
    }

    // 6. Optional: validate client NF type
    allowedNfTypes := []string{"AMF", "AUSF"}
    if !contains(allowedNfTypes, claims.NfType) {
        return nil, ErrInvalidNfType
    }

    return claims, nil
}
```

---

## 4. mTLS Configuration

### 4.1 Istio PeerAuthentication

```yaml
# mTLS STRICT mode: all traffic must be mTLS
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: nssAAF-mtls
  namespace: nssAAF
spec:
  mtls:
    mode: STRICT
  # Per-port override (if needed):
  portLevelMtls:
    9090:  # gRPC port
      mode: PERMISSIVE
```

### 4.2 AuthorizationPolicy

```yaml
# Who can call NSSAAF?
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: nssAAF-authz
  namespace: nssAAF
spec:
  selector:
    matchLabels:
      app: nssAAF
  action: ALLOW
  rules:
    # AMF can call Nnssaaf_NSSAA
    - from:
        - source:
            principals:
              - "cluster.local/ns/5g-core/sa/amf-service-account"
      to:
        - operation:
            methods: ["POST", "PUT"]
            paths: ["/nnssaaf-nssaa/*"]

    # AUSF can call Nnssaaf_AIW
    - from:
        - source:
            principals:
              - "cluster.local/ns/5g-core/sa/ausf-service-account"
      to:
        - operation:
            methods: ["POST", "PUT"]
            paths: ["/nnssaaf-aiw/*"]

    # NRF can call for heartbeat/health
    - from:
        - source:
            principals:
              - "cluster.local/ns/5g-core/sa/nrf-service-account"
      to:
        - operation:
            methods: ["GET"]
            paths: ["/healthz/*"]

    # Default: DENY all others
    - to:
        - operation:
            methods: ["*"]
```

---

## 5. IP Allowlist

```go
// IP allowlist for AMF CIDR ranges
type IPAccessControl struct {
    amfCIDRs     []*net.IPNet
    ausfCIDRs    []*net.IPNet
    udmCIDRs     []*net.IPNet
    nrfCIDRs     []*net.IPNet
}

func (c *IPAccessControl) ValidateAccess(
    clientIP net.IP,
    service string,
) error {

    var allowedCIDRs []*net.IPNet

    switch service {
    case "nnssaaf-nssaa":
        allowedCIDRs = c.amfCIDRs
    case "nnssaaf-aiw":
        allowedCIDRs = c.ausfCIDRs
    case "nudm-uem":
        allowedCIDRs = c.udmCIDRs
    default:
        return ErrUnknownService
    }

    for _, cidr := range allowedCIDRs {
        if cidr.Contains(clientIP) {
            return nil
        }
    }

    return ErrAccessDenied
}

// Config:
config := &IPAccessControl{
    amfCIDRs: []*net.IPNet{
        parseCIDR("10.1.0.0/16"),  // AMF pool AZ1
        parseCIDR("10.2.0.0/16"),  // AMF pool AZ2
        parseCIDR("10.3.0.0/16"),  // AMF pool AZ3
    },
    ausfCIDRs: []*net.IPNet{
        parseCIDR("10.4.0.0/16"),  // AUSF pool
    },
}
```

---

## 6. AAA Protocol Security

### 6.1 RADIUS Shared Secret Management

```go
// Shared secret rotation without restart
type SecretManager struct {
    mu       sync.RWMutex
    current  string
    previous string  // still valid during rotation
    next     string  // not yet active
}

func (m *SecretManager) GetCurrent() string {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.current
}

// For incoming RADIUS packets, try both current and previous
func (m *SecretManager) ValidatePacket(packet []byte, secrets ...string) bool {
    for _, secret := range secrets {
        if validateMessageAuthenticator(packet, secret) {
            return true
        }
    }
    return false
}

// Scheduled rotation every quarter
// 1. Generate new secret
// 2. Configure all AAA servers with new secret
// 3. Update NSSAAF: previous = current, current = new
// 4. After grace period: discard previous
```

### 6.2 Diameter IPSec

```go
// Diameter over IPSec (RFC 3588 §13)
// Required for inter-PLMN diameter connections

type IPsecConfig struct {
    Mode           string  // "transport" or "tunnel"
    LocalIP        string
    RemoteIP       string
    SPIIn          uint32
    SPIOut         uint32
    EncryptionAlg  string  // "AES-256-GCM"
    IntegrityAlg   string  // "SHA-256"
    KeyLifetimeSec uint32  // 3600 (1 hour)
}

// Or use DTLS for simpler deployment:
type DTLSPeerConfig struct {
    AllowedCIDRs  []string  // Whitelist of peer IPs
    VerifyClient  bool       // Require client certificate
    MinTLSVersion string    // "1.3"
    CipherSuites  []string
}
```

---

## 7. Key Management

### 7.1 Encryption Keys Hierarchy

```
┌──────────────────────────────────────────────┐
│         Master Key (KEK) — from HSM/KMS       │
│         AES-256, stored in AWS KMS/HashiCorp Vault │
│         Never leaves HSM                       │
└────────────────────┬───────────────────────────┘
                     │ Key Derivation
                     ▼
┌──────────────────────────────────────────────┐
│   Data Encryption Keys (DEK) per session      │
│   AES-256-GCM, generated per auth session     │
│   Encrypted with KEK for storage              │
└────────────────────┬───────────────────────────┘
                     │
                     ▼
┌──────────────────────────────────────────────┐
│         Per-field encryption                   │
│   - eap_session_state: DEK                   │
│   - shared_secret: DEK                       │
│   - audit payloads: DEK                      │
└──────────────────────────────────────────────┘
```

### 7.2 HSM Integration

```go
// AWS CloudHSM integration
type HSMKeyManager struct {
    client *cloudhsm.Client
    kekID  string  // KEK stored in HSM
}

// Encrypt: generate DEK → encrypt with KEK from HSM → store DEK + ciphertext
func (m *HSMKeyManager) Encrypt(plaintext []byte) (*EncryptedData, error) {
    // 1. Generate random DEK
    dek := make([]byte, 32)
    rand.Read(dek)

    // 2. Encrypt data with DEK (AES-256-GCM)
    ciphertext, nonce := encryptAES256GCM(dek, plaintext)

    // 3. Encrypt DEK with KEK from HSM
    encryptedDEK, err := m.client.Encrypt(m.kekID, dek)
    if err != nil {
        return nil, err
    }

    return &EncryptedData{
        Ciphertext:   ciphertext,
        Nonce:        nonce,
        EncryptedDEK: encryptedDEK,
        KeyVersion:   m.currentVersion,
    }, nil
}
```

---

## 8. Audit Logging

```go
// Immutable audit log entries
type AuditEntry struct {
    Timestamp    time.Time     `json:"timestamp"`
    RequestID   string        `json:"request_id"`
    ClientNFType string       `json:"client_nf_type"`
    ClientNFId  string        `json:"client_nf_id"`
    ClientIP    string        `json:"client_ip"`
    Service     string        `json:"service"`
    Operation   string        `json:"operation"`
    GpsiHash    string        `json:"gpsi_hash"`   // Always hashed
    SnssaiSst   int           `json:"snssai_sst"`
    SnssaiSd    string        `json:"snssai_sd"`
    Result      string        `json:"result"`
    LatencyMs   int           `json:"latency_ms"`
    TLSCipher   string        `json:"tls_cipher"`
    AuthMethod  string        `json:"auth_method"`
}

// All audit entries go to:
// 1. PostgreSQL (immutable, 2-year retention)
// 2. Kafka/Splunk (SIEM integration)
// 3. Object storage S3 (cold storage, 5-year retention)
```

---

## 9. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | TLS 1.3 mandatory, no fallback by default | TLS config, cipher_suites |
| AC2 | Certificate rotation automated (90-day certs) | cert-manager ClusterIssuer |
| AC3 | OAuth2 token validation with NRF public key | TokenValidator struct |
| AC4 | mTLS STRICT mode via Istio | PeerAuthentication |
| AC5 | AuthorizationPolicy: AMF → Nnssaaf_NSSAA, AUSF → Nnssaaf_AIW | Istio AuthorizationPolicy |
| AC6 | IP allowlist for AMF/AUSF CIDR ranges | IPAccessControl |
| AC7 | RADIUS shared secret rotation quarterly | SecretManager with previous/next |
| AC8 | KEK from HSM/KMS, DEK per session | HSMKeyManager |
| AC9 | GPSI hashed in audit log | SHA-256 truncation |
| AC10 | Audit entries to PG + Kafka + S3 | Multi-destination logging |
