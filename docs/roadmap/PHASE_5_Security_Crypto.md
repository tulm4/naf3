# Phase 5: Security & Cryptography

## Overview

Phase 5 implements telecom-grade security for NSSAAF: TLS 1.3 for all external interfaces, mTLS between components, OAuth2/JWT validation, and AES-256-GCM encryption for session state. Key material is managed via KEK/DEK envelope encryption hierarchy with HSM/KMS interface.

**Spec Foundation:** TS 33.501 §5.13, RFC 8446, RFC 5246, RFC 7519, RFC 6750

---

## Modules to Implement

### 1. `internal/auth/` — OAuth2 & mTLS

**Priority:** P0
**Dependencies:** `internal/types/`, `internal/config/`
**Design Doc:** `docs/design/15_sbi_security.md`, `docs/design/16_aaa_security.md`

#### 1.1 JWT Token Validation (`token.go`)

```go
package auth

import (
    "context"
    "crypto/rsa"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "math/big"
    "net/http"
    "sync"
    "time"

    "github.com/golang-jwt/jwt/v5"
)

// NRF public keys cache
type JWKS struct {
    mu     sync.RWMutex
    keys   map[string]*rsa.PublicKey
    expiry time.Time
}

var globalJWKS = &JWKS{keys: make(map[string]*rsa.PublicKey)}

const (
    JWKSRefreshInterval = 5 * time.Minute
    TokenAudience       = "nnssaaf-nssaa"
    TokenIssuer         = "http://nrf.operator.com"
)

// NSSAAF required scopes
const (
    ScopeNnssaafNssaa = "nnssaaf-nssaa"
    ScopeNnssaafAiw    = "nnssaaf-aiw"
)

type TokenClaims struct {
    jwt.RegisteredClaims
    Scope    string   `json:"scope"`
    ClientId string   `json:"client_id"`
    Cn       string   `json:"cn"`  // Common Name
}

// ValidateToken validates JWT from NRF and extracts claims
func ValidateToken(ctx context.Context, tokenString string) (*TokenClaims, error) {
    // Parse token
    token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
        // Validate signing algorithm
        if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }

        // Get key ID
        kid, ok := token.Header["kid"].(string)
        if !ok {
            return nil, fmt.Errorf("missing key ID in token header")
        }

        // Get public key
        key, err := GetPublicKey(ctx, kid)
        if err != nil {
            return nil, err
        }

        return key, nil
    })

    if err != nil {
        return nil, fmt.Errorf("token validation failed: %w", err)
    }

    claims, ok := token.Claims.(*TokenClaims)
    if !ok || !token.Valid {
        return nil, fmt.Errorf("invalid token claims")
    }

    // Validate audience
    if !claims.VerifyAudience(TokenAudience, true) {
        return nil, fmt.Errorf("invalid audience")
    }

    // Validate issuer
    if !claims.VerifyIssuer(TokenIssuer, true) {
        return nil, fmt.Errorf("invalid issuer")
    }

    return claims, nil
}

// GetPublicKey retrieves RSA public key by key ID from NRF
func GetPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
    globalJWKS.mu.RLock()
    if key, ok := globalJWKS.keys[kid]; ok && time.Now().Before(globalJWKS.expiry) {
        globalJWKS.mu.RUnlock()
        return key, nil
    }
    globalJWKS.mu.RUnlock()

    // Refresh JWKS
    if err := refreshJWKS(ctx); err != nil {
        return nil, err
    }

    globalJWKS.mu.RLock()
    defer globalJWKS.mu.RUnlock()

    key, ok := globalJWKS.keys[kid]
    if !ok {
        return nil, fmt.Errorf("key %s not found in JWKS", kid)
    }

    return key, nil
}

// refreshJWKS fetches public keys from NRF
func refreshJWKS(ctx context.Context) error {
    nrfURL := config.Get().NRF.BaseURL + "/oauth2/jwks"

    req, err := http.NewRequestWithContext(ctx, "GET", nrfURL, nil)
    if err != nil {
        return err
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to fetch JWKS: %w", err)
    }
    defer resp.Body.Close()

    var jwks struct {
        Keys []struct {
            Kid string `json:"kid"`
            Kty string `json:"kty"`
            N   string `json:"n"`
            E   string `json:"e"`
        } `json:"keys"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
        return fmt.Errorf("failed to decode JWKS: %w", err)
    }

    globalJWKS.mu.Lock()
    defer globalJWKS.mu.Unlock()

    for _, key := range jwks.Keys {
        if key.Kty != "RSA" {
            continue
        }

        nBytes, _ := base64.RawURLEncoding.DecodeString(key.N)
        eBytes, _ := base64.RawURLEncoding.DecodeString(key.E)

        pubKey := &rsa.PublicKey{
            N: new(big.Int).SetBytes(nBytes),
            E: int(new(big.Int).SetBytes(eBytes).Int64()),
        }

        globalJWKS.keys[key.Kid] = pubKey
    }

    globalJWKS.expiry = time.Now().Add(JWKSRefreshInterval)

    return nil
}
```

#### 1.2 Auth Middleware (`middleware.go`)

```go
package auth

