package crypto

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

type SoftKeyManager struct {
	mu          sync.RWMutex
	masterKey   []byte
	currentVer  int
	kekOverlap  int
	previousKey []byte
}

func NewSoftKeyManager(masterKeyHex string) (*SoftKeyManager, error) {
	if len(masterKeyHex) != 64 {
		return nil, errors.New("SoftKeyManager: key must be 64 hex chars")
	}
	key, err := hex.DecodeString(masterKeyHex)
	if err != nil {
		return nil, err
	}
	return &SoftKeyManager{masterKey: key, currentVer: 1, kekOverlap: 30}, nil
}

func (m *SoftKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	enc, err := Encrypt(dek, m.masterKey, nil)
	if err != nil {
		return nil, 0, err
	}
	out := append(enc.Nonce, enc.Ciphertext...)
	out = append(out, enc.Tag...)
	return out, m.currentVer, nil
}

func (m *SoftKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dek, err := m.unwrapWithKey(wrappedDEK, m.masterKey)
	if err == nil {
		return dek, nil
	}
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

func (m *SoftKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentVer, nil
}

func (m *SoftKeyManager) Rotate(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	newKey, err := DeriveKey(m.masterKey, nil, []byte(fmt.Sprintf("nssaa-kek-rotation:v%d", m.currentVer+1)), 32)
	if err != nil {
		return err
	}
	m.previousKey = m.masterKey
	m.masterKey = newKey
	m.currentVer++
	return nil
}

func (m *SoftKeyManager) SetOverlapDays(days int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kekOverlap = days
}

type VaultKeyManager struct {
	address    string
	keyName    string
	authMethod string
	k8sRole    string
	token      string
	httpClient HTTPDoer
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func (m *VaultKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	return nil, 0, errors.New("VaultKeyManager.Wrap: not implemented in Wave 1 (see Wave 5)")
}

func (m *VaultKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	return nil, errors.New("VaultKeyManager.Unwrap: not implemented in Wave 1 (see Wave 5)")
}

func (m *VaultKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	return 1, nil
}

type SoftHSMKeyManager struct {
	libraryPath string
	tokenLabel  string
	pin         string
}

func (m *SoftHSMKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	return nil, 0, errors.New("SoftHSMKeyManager.Wrap: not implemented in Wave 1 (see Wave 5)")
}

func (m *SoftHSMKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	return nil, errors.New("SoftHSMKeyManager.Unwrap: not implemented in Wave 1 (see Wave 5)")
}

func (m *SoftHSMKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	return 1, nil
}

func NewSoftHSMKeyManager(cfg *SoftHSMConfig) (*SoftHSMKeyManager, error) {
	return &SoftHSMKeyManager{
		libraryPath: cfg.LibraryPath,
		tokenLabel:  cfg.TokenLabel,
		pin:        cfg.PIN,
	}, nil
}
