// Package config provides configuration loading and management for nssAAF.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ComponentType identifies which binary is being run.
type ComponentType string

const (
	ComponentBiz         ComponentType = "biz"
	ComponentAAAGateway  ComponentType = "aaa-gateway"
	ComponentHTTPGateway ComponentType = "http-gateway"
	keyManagerSoft       string        = "soft"
)

// Config holds all runtime configuration for nssAAF.
type Config struct {
	Component ComponentType `yaml:"component"`
	Version   string        `yaml:"version"`

	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	EAP       EAPConfig       `yaml:"eap"`
	AAA       AAAConfig       `yaml:"aaa"`
	RateLimit RateLimitConfig `yaml:"rateLimit"`
	Logging   LoggingConfig   `yaml:"logging"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	NRF       NRFConfig       `yaml:"nrf"`
	UDM       UDMConfig       `yaml:"udm"`
	AUSF      AUSFConfig      `yaml:"ausf"`
	Crypto    CryptoConfig    `yaml:"crypto"`

	// Per-component config (only one is non-nil based on Component field)
	Biz    *BizConfig    `yaml:"biz,omitempty"`
	AAAgw  *AAAgwConfig  `yaml:"aaaGateway,omitempty"`
	HTTPgw *HTTPgwConfig `yaml:"httpGateway,omitempty"`
}

// TLSConfig holds TLS certificate configuration.
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
	CA   string `yaml:"ca"`
}

// CryptoConfig holds cryptographic key management settings for the Biz Pod.
type CryptoConfig struct {
	// KeyManager is the key management backend: "soft", "softhsm", "vault"
	KeyManager string `yaml:"keyManager"`
	// MasterKeyHex is the 64-char hex-encoded 32-byte master key for soft mode.
	// Required when keyManager is "soft".
	MasterKeyHex string `yaml:"masterKeyHex"`
	// VaultConfig holds HashiCorp Vault transit engine settings.
	VaultConfig *VaultConfig `yaml:"vault,omitempty"`
	// SoftHSMConfig holds SoftHSM2 settings.
	SoftHSMConfig *SoftHSMConfig `yaml:"softHSM,omitempty"`
	// KEKOverlapDays is the overlap window for KEK rotation (default: 30).
	KEKOverlapDays int `yaml:"kekOverlapDays"`
}

// VaultConfig holds HashiCorp Vault transit engine configuration.
type VaultConfig struct {
	// Address is the Vault server address, e.g. "http://vault.vault.svc.cluster.local:8200"
	Address string `yaml:"address"`
	// KeyName is the transit key name, e.g. "nssaa-kek"
	KeyName string `yaml:"keyName"`
	// AuthMethod is the auth method: "kubernetes", "token"
	AuthMethod string `yaml:"authMethod"`
	// K8sRole is the Kubernetes SA role (required when authMethod is "kubernetes").
	K8sRole string `yaml:"k8sRole"`
	// Token is the Vault token (required when authMethod is "token").
	Token string `yaml:"token"`
}

// SoftHSMConfig holds SoftHSM2 configuration.
type SoftHSMConfig struct {
	// LibraryPath is the path to libsofthsm2.so.
	LibraryPath string `yaml:"libraryPath"`
	// TokenLabel is the SoftHSM token label containing the KEK.
	TokenLabel string `yaml:"tokenLabel"`
	// PIN is the SOFTHSM PIN (user:pin format).
	PIN string `yaml:"pin"`
}

// BizConfig holds Biz Pod configuration.
type BizConfig struct {
	AAAGatewayURL string     `yaml:"aaaGatewayUrl"` // http://svc-nssaa-aaa:9090
	UseMTLS       bool       `yaml:"useMTLS"`
	TLSCert       string     `yaml:"tlsCert"`
	TLSKey        string     `yaml:"tlsKey"`
	TLSCA         string     `yaml:"tlsCa"`
	TLS           *TLSConfig `yaml:"tls,omitempty"`
}

// AAAgwConfig holds AAA Gateway configuration.
type AAAgwConfig struct {
	BizServiceURL    string `yaml:"bizServiceUrl"`    // http://svc-nssaa-biz:8080
	ListenRADIUS     string `yaml:"listenRadius"`     // ":1812"
	ListenDIAMETER   string `yaml:"listenDiameter"`   // ":3868"
	DiameterProtocol string `yaml:"diameterProtocol"` // "tcp" or "sctp"

	// Diameter client-initiated config (PLAN §2.3.5):
	// Required for DER/DEA forwarding to AAA-S.
	DiameterServerAddress string `yaml:"diameterServerAddress"` // e.g. "nss-aaa-server:3868"
	DiameterRealm         string `yaml:"diameterRealm"`         // e.g. "operator.com"
	DiameterHost          string `yaml:"diameterHost"`          // Origin-Host for CER

	// RADIUS client-initiated config:
	// Required for Access-Request forwarding to AAA-S.
	RadiusServerAddress string `yaml:"radiusServerAddress"` // e.g. "nss-aaa-server:1812"
	RadiusSharedSecret  string `yaml:"radiusSharedSecret"`  // Shared secret with AAA-S

	RedisMode           string `yaml:"redisMode"`           // "standalone" or "sentinel"
	KeepalivedStatePath string `yaml:"keepalivedStatePath"` // "/var/run/keepalived/state"
}

// HTTPgwConfig holds HTTP Gateway configuration.
type HTTPgwConfig struct {
	BizServiceURL string     `yaml:"bizServiceUrl"` // http://svc-nssaa-biz:8080
	TLS           *TLSConfig `yaml:"tls,omitempty"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"readTimeout"`
	WriteTimeout time.Duration `yaml:"writeTimeout"`
	IdleTimeout  time.Duration `yaml:"idleTimeout"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	Name            string        `yaml:"name"`
	User            string        `yaml:"user"`
	Password        string        `yaml:"password"`
	MaxConns        int           `yaml:"maxConns"`
	MinConns        int           `yaml:"minConns"`
	ConnMaxLifetime time.Duration `yaml:"connMaxLifetime"`
	SSLMode         string        `yaml:"sslMode"`
}