import (
    "context"
    "net/http"
    "strings"
)

// Middleware returns HTTP middleware for JWT validation
func Middleware(requiredScope string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Extract token from Authorization header
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" {
                writeError(w, http.StatusUnauthorized, "MISSING_TOKEN", "Authorization header required")
                return
            }

            parts := strings.SplitN(authHeader, " ", 2)
            if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
                writeError(w, http.StatusUnauthorized, "INVALID_TOKEN_FORMAT", "Bearer token required")
                return
            }

            tokenString := parts[1]

            // Validate token
            claims, err := ValidateToken(r.Context(), tokenString)
            if err != nil {
                logging.Warn("token_validation_failed",
                    "error", err.Error(),
                    "request_id", r.Header.Get("X-Request-ID"),
                )
                writeError(w, http.StatusUnauthorized, "INVALID_TOKEN", err.Error())
                return
            }

            // Validate scope
            if !hasScope(claims.Scope, requiredScope) {
                writeError(w, http.StatusForbidden, "INSUFFICIENT_SCOPE",
                    "required scope: "+requiredScope)
                return
            }

            // Add claims to context
            ctx := context.WithValue(r.Context(), ClaimsKey, claims)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// hasScope checks if token has required scope
func hasScope(tokenScope, required string) bool {
    scopes := strings.Fields(tokenScope)
    for _, s := range scopes {
        if s == required {
            return true
        }
    }
    return false
}

// writeError writes RFC 7807 ProblemDetails response
func writeError(w http.ResponseWriter, status int, cause, detail string) {
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]any{
        "type":  fmt.Sprintf("https://nssaa.operator.com/problem/%s", cause),
        "title": http.StatusText(status),
        "status": status,
        "cause":  cause,
        "detail": detail,
    })
}
```

#### 1.3 mTLS Configuration (`mtls.go`)

```go
package auth

import (
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "os"
)

// TLSConfig holds TLS configuration
type TLSConfig struct {
    // Server-side
    CertFile string
    KeyFile  string
    ClientCA string  // CA certificate for client certificate verification

    // Client-side
    CaBundle string  // Trusted CA certificates

    // Protocol
    MinVersion uint16  // e.g., tls.VersionTLS13
}

