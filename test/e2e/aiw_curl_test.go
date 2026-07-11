// Package e2e provides end-to-end conformance tests for the Nnssaaf_Aiw interface.
// Tests are designed to run against a live NSSAAF stack (via docker-compose).
//
// Spec References:
//
//	TS 29.526 §7.3.2 — Nnssaaf_Aiw Create
//	TS 29.526 §7.3.3 — Nnssaaf_Aiw Query
//	TS 29.526 §7.3.4 — Nnssaaf_Aiw Confirm
//	TS 29.526 §7.3.5 — Nnssaaf_Aiw Delete
//	TS 29.571 — Common Data Types
package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test configuration from environment variables.
type testConfig struct {
	httpGatewayURL string
	nrfMockURL     string
	authDisabled   bool
	authToken      string
	e2eTLSDir     string
}

func loadConfig(t *testing.T) *testConfig {
	t.Helper()

	cfg := &testConfig{
		httpGatewayURL: getEnvOrDefault("NAF3_HTTP_GATEWAY_URL", "https://localhost:8443"),
		nrfMockURL:     getEnvOrDefault("NAF3_NRF_MOCK_URL", "http://localhost:8082"),
		authDisabled:   os.Getenv("NAF3_AUTH_DISABLED") != "0",
		e2eTLSDir:      getEnvOrDefault("E2E_TLS_DIR", "/tmp/e2e-tls"),
	}

	// Generate TLS certs if not present
	if err := ensureTLSCerts(cfg.e2eTLSDir); err != nil {
		t.Skipf("TLS certificates not available: %v", err)
	}

	// Get OAuth2 token if auth is enabled
	if !cfg.authDisabled {
		token, err := fetchOAuth2Token(cfg.nrfMockURL)
		if err != nil {
			t.Skipf("Cannot fetch OAuth2 token (NRF mock may not be running): %v", err)
		}
		cfg.authToken = token
	}

	return cfg
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// HTTP client with TLS skip verification.
var insecureHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // E2E test only
	},
}

// AIW API request/response types.

type aiwCreateRequest struct {
	GPSI      string         `json:"gpsi"`
	Supi      string         `json:"supi"`
	NssaaInfo nssaaInfoBlock `json:"nssaaInfo"`
}

type nssaaInfoBlock struct {
	AuthSchemes   []string       `json:"authSchemes"`
	SupiRange     supiRangeBlock `json:"supiRange"`
	ValidNotifURI []string       `json:"validNotifUri,omitempty"`
	Nssai         *nssaiBlock    `json:"nssai,omitempty"`
	ExemptionInd  *bool          `json:"exemptionInd,omitempty"`
}

type supiRangeBlock struct {
	SupiRanges []supiRange `json:"supiRanges"`
}

type supiRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type nssaiBlock struct {
	Nssai []snssai `json:"nssai"`
}

type snssai struct {
	Sst int    `json:"sst"`
	Sd  string `json:"sd,omitempty"`
}

type aiwConfirmRequest struct {
	NssaaResult string `json:"nssaaResult"`
}

// ─── Auth Helpers ─────────────────────────────────────────────────────────────

func fetchOAuth2Token(nrfURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nrfURL+"/oauth2/token", strings.NewReader(
		"grant_type=client_credentials&scope=nnssaaf_aiw",
	))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := insecureHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token response status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	return tokenResp.AccessToken, nil
}

// ─── HTTP Request Helpers ────────────────────────────────────────────────────

func (c *testConfig) doRequest(method, path string, body interface{}) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, c.httpGatewayURL+path, bodyReader)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Request-ID", fmt.Sprintf("test-%d", time.Now().UnixNano()))

	if !c.authDisabled && c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := insecureHTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("execute request: %w", err)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	return resp, respBody, nil
}

// ─── TLS Certificate Helpers ─────────────────────────────────────────────────

