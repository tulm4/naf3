//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/logging"
	"github.com/redis/go-redis/v9"
)

// TestE2E_DebugTrace_FullRoundTrip verifies that when debug is enabled,
// a full round-trip request produces debug events in Redis with the expected
// operations and structure.
func TestE2E_DebugTrace_FullRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E tests skipped in short mode")
	}

	h := NewHarnessForTest(t)
	defer h.Close()

	if !h.requireDebugEnabled() {
		t.Skip("debug.enabled is not true for this environment")
	}

	gpsi := "msisdn-208046000000099"
	resp := h.postNSSAA(t, gpsi, 1, "000001")
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	// Wait briefly for the stream to receive all events.
	time.Sleep(2 * time.Second)

	rdb := redis.NewClient(&redis.Options{Addr: h.RedisAddr()})
	defer rdb.Close()
	hash := logging.HashGPSI(gpsi)
	stream := "nssaa:debug:stream:" + hash
	msgs, err := rdb.XRange(context.Background(), stream, "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Fatal("no debug events recorded for the test GPSI")
	}

	seen := map[string]bool{}
	for _, m := range msgs {
		seen[asString(m.Values["op"])] = true
	}
	for _, want := range []string{"http.request", "pg.session.save", "aaa.radius.forward"} {
		if !seen[want] {
			t.Errorf("expected op %q in stream, got ops: %v", want, keys(seen))
		}
	}

	out := h.runCLITrace(t, gpsi)
	if !strings.Contains(out, "http.request") {
		t.Errorf("CLI output missing http.request: %s", out)
	}
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
