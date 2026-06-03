// Package nfclient provides common infrastructure for NF (Network Function) HTTP clients:
// OTel-instrumented transport, circuit breaker guards, and error normalization.
package nfclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/resilience"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Factory wires common NF client infrastructure: OTel-instrumented HTTP transport,
// circuit breaker guards, and timeout management. Each NF client calls factory.Do()
// instead of duplicating the wiring.
type Factory struct {
	cbRegistry *resilience.Registry
	transport  http.RoundTripper
	timeout    time.Duration
}

// NewFactory creates a factory with shared transport and registry.
func NewFactory(cbRegistry *resilience.Registry) *Factory {
	return &Factory{
		cbRegistry: cbRegistry,
		transport:  otelhttp.NewTransport(http.DefaultTransport),
		timeout:    30 * time.Second,
	}
}

// WithTimeout returns a copy of f with a custom default timeout.
func (f *Factory) WithTimeout(timeout time.Duration) *Factory {
	return &Factory{cbRegistry: f.cbRegistry, transport: f.transport, timeout: timeout}
}

// Do executes an HTTP request with circuit breaker guard and OTel instrumentation.
// Returns (statusCode, responseBody, error).
// The caller provides method, path, and body; factory owns transport + CB + OTel.
func (f *Factory) Do(ctx context.Context, baseURL, method, path string, body []byte) (int, []byte, error) {
	if f.cbRegistry != nil {
		cb := f.cbRegistry.Get(baseURL)
		if !cb.Allow() {
			return 0, nil, fmt.Errorf("nfclient: circuit breaker open for %s", baseURL)
		}
	}

	url := baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		f.recordFailure(baseURL)
		return 0, nil, fmt.Errorf("nfclient: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Transport: f.transport,
		Timeout:   f.timeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		f.recordFailure(baseURL)
		return 0, nil, fmt.Errorf("nfclient: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("nfclient: read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		f.recordFailure(baseURL)
	} else {
		f.recordSuccess(baseURL)
	}

	return resp.StatusCode, respBody, nil
}

func (f *Factory) recordFailure(baseURL string) {
	if f.cbRegistry != nil {
		f.cbRegistry.Get(baseURL).RecordFailure()
	}
}

func (f *Factory) recordSuccess(baseURL string) {
	if f.cbRegistry != nil {
		f.cbRegistry.Get(baseURL).RecordSuccess()
	}
}

// BreakerState returns the current state of the circuit breaker for the given baseURL.
// Returns resilience.StateClosed if the registry is nil or the breaker has not been initialized.
// This method exists for testability only — production code should not depend on breaker state.
func (f *Factory) BreakerState(baseURL string) resilience.State {
	if f.cbRegistry == nil {
		return resilience.StateClosed
	}
	return f.cbRegistry.Get(baseURL).State()
}
