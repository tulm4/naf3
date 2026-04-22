---
spec: NIST SP 800-38D / NIST SP 800-38A / RFC 3394 / RFC 5297 / RFC 5869 / AWS CloudHSM
section: Cryptography
interface: Internal
service: N/A
operation: N/A
---

# NSSAAF Cryptography Package Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, cryptographic operations (session state encryption, MSK handling) run in **Biz Pods** only. The AAA Gateway handles raw socket I/O without encryption needs. The HTTP Gateway has no cryptographic responsibilities. See `docs/design/01_service_model.md` §5.4.

Module `internal/crypto/` cung cấp tất cả cryptographic primitives cho NSSAAF:
- **AES-256-GCM** cho symmetric encryption (session state, secrets)
- **HKDF** cho key derivation
- **SHA-256/SHA-384** cho hashing (GPSI privacy, integrity)
- **Secure random** generation
- **KEK/DEK** hierarchy cho envelope encryption
- **HSM integration** cho production key management

Tất cả thiết kế tuân thủ NIST recommendations và operator security policy.

---

## 2. Key Hierarchy

### 2.1 Three-Tier Key Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                   Tier 1: Master Key (MEK)                      │
│                   Stored in HSM/KMS, never leaves HSM             │
│                   AES-256, operator-controlled                     │
│                   Rotation: quarterly or on security event         │
└─────────────────────────────┬────────────────────────────────┘
                              │ KEK = HKDF(MasterKey, context)
                              ▼
┌──────────────────────────────────────────────────────────────┐
│                 Tier 2: Key Encryption Keys (KEK)                │
│                 Stored encrypted in PostgreSQL                    │
│                 Per-version: KEK_v1, KEK_v2, ...                 │
│                 Rotation: annual, with 30-day overlap             │
│                 Encrypted DEKs stored alongside ciphertext         │
└─────────────────────────────┬────────────────────────────────┘
                              │ DEK = GenerateAES256()
                              ▼
┌──────────────────────────────────────────────────────────────┐
│              Tier 3: Data Encryption Keys (DEK)                  │
│              Generated per data item, used once or per session    │
│              Encrypted with current KEK before storage             │
│              In-memory only, never persisted plaintext             │
│              AEAD: AES-256-GCM, random 96-bit nonce              │
└──────────────────────────────────────────────────────────────┘
```

### 2.2 Key Scope

| Key | Scope | Generation | Storage |
|-----|-------|-----------|---------|
| MEK | Operator-wide | HSM | HSM/KMS only |
| KEK | Per NSSAAF deployment | HSM → software backup | PostgreSQL (encrypted) |
| DEK | Per data item | Software CSPRNG | PostgreSQL (encrypted with KEK) |

---

## 3. AES-256-GCM Encryption

### 3.1 Standard Encryption

```go
// Encrypt encrypts plaintext using AES-256-GCM.
// Returns: ciphertext (ciphertext || nonce || tag)
func Encrypt(plaintext, key []byte) ([]byte, error)

// Decrypt decrypts ciphertext (ciphertext || nonce || tag) using AES-256-GCM.
func Decrypt(ciphertext, key []byte) ([]byte, error)

// EncryptedData represents the encrypted output structure.
type EncryptedData struct {
    Ciphertext []byte // encrypted data
    Nonce     []byte // 12-byte random nonce
    Tag       []byte // 16-byte authentication tag
    KeyVersion int    // KEK version used
}
```

**Parameters:**
- Key: 32 bytes (AES-256)
- Nonce: 12 bytes, randomly generated per encryption (no nonce reuse)
- Tag: 16 bytes (GCM authentication tag)
- AAD (Additional Authenticated Data): optional, includes key version for integrity

### 3.2 Envelope Encryption

```go
// EnvelopeEncrypt encrypts data with a fresh DEK, then encrypts the DEK with KEK.
// This is the standard pattern for storing sensitive fields in PostgreSQL.
type Envelope struct {
    Ciphertext  []byte // encrypted with DEK
    EncryptedDEK []byte // DEK encrypted with KEK
    Nonce      []byte // nonce for data encryption
    DEKTag     []byte // GCM tag for DEK
    DataTag    []byte // GCM tag for ciphertext
    KEKVersion int    // which KEK version encrypted the DEK
}