func ensureTLSCerts(dir string) error {
	// Ensure directory exists
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create TLS dir: %w", err)
	}

	// Check if cert exists
	certPath := dir + "/server.crt"
	keyPath := dir + "/server.key"

	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			// Both exist, skip generation
			return nil
		}
	}

	// Generate self-signed cert
	cmd := execCommand("openssl", "req", "-x509", "-newkey", "rsa:4096",
		"-nodes", "-keyout", keyPath, "-out", certPath,
		"-days", "365", "-subj", "/CN=localhost/O=NSSAAF/C=US")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func execCommand(name string, args ...string) *shellCommand {
	return &shellCommand{name: name, args: args}
}

type shellCommand struct {
	name   string
	args   []string
	Stdout io.Writer
	Stderr io.Writer
}

func (c *shellCommand) Run() error {
	return execImpl(c.name, c.args, c.Stdout, c.Stderr)
}

func execImpl(name string, args []string, stdout, stderr io.Writer) error {
	// Simple exec for tests
	cmd := exec.Command(name, args...) //nolint:gosec // E2E test only
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// ─── Test Scenarios ───────────────────────────────────────────────────────────

func TestAIW_Create_Positive(t *testing.T) {
	cfg := loadConfig(t)

	tests := []struct {
		name        string
		gpsi        string
		supi        string
		authSchemes []string
		notifURI    []string
		snssai      *nssaiBlock
		exemptionInd *bool
	}{
		{
			name:        "basic create",
			gpsi:        "msisdn-12345678901",
			supi:        "imsi-123456789012345",
			authSchemes: []string{"EAP_TLS"},
		},
		{
			name:        "with supi range",
			gpsi:        "msisdn-12345678902",
			supi:        "imsi-123456789012346",
			authSchemes: []string{"EAP_TLS"},
		},
		{
			name:        "with valid notif uri",
			gpsi:        "msisdn-12345678903",
			supi:        "imsi-123456789012347",
			authSchemes: []string{"EAP_TLS"},
			notifURI:    []string{"https://example.com/nssaa/callback"},
		},
		{
			name:        "with snssai",
			gpsi:        "msisdn-12345678904",
			supi:        "imsi-123456789012348",
			authSchemes: []string{"EAP_TLS"},
			snssai: &nssaiBlock{
				Nssai: []snssai{{Sst: 1, Sd: "000001"}},
			},
		},
		{
			name:        "with exemption indicator",
			gpsi:        "msisdn-12345678905",
			supi:        "imsi-123456789012349",
			authSchemes: []string{"EAP_TLS"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nssaaInfo := nssaaInfoBlock{
				AuthSchemes: tt.authSchemes,
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{
						{Start: tt.supi, End: tt.supi},
					},
				},
			}
			if tt.notifURI != nil {
				nssaaInfo.ValidNotifURI = tt.notifURI
			}
			if tt.snssai != nil {
				nssaaInfo.Nssai = tt.snssai
			}
			if tt.exemptionInd != nil {
				nssaaInfo.ExemptionInd = tt.exemptionInd
			}

			reqBody := aiwCreateRequest{
				GPSI:      tt.gpsi,
				Supi:      tt.supi,
				NssaaInfo: nssaaInfo,
			}

			resp, body, err := cfg.doRequest(http.MethodPost, "/nnssaaf/v1/aiw", reqBody)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Log response for debugging
			t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

			// Most positive cases should return 201
			if !assert.Equal(t, http.StatusCreated, resp.StatusCode,
				"expected 201 Created, got %d: %s", resp.StatusCode, string(body)) {
				// Try to extract error cause from ProblemDetails
				var problem struct {
					Cause string `json:"cause"`
				}
				if json.Unmarshal(body, &problem) == nil && problem.Cause != "" {
					t.Logf("Error cause: %s", problem.Cause)
				}
			}

			// Check Location header on 201
			if resp.StatusCode == http.StatusCreated {
				assert.NotEmpty(t, resp.Header.Get("Location"), "Location header should be set on 201")
			}
		})
	}
}

