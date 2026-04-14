---
spec: RFC 2865 / RFC 3579 / RFC 4818 / RFC 3588 / RFC 4301
section: AAA Protocol Security
interface: N/A (NSSAAF ↔ AAA-S)
service: AAA Security
---

# NSSAAF AAA Protocol Security Design

## 1. Overview

Thiết kế bảo mật cho AAA protocol transport giữa NSSAAF và NSS-AAA Server. Bao gồm RADIUS shared secret management, Diameter IPSec/TLS, và credential storage.

---

## 2. RADIUS Shared Secret Management

### 2.1 Shared Secret Requirements

- Minimum 256-bit key (32 bytes)
- Randomly generated using cryptographically secure RNG
- Stored encrypted at rest (AES-256-GCM)
- Rotated quarterly (90 days)

### 2.2 Shared Secret Storage

```go
// Stored in PostgreSQL, encrypted with KEK from HSM
type AaaSharedSecret struct {
    ID             uuid.UUID
    AaaConfigID   uuid.UUID  // FK to aaa_server_configs
    SecretCipher   []byte    // AES-256-GCM encrypted
    Version        int
    CreatedAt      time.Time
    ExpiresAt     time.Time
    IsActive      bool
}

// KEK (Key Encryption Key) from HSM/KMS
type KEKManager struct {
    hsmClient *HSMClient
    currentVersion int
}

func (m *KEKManager) Encrypt(plaintext []byte) ([]byte, error) {
    // Generate data key
    dek := generateAES256Key()

    // Encrypt data with DEK
    ciphertext, nonce := encryptAES256GCM(dek, plaintext)

    // Encrypt DEK with KEK from HSM
    encryptedDEK, err := m.hsmClient.Encrypt(dek)
    if err != nil {
        return nil, err
    }

    return &EncryptedSecret{
        Ciphertext:   ciphertext,
        Nonce:        nonce,
        EncryptedDEK: encryptedDEK,
        KEKVersion:   m.currentVersion,
    }, nil
}
```

### 2.3 Shared Secret Rotation

```go
// Zero-downtime secret rotation
type SecretRotationManager struct {
    db      *pgxpool.Pool
    aaaConfig *AaaConfig
}

func (m *SecretRotationManager) Rotate(ctx context.Context) error {
    // 1. Generate new secret
    newSecret := generateRandomSecret(32)

    // 2. Encrypt and store as "pending"
    pendingID, err := m.storeSecret(ctx, newSecret, VERSION_PENDING)
    if err != nil {
        return err
    }

    // 3. Push new secret to AAA server
    // (AAA server must support dual-secret mode during transition)
    if err := m.pushToAaaServer(ctx, m.aaaConfig, newSecret); err != nil {
        m.markFailed(ctx, pendingID)
        return err
    }

    // 4. Activate new secret
    if err := m.activateSecret(ctx, pendingID); err != nil {
        return err
    }

    // 5. Wait grace period (1 week)
    go func() {
        time.Sleep(7 * 24 * time.Hour)
        m.purgeOldSecrets(ctx)
    }()

    return nil
}

// Decryption: try current first, then previous (for transition window)
func (m *SecretRotationManager) Decrypt(packet []byte, secrets [][]byte) bool {
    for _, secret := range secrets {
        if validateMessageAuthenticator(packet, secret) {
            return true
        }
    }
    return false
}
```

### 2.4 Message-Authenticator Integrity

```go
// RFC 3579 §3.2: HMAC-MD5(Message + MA-placeholder + Secret)
// Message-Authenticator = HMAC-MD5(Code+ID+Length+RequestAuth+Attributes, Secret)

func ComputeMessageAuthenticator(packet []byte, sharedSecret string) []byte {
    // 1. Create copy to avoid mutation
    p := make([]byte, len(packet))
    copy(p, packet)

    // 2. Find and zero out existing MA attribute
    i := 20  // After header
    for i < len(p) {
        attrType := p[i]
        attrLen := int(p[i+1])
        if attrType == 80 && attrLen == 18 {  // Message-Authenticator
            for j := 0; j < 16; j++ {
                p[i+2+j] = 0
            }
        }
        i += attrLen
    }

    // 3. HMAC-MD5
    mac := hmac.New(md5.New, []byte(sharedSecret))
    mac.Write(p)
    return mac.Sum(nil)
}
```

---

## 3. RADIUS DTLS Security

### 3.1 DTLS Overview

RFC 4818 defines DTLS as transport for RADIUS in untrusted networks:

```
UDP Port: 2083 (DTLS-RADIUS)
Security: Datagram TLS 1.2
```

### 3.2 DTLS Configuration