// RedisConfig holds Redis cluster settings.
type RedisConfig struct {
	Addr     string   `yaml:"addr"` // Single address for Biz Pod / AAA Gateway (e.g., "redis:6379")
	Addrs    []string `yaml:"addrs"`
	Password string   `yaml:"password"`
	DB       int      `yaml:"db"`
	PoolSize int      `yaml:"poolSize"`
}

// EAPConfig holds EAP session settings.
type EAPConfig struct {
	MaxRounds    int           `yaml:"maxRounds"`
	RoundTimeout time.Duration `yaml:"roundTimeout"`
	SessionTTL   time.Duration `yaml:"sessionTtl"`
}

// AAAConfig holds AAA server settings.
type AAAConfig struct {
	ResponseTimeout  time.Duration `yaml:"responseTimeout"`
	MaxRetries       int           `yaml:"maxRetries"`
	FailureThreshold int           `yaml:"failureThreshold"`
	RecoveryTimeout  time.Duration `yaml:"recoveryTimeout"`
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	PerGpsiPerMin int `yaml:"perGpsiPerMin"`
	PerAmfPerSec  int `yaml:"perAmfPerSec"`
	GlobalPerSec  int `yaml:"globalPerSec"`
}

// LoggingConfig holds logging settings.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// MetricsConfig holds Prometheus metrics settings.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// NRFConfig holds NRF service discovery settings.
type NRFConfig struct {
	BaseURL         string        `yaml:"baseURL"`
	DiscoverTimeout time.Duration `yaml:"discoverTimeout"`
}

// UDMConfig holds UDM API settings.
type UDMConfig struct {
	BaseURL string        `yaml:"baseURL"`
	Timeout time.Duration `yaml:"timeout"`
}

// AUSFConfig holds AUSF API settings.
type AUSFConfig struct {
	BaseURL string        `yaml:"baseURL"`
	Timeout time.Duration `yaml:"timeout"`
}

// Load reads and parses a YAML configuration file.
// Environment variable placeholders like ${VAR_NAME} are expanded.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	// Expand environment variable placeholders
	expanded := expandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %w", path, err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	// Validate component-specific required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks that required fields are present for the configured component.
