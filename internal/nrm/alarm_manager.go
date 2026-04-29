// Package nrm implements the Network Resource Model (NRM) for NSSAAF.
package nrm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/operator/nssAAF/internal/restconf"
)

// Alarm type constants per 3GPP TS 28.541 and ITU-T X.733.
//
// Spec: TS 28.541 §5.3; ITU-T X.733.
const (
	AlarmAAAUnreachable       = "NSSAA_AAA_SERVER_UNREACHABLE"
	AlarmSessionTableFull     = "NSSAA_SESSION_TABLE_FULL"
	AlarmDBUnreachable        = "NSSAA_DB_UNREACHABLE"
	AlarmRedisUnreachable     = "NSSAA_REDIS_UNREACHABLE"
	AlarmNRFUnreachable       = "NSSAA_NRF_UNREACHABLE"
	AlarmHighAuthFailureRate  = "NSSAA_HIGH_AUTH_FAILURE_RATE"  // REQ-33
	AlarmCircuitBreakerOpen   = "NSSAA_CIRCUIT_BREAKER_OPEN"   // REQ-34
)

// AlarmManager manages alarm lifecycle: raising, clearing, and evaluating.
// Thread-safe via mutex. Uses AlarmStore for persistence.
type AlarmManager struct {
	store     *AlarmStore
	thresholds *AlarmThresholds
	logger    *slog.Logger
	mu        sync.RWMutex

	// Circuit breaker state tracking per AAA server.
	cbState map[string]bool // server -> isOpen

	// Auth metrics for failure rate evaluation.
	authTotal   int64
	authFailures int64
}

// AlarmThresholds defines thresholds for alarm evaluation.
type AlarmThresholds struct {
	FailureRatePercent  float64 // e.g. 10.0 for 10%
	EvaluationWindowSec int    // e.g. 300 for 5 minutes
}

// DefaultAlarmThresholds returns the default alarm thresholds:
// failure rate > 10% over a 5-minute window.
func DefaultAlarmThresholds() *AlarmThresholds {
	return &AlarmThresholds{
		FailureRatePercent:   10.0,
		EvaluationWindowSec: 300,
	}
}

// NewAlarmManager creates a new AlarmManager with the given store and thresholds.
// If thresholds is nil, defaults are applied.
func NewAlarmManager(store *AlarmStore, thresholds *AlarmThresholds, logger *slog.Logger) *AlarmManager {
	if store == nil {
		store = NewAlarmStore()
	}
	if thresholds == nil {
		thresholds = DefaultAlarmThresholds()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AlarmManager{
		store:      store,
		thresholds: thresholds,
		logger:     logger,
		cbState:    make(map[string]bool),
	}
}

// StartMetricsWindow starts a background goroutine that periodically resets
// auth metrics to implement the sliding window evaluation period.
// Must be called after AlarmManager is fully initialized.
func (m *AlarmManager) StartMetricsWindow(ctx context.Context) {
	if m.thresholds.EvaluationWindowSec <= 0 {
		return
	}
	window := time.Duration(m.thresholds.EvaluationWindowSec) * time.Second
	go func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.ResetAuthMetrics()
				m.logger.Info("auth_metrics_window_reset",
					"window_sec", m.thresholds.EvaluationWindowSec)
			}
		}
	}()
}

// RaiseAlarm creates and stores a new alarm with the given parameters.
// Returns the generated alarm ID.
func (m *AlarmManager) RaiseAlarm(eventType, backupObject, specificProblem string, severity string) string {
	alarm := &Alarm{
		AlarmID:              uuid.New().String(),
		AlarmType:            eventType,
		ProbableCause:        probableCause(eventType),
		SpecificProblem:      specificProblem,
		Severity:             severity,
		PerceivedSeverity:    severity,
		BackupObject:        backupObject,
		ProposedRepairActions: proposedRepairAction(eventType),
		EventTime:           time.Now(),
	}

	id, err := m.store.Save(alarm)
	if err != nil {
		m.logger.Error("failed to save alarm",
			"alarm_type", eventType,
			"error", err,
		)
		return ""
	}

	m.logger.Info("alarm raised",
		"alarm_id", id,
		"alarm_type", eventType,
		"backup_object", backupObject,
		"severity", severity,
	)
	return id
}