func EnvelopeEncrypt(plaintext []byte, kek []byte, kekVersion int) (*Envelope, error)
func EnvelopeDecrypt(env *Envelope, kek []byte) ([]byte, error)
```

### 3.3 DEK Generation

```go
// GenerateDEK generates a new 32-byte AES-256 key.
// Uses crypto/rand for cryptographically secure random.
func GenerateDEK() ([]byte, error) {
    key := make([]byte, 32)
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("failed to generate DEK: %w", err)
    }
    return key, nil
}
```

---

## 4. Key Derivation

### 4.1 HKDF (RFC 5869)

```go
// DeriveKey derives a key from input key material using HKDF-SHA-256.
// info: context-specific string (e.g., "eap-session-state", "shared-secret")
// salt: optional salt (uses zero salt if nil)
func DeriveKey(ikm, salt, info []byte, length int) ([]byte, error)

// SessionKEK derives a per-session KEK from master key + authCtxId.
// Prevents cross-session key reuse.
func SessionKEK(masterKey []byte, authCtxId string) ([]byte, error) {
    info := []byte("nssaa-session-kek:" + authCtxId)
    return DeriveKey(masterKey, nil, info, 32)
}
```

### 4.2 KDF for MSK (EAP-TLS, RFC 5216)

MSK derivation handled in `internal/eap/`, but crypto package provides the base primitives:

```go
// TLSExporter implements RFC 5705 TLS Exporter interface.
// Used by EAP engine for MSK/EMSK derivation.
func TLSExporter(masterSecret, label string, context []byte, length int) ([]byte, error)
```

---

## 5. HSM Integration

### 5.1 HSM Interface

```go
// KeyManager abstracts HSM operations.
// Supports AWS CloudHSM, Thales Luna, SoftHSM for dev.
type KeyManager interface {
    // GenerateKey generates a new key inside the HSM.
    // Returns key ID and metadata (never the raw key).
    GenerateKey(ctx context.Context, alg string, bits int) (*KeyMetadata, error)

    // Encrypt encrypts data using a key stored in the HSM.
    Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error)

    // Decrypt decrypts data using a key stored in the HSM.
    Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error)

    // Wrap encrypts a key (DEK) using the HSM-managed KEK.
    Wrap(ctx context.Context, kekID string, key []byte) ([]byte, error)

    // Unwrap decrypts a wrapped key (DEK).
    Unwrap(ctx context.Context, kekID string, wrappedKey []byte) ([]byte, error)

    // GetKeyVersion returns the current active key version.
    GetKeyVersion(ctx context.Context, keyID string) (int, error)

    // RotateKey marks a new key version as active.
    RotateKey(ctx context.Context, keyID string) error
}

type KeyMetadata struct {
    ID        string
    Version   int
    Algorithm string
    CreatedAt time.Time
}
```

### 5.2 AWS CloudHSM Implementation

```go
// CloudHSMKeyManager implements KeyManager for AWS CloudHSM.
type CloudHSMKeyManager struct {
    client   *cloudhsm.Client
    kekID    string    // KEK stored in HSM
    region   string
}

func (m *CloudHSMKeyManager) Wrap(ctx context.Context, kekID string, key []byte) ([]byte, error) {
    // Use PKCS#11 C_WrapKey or AWS CloudHSM SDK
    wrapped, err := m.client.WrapKey(kekID, key)
    if err != nil {
        return nil, fmt.Errorf("HSM wrap failed: %w", err)
    }
    return wrapped, nil
}

