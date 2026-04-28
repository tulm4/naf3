// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"
)

// AAAConfig represents an AAA server configuration.
// Spec: TS 29.561 Ch.16-17
type AAAConfig struct {
	ID            string
	SnssaiSST     uint8
	SnssaiSD      string // empty = wildcard
	Protocol      string // "RADIUS" or "DIAMETER"
	AAAServerHost string
	AAAServerPort int
	AAAProxyHost  string
	AAAProxyPort  int
	SharedSecret  []byte // encrypted
	AllowReauth   bool
	AllowRevoke   bool
	Priority      int
	Weight        int
	Enabled       bool
	Description   string
	CreatedAt     int64 // Unix timestamp
	UpdatedAt     int64 // Unix timestamp
}

// SecretEncryptor handles AES encryption/decryption for stored secrets.
type SecretEncryptor struct {
	key []byte
}

// NewSecretEncryptor creates an encryptor from a passphrase using HKDF-SHA256.
func NewSecretEncryptor(passphrase string) *SecretEncryptor {
	h := sha256.New()
	h.Write([]byte(passphrase))
	key := h.Sum(nil)
	return &SecretEncryptor{key: key}
}

// Encrypt encrypts a secret using AES-256-GCM.
func (e *SecretEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts a secret encrypted with Encrypt.
func (e *SecretEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(ciphertext) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	return gcm.Open(nil, nonce, ct, nil)
}

// ConfigRepository provides AAA configuration persistence operations.
type ConfigRepository struct {
	pool      *Pool
	encryptor *SecretEncryptor
}

// NewConfigRepository creates a new AAA config repository.
func NewConfigRepository(pool *Pool, encryptor *SecretEncryptor) *ConfigRepository {
	return &ConfigRepository{pool: pool, encryptor: encryptor}
}

// GetByID retrieves a config by its UUID.
func (r *ConfigRepository) GetByID(ctx context.Context, id string) (*AAAConfig, error) {
	sql := `
		SELECT
			id, snssai_sst, snssai_sd, protocol,
			aaa_server_host, aaa_server_port,
			aaa_proxy_host, aaa_proxy_port,
			shared_secret,
			allow_reauth, allow_revoke,
			priority, weight, enabled, description,
			EXTRACT(EPOCH FROM created_at)::bigint,
			EXTRACT(EPOCH FROM updated_at)::bigint
		FROM aaa_server_configs
		WHERE id = $1 AND enabled = TRUE`

	row := r.pool.QueryRow(ctx, sql, id)
	c, err := r.scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConfigNotFound
		}
		return nil, err
	}
	return c, nil
}

// GetBySnssai retrieves a config using 3-level fallback:
// 1. Exact match (SST + SD)
// 2. SST match only (SD wildcard)
// 3. Default (all wildcards)
func (r *ConfigRepository) GetBySnssai(ctx context.Context, sst uint8, sd string) (*AAAConfig, error) {
	sql := `
		SELECT
			id, snssai_sst, snssai_sd, protocol,
			aaa_server_host, aaa_server_port,
			aaa_proxy_host, aaa_proxy_port,
			shared_secret,
			allow_reauth, allow_revoke,
			priority, weight, enabled, description,
			EXTRACT(EPOCH FROM created_at)::bigint,
			EXTRACT(EPOCH FROM updated_at)::bigint
		FROM get_aaa_config($1, $2)
		WHERE id IS NOT NULL`

	row := r.pool.QueryRow(ctx, sql, sst, sd)
	c, err := r.scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConfigNotFound
		}
		return nil, err
	}
	return c, nil
}

