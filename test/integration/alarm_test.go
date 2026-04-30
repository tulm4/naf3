// Package integration provide integration tests for NSSAAF against real infrastructure.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/nrm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoNRMBinary(t *testing.T) {
	// Try building to verify the NRM binary compiles.
	// The test will build its own binary in temp dir regardless.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tmpCheck := filepath.Join(os.TempDir(), "nrm_skip_check")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", tmpCheck, "./cmd/nrm/")
	cmd.Dir = os.Getenv("NAF3_ROOT")
	if cmd.Dir == "" {
		cmd.Dir = "/home/tulm/naf3"
	}
	if err := cmd.Run(); err != nil {
		t.Skip("NRM binary cannot be built — go build failed:", err)
	}
	_ = os.Remove(tmpCheck)
}

func buildNRMBinary(t *testing.T) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tmpBin := filepath.Join(t.TempDir(), "nrm_test")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", tmpBin, "./cmd/nrm/")
	cmd.Dir = os.Getenv("NAF3_ROOT")
	if cmd.Dir == "" {
		cmd.Dir = "/home/tulm/naf3"
	}
	if err := cmd.Run(); err != nil {
		t.Skip("failed to build NRM binary:", err)
	}
	return tmpBin
}

func startNRMServer(t *testing.T, binaryPath string) (*exec.Cmd, string) {
	// Resolve config path relative to repo root (where tests run from).
	naf3Root := os.Getenv("NAF3_ROOT")
	if naf3Root == "" {
		naf3Root = "/home/tulm/naf3"
	}
	configPath := filepath.Join(naf3Root, "compose/configs/nrm.yaml")

	cmd := exec.Command(binaryPath, "--config="+configPath)
	cmd.Env = append(os.Environ(), "NRM_LISTEN_ADDR=127.0.0.1:8081")
	if err := cmd.Start(); err != nil {
		t.Skip("failed to start NRM binary:", err)
	}
	time.Sleep(500 * time.Millisecond)
	return cmd, "http://127.0.0.1:8081"
}