func (m *CloudHSMKeyManager) Unwrap(ctx context.Context, kekID string, wrapped []byte) ([]byte, error) {
    return m.client.UnwrapKey(kekID, wrapped)
}
```

### 5.3 SoftHSM for Development

```go
// SoftHSMKeyManager implements KeyManager using SoftHSM2 for local dev.
// Uses pkcs11 library to interface with SoftHSM2 token.
type SoftHSMKeyManager struct {
    pkcs11lib *pkcs11.Ctx
    session   pkcs11.SessionHandle
    kekObject pkcs11.ObjectHandle
}

// NewSoftHSMKeyManager initializes SoftHSM for development.
// PIN read from SOFTHSM2_CONF environment variable.
func NewSoftHSMKeyManager() (*SoftHSMKeyManager, error) {
    libPath := os.Getenv("SOFTHSM2_LIB")
    if libPath == "" {
        libPath = "/usr/lib/softhsm/libsofthsm2.so"
    }
    // Initialize PKCS#11 context
    // Login to token
    // Find KEK object by label
}
```

---

## 6. Key Rotation

### 6.1 KEK Rotation

```go
// KEKRotator manages KEK version lifecycle.
// Supports zero-downtime rotation with dual-key validation window.
type KEKRotator struct {
    mu         sync.RWMutex
    current    []byte        // active KEK
    currentVer int
    previous   []byte        // previous KEK (valid during overlap window)
    overlapDays int         // overlap window in days

    db *pgxpool.Pool        // for storing encrypted DEKs
}

// Rotate generates a new KEK and marks it active after overlap window.
// Steps:
//  1. Generate new KEK (in HSM)
//  2. Store encrypted backup in DB
//  3. Schedule activation in overlapDays
//  4. After overlap: discard old KEK
func (r *KEKRotator) Rotate(ctx context.Context) error {
    newKey, err := generateKeyFromHSM(ctx)
    if err != nil {
        return fmt.Errorf("failed to generate new KEK: %w", err)
    }

    r.mu.Lock()
    r.previous = r.current
    r.current = newKey
    r.currentVer++
    overlapAt := time.Now().Add(time.Duration(r.overlapDays) * 24 * time.Hour)
    r.mu.Unlock()

    // Re-encrypt all DEKs with new KEK in background
    go r.reEncryptDEKs(ctx, r.currentVer-1, r.currentVer, newKey)

    return nil
}

// Decrypt tries current KEK first, then previous (for rotation window).
func (r *KEKRotator) Decrypt(encryptedDEK []byte) ([]byte, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    // Try current KEK
    dek, err := decryptDEK(encryptedDEK, r.current)
    if err == nil {
        return dek, nil
    }

    // Try previous KEK (if in overlap window)
    if r.previous != nil {
        dek, err = decryptDEK(encryptedDEK, r.previous)
        if err == nil {
            return dek, nil
        }
    }

    return nil, ErrKeyNotFound
}
```

### 6.2 Rotation Schedule

```
Quarterly rotation:
  Jan 1     → Jan 30   : overlap (old + new active)
  Jan 30    → Apr 1    : new active, old discarded
  Apr 1     → Apr 30   : overlap
  Apr 30    → Jul 1    : new active
  ...

Emergency rotation:
  Triggered by: key compromise, security incident, HSM event
  Immediate: mark old key invalid, generate new key
  No overlap: reject any DEKs encrypted with old key
```

---

## 7. Encrypted Fields

### 7.1 Fields Requiring Encryption

| Field | Table | Encryption | Notes |
|-------|-------|-----------|-------|
| `eap_session_state` | `slice_auth_sessions` | DEK per session | Contains MSK, TLS keys |
| `eap_session_state` | `aiw_auth_sessions` | DEK per session | Contains MSK |
| `shared_secret` | `aaa_server_configs` | DEK per secret | RADIUS shared secret |
| `msk_encrypted` | `aiw_auth_sessions` | DEK per session | Not persisted permanently |
| Audit payload blobs | `nssaa_audit_log` | DEK per entry | Optional, for PII |

### 7.2 Session State Encryption

```go
// EncryptSession serializes EAP session state and encrypts with per-session DEK.
// DEK is then encrypted with KEK and stored alongside ciphertext.
type EncryptedSession struct {
    AuthCtxID      string    `json:"auth_ctx_id"`
    Ciphertext    []byte    `json:"ct"`   // AES-256-GCM encrypted session
    Nonce          []byte    `json:"n"`    // 12-byte nonce
    Tag            []byte    `json:"t"`    // 16-byte GCM tag
    EncryptedDEK   []byte    `json:"ed"`  // DEK encrypted with KEK
    DEKVersion    int       `json:"dv"`   // KEK version
    EncryptedAt    time.Time `json:"ea"`
}

