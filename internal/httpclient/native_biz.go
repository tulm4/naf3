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
	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/operator/nssAAF/internal/resilience"
)

// nativeBizClient implements proto.BizServiceClient with retry + circuit breaker.
type nativeBizClient struct {
	baseURL    string
	httpClient *http.Client
	cbRegistry *resilience.Registry
	retryCfg   resilience.RetryConfig
	source     string
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
		source:  "nssAAF",
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        poolCfg.MaxIdleConns,
				MaxIdleConnsPerHost: poolCfg.MaxIdleConnsPerHost,
				IdleConnTimeout:     poolCfg.IdleConnTimeout,
			},
			Timeout: cfg.Timeout,
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
func (c *nativeBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
	// Per-destination circuit breaker (REQ-11: per-host:port isolation)
	cb := c.cbRegistry.Get(c.baseURL)
	if !cb.Allow() {
		metrics.HTTPClientCircuitBreakerState.WithLabelValues(c.baseURL).Set(float64(cb.State()))
		metrics.HTTPClientRequestDuration.WithLabelValues(c.source, c.baseURL, "circuit_open").Observe(0)
		return nil, 503, fmt.Errorf("circuit breaker open for %s", c.baseURL)
	}

	var lastBody []byte
	var lastStatus int
	var lastErr error
	var retryCount int
	prevCBState := cb.State()

	err := resilience.Do(ctx, c.retryCfg, func() error {
		respBody, status, err := c.doRequest(ctx, path, method, body, requestID)
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
			retryCount++
			lastErr = fmt.Errorf("retryable status: %d", status)
			return lastErr
		}

		return nil
	})

	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		statusStr := fmt.Sprintf("%d", lastStatus)
		if lastErr != nil {
			statusStr = "error"
		}
		metrics.HTTPClientRequestDuration.WithLabelValues(c.source, c.baseURL, statusStr).Observe(duration)
	}()

	if err != nil {
		prevCBState = cb.State()
		cb.RecordFailure()
		currCBState := cb.State()
		if prevCBState != currCBState {
			metrics.HTTPClientCircuitBreakerTransitions.WithLabelValues(c.baseURL, prevCBState.String(), currCBState.String()).Inc()
		}
		metrics.HTTPClientCircuitBreakerState.WithLabelValues(c.baseURL).Set(float64(currCBState))
		return lastBody, lastStatus, lastErr
	}
	cb.RecordSuccess()
	currCBState := cb.State()
	if prevCBState != currCBState {
		metrics.HTTPClientCircuitBreakerTransitions.WithLabelValues(c.baseURL, prevCBState.String(), currCBState.String()).Inc()
	}
	metrics.HTTPClientCircuitBreakerState.WithLabelValues(c.baseURL).Set(float64(currCBState))
	if retryCount > 0 {
		metrics.HTTPClientRequestRetries.WithLabelValues(c.source, c.baseURL).Add(float64(retryCount))
	}
	return lastBody, lastStatus, nil
}

func (c *nativeBizClient) doRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	if requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 503, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return respBody, resp.StatusCode, nil
}

var _ proto.BizServiceClient = (*nativeBizClient)(nil)
