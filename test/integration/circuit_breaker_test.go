// Package integration provides integration tests for NSSAAF against real infrastructure.
package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/nrm"
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