// EncryptSession encrypts session state for storage.
func EncryptSession(state *EapSessionState, kek []byte, kekVersion int) (*EncryptedSession, error) {
    // Serialize state
    raw, err := proto.Marshal(state)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal session: %w", err)
    }

    // Generate per-session DEK
    dek, err := GenerateDEK()
    if err != nil {
        return nil, fmt.Errorf("failed to generate DEK: %w", err)
    }

    // Encrypt data with DEK
    ct, nonce, tag, err := Encrypt(raw, dek)
    if err != nil {
        return nil, fmt.Errorf("failed to encrypt session: %w", err)
    }

    // Encrypt DEK with KEK
    encryptedDEK, err := Encrypt(dek, kek)
    if err != nil {
        return nil, fmt.Errorf("failed to wrap DEK: %w", err)
    }

    return &EncryptedSession{
        AuthCtxID:    state.AuthCtxId,
        Ciphertext:  ct,
        Nonce:       nonce,
        Tag:         tag,
        EncryptedDEK: encryptedDEK,
        DEKVersion: kekVersion,
        EncryptedAt: time.Now(),
    }, nil
}

// DecryptSession decrypts and deserializes session state.
func DecryptSession(es *EncryptedSession, kek []byte) (*EapSessionState, error) {
    // Decrypt DEK
    dek, err := Decrypt(es.EncryptedDEK, kek)
    if err != nil {
        return nil, fmt.Errorf("failed to unwrap DEK: %w", err)
    }

    // Decrypt data
    raw, err := DecryptWithTag(es.Ciphertext, es.Nonce, es.Tag, dek)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt session: %w", err)
    }

    // Deserialize
    state := &EapSessionState{}
    if err := proto.Unmarshal(raw, state); err != nil {
        return nil, fmt.Errorf("failed to unmarshal session: %w", err)
    }

    return state, nil
}
```

---

## 8. Shared Secret Encryption

### 8.1 AAA Server Shared Secret Storage

```go
// EncryptedSecret represents a RADIUS shared secret stored in PostgreSQL.
type EncryptedSecret struct {
    ID            uuid.UUID
    AaaConfigID   uuid.UUID
    Ciphertext    []byte    // DEK-encrypted secret
    Nonce         []byte
    Tag           []byte
    EncryptedDEK  []byte    // DEK encrypted with KEK
    DEKVersion   int
    Version       int       // Secret version (for rotation tracking)
    CreatedAt     time.Time
    ExpiresAt     time.Time
    IsActive      bool
}

// EncryptSecret encrypts a shared secret for storage.
// Called when configuring a new AAA server or rotating secrets.
func EncryptSecret(plaintextSecret string, kek []byte, kekVersion int) (*EncryptedSecret, error) {
    dek, err := GenerateDEK()
    if err != nil {
        return nil, err
    }

    ct, nonce, tag, err := Encrypt([]byte(plaintextSecret), dek)
    if err != nil {
        return nil, err
    }

    encryptedDEK, err := Encrypt(dek, kek)
    if err != nil {
        return nil, err
    }

    return &EncryptedSecret{
        Ciphertext:   ct,
        Nonce:       nonce,
        Tag:         tag,
        EncryptedDEK: encryptedDEK,
        DEKVersion: kekVersion,
        Version:     1,
        CreatedAt:  time.Now(),
        ExpiresAt:  time.Now().Add(90 * 24 * time.Hour), // 90-day secret
        IsActive:   true,
    }, nil
}