// ClearAlarm removes the alarm with the given ID.
// Returns true if the alarm existed and was cleared.
func (m *AlarmManager) ClearAlarm(alarmID string) bool {
	if alarm := m.store.Get(alarmID); alarm != nil {
		m.logger.Info("alarm cleared",
			"alarm_id", alarmID,
			"alarm_type", alarm.AlarmType,
		)
	}
	return m.store.Clear(alarmID)
}

// ClearByType clears all alarms matching the given type and backup object.
// Used when a condition resolves (e.g. circuit breaker closed).
func (m *AlarmManager) ClearByType(alarmType, backupObject string) {
	alarms := m.store.List()
	for _, a := range alarms {
		if a.AlarmType == alarmType && a.BackupObject == backupObject {
			m.store.Clear(a.AlarmID)
			m.logger.Info("alarm cleared by type",
				"alarm_id", a.AlarmID,
				"alarm_type", alarmType,
				"backup_object", backupObject,
			)
		}
	}
}

// ListAlarms returns all active alarms.
func (m *AlarmManager) ListAlarms() []*Alarm {
	return m.store.List()
}

// ListAlarmInfos returns all active alarms as restconf.AlarmInfo structs.
func (m *AlarmManager) ListAlarmInfos() []*restconf.AlarmInfo {
	alarms := m.store.List()
	infos := make([]*restconf.AlarmInfo, len(alarms))
	for i, a := range alarms {
		infos[i] = &restconf.AlarmInfo{
			AlarmID:              a.AlarmID,
			AlarmType:            a.AlarmType,
			ProbableCause:        a.ProbableCause,
			SpecificProblem:      a.SpecificProblem,
			Severity:             a.Severity,
			PerceivedSeverity:    a.PerceivedSeverity,
			BackupObject:         a.BackupObject,
			CorrelatedAlarms:     a.CorrelatedAlarms,
			ProposedRepairActions: a.ProposedRepairActions,
			EventTime:            a.EventTime,
			Acked:               a.Acked,
			AckedBy:             a.AckedBy,
			AckedAt:             a.AckedAt,
		}
	}
	return infos
}

// GetAlarm returns the alarm with the given ID.
func (m *AlarmManager) GetAlarm(id string) *Alarm {
	return m.store.Get(id)
}

// GetAlarmInfo returns the alarm with the given ID as an AlarmInfo.
func (m *AlarmManager) GetAlarmInfo(id string) *restconf.AlarmInfo {
	if a := m.store.Get(id); a != nil {
		return &restconf.AlarmInfo{
			AlarmID:              a.AlarmID,
			AlarmType:            a.AlarmType,
			ProbableCause:        a.ProbableCause,
			SpecificProblem:      a.SpecificProblem,
			Severity:             a.Severity,
			PerceivedSeverity:    a.PerceivedSeverity,
			BackupObject:         a.BackupObject,
			CorrelatedAlarms:     a.CorrelatedAlarms,
			ProposedRepairActions: a.ProposedRepairActions,
			EventTime:            a.EventTime,
			Acked:               a.Acked,
			AckedBy:             a.AckedBy,
			AckedAt:             a.AckedAt,
		}
	}
	return nil
}

// AckAlarm acknowledges the alarm with the given ID.
func (m *AlarmManager) AckAlarm(id string, ackedBy string) bool {
	return m.store.UpdateAck(id, true, ackedBy)
}

// AckAlarmInfo acknowledges the alarm with the given ID.
func (m *AlarmManager) AckAlarmInfo(id string, ackedBy string) bool {
	return m.store.UpdateAck(id, true, ackedBy)
}

