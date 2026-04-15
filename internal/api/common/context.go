// Package common provides request-scoped context helpers.
package common

import "context"

// requestIDKey is the context key for the X-Request-ID.
type requestIDKey struct{}

// WithRequestID stores a request ID in the context.
func WithRequestID(ctx context.Context, reqID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, reqID)
}

// GetRequestID retrieves the X-Request-ID from the context.
// Returns empty string if not set.
func GetRequestID(ctx context.Context) string {
	if v := ctx.Value(requestIDKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