func stopNRMServer(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func pushEventToNRM(url string, event *nrm.AlarmEvent) (*http.Response, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(url+"/internal/events", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func getAlarms(url string) ([]*nrm.AlarmInfo, error) {
	resp, err := http.Get(url + "/restconf/data/3gpp-nssaaf-nrm:alarms")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// YANG JSON wraps alarms as {"3gpp-nssaaf-nrm:alarms":{"alarm":[...]}}.
	var wrapped struct {
		Alarms struct {
			Alarms []*nrm.AlarmInfo `json:"alarm"`
		} `json:"3gpp-nssaaf-nrm:alarms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return nil, err
	}
	return wrapped.Alarms.Alarms, nil
}

// TestIntegration_Alarm_RaiseViaRESTCONF verifies POST /internal/events raises an alarm
// that is visible via GET /restconf/data/3gpp-nssaaf-nrm:alarms.
func TestIntegration_Alarm_RaiseViaRESTCONF(t *testing.T) {
	skipIfNoNRMBinary(t)
	binaryPath := buildNRMBinary(t)
	cmd, url := startNRMServer(t, binaryPath)
	defer stopNRMServer(cmd)

	resp, err := pushEventToNRM(url, &nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "test-aaa-01:1812",
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	alarms, err := getAlarms(url)
	require.NoError(t, err)
	require.Len(t, alarms, 1)
	assert.Equal(t, nrm.AlarmCircuitBreakerOpen, alarms[0].AlarmType)
}

// TestIntegration_Alarm_ClearViaRESTCONF verifies that CIRCUIT_BREAKER_CLOSED clears
// the corresponding alarm.
func TestIntegration_Alarm_ClearViaRESTCONF(t *testing.T) {
	skipIfNoNRMBinary(t)
	binaryPath := buildNRMBinary(t)
	cmd, url := startNRMServer(t, binaryPath)
	defer stopNRMServer(cmd)

	_, err := pushEventToNRM(url, &nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "test-aaa-02:1812",
	})
	require.NoError(t, err)

	_, err = pushEventToNRM(url, &nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_CLOSED",
		Target:    "test-aaa-02:1812",
	})
	require.NoError(t, err)

	alarms, err := getAlarms(url)
	require.NoError(t, err)
	assert.Len(t, alarms, 0, "alarms should be cleared after CB CLOSED")
}

// TestIntegration_Alarm_Acknowledge verifies POST ack returns 204 and marks alarm acked.
func TestIntegration_Alarm_Acknowledge(t *testing.T) {
	skipIfNoNRMBinary(t)
	binaryPath := buildNRMBinary(t)
	cmd, url := startNRMServer(t, binaryPath)
	defer stopNRMServer(cmd)

	resp, err := pushEventToNRM(url, &nrm.AlarmEvent{
		EventType: "AAA_UNREACHABLE",
		Target:    "test-aaa-03:1812",
	})
	require.NoError(t, err)
	resp.Body.Close()

	alarms, err := getAlarms(url)
	require.NoError(t, err)
	require.Len(t, alarms, 1)
	alarmID := alarms[0].AlarmID

	ackURL := fmt.Sprintf("%s/restconf/data/3gpp-nssaaf-nrm:alarms=%s/ack", url, alarmID)
	ackBody := []byte(`{"acked-by":"operator1"}`)
	ackResp, err := http.Post(ackURL, "application/json", bytes.NewReader(ackBody))
	require.NoError(t, err)
	defer ackResp.Body.Close()
	require.Equal(t, http.StatusNoContent, ackResp.StatusCode, "ack should return 204")

	alarms, err = getAlarms(url)
	require.NoError(t, err)
	require.Len(t, alarms, 1)
	assert.True(t, alarms[0].Acked, "alarm should be acknowledged")
	assert.Equal(t, "operator1", alarms[0].AckedBy)
}

// TestIntegration_Alarm_Deduplication verifies duplicate alarms within 5-min window
// are deduplicated.
func TestIntegration_Alarm_Deduplication(t *testing.T) {
	skipIfNoNRMBinary(t)
	binaryPath := buildNRMBinary(t)
	cmd, url := startNRMServer(t, binaryPath)
	defer stopNRMServer(cmd)

	for i := 0; i < 2; i++ {
		resp, err := pushEventToNRM(url, &nrm.AlarmEvent{
			EventType: "CIRCUIT_BREAKER_OPEN",
			Target:    "test-aaa-04:1812",
		})
		require.NoError(t, err)
		resp.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}

	alarms, err := getAlarms(url)
	require.NoError(t, err)
	require.Len(t, alarms, 1, "duplicate alarm should be deduplicated within 5-min window")
}

// TestIntegration_Alarm_NssaaFunction verifies GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function
// returns function data.
func TestIntegration_Alarm_NssaaFunction(t *testing.T) {
	skipIfNoNRMBinary(t)
	binaryPath := buildNRMBinary(t)
	cmd, url := startNRMServer(t, binaryPath)
	defer stopNRMServer(cmd)

	resp, err := http.Get(url + "/restconf/data/3gpp-nssaaf-nrm:nssaa-function")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result, "nssaa-function data should be returned")
	// YANG JSON wraps the response with the module prefix.
	assert.Contains(t, result, "3gpp-nssaaf-nrm:nssaa-function",
		"response should use YANG JSON module-prefixed key")
}

// TestIntegration_Alarm_FailureRateAlarm verifies REQ-33: >10% auth failure rate
// raises NSSAA_HIGH_AUTH_FAILURE_RATE alarm.
func TestIntegration_Alarm_FailureRateAlarm(t *testing.T) {
	skipIfNoNRMBinary(t)
	binaryPath := buildNRMBinary(t)
	cmd, url := startNRMServer(t, binaryPath)
	defer stopNRMServer(cmd)

	// 5 failures + 5 successes = 50% failure rate (exceeds 10% threshold).
	for i := 0; i < 5; i++ {
		resp, err := pushEventToNRM(url, &nrm.AlarmEvent{EventType: "AUTH_FAILURE"})
		require.NoError(t, err)
		resp.Body.Close()
	}
	for i := 0; i < 5; i++ {
		resp, err := pushEventToNRM(url, &nrm.AlarmEvent{EventType: "AUTH_SUCCESS"})
		require.NoError(t, err)
		resp.Body.Close()
	}

	alarms, err := getAlarms(url)
	require.NoError(t, err)
	require.Len(t, alarms, 1, "high auth failure rate alarm should be raised")
	assert.Equal(t, nrm.AlarmHighAuthFailureRate, alarms[0].AlarmType)
	assert.Equal(t, "global", alarms[0].BackupObject)
	assert.Equal(t, "MAJOR", alarms[0].Severity)
}

// TestIntegration_Alarm_CircuitBreakerAlarm verifies REQ-34: CIRCUIT_BREAKER_OPEN
// raises NSSAA_CIRCUIT_BREAKER_OPEN alarm.
func TestIntegration_Alarm_CircuitBreakerAlarm(t *testing.T) {
	skipIfNoNRMBinary(t)
	binaryPath := buildNRMBinary(t)
	cmd, url := startNRMServer(t, binaryPath)
	defer stopNRMServer(cmd)

	resp, err := pushEventToNRM(url, &nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "test-aaa-05:1812",
	})
	require.NoError(t, err)
	resp.Body.Close()

	alarms, err := getAlarms(url)
	require.NoError(t, err)
	require.Len(t, alarms, 1, "CB alarm should be raised")
	assert.Equal(t, nrm.AlarmCircuitBreakerOpen, alarms[0].AlarmType)
	assert.Equal(t, "test-aaa-05:1812", alarms[0].BackupObject)
	assert.Equal(t, "MAJOR", alarms[0].Severity)
}

// TestIntegration_Alarm_MinimalServer tests the alarm flow end-to-end using an httptest
// server without requiring a real binary.
func TestIntegration_Alarm_MinimalServer(t *testing.T) {
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
		w.Header().Set("Content-Type", "application/yang-data+json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"3gpp-nssaaf-nrm:alarms": map[string]interface{}{
				"alarm": infos,
			},
		})
	}
	nssaaFnHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"nssaa-function": []interface{}{},
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/events", eventHandler)
	mux.HandleFunc("GET /restconf/data/3gpp-nssaaf-nrm:alarms", alarmsHandler)
	mux.HandleFunc("GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function", nssaaFnHandler)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := pushEventToNRM(ts.URL, &nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "httptest-server:1812",
	})
	require.NoError(t, err)
	resp.Body.Close()

	alarms, err := getAlarms(ts.URL)
	require.NoError(t, err)
	require.Len(t, alarms, 1)
	assert.Equal(t, nrm.AlarmCircuitBreakerOpen, alarms[0].AlarmType)

	_, err = pushEventToNRM(ts.URL, &nrm.AlarmEvent{
		EventType: "CIRCUIT_BREAKER_CLOSED",
		Target:    "httptest-server:1812",
	})
	require.NoError(t, err)

	alarms, err = getAlarms(ts.URL)
	require.NoError(t, err)
	assert.Len(t, alarms, 0)
}
