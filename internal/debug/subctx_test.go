package debug

import (
	"context"
	"testing"
)

func TestSubscriberFrom_Empty(t *testing.T) {
	g, s := SubscriberFrom(context.Background())
	if g != "" || s != "" {
		t.Fatalf("expected empty, got (%q, %q)", g, s)
	}
}

func TestWithSubscriber_RoundTrip(t *testing.T) {
	ctx := WithSubscriber(context.Background(), "msisdn-208046123456789", "")
	g, s := SubscriberFrom(ctx)
	if g != "msisdn-208046123456789" || s != "" {
		t.Fatalf("got (%q, %q)", g, s)
	}
}

func TestWithSubscriber_Replace(t *testing.T) {
	// WithSubscriber replaces the entire (gpsi, supi) pair, matching Go's
	// normal context idiom.
	ctx := WithSubscriber(context.Background(), "msisdn-1", "")
	ctx = WithSubscriber(ctx, "", "imsi-208046000000001")
	g, s := SubscriberFrom(ctx)
	if g != "" || s != "imsi-208046000000001" {
		t.Fatalf("got (%q, %q)", g, s)
	}
}

func TestWithSubscriber_BothAtOnce(t *testing.T) {
	ctx := WithSubscriber(context.Background(), "msisdn-1", "imsi-1")
	g, s := SubscriberFrom(ctx)
	if g != "msisdn-1" || s != "imsi-1" {
		t.Fatalf("got (%q, %q)", g, s)
	}
}
