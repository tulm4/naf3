package crypto

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

var (
	globalKM     KeyManager
	globalConfig *Config
	kmMu         sync.RWMutex
)

// Config holds crypto package configuration.
type Config struct {
	KeyManager     string
	MasterKeyHex   string
	KEKOverlapDays int
	Vault          *VaultConfig
	SoftHSM        *SoftHSMConfig
}

// VaultConfig holds HashiCorp Vault transit engine settings.
type VaultConfig struct {
	Address    string
	KeyName    string
	AuthMethod string // "kubernetes" or "token"
	K8sRole    string
	Token      string
}

// SoftHSMConfig holds SoftHSM2 configuration.
type SoftHSMConfig struct {
	LibraryPath string
	TokenLabel  string
	PIN         string
}

// DefaultConfig returns sensible defaults for development.
func DefaultConfig() *Config {
	return &Config{
		KeyManager:     "soft",
		KEKOverlapDays: 30,
	}
}

// Init initializes the crypto package with configuration.
// Must be called before any crypto operations.
func Init(cfg *Config) error {
	kmMu.Lock()
	defer kmMu.Unlock()

	if globalKM != nil {
		return errors.New("crypto.Init called twice")
	}

	globalConfig = cfg

	switch cfg.KeyManager {
	case "soft":
		if cfg.MasterKeyHex == "" {
			return errors.New("crypto: MasterKeyHex required for soft key manager")
		}
		if len(cfg.MasterKeyHex) != 64 {
			return errors.New("crypto: MasterKeyHex must be 64 hex chars (32 bytes)")
		}
		key, err := hex.DecodeString(cfg.MasterKeyHex)
		if err != nil {
			return errors.New("crypto: invalid MasterKeyHex: " + err.Error())
		}
		globalKM = &SoftKeyManager{
			masterKey:  key,
			currentVer: 1,
			kekOverlap: cfg.KEKOverlapDays,
		}

	case "vault":
		if cfg.Vault == nil || cfg.Vault.Address == "" {
			return errors.New("crypto: Vault.Address required for vault key manager")
		}
		if cfg.Vault.KeyName == "" {
			return errors.New("crypto: Vault.KeyName required")
		}
		globalKM = &VaultKeyManager{
			address:    cfg.Vault.Address,
			keyName:    cfg.Vault.KeyName,
			authMethod: cfg.Vault.AuthMethod,
			k8sRole:    cfg.Vault.K8sRole,
			token:      cfg.Vault.Token,
			httpClient: &http.Client{Timeout: 10 * time.Second},
		}

	case "softhsm":
		if cfg.SoftHSM == nil {
			return errors.New("crypto: SoftHSMConfig required for softhsm key manager")
		}
		mgr, err := NewSoftHSMKeyManager(cfg.SoftHSM)
		if err != nil {
			return errors.New("crypto: failed to init SoftHSM: " + err.Error())
		}
		globalKM = mgr

	default:
		return errors.New("crypto: unknown key manager: " + cfg.KeyManager)
	}

	return nil
}

// KM returns the global KeyManager instance.
// Panics if crypto.Init has not been called.
func KM() KeyManager {
	kmMu.RLock()
	defer kmMu.RUnlock()
	if globalKM == nil {
		panic("crypto package not initialized, call crypto.Init() first")
	}
	return globalKM
}

// KeyManager abstracts all HSM/KMS operations for NSSAAF.
// Three implementations: SoftKeyManager (env var), SoftHSMKeyManager (PKCS#11), VaultKeyManager (Vault transit).
type KeyManager interface {
	// Wrap encrypts a DEK using the managed KEK. Returns wrapped DEK.
	Wrap(ctx context.Context, dek []byte) ([]byte, int, error)
	// Unwrap decrypts a wrapped DEK. Returns plaintext DEK.
	Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error)
	// GetKeyVersion returns the current active KEK version number.
	GetKeyVersion(ctx context.Context) (int, error)
}

// KeyMetadata holds HSM key metadata (never the raw key).
type KeyMetadata struct {
	ID        string
	Version   int
	Algorithm string
	CreatedAt time.Time
}
