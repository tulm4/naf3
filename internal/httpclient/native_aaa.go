package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
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

// nativeAAAClient implements proto.BizAAAClient with stricter retry + circuit breaker.
// AAA protocol is more sensitive: fewer retries, faster circuit breaker.
type nativeAAAClient struct {
	aaaGatewayURL string
	httpClient    *http.Client
	cbRegistry    *resilience.Registry
	retryCfg      resilience.RetryConfig
	source        string
}

func newNativeAAAClient(aaaGatewayURL string, cfg config.NativeCommConfig) *nativeAAAClient {
	// Stricter settings for AAA protocol
	cfg.Retry.MaxAttempts = 2
	cfg.Retry.MaxDelay = 10 * time.Second
	if cfg.Retry.BaseDelay == 0 {
		cfg.Retry.BaseDelay = 500 * time.Millisecond
	}

	cfg.CB.FailureThreshold = 3 // More sensitive for AAA
	cfg.CB.RecoveryTimeout = 15 * time.Second
	cfg.CB.SuccessThreshold = 2

	if cfg.Pool.MaxIdleConnsPerHost == 0 {
		cfg.Pool.MaxIdleConnsPerHost = 50
	}

	return &nativeAAAClient{
		aaaGatewayURL: aaaGatewayURL,
		source:        "nssAAF",
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        cfg.Pool.MaxIdleConns,
				MaxIdleConnsPerHost: cfg.Pool.MaxIdleConnsPerHost,
				IdleConnTimeout:      cfg.Pool.IdleConnTimeout,
			},
			Timeout: 20 * time.Second, // Stricter timeout for AAA
		},
		cbRegistry: resilience.NewRegistry(
			cfg.CB.FailureThreshold,
			cfg.CB.RecoveryTimeout,
			cfg.CB.SuccessThreshold,
		),
		retryCfg: resilience.RetryConfig{
			MaxAttempts: cfg.Retry.MaxAttempts,
			BaseDelay:   cfg.Retry.BaseDelay,
			MaxDelay:    cfg.Retry.MaxDelay,
		},
	}
}

// ForwardEAP implements proto.BizAAAClient with retry + circuit breaker.
func (c *nativeAAAClient) ForwardEAP(ctx context.Context, req *proto.AaaForwardRequest) (*proto.AaaForwardResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	cb := c.cbRegistry.Get(c.aaaGatewayURL)
	if !cb.Allow() {
		metrics.HTTPClientCircuitBreakerState.WithLabelValues(c.aaaGatewayURL).Set(float64(cb.State()))
		metrics.HTTPClientRequestDuration.WithLabelValues(c.source, c.aaaGatewayURL, "circuit_open").Observe(0)
		return nil, fmt.Errorf("circuit breaker open for %s", c.aaaGatewayURL)
	}

	var lastBody []byte
	var lastErr error
	var retryCount int
	var prevCBState resilience.State

	err = resilience.Do(ctx, c.retryCfg, func() error {
		respBody, status, err := c.doPost(ctx, body)
		if err != nil {
			lastErr = err
			return err
		}

		lastBody = respBody
		lastErr = nil

		// Don't retry 4xx errors
		if status >= 400 && status < 500 {
			return nil
		}

		// Retry 5xx errors
		if resilience.IsRetryable(status) {
			retryCount++
			prevCBState = cb.State()
			lastErr = fmt.Errorf("retryable status: %d", status)
			return lastErr
		}

		return nil
	})

	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		statusStr := "success"
		if lastErr != nil {
			statusStr = "error"
		}
		metrics.HTTPClientRequestDuration.WithLabelValues(c.source, c.aaaGatewayURL, statusStr).Observe(duration)
	}()

	if err != nil {
		prevCBState = cb.State()
		cb.RecordFailure()
		currCBState := cb.State()
		if prevCBState != currCBState {
			metrics.HTTPClientCircuitBreakerTransitions.WithLabelValues(c.aaaGatewayURL, prevCBState.String(), currCBState.String()).Inc()
		}
		metrics.HTTPClientCircuitBreakerState.WithLabelValues(c.aaaGatewayURL).Set(float64(currCBState))
		return nil, lastErr
	}
	cb.RecordSuccess()
	currCBState := cb.State()
	if prevCBState != currCBState {
		metrics.HTTPClientCircuitBreakerTransitions.WithLabelValues(c.aaaGatewayURL, prevCBState.String(), currCBState.String()).Inc()
	}
	metrics.HTTPClientCircuitBreakerState.WithLabelValues(c.aaaGatewayURL).Set(float64(currCBState))
	if retryCount > 0 {
		metrics.HTTPClientRequestRetries.WithLabelValues(c.source, c.aaaGatewayURL).Add(float64(retryCount))
	}

	var resp proto.AaaForwardResponse
	if err := json.Unmarshal(lastBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &resp, nil
}

func (c *nativeAAAClient) doPost(ctx context.Context, body []byte) ([]byte, int, error) {
	url := c.aaaGatewayURL + "/aaa/forward"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
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

var _ proto.BizAAAClient = (*nativeAAAClient)(nil)
