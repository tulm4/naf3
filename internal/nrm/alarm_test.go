// Package nrm provides NRM alarm management unit tests.
// Spec: ITU-T X.733, 3GPP TS 28.541 §5.3
package nrm

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// TestAlarmStore_SaveAndList verifies that saving an alarm makes it retrievable via List.
func TestAlarmStore_SaveAndList(t *testing.T) {
	store := NewAlarmStore()
	alarm := &Alarm{
		AlarmID:   "alarm-001",
		AlarmType: AlarmAAAUnreachable,
		Severity:  SeverityCritical,
		EventTime: time.Now(),
	}

	id, err := store.Save(alarm)
	require.NoError(t, err)
	assert.Equal(t, "alarm-001", id)

	alarms := store.List()
	require.Len(t, alarms, 1)
	assert.Equal(t, "alarm-001", alarms[0].AlarmID)
}

// TestAlarmStore_Deduplication verifies that a second alarm with the same
// (AlarmType, BackupObject) key within the 5-minute window is deduplicated
// (returns the existing alarm ID without creating a new one).
func TestAlarmStore_Deduplication(t *testing.T) {
	store := NewAlarmStore()

	alarm1 := &Alarm{
		AlarmID:       "alarm-first",
		AlarmType:     AlarmCircuitBreakerOpen,
		BackupObject:  "aaa-server-1",
		Severity:      SeverityMajor,
		EventTime:     time.Now(),
	}

	id1, err := store.Save(alarm1)
	require.NoError(t, err)
	assert.Equal(t, "alarm-first", id1)
	assert.Equal(t, 1, store.Count())

	// Second alarm with same (type, object) — must be deduplicated
	alarm2 := &Alarm{
		AlarmID:       "alarm-duplicate", // should be ignored
		AlarmType:     AlarmCircuitBreakerOpen,
		BackupObject:  "aaa-server-1",
		Severity:      SeverityMajor,
		EventTime:     time.Now(),
	}

	id2, err := store.Save(alarm2)
	require.NoError(t, err)
	assert.Equal(t, "alarm-first", id2, "deduplication must return existing alarm ID")
	assert.Equal(t, 1, store.Count(), "no new alarm should be created")
}

// TestAlarmStore_DifferentKeys verifies that alarms with different (AlarmType, BackupObject)
// keys are both stored independently.
func TestAlarmStore_DifferentKeys(t *testing.T) {
	store := NewAlarmStore()

	alarm1 := &Alarm{
		AlarmType:    AlarmAAAUnreachable,
		BackupObject: "server-a",
		Severity:     SeverityCritical,
	}
	alarm2 := &Alarm{
		AlarmType:    AlarmCircuitBreakerOpen,
		BackupObject: "server-a",
	}
	alarm3 := &Alarm{
		AlarmType:    AlarmAAAUnreachable,
		BackupObject: "server-b", // same type, different object
	}

	id1, err := store.Save(alarm1)
	require.NoError(t, err)
	id2, err := store.Save(alarm2)
	require.NoError(t, err)
	id3, err := store.Save(alarm3)
	require.NoError(t, err)

	assert.NotEqual(t, id1, id2)
	assert.NotEqual(t, id1, id3)
	assert.NotEqual(t, id2, id3)
	assert.Equal(t, 3, store.Count())
}

// TestAlarmStore_Clear verifies that Clear removes an alarm by ID and returns true.
func TestAlarmStore_Clear(t *testing.T) {
	store := NewAlarmStore()
	alarm := &Alarm{
		AlarmType:    AlarmHighAuthFailureRate,
		BackupObject: "global",
		Severity:    SeverityMajor,
	}

	id, err := store.Save(alarm)
	require.NoError(t, err)
	require.Equal(t, 1, store.Count())

	cleared := store.Clear(id)
	assert.True(t, cleared, "Clear must return true when alarm exists")
	assert.Equal(t, 0, store.Count())

	// Clear non-existent ID
	assert.False(t, store.Clear("nonexistent"), "Clear must return false for non-existent ID")
}