// NewServerTLSConfig creates TLS config for server (mTLS required)
func NewServerTLSConfig(cfg TLSConfig) (*tls.Config, error) {
    // Load server certificate
    cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
    if err != nil {
        return nil, fmt.Errorf("failed to load server cert: %w", err)
    }

    // Load client CA for mTLS
    clientCA := x509.NewCertPool()
    caBytes, err := os.ReadFile(cfg.ClientCA)
    if err != nil {
        return nil, fmt.Errorf("failed to read client CA: %w", err)
    }
    if !clientCA.AppendCertsFromPEM(caBytes) {
        return nil, fmt.Errorf("failed to parse client CA")
    }

    return &tls.Config{
        Certificates: []tls.Certificate{cert},
        ClientCAs:    clientCA,
        ClientAuth:   tls.RequireAndVerifyClientCert,
        MinVersion:   tls.VersionTLS13,
        CurvePreferences: []tls.CurveID{
            tls.X25519,
            tls.CurveP256,
        },
        CipherSuites: []uint16{
            tls.TLS_AES_256_GCM_SHA384,
            tls.TLS_AES_128_GCM_SHA256,
            tls.TLS_CHACHA20_POLY1305_SHA256,
        },
    }, nil
}

// NewClientTLSConfig creates TLS config for client (mTLS optional)
func NewClientTLSConfig(cfg TLSConfig) (*tls.Config, error) {
    // Load CA bundle for server certificate verification
    caBundle := x509.NewCertPool()
    caBytes, err := os.ReadFile(cfg.CaBundle)
    if err != nil {
        return nil, fmt.Errorf("failed to read CA bundle: %w", err)
    }
    if !caBundle.AppendCertsFromPEM(caBytes) {
        return nil, fmt.Errorf("failed to parse CA bundle")
    }

    // Load client certificate for mTLS
    var certificates []tls.Certificate
    if cfg.CertFile != "" && cfg.KeyFile != "" {
        cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
        if err != nil {
            return nil, fmt.Errorf("failed to load client cert: %w", err)
        }
        certificates = []tls.Certificate{cert}
    }

    return &tls.Config{
        RootCAs:      caBundle,
        Certificates: certificates,
        MinVersion:   tls.VersionTLS13,
    }, nil
}
```

---

### 2. `internal/crypto/` — Cryptography

**Priority:** P0
**Dependencies:** None (standalone)
**Design Doc:** `docs/design/17_crypto.md`, `docs/design/16_aaa_security.md`

#### 2.1 AES-256-GCM Encryption (`encrypt.go`)

```go
package crypto

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "fmt"
    "io"
)

const (
    KeySize    = 32  // AES-256
    NonceSize  = 12  // GCM standard nonce
    TagSize    = 16  // GCM authentication tag
)

// EncryptAESGCM encrypts plaintext using AES-256-GCM
func EncryptAESGCM(key, plaintext []byte) ([]byte, error) {
    if len(key) != KeySize {
        return nil, fmt.Errorf("invalid key size: expected %d, got %d", KeySize, len(key))
    }

    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("failed to create cipher: %w", err)
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("failed to create GCM: %w", err)
    }

    // Generate random nonce
    nonce := make([]byte, NonceSize)
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, fmt.Errorf("failed to generate nonce: %w", err)
    }

    // Encrypt (nonce appended automatically by GCM)
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

    return ciphertext, nil
}

// DecryptAESGCM decrypts ciphertext using AES-256-GCM
func DecryptAESGCM(key, ciphertext []byte) ([]byte, error) {
    if len(key) != KeySize {
        return nil, fmt.Errorf("invalid key size: expected %d, got %d", KeySize, len(key))
    }

    if len(ciphertext) < NonceSize+TagSize {
        return nil, fmt.Errorf("ciphertext too short")
    }

    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("failed to create cipher: %w", err)
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("failed to create GCM: %w", err)
    }

    // Extract nonce and ciphertext+tag
    nonce := ciphertext[:NonceSize]
    encrypted := ciphertext[NonceSize:]

    plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
    if err != nil {
        return nil, fmt.Errorf("decryption failed: %w", err)
    }

    return plaintext, nil
}
```

#### 2.2 Envelope Encryption Hierarchy (`envelope.go`)

```
Key hierarchy:

HSM/KMS (MEK - Master Encryption Key, never leaves HSM)
    │
    ▼ (wrapped by MEK)
KEK (Key Encryption Key, stored encrypted in secret store)
    │
    ▼ (wrapped by KEK)
