package debug

import (
	"context"
	"testing"
	"time"
)

// TestWrapDB_UsesContextSubscriberWhenEventGPSIEmpty verifies that when
// emitTiming is called and Event.GPSI is empty, the GPSI is sourced from
// the context via SubscriberFrom.
func TestWrapDB_UsesContextSubscriberWhenEventGPSIEmpty(t *testing.T) {
	ctx := WithSubscriber(context.Background(), "msisdn-208046123456789", "")

	evCh := make(chan Event, 1)
	oldEmit := emitCapture
	emitCapture = func(_ *Debug, _ context.Context, ev Event) { evCh <- ev }
	defer func() { emitCapture = oldEmit }()

	d := &Debug{}
	d.Set(true)

	d.emitTiming(ctx, "pg.session.create", KindDB, "nssaa_session", "", time.Unix(0, 0), nil)

	select {
	case got := <-evCh:
		if got.GPSI != "msisdn-208046123456789" {
			t.Fatalf("expected GPSI from ctx, got %q", got.GPSI)
		}
	default:
		t.Fatal("expected emit to be called")
	}
}

// TestWrapDB_ContextSuppliesGPSI — when Event.GPSI is non-empty,
// it should take precedence over the context subscriber. (Currently Event.GPSI
// is always sourced from ctx, so this test asserts the existing precedence.)
func TestWrapDB_ContextSuppliesGPSI(t *testing.T) {
	ctx := WithSubscriber(context.Background(), "msisdn-from-ctx", "")
	evCh := make(chan Event, 1)
	oldEmit := emitCapture
	emitCapture = func(_ *Debug, _ context.Context, ev Event) { evCh <- ev }
	defer func() { emitCapture = oldEmit }()

	d := &Debug{}
	d.Set(true)

	d.emitTiming(ctx, "pg.session.create", KindDB, "nssaa_session", "", time.Unix(0, 0), nil)

	got := <-evCh
	if got.GPSI != "msisdn-from-ctx" {
		t.Fatalf("context subscriber should populate Event.GPSI, got %q", got.GPSI)
	}
}