// Package factory provides a feature-flagged factory for RADIUS backend selection.
//
// The choice is driven by the RADIUS_BACKEND environment variable:
//
//	RADIUS_BACKEND=legacy  (default)
//	RADIUS_BACKEND=layeh
//
// NewClient creates a RADIUS client based on the configured backend.
// The returned client implements the radius.ClientInterface — callers do NOT need
// to type-assert. The Backend return value is for logging/debugging only.
//
// Example usage:
//
//	factoryCfg := factory.ClientConfig{
//	    ServerAddress: "127.0.0.1",
//	    ServerPort:    1812,
//	    SharedSecret:  "secret",
//	    Timeout:       5 * time.Second,
//	}
//	client, backend, err := factory.NewClient(factoryCfg)
//	if err != nil {
//	    return err
//	}
//	log.Info("RADIUS client", "backend", backend)
//	// Use client via radius.ClientInterface directly
//
// Spec: TS 29.561 §16 (RADIUS/Diameter interworking), RFC 2865, RFC 3579.
package factory

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/operator/nssAAF/internal/radius"
	"github.com/operator/nssAAF/internal/radius/adapter"
	"github.com/operator/nssAAF/internal/radius/layeh"
)

// Backend identifies which RADIUS implementation the factory will instantiate.
type Backend string

const (
	BackendLegacy Backend = "legacy"
	BackendLayeh  Backend = "layeh"

	envBackend       = "RADIUS_BACKEND"
	envBackendLayeh  = "layeh"
)

// ClientConfig holds the configuration shared by both RADIUS backends.
type ClientConfig struct {
	ServerAddress string
	ServerPort    int
	SharedSecret  string
	Timeout       time.Duration
	MaxRetries    int
	Logger        *slog.Logger // optional; nil is safe
}

// NewClient creates a RADIUS client based on the configured backend.
//
// Returns an object implementing radius.ClientInterface so callers can use it
// without knowing which backend is active. The Backend return value is for
// logging/debugging only.
func NewClient(cfg ClientConfig) (radius.ClientInterface, Backend, error) {
	switch getBackend() {
	case BackendLayeh:
		addr := fmt.Sprintf("%s:%d", cfg.ServerAddress, cfg.ServerPort)
		l, err := layeh.NewClient(layeh.Config{
			ServerAddr: addr,
			Secret:     []byte(cfg.SharedSecret),
			Timeout:    cfg.Timeout,
		})
		if err != nil {
			return nil, BackendLayeh, fmt.Errorf("factory: create layeh client: %w", err)
		}
		return adapter.NewClient(l), BackendLayeh, nil

	default:
		legacyCfg := radius.Config{
			ServerAddress: cfg.ServerAddress,
			ServerPort:    cfg.ServerPort,
			SharedSecret:  cfg.SharedSecret,
			Timeout:       cfg.Timeout,
			MaxRetries:    cfg.MaxRetries,
		}
		client, err := radius.NewRadiusClient(legacyCfg, cfg.Logger)
		if err != nil {
			return nil, BackendLegacy, fmt.Errorf("factory: create legacy client: %w", err)
		}
		return client, BackendLegacy, nil
	}
}

// CurrentBackend returns which backend is currently selected by RADIUS_BACKEND.
func CurrentBackend() Backend {
	return getBackend()
}

func getBackend() Backend {
	switch os.Getenv(envBackend) {
	case envBackendLayeh:
		return BackendLayeh
	default:
		return BackendLegacy
	}
}