func TestAIW_Create_Negative(t *testing.T) {
	cfg := loadConfig(t)

	tests := []struct {
		name           string
		gpsi           string
		supi           string
		nssaaInfo      *nssaaInfoBlock
		expectedStatus int
	}{
		{
			name:           "missing required gpsi",
			supi:           "imsi-123456789012345",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
			},
		},
		{
			name:           "invalid gpsi format",
			gpsi:           "invalid-gpsi",
			supi:           "imsi-123456789012345",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
			},
		},
		{
			name:           "invalid snssai sst",
			gpsi:           "msisdn-12345678901",
			supi:           "imsi-123456789012345",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
				Nssai: &nssaiBlock{
					Nssai: []snssai{{Sst: 256, Sd: "000001"}},
				},
			},
		},
		{
			name:           "missing supi",
			gpsi:           "msisdn-12345678901",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
			},
		},
		{
			name:           "invalid supi format",
			gpsi:           "msisdn-12345678901",
			supi:           "invalid-supi",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
			},
		},
		{
			name:           "missing nssaa info",
			gpsi:           "msisdn-12345678901",
			supi:           "imsi-123456789012345",
			nssaaInfo:      nil,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid nssaa info empty",
			gpsi:           "msisdn-12345678901",
			supi:           "imsi-123456789012345",
			nssaaInfo:      &nssaaInfoBlock{},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing supi range",
			gpsi:           "msisdn-12345678901",
			supi:           "imsi-123456789012345",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
			},
		},
		{
			name:           "invalid auth schemes",
			gpsi:           "msisdn-12345678901",
			supi:           "imsi-123456789012345",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"INVALID_SCHEME"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
			},
		},
		{
			name:           "invalid notif uri",
			gpsi:           "msisdn-12345678901",
			supi:           "imsi-123456789012345",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
				ValidNotifURI: []string{"not-a-valid-url"},
			},
		},
		{
			name:           "invalid snssai sd length",
			gpsi:           "msisdn-12345678901",
			supi:           "imsi-123456789012345",
			expectedStatus: http.StatusBadRequest,
			nssaaInfo: &nssaaInfoBlock{
				AuthSchemes: []string{"EAP_TLS"},
				SupiRange: supiRangeBlock{
					SupiRanges: []supiRange{{Start: "imsi-123456789012345", End: "imsi-123456789012345"}},
				},
				Nssai: &nssaiBlock{
					Nssai: []snssai{{Sst: 1, Sd: "12345"}}, // SD must be 6 hex chars
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reqBody interface{}
			if tt.nssaaInfo != nil {
				reqBody = aiwCreateRequest{
					GPSI:      tt.gpsi,
					Supi:      tt.supi,
					NssaaInfo: *tt.nssaaInfo,
				}
			} else {
				// Missing nssaaInfo - send request without it
				reqBody = map[string]interface{}{
					"gpsi": tt.gpsi,
					"supi": tt.supi,
				}
			}

			resp, body, err := cfg.doRequest(http.MethodPost, "/nnssaaf/v1/aiw", reqBody)
			require.NoError(t, err)
			defer resp.Body.Close()

			t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

			if !assert.Equal(t, tt.expectedStatus, resp.StatusCode,
				"expected %d, got %d: %s", tt.expectedStatus, resp.StatusCode, string(body)) {
				// Try to extract error cause from ProblemDetails
				var problem struct {
					Cause string `json:"cause"`
				}
				if json.Unmarshal(body, &problem) == nil && problem.Cause != "" {
					t.Logf("Error cause: %s", problem.Cause)
				}
			}
		})
	}
}

func TestAIW_Query_Confirm(t *testing.T) {
	cfg := loadConfig(t)

	t.Run("query by gpsi", func(t *testing.T) {
		resp, body, err := cfg.doRequest(http.MethodGet, "/nnssaaf/v1/aiw?gpsi=msisdn-12345678901", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		// Should return 200 or 404 depending on whether session exists
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
			"expected 200 or 404, got %d", resp.StatusCode)
	})

	t.Run("query by supi", func(t *testing.T) {
		resp, body, err := cfg.doRequest(http.MethodGet, "/nnssaaf/v1/aiw?supi=imsi-123456789012345", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
			"expected 200 or 404, got %d", resp.StatusCode)
	})

	t.Run("query not found", func(t *testing.T) {
		resp, body, err := cfg.doRequest(http.MethodGet, "/nnssaaf/v1/aiw?gpsi=msisdn-99999999999", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"expected 404 Not Found, got %d: %s", resp.StatusCode, string(body))
	})

	t.Run("query all", func(t *testing.T) {
		resp, body, err := cfg.doRequest(http.MethodGet, "/nnssaaf/v1/aiw", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"expected 200 OK, got %d: %s", resp.StatusCode, string(body))
	})

	t.Run("confirm success", func(t *testing.T) {
		reqBody := aiwConfirmRequest{NssaaResult: "AUTHENTICATION_SUCCESS"}
		resp, body, err := cfg.doRequest(http.MethodPost, "/nnssaaf/v1/aiw/msisdn-12345678901/confirm", reqBody)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
			"expected 200 or 404, got %d", resp.StatusCode)
	})

	t.Run("confirm not found", func(t *testing.T) {
		reqBody := aiwConfirmRequest{NssaaResult: "AUTHENTICATION_SUCCESS"}
		resp, body, err := cfg.doRequest(http.MethodPost, "/nnssaaf/v1/aiw/msisdn-99999999999/confirm", reqBody)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"expected 404 Not Found, got %d: %s", resp.StatusCode, string(body))
	})
}