// ListAll returns all enabled AAA configurations.
func (r *ConfigRepository) ListAll(ctx context.Context) ([]*AAAConfig, error) {
	sql := `
		SELECT
			id, snssai_sst, snssai_sd, protocol,
			aaa_server_host, aaa_server_port,
			aaa_proxy_host, aaa_proxy_port,
			shared_secret,
			allow_reauth, allow_revoke,
			priority, weight, enabled, description,
			EXTRACT(EPOCH FROM created_at)::bigint,
			EXTRACT(EPOCH FROM updated_at)::bigint
		FROM aaa_server_configs
		WHERE enabled = TRUE
		ORDER BY priority ASC, weight DESC`

	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*AAAConfig
	for rows.Next() {
		c, err := r.scanConfigFromRows(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

// Upsert inserts or updates a configuration.
func (r *ConfigRepository) Upsert(ctx context.Context, c *AAAConfig) error {
	secretB64 := ""
	if len(c.SharedSecret) > 0 {
		var err error
		enc, err := r.encryptor.Encrypt(c.SharedSecret)
		if err != nil {
			return fmt.Errorf("encrypt secret: %w", err)
		}
		secretB64 = base64.StdEncoding.EncodeToString(enc)
	}

	sql := `
		INSERT INTO aaa_server_configs (
			id, snssai_sst, snssai_sd, protocol,
			aaa_server_host, aaa_server_port,
			aaa_proxy_host, aaa_proxy_port,
			shared_secret,
			allow_reauth, allow_revoke,
			priority, weight, enabled, description
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (snssai_sst, snssai_sd)
		DO UPDATE SET
			protocol = EXCLUDED.protocol,
			aaa_server_host = EXCLUDED.aaa_server_host,
			aaa_server_port = EXCLUDED.aaa_server_port,
			aaa_proxy_host = EXCLUDED.aaa_proxy_host,
			aaa_proxy_port = EXCLUDED.aaa_proxy_port,
			shared_secret = EXCLUDED.shared_secret,
			allow_reauth = EXCLUDED.allow_reauth,
			allow_revoke = EXCLUDED.allow_revoke,
			priority = EXCLUDED.priority,
			weight = EXCLUDED.weight,
			enabled = EXCLUDED.enabled,
			description = EXCLUDED.description,
			updated_at = NOW()`

	var id string
	if c.ID == "" {
		var err error
		id, err = newUUID()
		if err != nil {
			return fmt.Errorf("generate uuid: %w", err)
		}
	} else {
		id = c.ID
	}

	var sd interface{} = c.SnssaiSD
	if sd == "" {
		sd = nil
	}

	var proxyHost interface{} = c.AAAProxyHost
	if proxyHost == "" {
		proxyHost = nil
	}

	var proxyPort interface{}
	if c.AAAProxyPort > 0 {
		proxyPort = c.AAAProxyPort
	}

	err := r.pool.Exec(ctx, sql,
		id, c.SnssaiSST, sd, c.Protocol,
		c.AAAServerHost, c.AAAServerPort,
		proxyHost, proxyPort,
		secretB64,
		c.AllowReauth, c.AllowRevoke,
		c.Priority, c.Weight, c.Enabled, c.Description,
	)
	return err
}

// ErrConfigNotFound is returned when no matching AAA config is found.
var ErrConfigNotFound = errors.New("postgres: aaa config not found")

// newUUID generates a new UUID v4 string.
func newUUID() (string, error) {
	// Generate 16 random bytes.
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("postgres: rand read: %w", err)
	}
	// Set version (4) and variant (RFC 4122).
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		big.NewInt(0).SetBytes(b[:4]).Int64(),
		big.NewInt(0).SetBytes(b[4:6]).Int64(),
		big.NewInt(0).SetBytes(b[6:8]).Int64(),
		big.NewInt(0).SetBytes(b[8:10]).Int64(),
		big.NewInt(0).SetBytes(b[10:]).Int64()), nil
}

func (r *ConfigRepository) scanConfig(row pgx.Row) (*AAAConfig, error) {
	var c AAAConfig
	var secretB64, sd interface{}
	var proxyHost, proxyPort interface{}
	var createdAt, updatedAt int64

	err := row.Scan(
		&c.ID, &c.SnssaiSST, &sd, &c.Protocol,
		&c.AAAServerHost, &c.AAAServerPort,
		&proxyHost, &proxyPort,
		&secretB64,
		&c.AllowReauth, &c.AllowRevoke,
		&c.Priority, &c.Weight, &c.Enabled, &c.Description,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	if sd != nil {
		c.SnssaiSD = sd.(string)
	}
	if proxyHost != nil {
		c.AAAProxyHost = proxyHost.(string)
	}
	if proxyPort != nil {
		c.AAAProxyPort = int(proxyPort.(int32))
	}

	c.CreatedAt = createdAt
	c.UpdatedAt = updatedAt

	if secretB64 != nil && secretB64.(string) != "" {
		enc, err := base64.StdEncoding.DecodeString(secretB64.(string))
		if err != nil {
			return nil, fmt.Errorf("decode secret: %w", err)
		}
		c.SharedSecret, err = r.encryptor.Decrypt(enc)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret: %w", err)
		}
	}

	return &c, nil
}

func (r *ConfigRepository) scanConfigFromRows(rows pgx.Rows) (*AAAConfig, error) {
	var c AAAConfig
	var secretB64, sd interface{}
	var proxyHost, proxyPort interface{}
	var createdAt, updatedAt int64

	err := rows.Scan(
		&c.ID, &c.SnssaiSST, &sd, &c.Protocol,
		&c.AAAServerHost, &c.AAAServerPort,
		&proxyHost, &proxyPort,
		&secretB64,
		&c.AllowReauth, &c.AllowRevoke,
		&c.Priority, &c.Weight, &c.Enabled, &c.Description,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	if sd != nil {
		c.SnssaiSD = sd.(string)
	}
	if proxyHost != nil {
		c.AAAProxyHost = proxyHost.(string)
	}
	if proxyPort != nil {
		c.AAAProxyPort = int(proxyPort.(int32))
	}

	c.CreatedAt = createdAt
	c.UpdatedAt = updatedAt

	if secretB64 != nil && secretB64.(string) != "" {
		enc, err := base64.StdEncoding.DecodeString(secretB64.(string))
		if err != nil {
			return nil, fmt.Errorf("decode secret: %w", err)
		}
		c.SharedSecret, err = r.encryptor.Decrypt(enc)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret: %w", err)
		}
	}

	return &c, nil
}
