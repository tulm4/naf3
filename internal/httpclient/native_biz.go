package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/operator/nssAAF/internal/resilience"
)

// nativeBizClient implements proto.BizServiceClient with retry + circuit breaker.
type nativeBizClient struct {
	baseURL    string
	httpClient *http.Client
	cbRegistry *resilience.Registry
	retryCfg   resilience.RetryConfig
}

func newNativeBizClient(baseURL string, cfg config.NativeCommConfig) *nativeBizClient {
	retryCfg := resilience.RetryConfig{
		MaxAttempts: cfg.Retry.MaxAttempts,
		BaseDelay:   cfg.Retry.BaseDelay,
		MaxDelay:    cfg.Retry.MaxDelay,
	}
	if retryCfg.MaxAttempts == 0 {
		retryCfg.MaxAttempts = resilience.DefaultRetryConfig.MaxAttempts
	}
	if retryCfg.BaseDelay == 0 {
		retryCfg.BaseDelay = resilience.DefaultRetryConfig.BaseDelay
	}
	if retryCfg.MaxDelay == 0 {
		retryCfg.MaxDelay = resilience.DefaultRetryConfig.MaxDelay
	}

	cbCfg := cfg.CB
	if cbCfg.FailureThreshold == 0 {
		cbCfg.FailureThreshold = 5
	}
	if cbCfg.RecoveryTimeout == 0 {
		cbCfg.RecoveryTimeout = 30 * time.Second
	}
	if cbCfg.SuccessThreshold == 0 {
		cbCfg.SuccessThreshold = 3
	}

	poolCfg := cfg.Pool
	if poolCfg.MaxIdleConnsPerHost == 0 {
		poolCfg.MaxIdleConnsPerHost = 100
	}

	return &nativeBizClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        poolCfg.MaxIdleConns,
				MaxIdleConnsPerHost: poolCfg.MaxIdleConnsPerHost,
				IdleConnTimeout:     poolCfg.IdleConnTimeout,
			},
			Timeout: 30 * time.Second,
		},
		cbRegistry: resilience.NewRegistry(
			cbCfg.FailureThreshold,
			cbCfg.RecoveryTimeout,
			cbCfg.SuccessThreshold,
		),
		retryCfg: retryCfg,
	}
}

// ForwardRequest implements proto.BizServiceClient with retry + per-destination circuit breaker.
func (c *nativeBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	// Per-destination circuit breaker (REQ-11: per-host:port isolation)
	cb := c.cbRegistry.Get(c.baseURL)
	if !cb.Allow() {
		return nil, 503, fmt.Errorf("circuit breaker open for %s", c.baseURL)
	}

	var lastBody []byte
	var lastStatus int
	var lastErr error

	err := resilience.Do(ctx, c.retryCfg, func() error {
		respBody, status, err := c.doRequest(ctx, path, method, body)
		if err != nil {
			lastErr = err
			lastStatus = status
			return err
		}

		lastStatus = status
		lastBody = respBody
		lastErr = nil

		// Don't retry 4xx errors
		if status >= 400 && status < 500 {
			return nil
		}

		// Retry 5xx errors
		if resilience.IsRetryable(status) {
			lastErr = fmt.Errorf("retryable status: %d", status)
			return lastErr
		}

		return nil
	})

	if err != nil {
		cb.RecordFailure()
		return lastBody, lastStatus, lastErr
	}
	cb.RecordSuccess()
	return lastBody, lastStatus, nil
}

func (c *nativeBizClient) doRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 503, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return respBody, resp.StatusCode, nil
}

var _ proto.BizServiceClient = (*nativeBizClient)(nil)