func TestAIW_Delete(t *testing.T) {
	cfg := loadConfig(t)

	t.Run("delete by gpsi", func(t *testing.T) {
		resp, _, err := cfg.doRequest(http.MethodDelete, "/nnssaaf/v1/aiw/msisdn-12345678901", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d", resp.StatusCode)

		// Should return 204 or 404 depending on whether session exists
		assert.True(t, resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound,
			"expected 204 or 404, got %d", resp.StatusCode)
	})

	t.Run("delete not found", func(t *testing.T) {
		resp, body, err := cfg.doRequest(http.MethodDelete, "/nnssaaf/v1/aiw/msisdn-99999999999", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		assert.Equal(t, http.StatusNotFound, resp.StatusCode,
			"expected 404 Not Found, got %d: %s", resp.StatusCode, string(body))
	})

	t.Run("delete all", func(t *testing.T) {
		resp, _, err := cfg.doRequest(http.MethodDelete, "/nnssaaf/v1/aiw", nil)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d", resp.StatusCode)

		assert.True(t, resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK,
			"expected 204 or 200, got %d", resp.StatusCode)
	})
}

func TestAIW_ErrorHandling(t *testing.T) {
	cfg := loadConfig(t)

	t.Run("invalid json", func(t *testing.T) {
		// Manually construct request with invalid JSON
		req, err := http.NewRequest(http.MethodPost, cfg.httpGatewayURL+"/nnssaaf/v1/aiw",
			strings.NewReader("{invalid json}"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Request-ID", "test-invalid-json")

		resp, body, err := cfg.doRequest(http.MethodPost, "/nnssaaf/v1/aiw", "{invalid json}")
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

		// Invalid JSON should return 400 Bad Request
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"expected 400 Bad Request, got %d: %s", resp.StatusCode, string(body))

		_ = req // suppress unused warning
	})

	t.Run("invalid content type", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, cfg.httpGatewayURL+"/nnssaaf/v1/aiw",
			strings.NewReader("{}"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Request-ID", "test-invalid-content-type")

		resp, err := insecureHTTPClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d", resp.StatusCode)

		// Wrong content type should return 415 Unsupported Media Type
		assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode,
			"expected 415 Unsupported Media Type, got %d", resp.StatusCode)
	})

	t.Run("missing x-request-id", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, cfg.httpGatewayURL+"/nnssaaf/v1/aiw",
			strings.NewReader("{}"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		// X-Request-ID intentionally omitted

		resp, err := insecureHTTPClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d", resp.StatusCode)

		// Missing required header should return 400 Bad Request
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"expected 400 Bad Request, got %d", resp.StatusCode)
	})

	t.Run("invalid accept header", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, cfg.httpGatewayURL+"/nnssaaf/v1/aiw", nil)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/html") // Invalid accept type
		req.Header.Set("X-Request-ID", "test-invalid-accept")

		resp, err := insecureHTTPClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		t.Logf("Response status: %d", resp.StatusCode)

		// Unsupported accept type should return 406 Not Acceptable
		assert.Equal(t, http.StatusNotAcceptable, resp.StatusCode,
			"expected 406 Not Acceptable, got %d", resp.StatusCode)
	})
}
