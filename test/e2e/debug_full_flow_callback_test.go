//go:build e2e
// +build e2e

// Server-initiated (AMF callback) integration test for per-UE debug tracing.
//
// Exercises flow direction (c):
//
//	AAA-S → aaa-gw (RAR/ASR over RADIUS) → biz → AMF callback
//
// GPSI-keyed stream limitation: as documented in plan Task 1 §1.8, the
// AAA-GW server-initiated ingress has no GPSI in the protocol payload or
// DTOs, so reverse-direction events land in `nssaa:debug:stream:_no_sub`
// instead of the per-GPSI stream. This test scans both streams for a new
// trace_id rooted at the RAR arrival.
//
// Spec: TS 29.526 §7.2.4-5 (Server-Initiated Callbacks)
package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/logging"
)

const (
	callbackGPSI = "msisdn-208046999999001"
	callbackAuth = "auth-cb-1"
)

func TestDebugFullFlow_AMFCallback(t *testing.T) {
	if os.Getenv("RUN_E2E") != "1" {
		t.Skip("set RUN_E2E=1 to run")
	}
	require.NoError(t, ComposeRunning("aaa-sim"))
	h := NewHarnessForTest(t)

	gpsiHash := logging.HashGPSI(callbackGPSI)
	gpsiStream := "nssaa:debug:stream:" + gpsiHash
	noSubStream := "nssaa:debug:stream:_no_sub"

	rdb := redis.NewClient(&redis.Options{Addr: h.RedisAddr()})
	defer func() { _ = rdb.Close() }()
	require.NoError(t, rdb.Ping(context.Background()).Err())
	require.NoError(t, rdb.Del(context.Background(), gpsiStream).Err())
	require.NoError(t, rdb.Del(context.Background(), noSubStream).Err())

	// Seed a forward session before triggering the reverse flow.
	body := fmt.Sprintf(`{"gpsi":"%s","snssai":{"sst":1,"sd":"000001"},"eapIdRsp":"dGVzdA=="}`, callbackGPSI)
	postNSSAAAuth(t, body)

	time.Sleep(500 * time.Millisecond)

	preTraceIDs := snapshotTraceIDs(t, rdb, gpsiStream, noSubStream)

	driver := NewAaaSimDriver("aaa-sim")
	driver.TriggerRAR(t, callbackAuth, "172.0.3.15:1812")

	required := []string{
		"aaa-gw:aaa.radius.recv",
		"aaa-gw:http.request.out",
		"biz:http.request",
		"biz:pg.session.update",
		"biz:http.request.out",
		"biz:http.request.exit",
	}

	deadline := time.Now().Add(5 * time.Second)
	backoff := 100 * time.Millisecond
	var newTraceID string
	var newEvents []redis.XMessage
	for time.Now().Before(deadline) {
		all := readAllCallbackEvents(t, rdb, gpsiStream, noSubStream)
		if newTraceID == "" {
			for _, event := range all {
				traceID, _ := event.Values["trace"].(string)
				svc, _ := event.Values["svc"].(string)
				op, _ := event.Values["op"].(string)
				if traceID != "" && !preTraceIDs[traceID] && svc+":"+op == required[0] {
					newTraceID = traceID
					break
				}
			}
		}

		newEvents = newEvents[:0]
		for _, event := range all {
			traceID, _ := event.Values["trace"].(string)
			if traceID == newTraceID {
				newEvents = append(newEvents, event)
			}
		}
		if newTraceID != "" && hasRequiredDebugEvents(newEvents, required) {
			break
		}

		time.Sleep(backoff)
		backoff *= 2
		if backoff > 800*time.Millisecond {
			backoff = 800 * time.Millisecond
		}
	}
	require.NotEmpty(t, newTraceID, "no new trace_id rooted at RAR arrival appeared within 5s")

	present := map[string]bool{}
	for _, event := range newEvents {
		svc, _ := event.Values["svc"].(string)
		op, _ := event.Values["op"].(string)
		present[svc+":"+op] = true
	}
	for _, want := range required {
		require.True(t, present[want], "missing event %s (have: %v)", want, present)
	}

	traceIDs := traceIDsOfFirst(newEvents)
	require.Equal(t, map[string]bool{newTraceID: true}, traceIDs,
		"all RAR events must share the new trace_id")
	require.False(t, preTraceIDs[newTraceID], "RAR trace_id must be distinct from the forward trace")
}

// snapshotTraceIDs returns the trace IDs present before the RAR is triggered.
func snapshotTraceIDs(t *testing.T, rdb *redis.Client, streams ...string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, stream := range streams {
		events, err := rdb.XRange(context.Background(), stream, "-", "+").Result()
		require.NoError(t, err)
		for _, event := range events {
			if traceID, ok := event.Values["trace"].(string); ok && traceID != "" {
				out[traceID] = true
			}
		}
	}
	return out
}

// readAllCallbackEvents reads all callback candidates from the supplied streams.
func readAllCallbackEvents(t *testing.T, rdb *redis.Client, streams ...string) []redis.XMessage {
	t.Helper()
	var out []redis.XMessage
	for _, stream := range streams {
		events, err := rdb.XRange(context.Background(), stream, "-", "+").Result()
		require.NoError(t, err)
		out = append(out, events...)
	}
	return out
}

func traceIDsOfFirst(events []redis.XMessage) map[string]bool {
	out := map[string]bool{}
	for _, event := range events {
		if traceID, ok := event.Values["trace"].(string); ok && traceID != "" {
			out[traceID] = true
		}
	}
	return out
}
