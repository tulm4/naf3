package httpclient

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
)

// Mode determines which resilience strategy to use.
type Mode string

const (
	ModeNative Mode = "native"
	ModeIstio  Mode = "istio"
)

// Factory creates HTTP clients based on mode.
type Factory struct {
	mode      Mode
	cfg       config.InternalCommConfig
	transport http.RoundTripper // optional override applied to native clients
}

// NewFactory creates a new HTTP client factory.
// Mode is determined by:
// 1. cfg.Mode ("native" or "istio")
// 2. ISTIO_MTLS=1 env var (overrides config)
func NewFactory(cfg config.InternalCommConfig) *Factory {
	mode := ModeNative
	if cfg.Mode == "istio" || os.Getenv("ISTIO_MTLS") == "1" {
		mode = ModeIstio
	}
	return &Factory{mode: mode, cfg: cfg}
}

// Mode returns the active communication mode.
func (f *Factory) Mode() Mode {
	return f.mode
}

// SetTransport injects an http.RoundTripper used by native clients created
// from this factory. Pass nil to clear the override.
//
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §5.4
// The HTTP Gateway uses this to wrap native clients with an OTel-instrumented
// transport so W3C traceparent headers propagate to Biz Pod.
func (f *Factory) SetTransport(t http.RoundTripper) {
	f.transport = t
}

// NewBizServiceClient creates a BizServiceClient for HTTP GW -> Biz Pod.
func (f *Factory) NewBizServiceClient(bizServiceURL string, redisAddr string) proto.BizServiceClient {
	switch f.mode {
	case ModeIstio:
		return newIstioBizClient(bizServiceURL)
	default:
		return NewBizRegistry(redisAddr, bizServiceURL, f.cfg.Native, f.transport)
	}
}

// NewAAAClient creates an AAA client for Biz Pod -> AAA GW.
func (f *Factory) NewAAAClient(aaaGatewayURL string, logger *slog.Logger) proto.BizAAAClient {
	switch f.mode {
	case ModeIstio:
		return newIstioAAAClient(aaaGatewayURL)
	default:
		return NewNativeAAAClient(aaaGatewayURL, f.cfg.Native, logger)
	}
}