```yaml
# RADIUS DTLS configuration
radius_dtls:
  enabled: true
  port: 2083

  # TLS
  tls_version: "1.2"
  cipher_suites:
    - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256

  # Certificate
  cert_file: /etc/nssAAF/certs/radius-dtls.crt
  key_file: /etc/nssAAF/certs/radius-dtls.key
  ca_file: /etc/nssAAF/certs/ca.crt

  # Client verification
  verify_client_cert: true
  require_client_cert: true

  # MTLS
  mtls: true
  client_ca_file: /etc/nssAAF/certs/aaa-clients-ca.crt
```

### 3.3 DTLS Server Implementation

```go
type RADIUSDTLS struct {
    server   *dtls.Server
    config   *DTLSConfig
    pool     *sync.Pool  // Reuse packet buffers
}

func NewRADIUSDTLS(cfg *DTLSConfig) (*RADIUSDTLS, error) {
    cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
    if err != nil {
        return nil, err
    }

    rootCA := x509.NewCertPool()
    pem, _ := os.ReadFile(cfg.CAFile)
    rootCA.AppendCertsFromPEM(pem)

    server := &dtls.Server{
        Certificate:      cert,
        RootCAs:         rootCA,
        ClientCAs:        rootCA,
        ClientAuth:       dtls.RequireAndVerifyClientCert,
        PSK:              nil,
        PreSharedKey:     nil,
        RejectUnauthorized: true,
    }

    return &RADIUSDTLS{
        server: server,
        config: cfg,
        pool: &sync.Pool{
            New: func() interface{} {
                return make([]byte, 4096)
            },
        },
    }, nil
}
```

---

## 4. Diameter IPSec Security

### 4.1 IPSec Overview

RFC 3588 defines IPSec as the security mechanism for Diameter in inter-domain scenarios.

### 4.2 IPSec Configuration

```yaml
# IPSec for Diameter
diameter_ipsec:
  enabled: true
  mode: "transport"  # or "tunnel"

  # IPSec SAs
  encryption: AES-256-GCM
  integrity: HMAC-SHA256

  # Key lifetime
  lifetime_seconds: 3600  # 1 hour
  bytes_lifetime: 0      # unlimited

  # Rekeying
  rekey_interval: 300  # 5 min before expiry
```

### 4.3 IPSec Implementation

```go
// Using Go standard library or libstrongswan
import "golang.org/x/net/ipsec"

type IPSecManager struct {
    localIP   net.IP
    remoteIP  net.IP
    spiIn     uint32
    spiOut    uint32
    encryptKey []byte
    authKey   []byte
}

func (m *IPSecManager) Setup() error {
    // Create IPSec SA for outbound
    outSA := &ipsec.SA{
        Proto:  proto.IPPROTO_ESP,
        Mode:   ipsec.ModeTransport,
        SPI:    m.spiOut,
        Dst:    m.remoteIP,
        Src:    m.localIP,
    }

    // Set encryption key
    if err := ipsec.SetEncryptKey(outSA, m.encryptKey); err != nil {
        return err
    }

    // Set auth key
    if err := ipsec.SetAuthKey(outSA, m.authKey); err != nil {
        return err
    }

    // Set replay window
    outSA.ReplayWindow = 64

    // Establish SA
    return ipsec.Establish(outSA, nil)
}

// Encrypt Diameter packet with IPSec
func (m *IPSecManager) EncryptPacket(packet []byte) ([]byte, error) {
    // ESP header + encrypted payload + ICV
    encrypted := make([]byte, len(packet)+28)  // overhead
    copy(encrypted[28:], packet)

    // Encrypt in-place with AES-GCM
    gcm, _ := cipher.NewGCM(block)
    encrypted[28:] = gcm.Seal(nil, nonce, packet, nil)

    // Add ESP header
    encrypted[0] = 0  // SPI
    binary.BigEndian.PutUint32(encrypted[1:4], m.spiOut)
    binary.BigEndian.PutUint32(encrypted[4:8], m.sequenceNumber)

    return encrypted, nil
}
```

### 4.4 Diameter TLS/DTLS Alternative

For simpler deployments without full IPSec:

```yaml
# Diameter over TLS
diameter_tls:
  enabled: true
  port: 5868  # Diameter over TLS

  tls_version: "1.3"
  cipher_suites:
    - TLS_AES_256_GCM_SHA384

  # Mutual TLS
  mtls: true
  client_ca: /etc/nssAAF/certs/diameter-clients-ca.crt
```

---

## 5. AAA Credential Security

### 5.1 Credential Classification

| Credential | Storage | Encryption | Rotation |
|------------|---------|-----------|----------|
| RADIUS Shared Secret | PostgreSQL | AES-256-GCM | 90 days |
| RADIUS DTLS Cert | Kubernetes Secret | — | 90 days |
| Diameter IPSec Keys | HSM | HSM | 1 hour |
| Diameter TLS Cert | Kubernetes Secret | — | 90 days |

### 5.2 HSM Integration