DEK (Data Encryption Key, per-session or per-data-item)
    │
    ▼ (used directly)
Data Encryption (AES-256-GCM)
```

```go
package crypto

import (
    "fmt"
)

// EncryptedData represents envelope-encrypted data
type EncryptedData struct {
    Ciphertext   []byte  // Encrypted with DEK
    EncryptedDEK []byte  // DEK encrypted with KEK
    KeyVersion   int     // KEK version for rotation
    DEKVersion   int     // DEK version (for audit)
}

// EnvelopeEncrypt encrypts data using envelope encryption
func EnvelopeEncrypt(data []byte, kek []byte) (*EncryptedData, error) {
    // Generate new DEK for this data
    dek := make([]byte, KeySize)
    if _, err := rand.Read(dek); err != nil {
        return nil, fmt.Errorf("failed to generate DEK: %w", err)
    }

    // Encrypt data with DEK
    ciphertext, err := EncryptAESGCM(dek, data)
    if err != nil {
        return nil, fmt.Errorf("failed to encrypt data: %w", err)
    }

    // Encrypt DEK with KEK
    encryptedDEK, err := EncryptAESGCM(kek, dek)
    if err != nil {
        return nil, fmt.Errorf("failed to encrypt DEK: %w", err)
    }

    return &EncryptedData{
        Ciphertext:   ciphertext,
        EncryptedDEK: encryptedDEK,
        KeyVersion:   currentKEKVersion(),
        DEKVersion:  incrementDEKVersion(),
    }, nil
}

// EnvelopeDecrypt decrypts envelope-encrypted data
func EnvelopeDecrypt(ed *EncryptedData, kek []byte) ([]byte, error) {
    // Decrypt DEK with KEK
    dek, err := DecryptAESGCM(kek, ed.EncryptedDEK)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt DEK: %w", err)
    }

    // Decrypt data with DEK
    plaintext, err := DecryptAESGCM(dek, ed.Ciphertext)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt data: %w", err)
    }

    return plaintext, nil
}

// KEK rotation with overlap window
type KEKManager struct {
    currentKEK     []byte
    previousKEK    []byte
    rotationPeriod int  // days
}

func (m *KEKManager) RotateKEK(newKEK []byte) error {
    // Keep previous KEK for overlap window (30 days)
    m.previousKEK = m.currentKEK
    m.currentKEK = newKEK

    // Schedule previous KEK deletion after overlap window
    go func() {
        time.Sleep(30 * 24 * time.Hour)
        m.previousKEK = nil  // Secure deletion
    }()

    return nil
}
```

#### 2.3 Session State Encryption (`session.go`)

```go
package crypto

import (
    "encoding/json"
)

// SessionState represents encrypted EAP session state
type SessionState struct {
    AuthCtxId  string
    Gpsi       string  // Stored as-is (needed for routing), logged as hash
    SnssaiSst  int
    SnssaiSd   string
    Status     string
    EAPState   []byte  // Encrypted with session DEK
    CreatedAt  int64
    ExpiresAt  int64
}

// EncryptSession encrypts session state for storage
func EncryptSession(state *SessionState, kek []byte) (*EncryptedData, error) {
    // Serialize state (EAP state already encrypted separately)
    plaintext, err := json.Marshal(state)
    if err != nil {
        return nil, err
    }

    return EnvelopeEncrypt(plaintext, kek)
}

// DecryptSession decrypts session state from storage
func DecryptSession(ed *EncryptedData, kek []byte) (*SessionState, error) {
    plaintext, err := EnvelopeDecrypt(ed, kek)
    if err != nil {
        return nil, err
    }

    var state SessionState
    if err := json.Unmarshal(plaintext, &state); err != nil {
        return nil, err
    }

    return &state, nil
}
```

#### 2.4 GPSI Hashing (`hash.go`)

```go
package crypto

import (
    "crypto/sha256"
    "encoding/base64"
)

// HashGPSI creates a privacy-preserving hash for logging
// Uses fixed salt per deployment (rotated with KEK)
var gpsiSalt []byte

