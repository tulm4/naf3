// Package metrics provides Prometheus metrics for NSSAAF observability.
// REQ-14: Prometheus metrics at /metrics (requests, latency, EAP sessions, AAA stats, circuit breakers).
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is the custom registry used for all NSSAAF metrics.
// Using a custom registry avoids promauto's init-time registration with the
// default registry, which would panic on duplicate registration if this package
// is imported by multiple binaries.
var Registry = prometheus.NewRegistry()

func newCounterVec(opts prometheus.CounterOpts, labels []string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(opts, labels)
	if err := Registry.Register(c); err != nil {
		panic("prometheus: failed to register counter: " + err.Error())
	}
	return c
}

func newGauge(opts prometheus.GaugeOpts) prometheus.Gauge {
	g := prometheus.NewGauge(opts)
	if err := Registry.Register(g); err != nil {
		panic("prometheus: failed to register gauge: " + err.Error())
	}
	return g
}

func newHistogramVec(opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(opts, labels)
	if err := Registry.Register(h); err != nil {
		panic("prometheus: failed to register histogram: " + err.Error())
	}
	return h
}

func newHistogram(opts prometheus.HistogramOpts) prometheus.Histogram {
	h := prometheus.NewHistogram(opts)
	if err := Registry.Register(h); err != nil {
		panic("prometheus: failed to register histogram: " + err.Error())
	}
	return h
}

func newGaugeVec(opts prometheus.GaugeOpts, labels []string) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(opts, labels)
	if err := Registry.Register(g); err != nil {
		panic("prometheus: failed to register gauge vec: " + err.Error())
	}
	return g
}

var (
	// Request metrics — REQ-14
	RequestsTotal = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_requests_total",
		Help: "Total NSSAA API requests",
	}, []string{"service", "endpoint", "method", "status_code"})

	RequestDuration = newHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_request_duration_seconds",
		Help:    "NSSAA API request latency",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"service", "endpoint", "method"})

	// EAP session metrics — REQ-14
	EapSessionsActive = newGauge(prometheus.GaugeOpts{
		Name: "nssAAF_eap_sessions_active",
		Help: "Number of active EAP sessions",
	})

	EapSessionsTotal = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_eap_sessions_total",
		Help: "Total EAP sessions",
	}, []string{"result"})

	EapSessionDuration = newHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_eap_session_duration_seconds",
		Help:    "EAP session duration",
		Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
	}, []string{"eap_method"})

	EapRounds = newHistogram(prometheus.HistogramOpts{
		Name:    "nssAAF_eap_rounds",
		Help:    "Number of EAP rounds per session",
		Buckets: []float64{1, 2, 3, 5, 10, 20},
	})

	// AAA protocol metrics — REQ-14
	AaaRequestsTotal = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_aaa_requests_total",
		Help: "Total AAA protocol requests",
	}, []string{"protocol", "server", "result"})

	AaaRequestDuration = newHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_aaa_request_duration_seconds",
		Help:    "AAA request latency",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5},
	}, []string{"protocol", "server"})

	// Database metrics — REQ-14
	DbQueryDuration = newHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_db_query_duration_seconds",
		Help:    "Database query latency",
		Buckets: []float64{.001, .002, .005, .01, .025, .05, .1},
	}, []string{"operation", "table"})

	DbConnectionsActive = newGauge(prometheus.GaugeOpts{
		Name: "nssAAF_db_connections_active",
		Help: "Active database connections",
	})

	// Redis metrics — REQ-14
	RedisOperationsTotal = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_redis_operations_total",
		Help: "Total Redis operations",
	}, []string{"operation", "result"})

	// Circuit breaker metrics — REQ-14
	CircuitBreakerState = newGaugeVec(prometheus.GaugeOpts{
		Name: "nssAAF_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
	}, []string{"server"})

	CircuitBreakerFailures = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_circuit_breaker_failures_total",
		Help: "Total circuit breaker recorded failures",
	}, []string{"server"})

	// NRF discovery cache metrics — REQ-14
	NrfCacheHits = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_nrf_cache_hits_total",
		Help: "NRF cache hits",
	}, nil)

	NrfCacheMisses = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_nrf_cache_misses_total",
		Help: "NRF cache misses",
	}, nil)

	// DLQ metrics — REQ-14
	DLQDepth = newGauge(prometheus.GaugeOpts{
		Name: "nssAAF_dlq_depth",
		Help: "Number of items in AMF notification DLQ",
	})

	DLQProcessed = newCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_dlq_processed_total",
		Help: "Total DLQ items processed",
	}, nil)
)

// Handler returns an HTTP handler that exposes all registered NSSAAF metrics.
// Use this instead of promhttp.Handler() to avoid default registry conflicts.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}
