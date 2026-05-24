package httpclient

import (
	"log/slog"
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
	mode Mode
	cfg  config.InternalCommConfig
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

// NewBizServiceClient creates a BizServiceClient for HTTP GW -> Biz Pod.
func (f *Factory) NewBizServiceClient(bizServiceURL string) proto.BizServiceClient {
	switch f.mode {
	case ModeIstio:
		return newIstioBizClient(bizServiceURL)
	default:
		return newNativeBizClient(bizServiceURL, f.cfg.Native)
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