func SetGPSISalt(salt []byte) {
    gpsiSalt = salt
}

func HashGPSI(gpsi string) string {
    h := sha256.New()
    h.Write(gpsiSalt)
    h.Write([]byte(gpsi))
    sum := h.Sum(nil)
    return base64.RawURLEncoding.EncodeToString(sum[:16])  // Truncate to 128 bits
}

// VerifyGPSI checks if a GPSI matches a hash
func VerifyGPSI(gpsi, hash string) bool {
    return HashGPSI(gpsi) == hash
}
```

#### 2.5 HSM/KMS Interface (`kms.go`)

```go
package crypto

import (
    "context"
    "fmt"
)

// KMSProvider interface for key management systems
type KMSProvider interface {
    // GenerateKey generates a new key in the HSM
    GenerateKey(ctx context.Context, keyID string) error

    // Encrypt encrypts data using the HSM-managed key
    Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error)

    // Decrypt decrypts data using the HSM-managed key
    Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error)

    // WrapKey wraps (encrypts) a key for export
    WrapKey(ctx context.Context, keyID string, key []byte) ([]byte, error)

    // UnwrapKey unwraps (decrypts) a key for import
    UnwrapKey(ctx context.Context, keyID string, wrappedKey []byte) ([]byte, error)
}

// SoftKMS implements KMSProvider using software (for development/testing)
type SoftKMS struct {
    keys map[string][]byte
}

func NewSoftKMS() *SoftKMS {
    return &SoftKMS{keys: make(map[string][]byte)}
}

func (k *SoftKMS) GenerateKey(ctx context.Context, keyID string) error {
    key := make([]byte, KeySize)
    if _, err := rand.Read(key); err != nil {
        return err
    }
    k.keys[keyID] = key
    return nil
}

func (k *SoftKMS) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
    key, ok := k.keys[keyID]
    if !ok {
        return nil, fmt.Errorf("key %s not found", keyID)
    }
    return EncryptAESGCM(key, plaintext)
}

func (k *SoftKMS) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
    key, ok := k.keys[keyID]
    if !ok {
        return nil, fmt.Errorf("key %s not found", keyID)
    }
    return DecryptAESGCM(key, ciphertext)
}

func (k *SoftKMS) WrapKey(ctx context.Context, keyID string, key []byte) ([]byte, error) {
    return k.Encrypt(ctx, keyID, key)
}

func (k *SoftKMS) UnwrapKey(ctx context.Context, keyID string, wrappedKey []byte) ([]byte, error) {
    return k.Decrypt(ctx, keyID, wrappedKey)
}
```

---

### 3. Secrets Management

**Priority:** P0
**Dependencies:** Kubernetes secrets, Vault (optional)

#### 3.1 Kubernetes Secrets Integration

```yaml
# Per-component secrets
apiVersion: v1
kind: Secret
metadata:
  name: nssaa-http-gw-secrets
  namespace: nssaa
type: Opaque
stringData:
  # TLS certificates (mounted as files)
  tls.crt: |
    -----BEGIN CERTIFICATE-----
    ...
  tls.key: |
    -----BEGIN PRIVATE KEY-----
    ...
  # CA bundle for mTLS
  ca-bundle.pem: |
    -----BEGIN CERTIFICATE-----
    ...
---
apiVersion: v1
kind: Secret
metadata:
  name: nssaa-biz-secrets
  namespace: nssaa
type: Opaque
stringData:
  # Database password
  db-password: "secure-password-here"
  # Redis password
  redis-password: "secure-password-here"
  # KEK (encrypted)
  kek-encrypted: "base64-encoded-encrypted-kek"
---
apiVersion: v1
kind: Secret
metadata:
  name: nssaa-aaa-gw-secrets
  namespace: nssaa
type: Opaque
stringData:
  # AAA RADIUS shared secrets (encrypted per server)
  radius-secret-1: "base64-encrypted-shared-secret"
  radius-secret-2: "base64-encrypted-shared-secret"
