package debug

import "context"

type subscriberKey struct{}

type subscriber struct{ gpsi, supi string }

// WithSubscriber returns a new context carrying the GPSI/SUPI for the
// current request. Both may be empty for background jobs; helpers must
// tolerate that and fall through to the existing _no_sub stream.
func WithSubscriber(ctx context.Context, gpsi, supi string) context.Context {
	return context.WithValue(ctx, subscriberKey{}, subscriber{gpsi: gpsi, supi: supi})
}

// SubscriberFrom returns the GPSI/SUPI stored in ctx, if any.
func SubscriberFrom(ctx context.Context) (gpsi, supi string) {
	s, ok := ctx.Value(subscriberKey{}).(subscriber)
	if !ok {
		return "", ""
	}
	return s.gpsi, s.supi
}