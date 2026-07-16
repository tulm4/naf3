// Package config provides configuration loading and management for nssAAF.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// ComponentType identifies which binary is being run.
type ComponentType string

const (
	ComponentBiz         ComponentType = "biz"
	ComponentAAAGateway  ComponentType = "aaa-gateway"
	ComponentHTTPGateway ComponentType = "http-gateway"
	ComponentNRM         ComponentType = "nrm"
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
	Crypto       CryptoConfig       `yaml:"crypto"`
	InternalComm InternalCommConfig `yaml:"internalComm"`
	Debug        DebugConfig        `yaml:"debug"`

	// Per-component config (only one is non-nil based on Component field)
	Biz    *BizConfig    `yaml:"biz,omitempty"`
	AAAgw  *AAAgwConfig  `yaml:"aaaGateway,omitempty"`
	HTTPgw *HTTPgwConfig `yaml:"httpGateway,omitempty"`
	NRM    *NRMConfig    `yaml:"nrm,omitempty"`
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
	// Deprecated: prefer TokenFile to avoid holding the token in memory.
	Token string `yaml:"token"`
	// TokenFile is the path to a file containing the Vault token.
	// If set, the token is read from this file at startup (or refreshed if
	// the file changes). This avoids keeping the token in process memory.
	TokenFile string `yaml:"tokenFile"`
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
	BizServiceURL string `yaml:"bizServiceUrl"` // http://svc-nssaa-biz:8080
	ListenRADIUS  string `yaml:"listenRadius"`   // ":1812"

	// Diameter client-initiated config (PLAN §2.3.5):
	// Required for DER/DEA forwarding to AAA-S.
	DiameterServerAddress string `yaml:"diameterServerAddress"` // e.g. "nss-aaa-server:3868"
	DiameterRealm         string `yaml:"diameterRealm"`         // e.g. "operator.com"
	DiameterHost          string `yaml:"diameterHost"`          // Origin-Host for CER

	// DiameterTransport selects the dial network for the persistent forwarder
	// connection to AAA-S. "tcp" (default) or "sctp".
	// Spec: RFC 6733 §3; TS 29.561 §17.3.
	DiameterTransport string `yaml:"diameterTransport"`

	// RADIUS client-initiated config:
	// Required for Access-Request forwarding to AAA-S.
	RadiusServerAddress string `yaml:"radiusServerAddress"` // e.g. "nss-aaa-server:1812"
	RadiusSharedSecret  string `yaml:"radiusSharedSecret"`  // Shared secret with AAA-S

	RedisMode           string `yaml:"redisMode"`           // "standalone" or "sentinel"
	VIPAddress string `yaml:"vipAddress"` // e.g., "10.1.100.50"

	// DLQ holds Dead Letter Queue settings for server-initiated message retries.
	DLQ DLQConfig `yaml:"dlq"`
}

// DLQConfig holds Dead Letter Queue configuration for server-initiated message processing.
// These settings control how the DLQ consumer retries failed messages before discarding them.
type DLQConfig struct {
	// MaxRetries is the maximum number of delivery attempts before discarding a message.
	// Default: 10
	MaxRetries int `yaml:"maxRetries"`
	// RetryDelay is the interval between DLQ polling cycles.
	// Default: 30s
	PollInterval time.Duration `yaml:"pollInterval"`
}

// HTTPgwConfig holds HTTP Gateway configuration.
type HTTPgwConfig struct {
	BizServiceURL string      `yaml:"bizServiceUrl"` // http://svc-nssaa-biz:8080
	DiscoveryURL  string      `yaml:"discoveryUrl"`  // http://svc-nssaa-http-gw:8443
	Auth          *AuthConfig `yaml:"auth,omitempty"`
	TLS           *TLSConfig  `yaml:"tls,omitempty"`
}

