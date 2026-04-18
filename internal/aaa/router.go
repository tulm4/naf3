// Package aaa provides AAA proxy (AAA-P) functionality for routing between
// NSSAAF and NSS-AAA servers over RADIUS or Diameter.
// Spec: TS 29.561 §16-17
package aaa

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/diameter"
	"github.com/operator/nssAAF/internal/radius"
)

// ProxyMode represents the AAA routing mode.
type ProxyMode int

const (
	ProxyModeDirect ProxyMode = iota
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
	ProtocolRADIUS Protocol = iota
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

	// Protocol clients
	radiusClient   *radius.Client
	diameterClient *diameter.Client

	mu sync.RWMutex
}

// RouterOption configures a Router.
type RouterOption func(*Router)

// WithRadiusClient sets the RADIUS client.
func WithRadiusClient(c *radius.Client) RouterOption {
	return func(r *Router) { r.radiusClient = c }
}

// WithDiameterClient sets the Diameter client.
func WithDiameterClient(c *diameter.Client) RouterOption {
	return func(r *Router) { r.diameterClient = c }
}

// WithMetrics sets the metrics collector.
func WithMetrics(m *Metrics) RouterOption {
	return func(r *Router) { r.metrics = m }
}

// NewRouter creates a new AAA proxy router.
func NewRouter(cfg SnssaiConfig, logger *slog.Logger, opts ...RouterOption) *Router {
	r := &Router{
		cfg:    cfg,
		logger: logger,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// ResolveRoute determines the routing decision for an S-NSSAI.
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
	if cfg.Protocol == ProtocolDIAMETER && r.diameterClient != nil {
		mode = ProxyModeDirect
	} else if cfg.Protocol == ProtocolRADIUS && r.radiusClient != nil {
		mode = ProxyModeDirect
	}

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

// SendEAP forwards an EAP message to AAA-S and returns the response.
// Implements eap.AAAClient.
func (r *Router) SendEAP(ctx context.Context, authCtxID string, eapPayload []byte) ([]byte, error) {
	// Extract S-NSSAI from authCtxID if possible.
	// For now, use default routing.
	decision := r.ResolveRoute(0, "")
	if decision == nil {
		return nil, fmt.Errorf("aaa: no route configured")
	}

	r.logger.Debug("aaa_route",
		"auth_ctx_id", authCtxID,
		"protocol", decision.Protocol,
		"host", decision.Host,
		"mode", decision.Mode,
	)

	start := time.Now()

	var response []byte
	var err error

	switch decision.Protocol {
	case ProtocolRADIUS:
		response, err = r.sendRADIUS(ctx, authCtxID, eapPayload, decision)
	case ProtocolDIAMETER:
		response, err = r.sendDIAMETER(ctx, authCtxID, eapPayload, decision)
	default:
		return nil, fmt.Errorf("aaa: unsupported protocol: %s", decision.Protocol)
	}

	duration := time.Since(start)

	if err != nil {
		if r.metrics != nil {
			r.metrics.RecordAAARequest(decision.Protocol.String(), decision.Host, "failure")
			r.metrics.RecordAAALatency(decision.Protocol.String(), decision.Host, duration)
		}
		return nil, fmt.Errorf("aaa: %s error: %w", decision.Protocol, err)
	}

	if r.metrics != nil {
		r.metrics.RecordAAARequest(decision.Protocol.String(), decision.Host, "success")
		r.metrics.RecordAAALatency(decision.Protocol.String(), decision.Host, duration)
	}

	return response, nil
}

// sendRADIUS forwards an EAP message over RADIUS.
func (r *Router) sendRADIUS(ctx context.Context, authCtxID string, eapPayload []byte, decision *RouteDecision) ([]byte, error) {
	if r.radiusClient == nil {
		return nil, fmt.Errorf("aaa: RADIUS client not configured")
	}

	// Extract GPSI from context if available.
	// For now, use authCtxID as the identity.
	gpsi := authCtxID

	return r.radiusClient.SendEAP(ctx, gpsi, eapPayload, 0, "")
}

// sendDIAMETER forwards an EAP message over Diameter.
func (r *Router) sendDIAMETER(ctx context.Context, authCtxID string, eapPayload []byte, decision *RouteDecision) ([]byte, error) {
	if r.diameterClient == nil {
		return nil, fmt.Errorf("aaa: Diameter client not configured")
	}

	// Build session ID.
	sessionID := fmt.Sprintf("nssAAF;%d;%s", time.Now().UnixNano(), authCtxID)

	return r.diameterClient.SendDER(ctx, sessionID, authCtxID, eapPayload, 0, "")
}

// SetRadiusClient sets the RADIUS client.
func (r *Router) SetRadiusClient(c *radius.Client) {
	r.mu.Lock()
	r.radiusClient = c
	r.mu.Unlock()
}

// SetDiameterClient sets the Diameter client.
func (r *Router) SetDiameterClient(c *diameter.Client) {
	r.mu.Lock()
	r.diameterClient = c
	r.mu.Unlock()
}

// Stats returns router statistics.
func (r *Router) Stats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RouterStats{
		HasRadius:   r.radiusClient != nil,
		HasDiameter: r.diameterClient != nil,
	}
}

// RouterStats holds statistics for the AAA router.
type RouterStats struct {
	HasRadius   bool
	HasDiameter bool
}
