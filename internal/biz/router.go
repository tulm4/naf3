// Package biz provides the business logic layer for the NSSAAF Biz Pod.
// It contains routing decisions and business logic without direct AAA transport.
// Spec: TS 29.526 v18.7.0
package biz

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/prometheus/client_golang/prometheus"
)

// ProxyMode represents the AAA routing mode.
type ProxyMode int

const (
	// ProxyModeDirect forwards requests directly to NSS-AAA servers.
	ProxyModeDirect ProxyMode = iota
	// ProxyModeProxy forwards requests through an intermediate AAA proxy.
	// ProxyModeProxy forwards requests through an intermediate AAA proxy.
	ProxyModeProxy
)

// String implements fmt.Stringer.
func (m ProxyMode) String() string {
	switch m {
	case ProxyModeDirect:
		return "DIRECT"
	case ProxyModeProxy:
		return "PROXY"
	default:
		return fmt.Sprintf("ProxyMode(%d)", m)
	}
}

// Protocol represents the AAA protocol type.
type Protocol int

const (
	// ProtocolRADIUS indicates the AAA server uses RADIUS.
	ProtocolRADIUS Protocol = iota
	// ProtocolDIAMETER indicates the AAA server uses Diameter.
	// ProtocolDIAMETER indicates the AAA server uses Diameter.
	ProtocolDIAMETER
)

// String implements fmt.Stringer.
func (p Protocol) String() string {
	switch p {
	case ProtocolRADIUS:
		return "RADIUS"
	case ProtocolDIAMETER:
		return "DIAMETER"
	default:
		return fmt.Sprintf("Protocol(%d)", p)
	}
}

// RouteDecision contains the routing decision for an AAA request.
type RouteDecision struct {
	Mode     ProxyMode
	Protocol Protocol
	Host     string
	Port     int
	Timeout  time.Duration
}

// ServerConfig holds configuration for a single AAA server.
type ServerConfig struct {
	Protocol     Protocol
	Host         string
	Port         int
	SharedSecret string // RADIUS shared secret
	TLSCert      string
	TLSKey       string
	TLSCA        string
	Timeout      time.Duration
	Weight       int // for load balancing
}

// SnssaiConfig maps an S-NSSAI to AAA server configuration.
// 3-level lookup: exact (sst+sd), sst-only (sst, sd=*), default (sst=*, sd=*).
type SnssaiConfig struct {
	Exact   *ServerConfig // exact (sst, sd)
	SST     *ServerConfig // sst only, sd=*
	Default *ServerConfig // global default
}

// Router makes routing decisions for AAA requests based on S-NSSAI.
type Router struct {
	cfg     SnssaiConfig
	logger  *slog.Logger
	metrics *Metrics

	mu sync.RWMutex
}

// Metrics holds Biz Pod AAA metrics backed by Prometheus collectors.
// Spec: REQ-14
type Metrics struct {
	requestsTotal *prometheus.CounterVec
	latencyHist   *prometheus.HistogramVec
}

// NewMetrics creates a new Metrics instance backed by the global metrics registry.
func NewMetrics() *Metrics {
	return &Metrics{
		requestsTotal: metrics.AaaRequestsTotal,
		latencyHist:   metrics.AaaRequestDuration,
	}
}

// RecordAAARequest records an AAA request metric.
func (m *Metrics) RecordAAARequest(protocol, host, result string) {
	m.requestsTotal.WithLabelValues(protocol, host, result).Inc()
}

// RecordAAALatency records AAA request latency.
func (m *Metrics) RecordAAALatency(protocol, host string, d time.Duration) {
	m.latencyHist.WithLabelValues(protocol, host).Observe(d.Seconds())
}

// RouterOption configures a Router.
type RouterOption func(*Router)

// WithMetrics sets the metrics collector.
func WithMetrics(m *Metrics) RouterOption {
	return func(r *Router) { r.metrics = m }
}

// NewRouter creates a new Biz Pod router.
func NewRouter(cfg SnssaiConfig, logger *slog.Logger, opts ...RouterOption) *Router {
	r := &Router{
		cfg:    cfg,
		logger: logger,
	}

	for _, opt := range opts {
		opt(r)
	}

	// Default to real metrics if not provided
	if r.metrics == nil {
		r.metrics = NewMetrics()
	}

	return r
}

// ResolveRoute determines the routing decision for an S-NSSAI.
// 3-level lookup: exact (sst+sd), sst-only (sst, sd=*), default (sst=*, sd=*).
func (r *Router) ResolveRoute(sst uint8, sd string) *RouteDecision {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var cfg *ServerConfig

	// 3-level lookup.
	if r.cfg.Exact != nil {
		cfg = r.cfg.Exact
	}
	if cfg == nil && r.cfg.SST != nil {
		cfg = r.cfg.SST
	}
	if cfg == nil {
		cfg = r.cfg.Default
	}

	if cfg == nil {
		return nil
	}

	mode := ProxyModeDirect

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &RouteDecision{
		Mode:     mode,
		Protocol: cfg.Protocol,
		Host:     cfg.Host,
		Port:     cfg.Port,
		Timeout:  timeout,
	}
}

// BuildForwardRequest creates an AaaForwardRequest for the AAA Gateway.
// Does NOT send to the network — the HTTP client in cmd/biz/ does that.
// Spec: PHASE §2.1
func (r *Router) BuildForwardRequest(
	authCtxID string,
	eapPayload []byte,
	sst uint8,
	sd string,
) (*proto.AaaForwardRequest, error) {
	decision := r.ResolveRoute(sst, sd)
	if decision == nil {
		return nil, fmt.Errorf("biz: no route configured for sst=%d sd=%s", sst, sd)
	}

	transportType := proto.TransportRADIUS
	if decision.Protocol == ProtocolDIAMETER {
		transportType = proto.TransportDIAMETER
	}

	return &proto.AaaForwardRequest{
		Version:       proto.CurrentVersion,
		SessionID:     fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID),
		AuthCtxID:     authCtxID,
		TransportType: transportType,
		Sst:           sst,
		Sd:            sd,
		Direction:     proto.DirectionClientInitiated,
		Payload:       eapPayload,
	}, nil
}

// Stats returns router statistics.
func (r *Router) Stats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RouterStats{
		HasRadius:   false, // Biz Pod does not have direct RADIUS client
		HasDiameter: false, // Biz Pod does not have direct Diameter client
	}
}

// RouterStats holds statistics for the Biz router.
type RouterStats struct {
	HasRadius   bool
	HasDiameter bool
}
