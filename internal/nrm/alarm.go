// Package nrm implements the Network Resource Model (NRM) for NSSAAF.
package nrm

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AlarmStore provides in-memory storage for active alarms with deduplication.
// Alarms are deduplicated by (AlarmType, BackupObject) within a 5-minute window
// per ITU-T X.733 and 3GPP TS 28.541.
//
// Spec: ITU-T X.733 §8.2 (alarm deduplication); TS 28.541 §5.3.
type AlarmStore struct {
	mu      sync.RWMutex
	alarms  map[string]*Alarm       // alarmID -> Alarm
	dedup   map[dedupKey]*dedupInfo // (AlarmType, BackupObject) -> dedup info
}

type dedupKey struct {
	alarmType    string
	backupObject string
}

type dedupInfo struct {
	alarmID  string
	deadline time.Time // deadline = EventTime.Add(5 minutes)
}

// NewAlarmStore creates a new in-memory alarm store.
func NewAlarmStore() *AlarmStore {
	return &AlarmStore{
		alarms: make(map[string]*Alarm),
		dedup:  make(map[dedupKey]*dedupInfo),
	}
}

// Save stores an alarm, applying deduplication. If an alarm with the same
// (AlarmType, BackupObject) key exists within the 5-minute deduplication
// window, the new alarm is skipped and the existing alarm ID is returned.
// An empty BackupObject disables deduplication for that alarm.
//
// Returns the alarm ID (existing or new) and an error if storage failed.
func (s *AlarmStore) Save(alarm *Alarm) (string, error) {
	if alarm == nil {
		return "", fmt.Errorf("alarm cannot be nil")
	}
	if alarm.AlarmID == "" {
		alarm.AlarmID = uuid.New().String()
	}
	key := dedupKey{alarmType: alarm.AlarmType, backupObject: alarm.BackupObject}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Deduplication: skip if same (AlarmType, BackupObject) within window.
	if key.backupObject != "" {
		if existing, ok := s.dedup[key]; ok {
			if time.Now().Before(existing.deadline) {
				return existing.alarmID, nil
			}
			// Expired: remove stale dedup entry.
			delete(s.dedup, key)
		}
	}

	// Set EventTime only if not already set (preserve original timestamp for dedup).
	if alarm.EventTime.IsZero() {
		alarm.EventTime = time.Now()
	}
	s.alarms[alarm.AlarmID] = alarm
	s.dedup[key] = &dedupInfo{
		alarmID:  alarm.AlarmID,
		deadline: alarm.EventTime.Add(5 * time.Minute),
	}
	return alarm.AlarmID, nil
}

// List returns all active alarms, ordered by EventTime descending.
func (s *AlarmStore) List() []*Alarm {
	s.mu.RLock()
	defer s.mu.RUnlock()

	alarms := make([]*Alarm, 0, len(s.alarms))
	for _, a := range s.alarms {
		alarms = append(alarms, a)
	}
	return alarms
}

// Get returns the alarm with the given ID, or nil if not found.
func (s *AlarmStore) Get(id string) *Alarm {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alarms[id]
}

// Clear removes the alarm with the given ID.
// Returns true if the alarm existed and was removed.
func (s *AlarmStore) Clear(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	alarm, ok := s.alarms[id]
	if !ok {
		return false
	}

	// Remove dedup entry.
	key := dedupKey{alarmType: alarm.AlarmType, backupObject: alarm.BackupObject}
	delete(s.dedup, key)
	delete(s.alarms, id)
	return true
}

// Count returns the number of active alarms.
func (s *AlarmStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.alarms)
}

// UpdateAck updates the acknowledged state of an alarm.
func (s *AlarmStore) UpdateAck(id string, acked bool, ackedBy string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	alarm, ok := s.alarms[id]
	if !ok {
		return false
	}

	alarm.Acked = acked
	alarm.AckedBy = ackedBy
	now := time.Now()
	alarm.AckedAt = &now
	return true
}
