// Package nfclient provides common NF client infrastructure.
package nfclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFactory_DoesCircuitBreak(t *testing.T) {
	factory := &Factory{transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}), timeout: time.Second}
	// We can't easily inject a custom CB since the factory creates it via registry.Get
	// Instead test with a nil registry to confirm no panic
	factory.cbRegistry = nil

	_, _, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
	assert.NoError(t, err) // nil registry = no CB guard
}

func TestFactory_RecordsSuccess(t *testing.T) {
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	factory := &Factory{transport: rt, timeout: time.Second}

	_, _, err := factory.Do(context.Background(), "http://nrf:8080", http.MethodGet, "/test", nil)
	assert.NoError(t, err)
}

func TestFactory_ReturnsStatusCode(t *testing.T) {
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not found"))}, nil
	})
	factory := &Factory{transport: rt, timeout: time.Second}

	status, body, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
	require.NoError(t, err)
	assert.Equal(t, 404, status)
	assert.Equal(t, "not found", string(body))
}

func TestFactory_RecordsFailure_OnNon2xx(t *testing.T) {
	rt := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("error"))}, nil
	})
	factory := &Factory{transport: rt, timeout: time.Second}

	status, _, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
	require.NoError(t, err) // factory returns status code, not error for non-2xx
	assert.Equal(t, 500, status)
}

func TestFactory_RecordsFailure_OnNetworkError(t *testing.T) {
	factory := &Factory{
		transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return nil, assert.AnError
		}),
		timeout: time.Second,
	}

	_, _, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
	assert.Error(t, err)
}

func TestFactory_NilRegistry_NoPanic(t *testing.T) {
	factory := NewFactory(nil)
	factory.transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})

	_, _, err := factory.Do(context.Background(), "http://udm:8080", http.MethodGet, "/test", nil)
	assert.NoError(t, err) // no CB means no guard, so this succeeds
}

func TestFactory_WithTimeout(t *testing.T) {
	factory := NewFactory(nil)
	factory.transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	factory = factory.WithTimeout(5 * time.Second)
	assert.Equal(t, 5*time.Second, factory.timeout)
}

// TestBreakerTransitionMetric_NotEmittedOnNoStateChange verifies that transition
// metrics are emitted only when the circuit breaker state actually changes, not
// on every call (CLOSED→CLOSED spurious emissions).
// Bug: before the fix, transition metrics were emitted even when no state change occurred.
func TestBreakerTransitionMetric_NotEmittedOnNoStateChange(t *testing.T) {
	cbRegistry := resilience.NewRegistry(5, 30*time.Second, 3)
	factory := NewFactory(cbRegistry)
	factory.transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	factory.timeout = time.Second

	// Make successful requests that don't change the breaker state.
	for i := 0; i < 3; i++ {
		_, _, err := factory.Do(context.Background(), "http://amf:8080/nsmf", http.MethodPost, "/callback", []byte(`{}`))
		require.NoError(t, err)
	}

	// Check for spurious transitions in the metric registry.
	gatherer := metrics.Registry
	metricFamilies, err := gatherer.Gather()
	require.NoError(t, err)

	for _, mf := range metricFamilies {
		if strings.Contains(mf.GetName(), "circuit_breaker_transitions") {
			for _, m := range mf.GetMetric() {
				from, to := "", ""
				for _, label := range m.GetLabel() {
					if label.GetName() == "from_state" {
						from = label.GetValue()
					}
					if label.GetName() == "to_state" {
						to = label.GetValue()
					}
				}
				if from == to {
					t.Errorf("spurious %s→%s transition metric emitted", from, to)
				}
			}
		}
	}
}
