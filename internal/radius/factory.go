// Package radius — feature-flagged factory for backend selection.
//
// The RADIUS module supports two client implementations:
//   - legacy  — the hand-rolled RADIUS client in this package
//               (RFC 2865 / RFC 3579 byte-level packet handling).
//   - layeh   — a RADIUS client built on top of layeh.com/radius
//               (used by the EAP-TLS/EAP-AKA' wire path).
//
// The choice is driven by the RADIUS_BACKEND environment variable:
//
//	RADIUS_BACKEND=legacy  (default)
//	RADIUS_BACKEND=layeh
//
// The factory returns the concrete client (legacy *Client or layeh *layeh.Client)
// as an interface{}. Callers type-assert to the backend they expect:
//
//	client, backend, err := radius.NewClient(radius.ClientConfig{...})
//	switch backend {
//	case radius.BackendLegacy:
//	    legacyClient := client.(*radius.Client)
//	case radius.BackendLayeh:
//	    layehClient := client.(*layeh.Client)
//	}
//
// The factory intentionally does NOT normalize the two backends' request /
// response types. The legacy and layeh APIs differ in their type signatures
// (legacy uses []Attribute / []byte, layeh uses typed AccessRequest structs),
// so unifying them would require adapter shims that obscure the actual wire
// behavior. Callers that need to support both backends branch on the returned
// Backend value.
//
// Spec: TS 29.561 §16 (RADIUS/Diameter interworking), RFC 2865, RFC 3579.
package radius

import (
	"fmt"
	"os"
	"time"

	"github.com/operator/nssAAF/internal/radius/layeh"
)

// Backend identifies which RADIUS implementation the factory will instantiate.
type Backend string

const (
	// BackendLegacy selects the hand-rolled RADIUS client in this package.
	BackendLegacy Backend = "legacy"

	// BackendLayeh selects the layeh.com/radius-backed client.
	BackendLayeh Backend = "layeh"

	// envBackend is the name of the environment variable read by getBackend.
	envBackend = "RADIUS_BACKEND"

	// envBackendLayeh is the value that selects the layeh backend.
	envBackendLayeh = "layeh"
)

// ClientConfig holds the configuration shared by both RADIUS backends.
//
// Fields that are not meaningful for a given backend are ignored.
type ClientConfig struct {
	// ServerAddress is the RADIUS server hostname or IP (no port).
	ServerAddress string

	// ServerPort is the RADIUS server UDP port (typically 1812).
	ServerPort int

	// SharedSecret is the RADIUS shared secret.
	SharedSecret string

	// Timeout is the per-request timeout.
	Timeout time.Duration

	// MaxRetries is the number of retries on transient failure (legacy only).
	MaxRetries int
}

// NewClient creates a RADIUS client based on the configured backend.
//
// The first return value is the concrete client. Callers MUST type-assert it
// to the type corresponding to the returned Backend:
//   - BackendLegacy → *Client
//   - BackendLayeh  → *layeh.Client
//
// If neither backend matches the current configuration the function returns
// (nil, BackendLegacy, nil) — the legacy backend is the safe default.
func NewClient(cfg ClientConfig) (interface{}, Backend, error) {
	switch getBackend() {
	case BackendLayeh:
		addr := fmt.Sprintf("%s:%d", cfg.ServerAddress, cfg.ServerPort)
		client, err := layeh.NewClient(layeh.Config{
			ServerAddr: addr,
			Secret:     []byte(cfg.SharedSecret),
			Timeout:    cfg.Timeout,
		})
		if err != nil {
			return nil, BackendLayeh, fmt.Errorf("radius factory: create layeh client: %w", err)
		}
		return client, BackendLayeh, nil

	default:
		legacyCfg := Config{
			ServerAddress: cfg.ServerAddress,
			ServerPort:    cfg.ServerPort,
			SharedSecret:  cfg.SharedSecret,
			Timeout:       cfg.Timeout,
			MaxRetries:    cfg.MaxRetries,
		}
		client, err := NewRadiusClient(legacyCfg, nil)
		if err != nil {
			return nil, BackendLegacy, fmt.Errorf("radius factory: create legacy client: %w", err)
		}
		return client, BackendLegacy, nil
	}
}

// CurrentBackend returns which backend is currently selected by RADIUS_BACKEND.
func CurrentBackend() Backend {
	return getBackend()
}

// getBackend reads RADIUS_BACKEND and maps it to a Backend constant.
// Unrecognized values fall back to BackendLegacy.
func getBackend() Backend {
	switch os.Getenv(envBackend) {
	case envBackendLayeh:
		return BackendLayeh
	default:
		return BackendLegacy
	}
}