// DecryptSecret decrypts a stored shared secret.
// Used by RADIUS client to decrypt before computing Message-Authenticator.
func DecryptSecret(es *EncryptedSecret, kek []byte) (string, error) {
    dek, err := Decrypt(es.EncryptedDEK, kek)
    if err != nil {
        return "", fmt.Errorf("failed to unwrap DEK for secret: %w", err)
    }

    raw, err := DecryptWithTag(es.Ciphertext, es.Nonce, es.Tag, dek)
    if err != nil {
        return "", fmt.Errorf("failed to decrypt secret: %w", err)
    }

    return string(raw), nil
}
```

---

## 9. Hashing Functions

### 9.1 Identity Hashing (GPSI/SUPI Privacy)

```go
// HashGPSI returns SHA-256 hash of GPSI, truncated to first 16 bytes (32 hex chars).
// Used in audit logs and telemetry to protect subscriber privacy.
func HashGPSI(gpsi string) string {
    h := sha256.Sum256([]byte(gpsi))
    return hex.EncodeToString(h[:16])
}

// HashSUPI returns SHA-256 hash of SUPI, truncated to first 16 bytes.
// Used in AIW audit logs.
func HashSUPI(supi string) string {
    h := sha256.Sum256([]byte(supi))
    return hex.EncodeToString(h[:16])
}

// HashMessage returns SHA-256 hash of EAP message for idempotency.
func HashMessage(msg []byte) string {
    h := sha256.Sum256(msg)
    return hex.EncodeToString(h[:])
}
```

### 9.2 Integrity MAC

```go
// HMACSHA256 returns HMAC-SHA-256 of data with given key.
// Used for integrity verification of structured data.
func HMACSHA256(key, data []byte) []byte {
    mac := hmac.New(sha256.New, key)
    mac.Write(data)
    return mac.Sum(nil)
}

// VerifyHMAC checks if the provided MAC matches computed MAC.
func VerifyHMAC(key, data, mac []byte) bool {
    expected := HMACSHA256(key, data)
    return hmac.Equal(expected, mac)
}
```

---

## 10. Random Generation

### 10.1 Secure Random

```go
// cryptoRand wraps crypto/rand for convenience.
var cryptoRand = rand.Reader

// Bytes returns n cryptographically secure random bytes.
func RandomBytes(n int) ([]byte, error) {
    b := make([]byte, n)
    if _, err := io.ReadFull(cryptoRand, b); err != nil {
        return nil, fmt.Errorf("failed to read random bytes: %w", err)
    }
    return b, nil
}

// HexString returns a random hex string of length n (n must be even).
func RandomHexString(n int) (string, error) {
    if n%2 != 0 {
        return "", errors.New("n must be even for hex string")
    }
    b, err := RandomBytes(n / 2)
    if err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil
}

// Nonce returns a random 12-byte nonce for AES-GCM.
func GCMNonce() ([]byte, error) {
    return RandomBytes(12)
}
```

### 10.2 AuthCtxId Generation

```go
// GenerateAuthCtxId generates a unique, time-sortable authentication context ID.
// Uses UUIDv7 format: timestamp (48 bits) + random (80 bits).
func GenerateAuthCtxId() (string, error) {
    id, err := uuid.NewV7()
    if err != nil {
        return "", fmt.Errorf("failed to generate UUIDv7: %w", err)
    }
    return "nssaa-" + id.String(), nil
}
```

---

## 11. Configuration

### 11.1 Crypto Configuration

```go
// Config holds crypto package configuration.
type Config struct {
    // Key management mode: "soft", "softhsm", "cloudhsm"
    KeyManager string

    // KEK for soft mode (hex-encoded, loaded from env or config file)
    MasterKeyHex string

    // HSM configuration
    HSM HSMCfg

    // Rotation schedule
    KEKOverlapDays int

    // Which fields to encrypt (list of table.column)
    EncryptedFields []string
}

