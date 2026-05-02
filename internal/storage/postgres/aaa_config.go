// Package postgres provides PostgreSQL data persistence layer for NSSAAF.
// Spec: TS 28.541 §5.3, TS 29.571 §7
package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/operator/nssAAF/internal/crypto"
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

// ConfigRepository provides AAA configuration persistence operations.
type ConfigRepository struct {
	pool *Pool
}

// NewConfigRepository creates a new AAA config repository.
func NewConfigRepository(pool *Pool) *ConfigRepository {
	return &ConfigRepository{pool: pool}
}

// encryptSecret encrypts a shared secret using AES-256-GCM with a passphrase-derived key.
// The passphrase is stored in the application config (env var or file).
func encryptSecret(plaintext, passphrase []byte) ([]byte, error) {
	key := crypto.FromPassphrase(string(passphrase))
	return crypto.EncryptConcat(plaintext, key)
}

// decryptSecret decrypts a shared secret encrypted with encryptSecret.
func decryptSecret(ciphertext, passphrase []byte) ([]byte, error) {
	key := crypto.FromPassphrase(string(passphrase))
	return crypto.DecryptConcat(ciphertext, key)
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
func (r *ConfigRepository) Upsert(ctx context.Context, c *AAAConfig, passphrase []byte) error {
	secretB64 := ""
	if len(c.SharedSecret) > 0 && len(passphrase) > 0 {
		enc, err := encryptSecret(c.SharedSecret, passphrase)
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

	id := c.ID
	if id == "" {
		id = uuid.NewString()
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

	return &c, nil
}

// generateUUID delegates to github.com/google/uuid for RFC 4122 v4 UUID generation.
func generateUUID() (string, error) {
	return uuid.NewString(), nil
}
