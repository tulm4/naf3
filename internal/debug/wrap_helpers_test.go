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
	t.Cleanup(func() { emitCapture = oldEmit })

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

// TestWrapDB_SUPIFromContext verifies that SUPI is also propagated via
// context (covers the AIW/N60 path, separate from GPSI on the N58 path).
// Also verifies the asymmetric behavior: only the populated context field
// is set on Event.
func TestWrapDB_SUPIFromContext(t *testing.T) {
	ctx := WithSubscriber(context.Background(), "", "imsi-208046000000001")

	evCh := make(chan Event, 1)
	oldEmit := emitCapture
	emitCapture = func(_ *Debug, _ context.Context, ev Event) { evCh <- ev }
	t.Cleanup(func() { emitCapture = oldEmit })

	d := &Debug{}
	d.Set(true)

	d.emitTiming(ctx, "pg.session.update", KindDB, "nssaa_session", "", time.Unix(0, 0), nil)

	got := <-evCh
	if got.SUPI != "imsi-208046000000001" {
		t.Fatalf("expected SUPI from ctx, got %q", got.SUPI)
	}
	if got.GPSI != "" {
		t.Fatalf("GPSI should be empty when only SUPI is set, got %q", got.GPSI)
	}
}
