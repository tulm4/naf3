package crypto

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
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

func (m *SoftKeyManager) GetKey() ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.masterKey, true
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
	tokenFile  string
	httpClient HTTPDoer
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type vaultEncryptRequest struct {
	Plaintext string `json:"plaintext"`
}

type vaultEncryptResponse struct {
	Data struct {
		Ciphertext string `json:"ciphertext"`
	} `json:"data"`
}

type vaultDecryptRequest struct {
	Ciphertext string `json:"ciphertext"`
}

type vaultDecryptResponse struct {
	Data struct {
		Plaintext string `json:"plaintext"`
	} `json:"data"`
}

type vaultKeyInfo struct {
	Data struct {
		Keys map[string]int `json:"keys"`
	} `json:"data"`
}

func NewVaultKeyManager(cfg *VaultConfig) *VaultKeyManager {
	return &VaultKeyManager{
		address:    cfg.Address,
		keyName:    cfg.KeyName,
		authMethod: cfg.AuthMethod,
		k8sRole:    cfg.K8sRole,
		token:      cfg.Token,
		tokenFile:  cfg.TokenFile,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (m *VaultKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	ptB64 := base64.StdEncoding.EncodeToString(dek)
	body, err := json.Marshal(vaultEncryptRequest{Plaintext: ptB64})
	if err != nil {
		return nil, 0, fmt.Errorf("vault wrap: marshal: %w", err)
	}
	url := fmt.Sprintf("%s/v1/transit/encrypt/%s", m.address, m.keyName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("vault wrap: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authErr := m.setAuthHeader(req); authErr != nil {
		return nil, 0, fmt.Errorf("vault wrap: auth: %w", authErr)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("vault wrap: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("vault wrap: status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	var vaultResp vaultEncryptResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&vaultResp); decodeErr != nil {
		return nil, 0, fmt.Errorf("vault wrap: decode: %w", decodeErr)
	}
	ver, err := m.GetKeyVersion(ctx)
	if err != nil {
		ver = 1
	}
	return []byte(vaultResp.Data.Ciphertext), ver, nil
}

func (m *VaultKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	body, err := json.Marshal(vaultDecryptRequest{Ciphertext: string(wrappedDEK)})
	if err != nil {
		return nil, fmt.Errorf("vault unwrap: marshal: %w", err)
	}
	url := fmt.Sprintf("%s/v1/transit/decrypt/%s", m.address, m.keyName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vault unwrap: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authErr := m.setAuthHeader(req); authErr != nil {
		return nil, fmt.Errorf("vault unwrap: auth: %w", authErr)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault unwrap: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault unwrap: status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	var vaultResp vaultDecryptResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&vaultResp); decodeErr != nil {
		return nil, fmt.Errorf("vault unwrap: decode: %w", decodeErr)
	}
	dek, err := base64.StdEncoding.DecodeString(vaultResp.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("vault unwrap: decode base64: %w", err)
	}
	return dek, nil
}

func (m *VaultKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	url := fmt.Sprintf("%s/v1/transit/keys/%s", m.address, m.keyName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("vault keyversion: new request: %w", err)
	}
	if authErr := m.setAuthHeader(req); authErr != nil {
		return 0, fmt.Errorf("vault keyversion: auth: %w", authErr)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("vault keyversion: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("vault keyversion: status %d", resp.StatusCode)
	}
	var info vaultKeyInfo
	if decodeErr := json.NewDecoder(resp.Body).Decode(&info); decodeErr != nil {
		return 0, fmt.Errorf("vault keyversion: decode: %w", decodeErr)
	}
	maxVer := 0
	for v := range info.Data.Keys {
		var vInt int
		if _, err := fmt.Sscanf(v, "%d", &vInt); err != nil {
			continue
		}
		if vInt > maxVer {
			maxVer = vInt
		}
	}
	if maxVer == 0 {
		return 1, nil
	}
	return maxVer, nil
}

func (m *VaultKeyManager) Rotate(ctx context.Context) error {
	return m.RotateKey(ctx)
}

func (m *VaultKeyManager) GetKey() ([]byte, bool) {
	return nil, false
}

func (m *VaultKeyManager) RotateKey(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/transit/rotate/%s", m.address, m.keyName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("vault rotate: new request: %w", err)
	}
	if authErr := m.setAuthHeader(req); authErr != nil {
		return fmt.Errorf("vault rotate: auth: %w", authErr)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("vault rotate: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault rotate: status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func (m *VaultKeyManager) setAuthHeader(req *http.Request) error {
	switch m.authMethod {
	case "kubernetes":
		tokenBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err != nil {
			return fmt.Errorf("read k8s SA token: %w", err)
		}
		req.Header.Set("X-Vault-Request", "true")
		req.Header.Set("Authorization", "Bearer "+string(tokenBytes))
	case "token":
		var token string
		if m.tokenFile != "" {
			// Read token from file to avoid holding it in memory as a string.
			// TokenFile takes precedence over Token.
			data, err := os.ReadFile(m.tokenFile)
			if err != nil {
				return fmt.Errorf("read Vault token from %s: %w", m.tokenFile, err)
			}
			token = strings.TrimSpace(string(data))
		} else {
			token = m.token
		}
		if token == "" {
			return errors.New("vault token is empty (set token or tokenFile)")
		}
		req.Header.Set("X-Vault-Request", "true")
		req.Header.Set("Authorization", "Bearer "+token)
	default:
		return errors.New("unsupported auth method: " + m.authMethod)
	}
	return nil
}

// SoftHSMKeyManager is defined in kms_softhsm.go (with softhsm tag) or kms_softhsm_stub.go (without).

type SoftHSMKeyManager struct {
	libraryPath string
	tokenLabel  string
	pin         string
}

func NewSoftHSMKeyManager(cfg *SoftHSMConfig) (*SoftHSMKeyManager, error) {
	if cfg.LibraryPath == "" {
		return nil, errors.New("SoftHSMKeyManager: LibraryPath required")
	}
	return &SoftHSMKeyManager{
		libraryPath: cfg.LibraryPath,
		tokenLabel:  cfg.TokenLabel,
		pin:         cfg.PIN,
	}, nil
}

func (m *SoftHSMKeyManager) Wrap(ctx context.Context, dek []byte) ([]byte, int, error) {
	return nil, 0, errors.New("SoftHSMKeyManager.Wrap: not implemented (build with -tags=softhsm)")
}

func (m *SoftHSMKeyManager) Unwrap(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	return nil, errors.New("SoftHSMKeyManager.Unwrap: not implemented (build with -tags=softhsm)")
}

func (m *SoftHSMKeyManager) GetKeyVersion(ctx context.Context) (int, error) {
	return 1, nil
}

func (m *SoftHSMKeyManager) Rotate(ctx context.Context) error {
	return nil
}

func (m *SoftHSMKeyManager) GetKey() ([]byte, bool) {
	return nil, false
}

func (m *SoftHSMKeyManager) Close() error {
	return nil
}
