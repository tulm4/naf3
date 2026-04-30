// Package restconf provides a RESTCONF server (RFC 8040) for the NRM.
package restconf

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// AlarmAckHandler handles alarm acknowledgment at the mux level.
// Registered directly on the HTTP mux because chi's {path:.+} pattern
// cannot match multi-segment paths with POST (chi#704).
type AlarmAckHandler struct {
	alarmMgr AlarmManagerProvider
}

// NewAlarmAckHandler creates an AlarmAckHandler.
func NewAlarmAckHandler(alarmMgr AlarmManagerProvider) *AlarmAckHandler {
	return &AlarmAckHandler{alarmMgr: alarmMgr}
}

// HandleAck processes a POST to acknowledge an alarm.
// alarmID is extracted by the caller from the URL path.
func (h *AlarmAckHandler) HandleAck(w http.ResponseWriter, r *http.Request, alarmID string) {
	if r.Method != http.MethodPost {
		respondMethodNotAllowed(w, r)
		return
	}
	if alarmID == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Try to parse the acked-by from the request body first.
	ackedBy := "unknown"
	if r.Header.Get("Content-Type") == "application/json" {
		var body struct {
			AckedBy string `json:"acked-by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.AckedBy != "" {
			ackedBy = body.AckedBy
		}
	}
	// Fall back to the X-Authenticated-User header.
	if headerVal := r.Header.Get("X-Authenticated-User"); headerVal != "" {
		ackedBy = headerVal
	}

	acked := h.alarmMgr.AckAlarmInfo(alarmID, ackedBy)

	SetJSONHeaders(w)
	if !acked {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetNssaaFunction returns the list of NSSAAFFunction entries.
// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function
func handleGetNssaaFunction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMethodNotAllowed(w, r)
		return
	}

	SetJSONHeaders(w)
	w.WriteHeader(http.StatusOK)

	// Return a default NSSAAFFunction entry matching the YANG model.
	data := NewNssaaFunctionData([]NssaaFunctionEntry{
		{
			ManagedElementID:     "nssaa-1",
			ManagedElementTypeID: "NSSAA_FUNCTION",
			SBIFQDN:              "nssAAF.operator.com",
			PLMNInfoList:         []string{"208001"},
			CommModelList:        []string{"HTTP2_SBI"},
			NssaaInfo: &NssaaInfo{
				SupiRanges:            []string{"208001*"},
				SupportedSecurityAlgo: []string{"EAP-TLS", "EAP-TTLS"},
			},
			EpN58: []EndpointN58{
				{EndpointID: "n58-1", LocalAddress: "10.0.1.50"},
			},
			EpN59: []EndpointN59{
				{EndpointID: "n59-1", LocalAddress: "10.0.1.50"},
			},
		},
	})

	_ = json.NewEncoder(w).Encode(data)
}

// handleGetNssaaFunctionByID returns a single NSSAAFFunction entry by managed-element-id.
// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function={id}
func handleGetNssaaFunctionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMethodNotAllowed(w, r)
		return
	}

	id := chi.URLParam(r, "id")

	SetJSONHeaders(w)

	// Return the single entry (currently only one, "nssaa-1").
	if id != "nssaa-1" {
		respondNotFound(w, r, "NSSAAFFunction", id)
		return
	}

	entry := NssaaFunctionEntry{
		ManagedElementID:     "nssaa-1",
		ManagedElementTypeID: "NSSAA_FUNCTION",
		SBIFQDN:              "nssAAF.operator.com",
		PLMNInfoList:         []string{"208001"},
		CommModelList:        []string{"HTTP2_SBI"},
		NssaaInfo: &NssaaInfo{
			SupiRanges:            []string{"208001*"},
			SupportedSecurityAlgo: []string{"EAP-TLS", "EAP-TTLS"},
		},
		EpN58: []EndpointN58{
			{EndpointID: "n58-1", LocalAddress: "10.0.1.50"},
		},
		EpN59: []EndpointN59{
			{EndpointID: "n59-1", LocalAddress: "10.0.1.50"},
		},
	}

	data := WrapWithModule(entry, "3gpp-nssaaf-nrm", "nssaa-function")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(data)
}

// handleGetAlarms returns all active alarms.
// GET /restconf/data/3gpp-nssaaf-nrm:alarms
func handleGetAlarms(alarmMgr AlarmManagerProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			respondMethodNotAllowed(w, r)
			return
		}

		alarms := alarmMgr.ListAlarmInfos()
		data := NewAlarmData(alarms)

		SetJSONHeaders(w)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(data)
	}
}

// handleGetAlarm returns a single alarm by alarmId.
// GET /restconf/data/3gpp-nssaaf-nrm:alarms/{alarmId}[/ack]
// The path param captures the remainder after "alarms/" — e.g. "uuid" or "uuid/ack".
func handleGetAlarm(alarmMgr AlarmManagerProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			respondMethodNotAllowed(w, r)
			return
		}

		rawPath := chi.URLParam(r, "path")
		// rawPath is like "uuid" or "uuid/ack" — extract the alarmId
		alarmID := strings.SplitN(rawPath, "/", 2)[0]

		alarm := alarmMgr.GetAlarmInfo(alarmID)

		SetJSONHeaders(w)
		if alarm == nil {
			respondNotFound(w, r, "Alarm", alarmID)
			return
		}

		data := NewAlarmData([]*AlarmInfo{alarm})
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(data)
	}
}

// handleAckAlarm acknowledges an alarm.
// POST /restconf/data/3gpp-nssaaf-nrm:alarms/{alarmId}/ack
func handleAckAlarm(alarmMgr AlarmManagerProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondMethodNotAllowed(w, r)
			return
		}

		rawPath := chi.URLParam(r, "path")
		// rawPath is like "uuid/ack" — extract the alarmId
		parts := strings.SplitN(rawPath, "/", 2)
		alarmID := parts[0]

		// Acknowledge with the authenticated principal extracted from the request header.
		// In a real deployment this would come from a client certificate or JWT.
		// Fall back to a placeholder if the header is not present.
		ackedBy := r.Header.Get("X-Authenticated-User")
		if ackedBy == "" {
			ackedBy = "unknown"
		}
		acked := alarmMgr.AckAlarmInfo(alarmID, ackedBy)

		SetJSONHeaders(w)
		if !acked {
			respondNotFound(w, r, "Alarm", alarmID)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// handleOptionsData returns RFC 8040 §3.1 OPTIONS pre-flight for /restconf/data.
func handleOptionsData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodOptions {
		respondMethodNotAllowed(w, r)
		return
	}

	w.Header().Set("Allow", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Accept-Patch", mediaType)
	w.WriteHeader(http.StatusOK)
}

// handleModules returns the YANG module capability (RFC 8040 §3.8).
func handleModules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMethodNotAllowed(w, r)
		return
	}

	SetJSONHeaders(w)
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ietf-restconf-monitoring:modules-state": map[string]interface{}{
			"module": []map[string]interface{}{
				{
					"name":        "3gpp-nssaaf-nrm",
					"revision":    "2025-01-01",
					"namespace":   "urn:3gpp:ts:ts_28_541",
					"conformance": "implement",
				},
				{
					"name":        "ietf-restconf-monitoring",
					"revision":    "2016-08-15",
					"namespace":   "urn:ietf:params:xml:ns:yang:ietf-restconf-monitoring",
					"conformance": "implement",
				},
			},
		},
	})
}

// respondMethodNotAllowed returns 405 with Allow header.
func respondMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	SetJSONHeaders(w)
	w.Header().Set("Allow", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	body := NewErrorResponse(http.StatusMethodNotAllowed, "Method not allowed")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_ = json.NewEncoder(w).Encode(body)
}

// respondNotFound returns 404 with RFC 8040 error format.
func respondNotFound(w http.ResponseWriter, _ *http.Request, resource, id string) {
	body := NewErrorResponse(http.StatusNotFound,
		"Resource "+strings.ToLower(resource)+" with id="+id+" does not exist")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(body)
}