// TestAlarmStore_Get verifies that Get returns the stored alarm by ID.
func TestAlarmStore_Get(t *testing.T) {
	store := NewAlarmStore()
	alarm := &Alarm{
		AlarmID:    "alarm-get-001",
		AlarmType:  AlarmNRFUnreachable,
		Severity:   SeverityMajor,
	}

	id, err := store.Save(alarm)
	require.NoError(t, err)

	retrieved := store.Get(id)
	require.NotNil(t, retrieved)
	assert.Equal(t, "alarm-get-001", retrieved.AlarmID)
	assert.Equal(t, AlarmNRFUnreachable, retrieved.AlarmType)

	// Get non-existent
	assert.Nil(t, store.Get("nonexistent"))
}

// TestAlarmStore_Count verifies that Count reflects the number of active alarms.
func TestAlarmStore_Count(t *testing.T) {
	store := NewAlarmStore()
	assert.Equal(t, 0, store.Count())

	store.Save(&Alarm{AlarmType: AlarmRedisUnreachable, BackupObject: "redis-1", Severity: SeverityMajor})
	assert.Equal(t, 1, store.Count())

	store.Save(&Alarm{AlarmType: AlarmDBUnreachable, BackupObject: "postgres-1", Severity: SeverityCritical})
	assert.Equal(t, 2, store.Count())

	// Deduplication does not increase count
	store.Save(&Alarm{AlarmType: AlarmRedisUnreachable, BackupObject: "redis-1", Severity: SeverityMajor})
	assert.Equal(t, 2, store.Count())
}

// TestAlarmManager_RaiseAlarm verifies that RaiseAlarm generates a unique alarm ID.
func TestAlarmManager_RaiseAlarm(t *testing.T) {
	mgr := NewAlarmManager(NewAlarmStore(), nil, testLogger())

	id1 := mgr.RaiseAlarm(AlarmAAAUnreachable, "server-x", "unreachable", SeverityCritical)
	assert.NotEmpty(t, id1)

	id2 := mgr.RaiseAlarm(AlarmAAAUnreachable, "server-y", "unreachable", SeverityCritical)
	assert.NotEmpty(t, id2)

	assert.NotEqual(t, id1, id2, "each RaiseAlarm call must produce a unique ID")
}

// TestAlarmManager_ClearAlarm verifies that ClearAlarm removes an alarm by its ID.
func TestAlarmManager_ClearAlarm(t *testing.T) {
	mgr := NewAlarmManager(NewAlarmStore(), nil, testLogger())

	id := mgr.RaiseAlarm(AlarmSessionTableFull, "global", "session table at capacity", SeverityMajor)
	require.NotEmpty(t, id)

	alarms := mgr.ListAlarms()
	require.Len(t, alarms, 1)

	cleared := mgr.ClearAlarm(id)
	assert.True(t, cleared)

	assert.Len(t, mgr.ListAlarms(), 0, "cleared alarm must not appear in ListAlarms")
}

// TestAlarmManager_ListAlarms verifies that ListAlarms returns all active alarms.
func TestAlarmManager_ListAlarms(t *testing.T) {
	mgr := NewAlarmManager(NewAlarmStore(), nil, testLogger())

	mgr.RaiseAlarm(AlarmAAAUnreachable, "server-a", "unreachable", SeverityCritical)
	mgr.RaiseAlarm(AlarmRedisUnreachable, "redis", "redis down", SeverityMajor)

	alarms := mgr.ListAlarms()
	assert.Len(t, alarms, 2)
}