```

#### 3.2 Vault Integration (Optional)

```go
package secrets

import (
    "context"
    "fmt"
    "os"

    vault "github.com/hashicorp/vault/api"
)

// VaultProvider retrieves secrets from HashiCorp Vault
type VaultProvider struct {
    client *vault.Client
    pathPrefix string
}

func NewVaultProvider() (*VaultProvider, error) {
    token := os.Getenv("VAULT_TOKEN")
    if token == "" {
        return nil, fmt.Errorf("VAULT_TOKEN not set")
    }

    client, err := vault.NewClient(vault.DefaultConfig())
    if err != nil {
        return nil, err
    }
    client.SetToken(token)

    return &VaultProvider{
        client: client,
        pathPrefix: "secret/data/nssaa",
    }, nil
}

func (v *VaultProvider) GetSecret(ctx context.Context, key string) ([]byte, error) {
    path := fmt.Sprintf("%s/%s", v.pathPrefix, key)

    secret, err := v.client.KVv2("secret").Get(ctx, path)
    if err != nil {
        return nil, fmt.Errorf("failed to get secret %s: %w", path, err)
    }

    data, ok := secret.Data["data"].(map[string]any)
    if !ok {
        return nil, fmt.Errorf("invalid secret format")
    }

    value, ok := data["value"].(string)
    if !ok {
        return nil, fmt.Errorf("secret value not found")
    }

    return []byte(value), nil
}
```

---

## Validation Checklist

### TLS/mTLS

- [ ] TLS 1.3 configured for all external interfaces (SBI)
- [ ] mTLS required between HTTP Gateway and Biz Pod
- [ ] mTLS configured between Biz Pod and AAA Gateway
- [ ] Certificate rotation without downtime (90-day certs, 7-day rotation)
- [ ] Mutual authentication verified

### OAuth2/JWT

- [ ] JWT token validation with NRF public key
- [ ] JWKS cached with 5-minute refresh
- [ ] OAuth2 scopes validated: `nnssaaf-nssaa`, `nnssaaf-aiw`
- [ ] Token expiry validated
- [ ] Invalid tokens rejected with 401

### Cryptography

- [ ] AES-256-GCM encryption for session state
- [ ] KEK/DEK envelope encryption hierarchy implemented
- [ ] KEK rotation with 30-day overlap window
- [ ] GPSI hashed in audit logs (privacy-preserving)
- [ ] Secure random number generation (crypto/rand)
- [ ] HSM/KMS interface defined

### Secrets Management

- [ ] Kubernetes secrets mounted as files
- [ ] Vault integration for production
- [ ] No secrets in environment variables (only in files)
- [ ] Secrets rotation without restart

### Testing

- [ ] Unit test coverage >90%
- [ ] Encryption/decryption roundtrip tests
- [ ] Key rotation tests
- [ ] JWT validation tests (valid, expired, invalid signature)

---

## Success Criteria (What Must Be TRUE)

1. **All external traffic encrypted** — TLS 1.3 on all SBI interfaces, no plaintext fallback
2. **Component-to-component trust** — mTLS ensures only authorized components can communicate
3. **Session state protected** — EAP session data encrypted at rest with AES-256-GCM
4. **Keys never in plaintext** — KEK stored encrypted, DEK generated per-session
5. **Audit logs are privacy-compliant** — GPSI hashed, no PII in logs
6. **Token validation enforces access** — Invalid/expired tokens rejected, wrong scope rejected
7. **Key rotation works** — KEK rotation does not cause data loss, overlap window allows gradual migration

---

## Dependencies

| Module | Status | Blocking |
|--------|--------|----------|
| `internal/config/` | READY (Phase 1) | No |
| `internal/types/` | READY (Phase 1) | No |
| `internal/logging/` | Phase 4 | No (can work standalone) |

---

## Next Phase

Phase 6: Testing & NRM — Unit tests, integration tests, E2E tests, 3GPP conformance, NRM/FCAPS management