// Returns an error describing the first missing field found.
//
//nolint:gocyclo // complexity inherent in config validation
func (c *Config) Validate() error {
	switch c.Component {
	case ComponentBiz:
		if c.Biz == nil {
			return fmt.Errorf("config.biz is required for component=biz")
		}
		if c.Biz.AAAGatewayURL == "" {
			return fmt.Errorf("config.biz.aaaGatewayUrl is required")
		}
		if c.Biz.UseMTLS {
			if c.Biz.TLSCert == "" {
				return fmt.Errorf("config.biz.tlsCert is required when useMTLS is true")
			}
			if c.Biz.TLSKey == "" {
				return fmt.Errorf("config.biz.tlsKey is required when useMTLS is true")
			}
			if c.Biz.TLSCA == "" {
				return fmt.Errorf("config.biz.tlsCa is required when useMTLS is true")
			}
		}

	case ComponentAAAGateway:
		if c.AAAgw == nil {
			return fmt.Errorf("config.aaaGateway is required for component=aaa-gateway")
		}
		if c.AAAgw.BizServiceURL == "" {
			return fmt.Errorf("config.aaaGateway.bizServiceUrl is required")
		}

	case ComponentHTTPGateway:
		if c.HTTPgw == nil {
			return fmt.Errorf("config.httpGateway is required for component=http-gateway")
		}
		if c.HTTPgw.TLS != nil {
			if c.HTTPgw.TLS.Cert == "" {
				return fmt.Errorf("config.httpGateway.tls.cert is required when TLS is configured")
			}
			if c.HTTPgw.TLS.Key == "" {
				return fmt.Errorf("config.httpGateway.tls.key is required when TLS is configured")
			}
			// Note: If the HTTP Gateway needs to verify client certificates from AMF/AUSF
			// (mTLS), add a tls.ClientAuth check here and require TLS.CA.
			// Currently, AMF/AUSF use JWT tokens (not client certs) for HTTP Gateway mTLS,
			// so CA verification is optional. If ClientAuth == tls.RequireAndVerifyClientCert,
			// then c.HTTPgw.TLS.CA must be non-empty.
		}
	}

	if c.Crypto.KeyManager == keyManagerSoft {
		if c.Crypto.MasterKeyHex == "" {
			return fmt.Errorf("config.crypto.masterKeyHex is required when keyManager is soft (or set MASTER_KEY_HEX env var)")
		}
		if len(c.Crypto.MasterKeyHex) != 64 {
			return fmt.Errorf("config.crypto.masterKeyHex must be 64 hex chars (32 bytes), got %d", len(c.Crypto.MasterKeyHex))
		}
		_, err := hex.DecodeString(c.Crypto.MasterKeyHex)
		if err != nil {
			return fmt.Errorf("config.crypto.masterKeyHex is not valid hex: %w", err)
		}
	}

	if c.Crypto.KeyManager == "vault" {
		if c.Crypto.VaultConfig == nil {
			return fmt.Errorf("config.crypto.vault is required when keyManager is vault")
		}
		if c.Crypto.VaultConfig.Address == "" {
			return fmt.Errorf("config.crypto.vault.address is required")
		}
		if c.Crypto.VaultConfig.KeyName == "" {
			return fmt.Errorf("config.crypto.vault.keyName is required")
		}
		if c.Crypto.VaultConfig.AuthMethod == "" {
			return fmt.Errorf("config.crypto.vault.authMethod is required (kubernetes or token)")
		}
		if c.Crypto.VaultConfig.AuthMethod == "kubernetes" && c.Crypto.VaultConfig.K8sRole == "" {
			return fmt.Errorf("config.crypto.vault.k8sRole is required when authMethod is kubernetes")
		}
		if c.Crypto.VaultConfig.AuthMethod == "token" && c.Crypto.VaultConfig.Token == "" {
			return fmt.Errorf("config.crypto.vault.token is required when authMethod is token")
		}
	}

	if c.Crypto.KeyManager == "softhsm" {
		if c.Crypto.SoftHSMConfig == nil {
			return fmt.Errorf("config.crypto.softHSM is required when keyManager is softhsm")
		}
		if c.Crypto.SoftHSMConfig.TokenLabel == "" {
			return fmt.Errorf("config.crypto.softHSM.tokenLabel is required")
		}
		if c.Crypto.SoftHSMConfig.PIN == "" {
			return fmt.Errorf("config.crypto.softHSM.pin is required")
		}
		if c.Crypto.SoftHSMConfig.LibraryPath == "" {
			c.Crypto.SoftHSMConfig.LibraryPath = "/usr/lib/softhsm/libsofthsm2.so"
		}
	}

	return nil
}