type HSMCfg struct {
    Type       string // "cloudhsm", "luna", "softhsm"
    Endpoint   string
    Region     string
    KeyLabel   string
}

// DefaultConfig returns sensible defaults for development.
func DefaultConfig() *Config {
    return &Config{
        KeyManager:     "soft",
        KEKOverlapDays: 30,
    }
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
    switch c.KeyManager {
    case "soft":
        if c.MasterKeyHex == "" {
            return errors.New("MasterKeyHex required for soft key manager")
        }
        if len(c.MasterKeyHex) != 64 { // 32 bytes = 64 hex chars
            return errors.New("MasterKeyHex must be 64 hex chars (32 bytes)")
        }
    case "softhsm":
        if os.Getenv("SOFTHSM2_CONF") == "" {
            return errors.New("SOFTHSM2_CONF env var required for SoftHSM")
        }
    case "cloudhsm":
        if c.HSM.Region == "" {
            return errors.New("HSM.Region required for CloudHSM")
        }
    default:
        return fmt.Errorf("unknown key manager: %s", c.KeyManager)
    }
    return nil
}
```

### 11.2 Integration with Config Package

```go
// Package-level singleton (initialized in main.go).
var globalKeyManager KeyManager

// Init initializes the crypto package with configuration.
func Init(cfg *Config) error {
    if err := cfg.Validate(); err != nil {
        return fmt.Errorf("crypto config invalid: %w", err)
    }

    switch cfg.KeyManager {
    case "soft":
        key, err := hex.DecodeString(cfg.MasterKeyHex)
        if err != nil {
            return fmt.Errorf("invalid MasterKeyHex: %w", err)
        }
        globalKeyManager = &SoftKeyManager{masterKey: key}

    case "softhsm":
        mgr, err := NewSoftHSMKeyManager()
        if err != nil {
            return fmt.Errorf("failed to init SoftHSM: %w", err)
        }
        globalKeyManager = mgr

    case "cloudhsm":
        mgr, err := NewCloudHSMKeyManager(&cfg.HSM)
        if err != nil {
            return fmt.Errorf("failed to init CloudHSM: %w", err)
        }
        globalKeyManager = mgr
    }

    return nil
}

// KM returns the global KeyManager instance.
func KM() KeyManager {
    if globalKeyManager == nil {
        panic("crypto package not initialized, call crypto.Init() first")
    }
    return globalKeyManager
}
```

---

## 12. Package Structure

```
internal/crypto/
├── encrypt.go        # AES-256-GCM encrypt/decrypt
├── envelope.go      # Envelope encryption (DEK wrapping)
├── kdf.go           # HKDF key derivation
├── hash.go          # SHA-256 hashing, HMAC
├── random.go        # Secure random generation
├── keys.go          # DEK/KEK generation
├── kms.go           # HSM interface and implementations
├── rotation.go     # KEK rotation logic
├── session.go      # Session state encryption
├── secret.go       # Shared secret encryption
└── crypto_test.go  # Unit tests
```

---

## 13. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | AES-256-GCM encryption with random 12-byte nonce | `Encrypt()` with AEAD |
| AC2 | Envelope encryption: DEK per data item, KEK per deployment | `EnvelopeEncrypt()` |
| AC3 | DEK generated with crypto/rand (CSPRNG) | `GenerateDEK()` |
| AC4 | GPSI/SUPI hashed with SHA-256, truncated 16 bytes | `HashGPSI()`, `HashSUPI()` |
| AC5 | HMAC-SHA-256 for integrity verification | `HMACSHA256()` |
| AC6 | KEK rotation with 30-day overlap window | `KEKRotator` |
| AC7 | SoftHSM fallback for development | `SoftHSMKeyManager` |
| AC8 | CloudHSM integration for production | `CloudHSMKeyManager` |
| AC9 | No plaintext secrets in logs or memory dumps | Encryption at rest for all sensitive fields |
| AC10 | Key version tracked with each ciphertext | `KeyVersion` field in all encrypted structs |
