package httpclient

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestDuration tracks the duration of internal HTTP requests.
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nssaa_internal_request_duration_seconds",
			Help:    "Duration of internal HTTP requests",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"source", "destination", "status"},
	)

	// RequestRetries tracks the number of retries for internal requests.
	RequestRetries = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nssaa_internal_request_retries_total",
			Help: "Total number of retries for internal requests",
		},
		[]string{"source", "destination"},
	)

	// CircuitBreakerState tracks the state of circuit breakers (0=closed, 1=open, 2=half-open).
	CircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nssaa_circuit_breaker_state",
			Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
		},
		[]string{"destination"},
	)
)
