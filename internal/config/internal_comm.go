package config

import "time"

// InternalCommConfig holds configuration for internal component communication.
type InternalCommConfig struct {
	// Mode selects the communication mode: "native" or "istio"
	// Default: "native"
	// Can be overridden by ISTIO_MTLS=1 env var
	Mode string `yaml:"mode"`

	// Native holds settings for Go native HTTP client (default)
	Native NativeCommConfig `yaml:"native"`

	// Istio holds settings for Istio service mesh mode
	Istio IstioCommConfig `yaml:"istio"`
}

// NativeCommConfig for Go native HTTP client.
type NativeCommConfig struct {
	// Timeout is the per-request timeout for HTTP calls (default: 30s).
	Timeout time.Duration `yaml:"timeout"`
	// Retry configures retry behavior
	Retry RetryConfig `yaml:"retry"`
	// CB configures per-destination circuit breaking
	CB CircuitBreakerConfig `yaml:"circuitBreaker"`
	// Pool configures http.Transport connection pool
	Pool ConnectionPoolConfig `yaml:"connectionPool"`
	// TLS holds mTLS client certificate settings.
	// Use when the target service requires client certificate authentication.
	TLS *TLSClientConfig `yaml:"tls,omitempty"`
	// KeepalivedHealthURL is the health check endpoint for the AAA Gateway VIP.
	// Used by NativeAAAClient to detect VIP state changes for circuit breaker reset.
	// Example: "http://aaa-gateway:9090/health/vip"
	KeepalivedHealthURL string `yaml:"keepalivedHealthURL"`
}

// TLSClientConfig for mTLS client authentication.
type TLSClientConfig struct {
	// CACert is the root CA certificate to verify the server certificate.
	CACert string `yaml:"caCert"`
	// ClientCert is the client certificate file (PEM-encoded).
	ClientCert string `yaml:"clientCert"`
	// ClientKey is the client private key file (PEM-encoded).
	ClientKey string `yaml:"clientKey"`
	// ServerName is the SNI value for TLS handshake.
	ServerName string `yaml:"serverName"`
}

// RetryConfig for exponential backoff retry.
type RetryConfig struct {
	MaxAttempts int           `yaml:"maxAttempts"`
	BaseDelay  time.Duration `yaml:"baseDelay"`
	MaxDelay   time.Duration `yaml:"maxDelay"`
}

// CircuitBreakerConfig mirrors resilience.CircuitBreaker defaults.
type CircuitBreakerConfig struct {
	FailureThreshold int           `yaml:"failureThreshold"`
	RecoveryTimeout  time.Duration `yaml:"recoveryTimeout"`
	SuccessThreshold int          `yaml:"successThreshold"`
}

// ConnectionPoolConfig for http.Transport tuning.
type ConnectionPoolConfig struct {
	MaxIdleConns        int           `yaml:"maxIdleConns"`
	MaxIdleConnsPerHost int           `yaml:"maxIdleConnsPerHost"`
	IdleConnTimeout     time.Duration `yaml:"idleConnTimeout"`
	DialTimeout         time.Duration `yaml:"dialTimeout"`
}

// IstioCommConfig for Istio service mesh mode.
type IstioCommConfig struct {
	TrustDomain string `yaml:"trustDomain"`
}