```go
// AWS CloudHSM or Thales Luna HSM
type HSMKeyManager struct {
    client   *cloudhsm.Client
    keyUsage keyusage.KeyUsage
}

func (m *HSMKeyManager) GenerateDEK() ([]byte, error) {
    // Generate Data Encryption Key in HSM
    return m.client.GenerateKey(
        alg: "AES",
        length: 256,
        usage: keyusage.Encrypt,
    )
}

func (m *HSMKeyManager) EncryptDEK(dek []byte) ([]byte, error) {
    // Encrypt DEK with KEK stored in HSM
    return m.client.Encrypt(
        keyUsage: keyusage.Wrap,
        data: dek,
    )
}
```

### 5.3 Key Lifecycle

```
┌─────────────────────────────────────────────────────────────────┐
│                    Key Lifecycle Management                         │
│                                                                  │
│  1. Key Generation                                               │
│     └── HSM generates KEK → stored in HSM, never exported        │
│                                                                  │
│  2. Key Distribution                                             │
│     └── DEK encrypted with KEK → stored in DB                    │
│                                                                  │
│  3. Key Usage                                                   │
│     └── DEK decrypted in application memory only                 │
│                                                                  │
│  4. Key Rotation                                                │
│     └── New DEK → old DEK encrypted with new KEK version        │
│                                                                  │
│  5. Key Revocation                                             │
│     └── Old KEK marked inactive → old DEKs re-encrypted         │
│                                                                  │
│  6. Key Destruction                                             │
│     └── KEK destroyed in HSM → all DEKs unusable                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## 6. IP Allowlist for AAA Servers

### 6.1 AAA Server Whitelist

```go
type AAAccessControl struct {
    // Per-PLMN or per-S-NSSAI allowlists
    allowedServers map[string]*net.IPNet  // configID → CIDR
}

func (c *AAAccessControl) ValidatePacket(source net.Addr, configID string) error {
    cidr, ok := c.allowedServers[configID]
    if !ok {
        return ErrServerNotWhitelisted
    }

    ip := source.(*net.UDPAddr).IP
    if !cidr.Contains(ip) {
        return ErrSourceIPNotAllowed
    }

    return nil
}
```

### 6.2 Configuration

```yaml
# Per-AAA-server access control
aaa_servers:
  - id: "aaa-ent-001"
    host: "192.168.1.100"
    port: 1812
    allowed_cidrs:
      - "192.168.0.0/16"
      - "10.0.0.0/8"
    protocol: RADIUS
    shared_secret_ref: "aaa-secret-ent-001"

  - id: "aaa-carrier-001"
    host: "10.100.1.50"
    port: 3868
    allowed_cidrs:
      - "10.100.0.0/16"
    protocol: DIAMETER
    tls: true
    cert_ref: "diameter-cert-carrier"
```

---

## 7. Security Logging & Audit

### 7.1 AAA Security Events

```go
// Events to log for AAA security
type AAASecurityEvent struct {
    Timestamp       time.Time `json:"timestamp"`
    EventType      string   `json:"event_type"`
    AaaServerID    string   `json:"aaa_server_id"`
    SourceIP       string   `json:"source_ip"`
    Protocol       string   `json:"protocol"`  // RADIUS or DIAMETER
    Result         string   `json:"result"`  // SUCCESS, FAIL_AUTH, FAIL_MAC, FAIL_CIPHER
    ErrorDetail   string   `json:"error_detail,omitempty"`
    SecretVersion  int      `json:"secret_version,omitempty"`
}

const (
    EVENT_SECRET_VALIDATION_SUCCESS = "SECRET_VALIDATION_SUCCESS"
    EVENT_SECRET_VALIDATION_FAILED  = "SECRET_VALIDATION_FAILED"
    EVENT_CERT_VALIDATION_SUCCESS  = "CERT_VALIDATION_SUCCESS"
    EVENT_CERT_VALIDATION_FAILED  = "CERT_VALIDATION_FAILED"
    EVENT_IPSEC_SA_ESTABLISHED    = "IPSEC_SA_ESTABLISHED"
    EVENT_IPSEC_SA_EXPIRED        = "IPSEC_SA_EXPIRED"
    EVENT_SOURCE_IP_REJECTED      = "SOURCE_IP_REJECTED"
    EVENT_SECRET_ROTATED          = "SECRET_ROTATED"
)
```

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | RADIUS shared secret ≥256-bit, encrypted at rest | AES-256-GCM, KEK from HSM |
| AC2 | Shared secret rotation quarterly | SecretRotationManager, dual-secret mode |
| AC3 | RADIUS DTLS for untrusted transport | RFC 4818, port 2083 |
| AC4 | Diameter IPSec for inter-PLMN | RFC 3588, AES-256-GCM |
| AC5 | mTLS for Diameter | TLS 1.3, mutual certificate verification |
| AC6 | IP allowlist per AAA server | AAAccessControl struct |
| AC7 | HSM integration for KEK | AWS CloudHSM / Thales Luna |
| AC8 | Security events logged | AAASecurityEvent, immutable audit |
