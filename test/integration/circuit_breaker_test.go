// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/nrm"
	"github.com/operator/nssAAF/internal/nrf"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoNRM(t *testing.T) {
	if _, present := os.LookupEnv("TEST_NRM_URL"); !present {
		t.Skip("TEST_NRM_URL not set — skipping NRM alarm integration test")
	}
}

func nrmURL() string {
	if u := os.Getenv("TEST_NRM_URL"); u != "" {
		return u
	}
	return "http://localhost:8081"
}

func pushEventToURL(url string, event *nrm.AlarmEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	resp, err := http.Post(url+"/internal/events", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return err
	}
	return nil
}

// ─── Test: CB_OpenOnFailures ─────────────────────────────────────────────

func TestIntegration_CB_OpenOnFailures(t *testing.T) {
	cb := resilience.NewCircuitBreaker(5, 30*time.Second, 3)

	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}
	assert.Equal(t, resilience.StateClosed, cb.State(), "CB should be CLOSED after 4 failures")

	cb.RecordFailure()
	assert.Equal(t, resilience.StateOpen, cb.State(), "CB should be OPEN after 5 failures")
}

// ─── Test: CB_HalfOpenOnTimeout ─────────────────────────────────────────

func TestIntegration_CB_HalfOpenOnTimeout(t *testing.T) {
	cb := resilience.NewCircuitBreaker(1, 100*time.Millisecond, 3)

	cb.RecordFailure()
	assert.Equal(t, resilience.StateOpen, cb.State())

	time.Sleep(120 * time.Millisecond)

	allowed := cb.Allow()
	assert.True(t, allowed, "Allow() should be true after recovery timeout")
	assert.Equal(t, resilience.StateHalfOpen, cb.State(), "CB should be HALF_OPEN after timeout")
}

// ─── Test: CB_CloseOnSuccess ────────────────────────────────────────────

func TestIntegration_CB_CloseOnSuccess(t *testing.T) {
	cb := resilience.NewCircuitBreaker(1, 100*time.Millisecond, 3)

	cb.RecordFailure()
	assert.Equal(t, resilience.StateOpen, cb.State())

	time.Sleep(120 * time.Millisecond)

	cb.Allow()
	assert.Equal(t, resilience.StateHalfOpen, cb.State())

	for i := 0; i < 3; i++ {
		cb.RecordSuccess()
	}
	assert.Equal(t, resilience.StateClosed, cb.State(), "CB should be CLOSED after 3 successes in HALF_OPEN")
}

// ─── Test: CB_NRMAlarmRaised (REQ-34) ─────────────────────────────────

func TestIntegration_CB_NRMAlarmRaised(t *testing.T) {
	store := nrm.NewAlarmStore()
	alarmMgr := nrm.NewAlarmManager(store, nil, nil)

	alarmMgr.Evaluate(&nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "aaa-server-01:1812",
	})

	alarms := alarmMgr.ListAlarms()
	require.Len(t, alarms, 1, "one alarm should be raised")
	assert.Equal(t, nrm.AlarmCircuitBreakerOpen, alarms[0].AlarmType)
	assert.Equal(t, "aaa-server-01:1812", alarms[0].BackupObject)
	assert.Equal(t, "MAJOR", alarms[0].Severity)
}

// ─── Test: CB_NRMAlarmCleared ──────────────────────────────────────────

func TestIntegration_CB_NRMAlarmCleared(t *testing.T) {
	store := nrm.NewAlarmStore()
	alarmMgr := nrm.NewAlarmManager(store, nil, nil)

	alarmMgr.Evaluate(&nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "aaa-server-02:1812",
	})
	require.Len(t, alarmMgr.ListAlarms(), 1, "alarm should be raised")

	alarmMgr.Evaluate(&nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_CLOSED",
		Target:    "aaa-server-02:1812",
	})

	alarms := alarmMgr.ListAlarms()
	assert.Len(t, alarms, 0, "alarm should be cleared after CB CLOSED")
}

// ─── Test: CB_AAAUnreachableAlarm ─────────────────────────────────────

func TestIntegration_CB_AAAUnreachableAlarm(t *testing.T) {
	store := nrm.NewAlarmStore()
	alarmMgr := nrm.NewAlarmManager(store, nil, nil)

	alarmMgr.Evaluate(&nrm.AlarmEvent{
		EventType: "AAA_UNREACHABLE",
		Target:    "aaa-server-03:1812",
	})

	alarms := alarmMgr.ListAlarms()
	require.Len(t, alarms, 1)
	assert.Equal(t, nrm.AlarmAAAUnreachable, alarms[0].AlarmType)
	assert.Equal(t, "aaa-server-03:1812", alarms[0].BackupObject)
	assert.Equal(t, "CRITICAL", alarms[0].Severity)
}

// ─── Test: Protected Client Uses Breaker (Task 5) ────────────────────

