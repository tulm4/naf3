// Package nrm implements the Network Resource Model (NRM) for NSSAAF,
// providing FCAPS fault management via YANG model structs, an AlarmManager
// with deduplication, and a RESTCONF server (RFC 8040 JSON) that exposes
// NSSAAFFunction IOC and alarm data.
//
// Spec: TS 28.541 §5.3.145-148 (NRM), RFC 8040 (RESTCONF).
package nrm

import "time"

// Severity constants per ITU-T X.733 and 3GPP TS 28.541.
const (
	SeverityCritical       = "CRITICAL"
	SeverityMajor         = "MAJOR"
	SeverityMinor         = "MINOR"
	SeverityWarning       = "WARNING"
	SeverityIndeterminate = "INDETERMINATE"
)

// NssaaFunction is the root container for the NSSAAFFunction NRM IOC.
// It represents the managed network function entity and is serialized
// under the "nssaa-function" YANG list.
//
// Spec: TS 28.541 §5.3.145.
type NssaaFunction struct {
	NssaaFunction []NssaaFunctionEntry `json:"nssaa-function"`
}

// NssaaFunctionEntry represents a single NSSAAFFunction instance in the
// YANG list. The key is "managed-element-id".
//
// Spec: TS 28.541 §5.3.145.
type NssaaFunctionEntry struct {
	// ManagedElementID is the unique identifier for this NSSAAF instance.
	// YANG key field.
	ManagedElementID string `json:"managed-element-id"`

	// ManagedElementTypeID is the type discriminator, e.g. "NSSAA_FUNCTION".
	ManagedElementTypeID string `json:"managed-element-type-id,omitempty"`

	// UserDefinedData holds operator-specific metadata.
	UserDefinedData string `json:"user-defined-data,omitempty"`

	// PLMNInfoList holds the PLMN IDs served by this NSSAAF.
	PLMNInfoList []string `json:"p-l-m-n-info-list,omitempty"`

	// SBIFQDN is the SBI-facing FQDN of this NSSAAF instance.
	SBIFQDN string `json:"s-b-i-f-q-d-n,omitempty"`

	// CNSIIdList holds the Network Slice Instance IDs configured.
	CNSIIdList []string `json:"c-n-s-i-id-list,omitempty"`

	// CommModelList holds the communication models supported.
	CommModelList []string `json:"comm-model-list,omitempty"`

	// NssaaInfo holds NSSAAF-specific configuration.
	NssaaInfo *NssaaInfo `json:"nssaa-info,omitempty"`

	// EpN58 holds the N58 (AMF-NSSAAF) endpoint configurations.
	EpN58 []EndpointN58 `json:"ep-n58,omitempty"`

	// EpN59 holds the N59 (NSSAAF-UDM) endpoint configurations.
	EpN59 []EndpointN59 `json:"ep-n59,omitempty"`
}

// NssaaInfo holds NSSAAF-specific information attached to the managed function.
// Spec: TS 28.541 §5.3.145.
type NssaaInfo struct {
	// SupiRanges holds the SUPI ranges served by this NSSAAF.
	SupiRanges []string `json:"supi-ranges,omitempty"`

	// InternalGroupIDRanges holds internal group ID ranges served.
	InternalGroupIDRanges []string `json:"internal-group-id-ranges,omitempty"`

	// SupportedSecurityAlgo holds the EAP methods supported (e.g. EAP-TLS).
	SupportedSecurityAlgo []string `json:"supported-security-algo,omitempty"`
}

// EndpointN58 represents the N58 (AMF-NSSAAF) interface endpoint.
// Spec: TS 28.541 §5.3.147.
type EndpointN58 struct {
	EndpointID   string `json:"endpoint-id"`
	LocalAddress string `json:"local-address,omitempty"`
}

// EndpointN59 represents the N59 (NSSAAF-UDM) interface endpoint.
// Spec: TS 28.541 §5.3.148.
type EndpointN59 struct {
	EndpointID   string `json:"endpoint-id"`
	LocalAddress string `json:"local-address,omitempty"`
}

// Alarm represents an alarm instance as defined in ITU-T X.733 and
// 3GPP TS 28.541. Alarms are the primary Fault Management data
// structure in the FCAPS framework.
//
// Spec: ITU-T X.733; TS 28.541 §5.3.
type Alarm struct {
	// AlarmID uniquely identifies this alarm instance.
	AlarmID string `json:"alarm-id"`

	// AlarmType identifies the type of alarm (e.g. NSSAA_AAA_SERVER_UNREACHABLE).
	AlarmType string `json:"alarm-type"`

	// ProbableCause describes the probable cause.
	ProbableCause string `json:"probable-cause"`

	// SpecificProblem provides further details about the problem.
	SpecificProblem string `json:"specific-problem,omitempty"`

	// Severity is the ITU-T X.733 severity assignment.
	Severity string `json:"severity"`

	// PerceivedSeverity is the perceived severity after operator action.
	PerceivedSeverity string `json:"perceived-severity,omitempty"`

	// BackupObject identifies the object (e.g. aaa-server-id) that is the
	// source of the alarm. Used as part of the deduplication key.
	BackupObject string `json:"backup-object,omitempty"`

	// CorrelatedAlarms lists alarm IDs of related alarms.
	CorrelatedAlarms []string `json:"correlated-alarms,omitempty"`

	// ProposedRepairActions suggests remediation steps.
	ProposedRepairActions string `json:"proposed-repair-actions,omitempty"`

	// EventTime is when the alarm was raised.
	EventTime time.Time `json:"event-time"`

	// Acked indicates whether the alarm has been acknowledged.
	Acked bool `json:"acked,omitempty"`

	// AckedBy holds the identifier of the operator who acknowledged the alarm.
	AckedBy string `json:"acked-by,omitempty"`

	// AckedAt holds the time when the alarm was acknowledged.
	AckedAt *time.Time `json:"acked-at,omitempty"`
}

// AlarmEvent represents an event pushed from the Biz Pod to the NRM for
// alarm evaluation. Received at POST /internal/events.
type AlarmEvent struct {
	// EventType identifies the type of event.
	EventType string `json:"event-type"`

	// Metrics holds optional metrics (e.g. failureRate) for evaluation.
	Metrics map[string]float64 `json:"metrics,omitempty"`

	// Target holds optional target information (e.g. aaaServer ID).
	Target string `json:"target,omitempty"`

	// SpecificProblem holds optional problem details.
	SpecificProblem string `json:"specific-problem,omitempty"`
}

// AlarmInfo is the subset of Alarm fields exposed by the RESTCONF alarm API.
// Used by the restconf package to avoid a direct import cycle.
type AlarmInfo struct {
	AlarmID              string    `json:"alarm-id"`
	AlarmType            string    `json:"alarm-type"`
	ProbableCause        string    `json:"probable-cause"`
	SpecificProblem      string    `json:"specific-problem,omitempty"`
	Severity             string    `json:"severity"`
	PerceivedSeverity    string    `json:"perceived-severity,omitempty"`
	BackupObject         string    `json:"backup-object,omitempty"`
	CorrelatedAlarms     []string  `json:"correlated-alarms,omitempty"`
	ProposedRepairActions string   `json:"proposed-repair-actions,omitempty"`
	EventTime            time.Time `json:"event-time"`
	Acked               bool      `json:"acked,omitempty"`
	AckedBy             string    `json:"acked-by,omitempty"`
	AckedAt             *time.Time `json:"acked-at,omitempty"`
}

// ToAlarmInfo converts an Alarm to AlarmInfo.
func (a *Alarm) ToAlarmInfo() *AlarmInfo {
	return &AlarmInfo{
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