// TestAlarmManager_All7AlarmTypes verifies that all 7 predefined alarm type constants
// can be raised without error.
func TestAlarmManager_All7AlarmTypes(t *testing.T) {
	mgr := NewAlarmManager(NewAlarmStore(), nil, testLogger())

	alarmTypes := []struct {
		alarmType    string
		backupObject string
		severity     string
	}{
		{AlarmAAAUnreachable, "server-a", SeverityCritical},
		{AlarmSessionTableFull, "global", SeverityMajor},
		{AlarmDBUnreachable, "postgres", SeverityCritical},
		{AlarmRedisUnreachable, "redis", SeverityMajor},
		{AlarmNRFUnreachable, "nrf", SeverityMajor},
		{AlarmHighAuthFailureRate, "global", SeverityMajor},
		{AlarmCircuitBreakerOpen, "aaa-server-1", SeverityMajor},
	}

	for _, at := range alarmTypes {
		id := mgr.RaiseAlarm(at.alarmType, at.backupObject, "test problem", at.severity)
		assert.NotEmpty(t, id, "RaiseAlarm must return non-empty ID for %s", at.alarmType)
	}

	// All 7 alarms must be stored
	assert.Equal(t, 7, len(mgr.ListAlarms()))
}

// TestAlarmManager_FailureRateAlarm verifies that when authentication failure rate
// exceeds 10%, a high-auth-failure-rate alarm is raised.
func TestAlarmManager_FailureRateAlarm(t *testing.T) {
	thresholds := &AlarmThresholds{
		FailureRatePercent:   10.0,
		EvaluationWindowSec: 300,
	}
	mgr := NewAlarmManager(NewAlarmStore(), thresholds, testLogger())

	// Simulate 11 failures out of 20 attempts → 55% failure rate
	for i := 0; i < 20; i++ {
		if i < 11 {
			mgr.Evaluate(&AlarmEvent{EventType: "AUTH_FAILURE"})
		} else {
			mgr.Evaluate(&AlarmEvent{EventType: "AUTH_SUCCESS"})
		}
	}

	// High failure rate alarm must be raised
	alarms := mgr.ListAlarms()
	var found bool
	for _, a := range alarms {
		if a.AlarmType == AlarmHighAuthFailureRate {
			found = true
			break
		}
	}
	assert.True(t, found, "High-Auth-Failure-Rate alarm must be raised when failure rate > 10%")
}

// TestAlarmManager_CircuitBreakerOpenAlarm verifies that when a circuit breaker
// opens, an alarm is raised for that AAA server.
func TestAlarmManager_CircuitBreakerOpenAlarm(t *testing.T) {
	mgr := NewAlarmManager(NewAlarmStore(), nil, testLogger())

	mgr.Evaluate(&AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    "aaa-server-1",
	})

	alarms := mgr.ListAlarms()
	require.Len(t, alarms, 1)
	assert.Equal(t, AlarmCircuitBreakerOpen, alarms[0].AlarmType)
	assert.Equal(t, "aaa-server-1", alarms[0].BackupObject)
}

// TestAlarmManager_DeduplicationAcrossTypes verifies that the same BackupObject
// with different AlarmType values are stored as separate alarms (deduplication
// is keyed on the tuple, not just BackupObject alone).
func TestAlarmManager_DeduplicationAcrossTypes(t *testing.T) {
	mgr := NewAlarmManager(NewAlarmStore(), nil, testLogger())

	mgr.RaiseAlarm(AlarmAAAUnreachable, "server-a", "unreachable", SeverityCritical)
	mgr.RaiseAlarm(AlarmCircuitBreakerOpen, "server-a", "cb open", SeverityMajor)

	// Same object, different types → both must be stored
	alarms := mgr.ListAlarms()
	assert.Len(t, alarms, 2, "different alarm types with same backup object must both be stored")

	// Deduplication must still work within the same type
	mgr.RaiseAlarm(AlarmCircuitBreakerOpen, "server-a", "cb open again", SeverityMajor)
	alarms = mgr.ListAlarms()
	assert.Len(t, alarms, 2, "duplicate within same type must be deduplicated")
}