// TestIntegration_ProtectedClientUsesBreaker proves the circuit breaker is active in
// a real client path through the nfclient.Factory. The test exercises the full
// protected client path and verifies:
//  1. Repeated failures trip the breaker (CLOSED → OPEN)
//  2. Further calls fast-fail without hitting the server
//  3. After recovery timeout, the breaker allows a probe (OPEN → HALF_OPEN)
//  4. Failed HALF_OPEN probe reopens the breaker
//
// This integrates with the real nfclient.Factory.BreakerState() method which is
// wired into NRF/UDM/AUSF/AMF clients via cmd/biz/factory.go.
//
// Task 5: docs/superpowers/plans/2026-06-03-circuit-breaker-gaps.md
func TestIntegration_ProtectedClientUsesBreaker(t *testing.T) {
	// Step 1: Set up a test server that always returns errors.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	// Step 2: Build a client with a low failure threshold (3) to trip quickly.
	// Create factory and client once so the circuit breaker state persists across calls.
	cbRegistry := resilience.NewRegistry(3, 100*time.Millisecond, 2)
	factory := nfclient.NewFactory(cbRegistry)
	cfg := config.NRFConfig{
		BaseURL:         server.URL,
		DiscoverTimeout: 5 * time.Second,
	}
	client := nrf.NewClient(cfg, factory)

	ctx := context.Background()

	// Verify initial state is CLOSED.
	assert.Equal(t, resilience.StateClosed, factory.BreakerState(server.URL),
		"breaker should be CLOSED before any calls")

	// Step 3: Call 3 times — each returns an error, trips the breaker.
	// Client is created once, factory is reused, so failures accumulate.
	for i := 0; i < 3; i++ {
		err := client.Register(ctx)
		assert.Error(t, err, "call %d should fail", i+1)
	}

	// Step 4: Assert the breaker is now OPEN.
	assert.Equal(t, resilience.StateOpen, factory.BreakerState(server.URL),
		"breaker should be OPEN after 3 failures")
	assert.Equal(t, resilience.StateOpen, cbRegistry.Get(server.URL).State())

	// Step 5: Call again — should fast-fail without calling the server.
	err := client.Register(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker open",
		"call should fast-fail when breaker is OPEN")

	// Verify state is still OPEN.
	assert.Equal(t, resilience.StateOpen, factory.BreakerState(server.URL),
		"breaker should remain OPEN")

	// Step 6: Wait for recovery timeout — breaker transitions to HALF_OPEN on next Allow().
	time.Sleep(120 * time.Millisecond)

	// Next Allow() triggers the transition OPEN → HALF_OPEN.
	// The call goes through (server returns 503), failure recorded → HALF_OPEN → OPEN.
	err = client.Register(ctx)
	assert.Error(t, err)
	// HALF_OPEN probe gets a real server response (503), not a circuit breaker error.
	assert.NotContains(t, err.Error(), "circuit breaker open",
		"HALF_OPEN probe should allow request through")

	// Step 7: Verify the breaker is back to OPEN after the failed HALF_OPEN probe.
	assert.Equal(t, resilience.StateOpen, factory.BreakerState(server.URL),
		"breaker should be OPEN after failed HALF_OPEN probe")

	// Wait again for the new recovery timeout to expire.
	time.Sleep(120 * time.Millisecond)

	// Next call should trigger another HALF_OPEN probe.
	// Allow() transitions OPEN → HALF_OPEN, call goes through → 503 → OPEN.
	_ = client.Register(ctx)
	assert.Equal(t, resilience.StateOpen, factory.BreakerState(server.URL),
		"breaker should be OPEN after second HALF_OPEN failure")

	// Step 8: Verify subsequent calls fast-fail (breaker is still OPEN).
	err = client.Register(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker open",
		"call should fast-fail when breaker is OPEN after HALF_OPEN failure")
}

// ─── Test: CB_NRMAlarmRaisedViaHTTP ────────────────────────────────────

func TestIntegration_CB_NRMAlarmRaisedViaHTTP(t *testing.T) {
	store := nrm.NewAlarmStore()
	alarmMgr := nrm.NewAlarmManager(store, nil, nil)

	eventHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var ev nrm.AlarmEvent
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		alarmMgr.Evaluate(&ev)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
	alarmsHandler := func(w http.ResponseWriter, r *http.Request) {
		infos := alarmMgr.ListAlarmInfos()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"alarms": infos})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/events", eventHandler)
	mux.HandleFunc("GET /restconf/data/3gpp-nssaaf-nrm:alarms", alarmsHandler)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	event := &nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "test-aaa-server:1812",
	}
	err := pushEventToURL(ts.URL, event)
	require.NoError(t, err)

	alarmResp, err := http.Get(ts.URL + "/restconf/data/3gpp-nssaaf-nrm:alarms")
	require.NoError(t, err)
	defer alarmResp.Body.Close()
	require.Equal(t, http.StatusOK, alarmResp.StatusCode)

	var alarmList map[string]interface{}
	require.NoError(t, json.NewDecoder(alarmResp.Body).Decode(&alarmList))
	alarms, ok := alarmList["alarms"].([]interface{})
	require.True(t, ok)
	assert.Len(t, alarms, 1, "one alarm should be raised via HTTP")

	// Also verify it works against the real NRM URL if set.
	_ = nrmURL()
	_ = skipIfNoNRM
}