// expandEnv expands ${VAR} and ${VAR:-default} placeholders.
func expandEnv(s string) string {
	// Simple expansion: ${VAR} → os.Getenv("VAR")
	result := os.Expand(s, func(key string) string {
		return os.Getenv(key)
	})
	return result
}

// applyDefaults sets sensible defaults for unset fields.
//
//nolint:gocyclo // complexity inherent in config defaults
func applyDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 10 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 30 * time.Second
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = 120 * time.Second
	}

	if cfg.EAP.MaxRounds == 0 {
		cfg.EAP.MaxRounds = 20
	}
	if cfg.EAP.RoundTimeout == 0 {
		cfg.EAP.RoundTimeout = 30 * time.Second
	}
	if cfg.EAP.SessionTTL == 0 {
		cfg.EAP.SessionTTL = 5 * time.Minute
	}

	if cfg.AAA.ResponseTimeout == 0 {
		cfg.AAA.ResponseTimeout = 10 * time.Second
	}
	if cfg.AAA.MaxRetries == 0 {
		cfg.AAA.MaxRetries = 3
	}
	if cfg.AAA.FailureThreshold == 0 {
		cfg.AAA.FailureThreshold = 5
	}
	if cfg.AAA.RecoveryTimeout == 0 {
		cfg.AAA.RecoveryTimeout = 30 * time.Second
	}

	if cfg.Metrics.Path == "" {
		cfg.Metrics.Path = "/metrics"
	}

	if cfg.Redis.PoolSize == 0 {
		cfg.Redis.PoolSize = 50
	}

	// Redis Addr default
	if cfg.Redis.Addr == "" && len(cfg.Redis.Addrs) > 0 {
		cfg.Redis.Addr = cfg.Redis.Addrs[0]
	}
	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = "localhost:6379"
	}

	// AAA Gateway defaults
	if cfg.AAAgw != nil {
		if cfg.AAAgw.ListenRADIUS == "" {
			cfg.AAAgw.ListenRADIUS = ":1812"
		}
		if cfg.AAAgw.ListenDIAMETER == "" {
			cfg.AAAgw.ListenDIAMETER = ":3868"
		}
		if cfg.AAAgw.DiameterProtocol == "" {
			cfg.AAAgw.DiameterProtocol = "tcp"
		}
		if cfg.AAAgw.RedisMode == "" {
			cfg.AAAgw.RedisMode = "standalone"
		}
		if cfg.AAAgw.KeepalivedStatePath == "" {
			cfg.AAAgw.KeepalivedStatePath = "/var/run/keepalived/state"
		}
		// Diameter client config defaults (PLAN §2.3.5 — required for DER/DEA forwarding)
		if cfg.AAAgw.DiameterServerAddress == "" {
			cfg.AAAgw.DiameterServerAddress = "nss-aaa-server:3868"
		}
		if cfg.AAAgw.DiameterRealm == "" {
			cfg.AAAgw.DiameterRealm = "operator.com"
		}
		if cfg.AAAgw.DiameterHost == "" {
			cfg.AAAgw.DiameterHost = "nssaa-gw.operator.com"
		}
		// RADIUS client config defaults — no required fields (disabled if RadiusServerAddress empty)
	}

	// AUSF defaults (Phase 4 — N60 interface integration)
	if cfg.AUSF.BaseURL == "" {
		cfg.AUSF.BaseURL = cfg.NRF.BaseURL // Default: discover via NRF
	}
	if cfg.AUSF.Timeout == 0 {
		cfg.AUSF.Timeout = 10 * time.Second
	}

	// Crypto defaults (Phase 5 — Security & Crypto)
	if cfg.Crypto.KeyManager == "" {
		cfg.Crypto.KeyManager = keyManagerSoft
	}
	if cfg.Crypto.KEKOverlapDays == 0 {
		cfg.Crypto.KEKOverlapDays = 30
	}
	if cfg.Crypto.KeyManager == keyManagerSoft && cfg.Crypto.MasterKeyHex == "" {
		cfg.Crypto.MasterKeyHex = os.Getenv("MASTER_KEY_HEX")
	}
}
