package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad_BizConfig_MissingAAAGatewayURL verifies that loading a biz config
// with an empty biz.aaaGatewayUrl field returns an error.
func TestLoad_BizConfig_MissingAAAGatewayURL(t *testing.T) {
	configYAML := `
component: biz
version: "1.0.0"
biz:
  aaaGatewayUrl: ""
  useMTLS: false
server:
  addr: ":8080"
redis:
  addr: "localhost:6379"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "biz.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "aaaGatewayUrl")
}

// TestLoad_BizConfig_MTLSEnabled_MissingCertFields verifies that loading a biz
// config with useMTLS:true but missing tlsCert/tlsKey/tlsCa fields returns an error.
func TestLoad_BizConfig_MTLSEnabled_MissingCertFields(t *testing.T) {
	configYAML := `
component: biz
version: "1.0.0"
biz:
  aaaGatewayUrl: "http://aaa-gateway:9090"
  useMTLS: true
  tlsCert: ""
  tlsKey: ""
  tlsCa: ""
server:
  addr: ":8080"
redis:
  addr: "localhost:6379"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "biz.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "tlsCert")
}

// TestLoad_BizConfig_MTLSDisabled_Succeeds verifies that loading a biz config
// with useMTLS:false succeeds even when TLS cert fields are empty.
func TestLoad_BizConfig_MTLSDisabled_Succeeds(t *testing.T) {
	configYAML := `
component: biz
version: "1.0.0"
biz:
  aaaGatewayUrl: "http://aaa-gateway:9090"
  useMTLS: false
server:
  addr: ":8080"
redis:
  addr: "localhost:6379"
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "biz.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ComponentBiz, cfg.Component)
	assert.False(t, cfg.Biz.UseMTLS)
}

// TestLoad_AAAGatewayConfig_MissingBizServiceURL verifies that loading an aaa-gateway
// config with an empty aaaGateway.bizServiceUrl field returns an error.
func TestLoad_AAAGatewayConfig_MissingBizServiceURL(t *testing.T) {
	configYAML := `
component: aaa-gateway
version: "1.0.0"
aaaGateway:
  bizServiceUrl: ""
  radiusServerAddress: "aaa-server:1812"
  radiusSharedSecret: "testing123"
server:
  addr: ":8080"
redis:
  addr: "localhost:6379"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "aaa-gateway.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "bizServiceUrl")
}

// TestLoad_HTTPGatewayConfig_MissingTLSCert verifies that loading an http-gateway
// config with TLS configured but missing cert returns an error.
func TestLoad_HTTPGatewayConfig_MissingTLSCert(t *testing.T) {
	configYAML := `
component: http-gateway
version: "1.0.0"
httpGateway:
  bizServiceUrl: "http://biz:8080"
  tls:
    cert: ""
    key: "/etc/certs/server.key"
    ca: ""
server:
  addr: ":8443"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "http-gateway.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "tls.cert")
}

// TestLoad_HTTPGatewayConfig_MissingTLSKey verifies that loading an http-gateway
// config with TLS configured but missing key returns an error.
func TestLoad_HTTPGatewayConfig_MissingTLSKey(t *testing.T) {
	configYAML := `
component: http-gateway
version: "1.0.0"
httpGateway:
  bizServiceUrl: "http://biz:8080"
  tls:
    cert: "/etc/certs/server.crt"
    key: ""
    ca: ""
server:
  addr: ":8443"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "http-gateway.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "tls.key")
}

// TestLoad_HTTPGatewayConfig_NoTLSSucceeds verifies that an http-gateway config
// without TLS succeeds.
func TestLoad_HTTPGatewayConfig_NoTLSSucceeds(t *testing.T) {
	configYAML := `
component: http-gateway
version: "1.0.0"
httpGateway:
  bizServiceUrl: "http://biz:8080"
server:
  addr: ":8080"
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "http-gateway.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ComponentHTTPGateway, cfg.Component)
	assert.Nil(t, cfg.HTTPgw.TLS)
}

// TestLoad_UnknownComponentType verifies that loading a config with an unknown
// component type succeeds (the binary will reject it at startup).
func TestLoad_UnknownComponentType(t *testing.T) {
	configYAML := `
component: unknown-component
version: "1.0.0"
server:
  addr: ":8080"
redis:
  addr: "localhost:6379"
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "unknown.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ComponentType("unknown-component"), cfg.Component)
}

// TestLoad_BizConfig_AllFieldsValid verifies that a complete, valid biz config
// loads successfully.
func TestLoad_BizConfig_AllFieldsValid(t *testing.T) {
	configYAML := `
component: biz
version: "1.0.0"
biz:
  aaaGatewayUrl: "http://aaa-gateway:9090"
  useMTLS: true
  tlsCert: "/etc/certs/biz.crt"
  tlsKey: "/etc/certs/biz.key"
  tlsCa: "/etc/certs/ca.crt"
server:
  addr: ":8080"
redis:
  addr: "redis:6379"
eap:
  maxRounds: 20
  roundTimeout: 30s
  sessionTtl: 5m
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "biz-full.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, ComponentBiz, cfg.Component)
	assert.Equal(t, "http://aaa-gateway:9090", cfg.Biz.AAAGatewayURL)
	assert.True(t, cfg.Biz.UseMTLS)
	assert.Equal(t, "/etc/certs/biz.crt", cfg.Biz.TLSCert)
	assert.Equal(t, 20, cfg.EAP.MaxRounds)
}

// TestLoad_FileNotFound verifies that Load returns an error when the config file
// does not exist.
func TestLoad_FileNotFound(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestLoad_InvalidYAML verifies that Load returns an error when the config file
// contains invalid YAML.
func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(configFile, []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	cfg, err := Load(configFile)

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

// TestLoad_EnvironmentVariableExpansion verifies that environment variable
// placeholders like ${VAR_NAME} are expanded in the config file.
func TestLoad_EnvironmentVariableExpansion(t *testing.T) {
	configYAML := `
component: biz
version: "1.0.0"
biz:
  aaaGatewayUrl: "${AAA_GW_URL}"
  useMTLS: false
server:
  addr: "${SERVER_ADDR}"
redis:
  addr: "${REDIS_ADDR}"
crypto:
  keyManager: "soft"
  masterKeyHex: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "env.yaml")
	err := os.WriteFile(configFile, []byte(configYAML), 0644)
	require.NoError(t, err)

	t.Setenv("AAA_GW_URL", "http://custom-aaa-gateway:9090")
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("REDIS_ADDR", "redis-master:6379")

	cfg, err := Load(configFile)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "http://custom-aaa-gateway:9090", cfg.Biz.AAAGatewayURL)
	assert.Equal(t, ":9090", cfg.Server.Addr)
	assert.Equal(t, "redis-master:6379", cfg.Redis.Addr)
}
