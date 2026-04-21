// Package proto defines the wire protocol between NSSAAF components.
package proto

// Version header name and current version string.
// Injected at build time via ldflags -X.
// Spec: PHASE §1.5
const (
	// HeaderName is the HTTP header used for proto schema version on all internal calls.
	HeaderName = "X-NSSAAF-Version"
	// CurrentVersion is the default proto schema version.
	// Overridden at build time: go build -ldflags '-X github.com/operator/nssAAF/internal/proto.CurrentVersion=${VERSION}'
	CurrentVersion = "1.0"
)
