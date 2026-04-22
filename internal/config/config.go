// Package config provides configuration loading and management for nssAAF.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ComponentType identifies which binary is being run.
type ComponentType string

const (
	ComponentBiz         ComponentType = "biz"
	ComponentAAAGateway ComponentType = "aaa-gateway"
	ComponentHTTPGateway ComponentType = "http-gateway"
)

// Config holds all runtime configuration for nssAAF.
type Config struct {
	Component ComponentType `yaml:"component"`
	Version   string       `yaml:"version"`

	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	EAP       EAPConfig       `yaml:"eap"`
	AAA       AAAConfig       `yaml:"aaa"`
	RateLimit RateLimitConfig `yaml:"rateLimit"`
	Logging   LoggingConfig   `yaml:"logging"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	NRF       NRFConfig     `yaml:"nrf"`
	UDM       UDMConfig     `yaml:"udm"`

	// Per-component config (only one is non-nil based on Component field)
	Biz     *BizConfig     `yaml:"biz,omitempty"`
	AAAgw   *AAAgwConfig   `yaml:"aaaGateway,omitempty"`
	HTTPgw  *HTTPgwConfig  `yaml:"httpGateway,omitempty"`
}

// TLSConfig holds TLS certificate configuration.
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
	CA   string `yaml:"ca"`
}

// BizConfig holds Biz Pod configuration.
type BizConfig struct {
	AAAGatewayURL string     `yaml:"aaaGatewayUrl"` // http://svc-nssaa-aaa:9090
	UseMTLS      bool       `yaml:"useMTLS"`
	TLSCert      string     `yaml:"tlsCert"`
	TLSKey       string     `yaml:"tlsKey"`
	TLSCA        string     `yaml:"tlsCa"`
	TLS          *TLSConfig `yaml:"tls,omitempty"`
}

// AAAgwConfig holds AAA Gateway configuration.
type AAAgwConfig struct {
	BizServiceURL      string `yaml:"bizServiceUrl"`       // http://svc-nssaa-biz:8080
	ListenRADIUS      string `yaml:"listenRadius"`        // ":1812"
	ListenDIAMETER    string `yaml:"listenDiameter"`      // ":3868"
	DiameterProtocol   string `yaml:"diameterProtocol"`    // "tcp" or "sctp"

	// Diameter client-initiated config (PLAN §2.3.5):
	// Required for DER/DEA forwarding to AAA-S.
	DiameterServerAddress string `yaml:"diameterServerAddress"` // e.g. "nss-aaa-server:3868"
	DiameterRealm        string `yaml:"diameterRealm"`         // e.g. "operator.com"
	DiameterHost         string `yaml:"diameterHost"`          // Origin-Host for CER

	RedisMode         string `yaml:"redisMode"`          // "standalone" or "sentinel"
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

	return &cfg, nil
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
	}
}