// Evaluate processes an AlarmEvent from the Biz Pod and raises or clears
// alarms based on the event type and metrics.
func (m *AlarmManager) Evaluate(event *AlarmEvent) {
	if event == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch event.EventType {
	case "AUTH_SUCCESS":
		m.authTotal++

	case "AUTH_FAILURE":
		m.authTotal++
		m.authFailures++
		// Evaluate failure rate alarm.
		if m.authTotal > 0 {
			rate := float64(m.authFailures) / float64(m.authTotal) * 100
			if rate > m.thresholds.FailureRatePercent {
				// Take a snapshot of current alarms (releases manager lock during List()).
				alarmSnapshot := m.takeAlarmSnapshot()
				raised := false
				for _, a := range alarmSnapshot {
					if a.AlarmType == AlarmHighAuthFailureRate {
						raised = true
						break
					}
				}
				if !raised {
					fr := fmt.Sprintf("%.2f%% failure rate over %d requests",
						rate, m.authTotal)
					m.RaiseAlarm(AlarmHighAuthFailureRate, "global",
						fr, SeverityMajor)
				}
			}
		}

	case "CIRCUIT_BREAKER_OPEN":
		m.cbState[event.Target] = true
		// Take a snapshot of current alarms (releases manager lock during List()).
		alarmSnapshot := m.takeAlarmSnapshot()
		raised := false
		for _, a := range alarmSnapshot {
			if a.AlarmType == AlarmCircuitBreakerOpen && a.BackupObject == event.Target {
				raised = true
				break
			}
		}
		if !raised {
			sp := fmt.Sprintf("circuit breaker open for AAA server: %s", event.Target)
			m.RaiseAlarm(AlarmCircuitBreakerOpen, event.Target, sp, SeverityMajor)
		}

	case "CIRCUIT_BREAKER_CLOSED":
		if wasOpen, ok := m.cbState[event.Target]; ok && wasOpen {
			m.cbState[event.Target] = false
			m.ClearByType(AlarmCircuitBreakerOpen, event.Target)
		}

	case "AAA_UNREACHABLE":
		sp := fmt.Sprintf("AAA server unreachable: %s", event.Target)
		m.RaiseAlarm(AlarmAAAUnreachable, event.Target, sp, SeverityCritical)

	case "DB_UNREACHABLE":
		m.RaiseAlarm(AlarmDBUnreachable, "postgres", event.Target, SeverityCritical)

	case "REDIS_UNREACHABLE":
		m.RaiseAlarm(AlarmRedisUnreachable, "redis", event.Target, SeverityMajor)

	case "NRF_UNREACHABLE":
		m.RaiseAlarm(AlarmNRFUnreachable, "nrf", event.Target, SeverityMajor)
	}
}

// ResetAuthMetrics resets the authentication success/failure counters.
// Called periodically to implement the evaluation window.
func (m *AlarmManager) ResetAuthMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authTotal = 0
	m.authFailures = 0
}

// takeAlarmSnapshot returns a copy of current alarms while releasing the manager lock.
// This avoids holding AlarmManager.mu during the store.List() call.
func (m *AlarmManager) takeAlarmSnapshot() []*Alarm {
	// Note: Caller must hold m.mu before calling. We release and re-acquire to avoid lock-in-lock.
	m.mu.Unlock()
	defer m.mu.Lock()
	return m.store.List()
}

// probableCause returns the ITU-T X.733 probable cause string for a given alarm type.
func probableCause(alarmType string) string {
	switch alarmType {
	case AlarmAAAUnreachable:
		return "AAA_SERVER_UNREACHABLE"
	case AlarmSessionTableFull:
		return "RESOURCE_CAPACITY_EXCEEDED"
	case AlarmDBUnreachable:
		return "DATABASE_UNAVAILABLE"
	case AlarmRedisUnreachable:
		return "CACHING_SERVER_UNAVAILABLE"
	case AlarmNRFUnreachable:
		return "NETWORK_FUNCTION_REGISTRY_UNAVAILABLE"
	case AlarmHighAuthFailureRate:
		return "AUTHENTICATION_FAILURE_RATE_HIGH"
	case AlarmCircuitBreakerOpen:
		return "CIRCUIT_BREAKER_OPEN"
	default:
		return "UNKNOWN"
	}
}

// proposedRepairAction returns a suggested repair action for the given alarm type.
func proposedRepairAction(alarmType string) string {
	switch alarmType {
	case AlarmAAAUnreachable:
		return "Check AAA server connectivity and configuration"
	case AlarmSessionTableFull:
		return "Increase session table capacity or reduce load"
	case AlarmDBUnreachable:
		return "Check PostgreSQL connectivity and configuration"
	case AlarmRedisUnreachable:
		return "Check Redis connectivity and configuration"
	case AlarmNRFUnreachable:
		return "Check NRF connectivity and registration"
	case AlarmHighAuthFailureRate:
		return "Investigate authentication failures and AAA server logs"
	case AlarmCircuitBreakerOpen:
		return "Check AAA server health and network connectivity"
	default:
		return "Contact network operations team"
	}
}
