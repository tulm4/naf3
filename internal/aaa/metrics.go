// Package aaa provides AAA proxy (AAA-P) functionality for routing between
// NSSAAF and NSS-AAA servers over RADIUS or Diameter.
// Spec: TS 29.561 §16-17
package aaa

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds AAA protocol metrics.
type Metrics struct {
	// Counters: protocol → host → result → count
	requests  map[string]map[string]map[string]int64
	latencies map[string]map[string]*atomic.Int64 // sum of latencies in ns
	counters  map[string]map[string]*atomic.Int64 // request counts

	mu sync.RWMutex
}

// NewMetrics creates a new metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{
		requests:  make(map[string]map[string]map[string]int64),
		latencies: make(map[string]map[string]*atomic.Int64),
		counters:  make(map[string]map[string]*atomic.Int64),
	}
}

// RecordAAARequest records an AAA request.
func (m *Metrics) RecordAAARequest(protocol, host, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.requests[protocol] == nil {
		m.requests[protocol] = make(map[string]map[string]int64)
	}
	if m.requests[protocol][host] == nil {
		m.requests[protocol][host] = make(map[string]int64)
	}
	m.requests[protocol][host][result]++
}

// RecordAAALatency records an AAA request latency.
func (m *Metrics) RecordAAALatency(protocol, host string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.latencies[protocol] == nil {
		m.latencies[protocol] = make(map[string]*atomic.Int64)
	}
	if m.latencies[protocol][host] == nil {
		m.latencies[protocol][host] = new(atomic.Int64)
	}
	m.latencies[protocol][host].Add(d.Nanoseconds())

	if m.counters[protocol] == nil {
		m.counters[protocol] = make(map[string]*atomic.Int64)
	}
	if m.counters[protocol][host] == nil {
		m.counters[protocol][host] = new(atomic.Int64)
	}
	m.counters[protocol][host].Add(1)
}

// RequestRate returns the requests per second for a protocol/host.
func (m *Metrics) RequestRate(protocol, host string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counter := m.counters[protocol]
	if counter == nil {
		return 0
	}
	count := counter[host]
	if count == nil {
		return 0
	}
	// This is a snapshot; in production, track over a time window.
	return float64(count.Load())
}

// AverageLatency returns the average latency in milliseconds.
func (m *Metrics) AverageLatency(protocol, host string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lat := m.latencies[protocol]
	if lat == nil {
		return 0
	}
	latency := lat[host]
	count := m.counters[protocol][host]
	if latency == nil || count == nil || count.Load() == 0 {
		return 0
	}
	return float64(latency.Load()) / float64(count.Load()) / 1e6 // ns → ms
}

// Stats returns a snapshot of metrics.
func (m *Metrics) Stats() MetricsStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s := MetricsStats{
		Requests: make(map[string]map[string]map[string]int64),
	}

	for proto, hosts := range m.requests {
		s.Requests[proto] = make(map[string]map[string]int64)
		for host, results := range hosts {
			s.Requests[proto][host] = make(map[string]int64)
			for result, count := range results {
				s.Requests[proto][host][result] = count
			}
		}
	}

	return s
}

// MetricsStats is a snapshot of metrics.
type MetricsStats struct {
	Requests map[string]map[string]map[string]int64
}
