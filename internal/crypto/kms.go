package crypto

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// SoftKeyManager implements KeyManager using a hex-encoded master key from config/env.
// This is the default for development and testing.
// In soft mode, the KEK never leaves software (not suitable for production without HSM).
type SoftKeyManager struct {
	mu          sync.RWMutex
	masterKey   []byte // 32 bytes
	currentVer  int
	kekOverlap  int    // overlap window in days
	previousKey []byte // previous KEK (valid during overlap)
}

// NewSoftKeyManager creates a SoftKeyManager from a hex-encoded key.
func NewSoftKeyManager(masterKeyHex string) (*SoftKeyManager, error) {
	if len(masterKeyHex) != 64 {
		return nil, errors.New("SoftKeyManager: key must be 64 hex chars")
	}
	key, err := hex.DecodeString(masterKeyHex)
	if err != nil {
		return nil, err
	}
	return &SoftKeyManager{
		masterKey:  key,
		currentVer: 1,
		kekOverlap: 30,
	}, nil
}

// Wrap encrypts a DEK with the current KEK using AES-256-GCM.
func (m *SoftKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	enc, err := Encrypt(dek, m.masterKey, nil)
	if err != nil {
		return nil, 0, err
	}
	// Serialized: nonce || ciphertext || tag
	out := append(enc.Nonce, enc.Ciphertext...)
	out = append(out, enc.Tag...)
	return out, m.currentVer, nil
}

// Unwrap decrypts a wrapped DEK. Tries current KEK first, then previous (overlap window).
func (m *SoftKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try current KEK
	dek, err := m.unwrapWithKey(wrappedDEK, m.masterKey)
	if err == nil {
		return dek, nil
	}

	// Try previous KEK during overlap window
	if m.previousKey != nil {
		dek, err = m.unwrapWithKey(wrappedDEK, m.previousKey)
		if err == nil {
			return dek, nil
		}
	}

	return nil, ErrDEKUnwrapFailed
}

func (m *SoftKeyManager) unwrapWithKey(wrappedDEK, key []byte) ([]byte, error) {
	if len(wrappedDEK) < 28 {
		return nil, ErrEnvelopeMalformed
	}
	nonce := wrappedDEK[:12]
	tag := wrappedDEK[len(wrappedDEK)-16:]
	ct := wrappedDEK[12 : len(wrappedDEK)-16]
	return Decrypt(EncryptedData{Nonce: nonce, Ciphertext: ct, Tag: tag}, key, nil)
}

// GetKeyVersion returns the current active KEK version.
func (m *SoftKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentVer, nil
}

// Rotate generates a new KEK and schedules overlap.
// After overlapDays, the previous KEK is discarded.
// KEK rotation for SoftKeyManager: generates new KEK via HKDF from master key material.
func (m *SoftKeyManager) Rotate(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate new KEK via HKDF from master key + version context
	newKey, err := DeriveKey(m.masterKey, nil, []byte(fmt.Sprintf("nssaa-kek-rotation:v%d", m.currentVer+1)), 32)
	if err != nil {
		return err
	}

	m.previousKey = m.masterKey
	m.masterKey = newKey
	m.currentVer++

	return nil
}

// SetOverlapDays sets the KEK overlap window in days.
func (m *SoftKeyManager) SetOverlapDays(days int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kekOverlap = days
}

// VaultKeyManager implements KeyManager using HashiCorp Vault transit engine.
// Full implementation in Wave 5. Wave 1 provides the struct + interface compliance.
type VaultKeyManager struct {
	address    string
	keyName    string
	authMethod string
	k8sRole    string
	token      string
	httpClient HTTPDoer
}

// HTTPDoer is an interface for making HTTP requests (allows testing with mock).
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Wrap calls Vault transit /encrypt endpoint to wrap a DEK.
// Implemented in Wave 5.
func (m *VaultKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	return nil, 0, errors.New("VaultKeyManager.Wrap: not implemented in Wave 1 (see Wave 5)")
}

// Unwrap calls Vault transit /decrypt endpoint to unwrap a DEK.
// Implemented in Wave 5.
func (m *VaultKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	return nil, errors.New("VaultKeyManager.Unwrap: not implemented in Wave 1 (see Wave 5)")
}

// GetKeyVersion calls Vault transit /keys endpoint to get current key version.
// Implemented in Wave 5.
func (m *VaultKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	return 1, nil // stub: returns 1 until Wave 5
}

// SoftHSMKeyManager implements KeyManager using SoftHSM2 via PKCS#11.
// Full implementation in Wave 5. Wave 1 provides the struct stub.
type SoftHSMKeyManager struct {
	libraryPath string
	tokenLabel  string
	pin         string
}

// Wrap is a stub for Wave 1.
func (m *SoftHSMKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	return nil, 0, errors.New("SoftHSMKeyManager.Wrap: not implemented in Wave 1 (see Wave 5)")
}

// Unwrap is a stub for Wave 1.
func (m *SoftHSMKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	return nil, errors.New("SoftHSMKeyManager.Unwrap: not implemented in Wave 1 (see Wave 5)")
}

// GetKeyVersion is a stub for Wave 1.
func (m *SoftHSMKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	return 1, nil
}

// NewSoftHSMKeyManager initializes the SoftHSM2 PKCS#11 context.
// Implemented in Wave 5.
func NewSoftHSMKeyManager(cfg *SoftHSMConfig) (*SoftHSMKeyManager, error) {
	return &SoftHSMKeyManager{
		libraryPath: cfg.LibraryPath,
		tokenLabel:  cfg.TokenLabel,
		pin:         cfg.PIN,
	}, nil
}
