package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/resilience"
)

// ProxyClient calls HTTP Gateway proxy endpoints for external NFs.
// Uses Option A: Kubernetes Service DNS with kube-proxy round-robin.
type ProxyClient struct {
	httpClient *http.Client
	gatewayURL string
	retryCfg   resilience.RetryConfig
}

// NewProxyClient creates a proxy client for HTTP Gateway.
func NewProxyClient(gatewayURL string, retryCfg config.RetryConfig, timeout time.Duration) *ProxyClient {
	rCfg := resilience.RetryConfig{
		MaxAttempts: retryCfg.MaxAttempts,
		BaseDelay:   retryCfg.BaseDelay,
		MaxDelay:    retryCfg.MaxDelay,
	}
	if rCfg.MaxAttempts == 0 {
		rCfg.MaxAttempts = resilience.DefaultRetryConfig.MaxAttempts
	}
	if rCfg.BaseDelay == 0 {
		rCfg.BaseDelay = resilience.DefaultRetryConfig.BaseDelay
	}
	if rCfg.MaxDelay == 0 {
		rCfg.MaxDelay = resilience.DefaultRetryConfig.MaxDelay
	}
	return &ProxyClient{
		httpClient: &http.Client{Timeout: timeout},
		gatewayURL: gatewayURL,
		retryCfg:   rCfg,
	}
}

// CallNRF forwards a request to the NRF via HTTP Gateway proxy.
func (p *ProxyClient) CallNRF(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	return p.call(ctx, "nrf", method, path, body)
}

// CallUDM forwards a request to the UDM via HTTP Gateway proxy.
func (p *ProxyClient) CallUDM(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	return p.call(ctx, "udm", method, path, body)
}

// CallAMF forwards a request to the AMF via HTTP Gateway proxy.
func (p *ProxyClient) CallAMF(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	return p.call(ctx, "amf", method, path, body)
}

func (p *ProxyClient) call(ctx context.Context, targetNF, method, path string, body []byte) (int, []byte, error) {
	var lastErr error
	var lastStatus int
	var lastBody []byte

	url := p.gatewayURL + "/internal/" + targetNF + path

	err := resilience.Do(ctx, p.retryCfg, func() error {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")
		if reqID, ok := ctx.Value(requestIDKey).(string); ok && reqID != "" {
			req.Header.Set("X-Request-ID", reqID)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = err
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		lastStatus = resp.StatusCode
		lastBody, _ = io.ReadAll(resp.Body)

		// Don't retry 4xx errors
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil
		}

		// Retry 5xx errors
		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			return lastErr
		}

		return nil
	})

	if err != nil {
		return lastStatus, lastBody, lastErr
	}
	return lastStatus, lastBody, nil
}

// requestIDKey is used to extract X-Request-ID from context.
var requestIDKey = struct{}{}