// AuthConfig holds JWT authentication settings.
type AuthConfig struct {
	// Disabled skips all JWT validation. Use for E2E tests or trusted deployments.
	Disabled bool `yaml:"disabled"`
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

// DebugConfig holds the per-UE debug subsystem configuration.
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §6
type DebugConfig struct {
	Enabled   bool          `yaml:"enabled"`
	RedisAddr string        `yaml:"redisAddr"`
	TTL       time.Duration `yaml:"ttl"`
	MaxLen    int64         `yaml:"maxLen"`
}

// NRFConfig holds NRF service discovery settings.
// Extended with OAuth2, heartbeat, and discovery cache settings for full NRF integration.
// Spec: TS 29.510 (NF discovery, NF profile); TS 29.526 (NSSAAF procedures).
type NRFConfig struct {
	BaseURL         string        `yaml:"baseURL"`
	DiscoverTimeout time.Duration `yaml:"discoverTimeout"`
	CacheTTL        time.Duration `yaml:"cacheTtl"` // Default: 5m

	// InstanceID is the NF instance UUID used as the NF profile's nfInstanceId
	// on NRF register/update. Required for self-registration against NRF.
	InstanceID string `yaml:"instanceId"`

	// ProfilePath is the filesystem path to the NFProfile YAML config.
	// When set, SetProfilePath is called on the NRF client at startup to load
	// the profile and initialize the HeartbeatManager before StartHeartbeat.
	ProfilePath string `yaml:"profilePath"`

	// AccessToken holds OAuth2 client credential settings for Nnrf access.
	AccessToken TokenConfig `yaml:"accessToken"`

	// Heartbeat drives the self-heartbeat refresh interval sent to NRF
	// to keep this NF's profile marked as available.
	Heartbeat HeartbeatConfig `yaml:"heartbeat"`

	// DiscoveryCache configures the in-memory NF discovery result cache.
	DiscoveryCache DiscoveryCacheConfig `yaml:"discoveryCache"`
}

// TokenConfig holds OAuth2 client credentials for NRF authentication.
// Spec: TS 29.510 §6 (NF authentication via OAuth2 access tokens).
type TokenConfig struct {
	Enabled      bool   `yaml:"enabled"`
	AuthServer   string `yaml:"authServer"`   // Token endpoint URL
	ClientID     string `yaml:"clientId"`     // OAuth2 client_id
	ClientSecret string `yaml:"clientSecret"` // OAuth2 client_secret (support ${VAR} expansion)
	Scope        string `yaml:"scope"`        // Requested scope, e.g. "nnrf-nfm"
}

// HeartbeatConfig holds heartbeat manager settings.
// Spec: TS 29.510 §6.4.2 (heartbeat / patch update).
type HeartbeatConfig struct {
	// InitialInterval is the interval between heartbeats sent to NRF
	// before negotiation. YAML accepts an integer (seconds) or a duration string.
	InitialInterval time.Duration `yaml:"initialIntervalSeconds"`
	// AcceptNegotiatedInterval permits NRF to push a shorter heartbeat interval
	// via the patch response (SecDependent: PUT .../subscriptions/<id>).
	AcceptNegotiatedInterval bool `yaml:"acceptNegotiatedInterval"`
	// MaxConsecutiveFailures is the tolerance before marking NRF unreachable.
	MaxConsecutiveFailures int `yaml:"maxConsecutiveFailures"`
}

// DiscoveryCacheConfig holds discovery cache settings.
type DiscoveryCacheConfig struct {
	// Enabled toggles the in-memory discovery result cache.
	Enabled bool `yaml:"enabled"`
	// DefaultTTL is the time a discovery result stays in the cache without
	// a successful refresh. YAML accepts an integer (seconds) or a duration string.
	DefaultTTL time.Duration `yaml:"defaultTTLSeconds"`
}

// UnmarshalYAML implements yaml.Unmarshaler for HeartbeatConfig so that the
// "initialIntervalSeconds" field can be expressed as either an integer
// (seconds) or a duration string.
func (c *HeartbeatConfig) UnmarshalYAML(node *yaml.Node) error {
	// Parse a shadow struct that mirrors the production fields but
	// holds the raw int-form the YAML permits for time fields.
	var raw struct {
		InitialIntervalSeconds interface{} `yaml:"initialIntervalSeconds"`
		AcceptNegotiated       bool        `yaml:"acceptNegotiatedInterval"`
		MaxConsecutive         int         `yaml:"maxConsecutiveFailures"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	if d, ok, err := decodeDurationLike(raw.InitialIntervalSeconds); err != nil {
		return fmt.Errorf("heartbeat.initialIntervalSeconds: %w", err)
	} else if ok {
		c.InitialInterval = d
	}
	c.AcceptNegotiatedInterval = raw.AcceptNegotiated
	c.MaxConsecutiveFailures = raw.MaxConsecutive
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler for DiscoveryCacheConfig so that
// the "defaultTTLSeconds" field accepts an integer (seconds) or duration string.
func (c *DiscoveryCacheConfig) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		Enabled         bool        `yaml:"enabled"`
		DefaultTTLValue interface{} `yaml:"defaultTTLSeconds"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	c.Enabled = raw.Enabled
	if d, ok, err := decodeDurationLike(raw.DefaultTTLValue); err != nil {
		return fmt.Errorf("discoveryCache.defaultTTLSeconds: %w", err)
	} else if ok {
		c.DefaultTTL = d
	}
	return nil
}

// decodeDurationLike accepts:
//   - an int (seconds)
//   - a float64 (seconds, rounded)
//   - a duration string (e.g. "5m", "300s") — parsed via time.ParseDuration
//
// Returns ok=true when the value was provided (including an explicit nil/empty).
func decodeDurationLike(v interface{}) (time.Duration, bool, error) {
	if v == nil {
		return 0, false, nil
	}
	switch x := v.(type) {
	case int:
		return time.Duration(x) * time.Second, true, nil
	case int64:
		return time.Duration(x) * time.Second, true, nil
	case float64:
		return time.Duration(x * float64(time.Second)), true, nil
	case string:
		d, err := time.ParseDuration(x)
		if err != nil {
			return 0, false, fmt.Errorf("invalid duration %q: %w", x, err)
		}
		return d, true, nil
	default:
		return 0, false, fmt.Errorf("unsupported type %T", v)
	}
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

// NRMConfig holds NRM RESTCONF server settings.
type NRMConfig struct {
	ListenAddr string `yaml:"listenAddr"`
	// AlarmThresholds is serialized separately to allow per-threshold YAML entries.
	// Use AlarmThresholds slice in YAML, converted to AlarmThresholds* in Go.
	AlarmThresholds *NRMAlarmThreshold `yaml:"alarmThresholds,omitempty"`
	// NRMURL is the base URL of this NRM server, used by Biz Pod NRMClient
	// to push events. Set automatically from ListenAddr.
	NRMURL string `yaml:"-"`
	ReadTimeout int `yaml:"readTimeout"`
	WriteTimeout int `yaml:"writeTimeout"`
	IdleTimeout int `yaml:"idleTimeout"`
}

// NRMAlarmThreshold defines thresholds for alarm evaluation.
type NRMAlarmThreshold struct {
	FailureRatePercent  float64 `yaml:"failureRatePercent"`
	EvaluationWindowSec int     `yaml:"evaluationWindowSec"`
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

	case ComponentNRM:
		if c.NRM == nil {
			return fmt.Errorf("config.nrm is required for component=nrm")
		}
		if c.NRM.ListenAddr == "" {
			return fmt.Errorf("config.nrm.listenAddr is required")
		}
		// NRM does not use crypto — skip crypto validation.
		return nil
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
		if c.Crypto.VaultConfig.AuthMethod == "token" && c.Crypto.VaultConfig.Token == "" && c.Crypto.VaultConfig.TokenFile == "" {
			return fmt.Errorf("config.crypto.vault.token or config.crypto.vault.tokenFile is required when authMethod is token")
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
// Supports empty-default form ${VAR:-} and variable-name-only form ${VAR}.
var envVarRegex = regexp.MustCompile(`\$\{([^}:]+)(?::-([^}]*))?\}`)

func expandEnv(s string) string {
	return envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
		parts := envVarRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		key := parts[1]
		defaultVal := ""
		if len(parts) >= 3 {
			defaultVal = parts[2]
		}
		if val := os.Getenv(key); val != "" {
			return val
		}
		return defaultVal
	})
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
		if cfg.AAAgw.RedisMode == "" {
			cfg.AAAgw.RedisMode = "standalone"
		}
		// VIPAddress has no default — empty means dev/test mode (no VIP check)
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
		if cfg.AAAgw.DiameterTransport == "" {
			cfg.AAAgw.DiameterTransport = "tcp"
		}
		// RADIUS client config defaults — no required fields (disabled if RadiusServerAddress empty)

		// DLQ defaults for server-initiated message retries
		if cfg.AAAgw.DLQ.MaxRetries == 0 {
			cfg.AAAgw.DLQ.MaxRetries = 10
		}
		if cfg.AAAgw.DLQ.PollInterval == 0 {
			cfg.AAAgw.DLQ.PollInterval = 30 * time.Second
		}
	}

	// NRF defaults (Phase 4 — NF Integration)
	if cfg.NRF.CacheTTL == 0 {
		cfg.NRF.CacheTTL = 5 * time.Minute
	}
	if cfg.NRF.Heartbeat.InitialInterval == 0 {
		cfg.NRF.Heartbeat.InitialInterval = 5 * time.Minute
	}
	if cfg.NRF.Heartbeat.MaxConsecutiveFailures == 0 {
		cfg.NRF.Heartbeat.MaxConsecutiveFailures = 3
	}
	if cfg.NRF.DiscoveryCache.DefaultTTL == 0 {
		cfg.NRF.DiscoveryCache.DefaultTTL = time.Hour
	}

	// InternalComm native defaults
	if cfg.InternalComm.Native.Timeout == 0 {
		cfg.InternalComm.Native.Timeout = 30 * time.Second
	}
	if cfg.InternalComm.Native.Radius.MaxRetries == 0 {
		cfg.InternalComm.Native.Radius.MaxRetries = 3
	}
	if cfg.InternalComm.Native.Radius.Timeout == 0 {
		cfg.InternalComm.Native.Radius.Timeout = 10 * time.Second
	}
	if cfg.InternalComm.Native.Radius.ResponseWindow == 0 {
		cfg.InternalComm.Native.Radius.ResponseWindow = 15 * time.Second
	}

	// AUSF defaults (Phase 4 — N60 interface integration)
	if cfg.AUSF.BaseURL == "" {
		cfg.AUSF.BaseURL = cfg.NRF.BaseURL // Default: discover via NRF
	}
	if cfg.AUSF.Timeout == 0 {
		cfg.AUSF.Timeout = 10 * time.Second
	}

	// NRM defaults (Phase 6 — Integration Testing & NRM)
	if cfg.Component == ComponentNRM {
		if cfg.NRM != nil && cfg.NRM.ListenAddr == "" {
			cfg.NRM.ListenAddr = ":8081"
		}
		if cfg.NRM != nil && cfg.NRM.ReadTimeout == 0 {
			cfg.NRM.ReadTimeout = 10
		}
		if cfg.NRM != nil && cfg.NRM.WriteTimeout == 0 {
			cfg.NRM.WriteTimeout = 30
		}
		if cfg.NRM != nil && cfg.NRM.IdleTimeout == 0 {
			cfg.NRM.IdleTimeout = 120
		}
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
