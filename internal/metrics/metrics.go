// Package metrics provides Prometheus metrics for NSSAAF observability.
// REQ-14: Prometheus metrics at /metrics (requests, latency, EAP sessions, AAA stats, circuit breakers).
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Request metrics — REQ-14
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_requests_total",
		Help: "Total NSSAA API requests",
	}, []string{"service", "endpoint", "method", "status_code"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_request_duration_seconds",
		Help:    "NSSAA API request latency",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"service", "endpoint", "method"})

	// EAP session metrics — REQ-14
	EapSessionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nssAAF_eap_sessions_active",
		Help: "Number of active EAP sessions",
	})

	EapSessionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_eap_sessions_total",
		Help: "Total EAP sessions",
	}, []string{"result"})

	EapSessionDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_eap_session_duration_seconds",
		Help:    "EAP session duration",
		Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
	}, []string{"eap_method"})

	EapRounds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "nssAAF_eap_rounds",
		Help:    "Number of EAP rounds per session",
		Buckets: []float64{1, 2, 3, 5, 10, 20},
	})

	// AAA protocol metrics — REQ-14
	AaaRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_aaa_requests_total",
		Help: "Total AAA protocol requests",
	}, []string{"protocol", "server", "result"})

	AaaRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_aaa_request_duration_seconds",
		Help:    "AAA request latency",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5},
	}, []string{"protocol", "server"})

	// Database metrics — REQ-14
	DbQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nssAAF_db_query_duration_seconds",
		Help:    "Database query latency",
		Buckets: []float64{.001, .002, .005, .01, .025, .05, .1},
	}, []string{"operation", "table"})

	DbConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nssAAF_db_connections_active",
		Help: "Active database connections",
	})

	// Redis metrics — REQ-14
	RedisOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_redis_operations_total",
		Help: "Total Redis operations",
	}, []string{"operation", "result"})

	// Circuit breaker metrics — REQ-14
	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nssAAF_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
	}, []string{"server"})

	CircuitBreakerFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nssAAF_circuit_breaker_failures_total",
		Help: "Total circuit breaker recorded failures",
	}, []string{"server"})

	// NRF discovery cache metrics — REQ-14
	NrfCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nssAAF_nrf_cache_hits_total",
		Help: "NRF cache hits",
	})

	NrfCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nssAAF_nrf_cache_misses_total",
		Help: "NRF cache misses",
	})

	// DLQ metrics — REQ-14
	DLQDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nssAAF_dlq_depth",
		Help: "Number of items in AMF notification DLQ",
	})

	DLQProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nssAAF_dlq_processed_total",
		Help: "Total DLQ items processed",
	})
)
