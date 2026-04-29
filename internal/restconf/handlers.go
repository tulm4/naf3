// Package restconf provides a RESTCONF server (RFC 8040) for the NRM.
package restconf

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// handleGetNssaaFunction returns the list of NSSAAFFunction entries.
// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function
func handleGetNssaaFunction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondMethodNotAllowed(w, r)
		return
	}
	if r.Header.Get("Accept") != mediaType && r.Header.Get("Accept") != "*/*" {
		respondNotAcceptable(w, r)
		return
	}

	SetJSONHeaders(w)
	w.WriteHeader(http.StatusOK)

	// Return a default NSSAAFFunction entry matching the YANG model.
	data := NewNssaaFunctionData([]NssaaFunctionEntry{
		{
			ManagedElementID:      "nssaa-1",
			ManagedElementTypeID:  "NSSAA_FUNCTION",
			SBIFQDN:              "nssAAF.operator.com",
			PLMNInfoList:         []string{"208001"},
			CommModelList:        []string{"HTTP2_SBI"},
			NssaaInfo: &NssaaInfo{
				SupiRanges:            []string{"208001*"},
				SupportedSecurityAlgo:  []string{"EAP-TLS", "EAP-TTLS"},
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
	if r.Header.Get("Accept") != mediaType && r.Header.Get("Accept") != "*/*" {
		respondNotAcceptable(w, r)
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
		ManagedElementID:      "nssaa-1",
		ManagedElementTypeID:  "NSSAA_FUNCTION",
		SBIFQDN:              "nssAAF.operator.com",
		PLMNInfoList:         []string{"208001"},
		CommModelList:        []string{"HTTP2_SBI"},
		NssaaInfo: &NssaaInfo{
			SupiRanges:            []string{"208001*"},
			SupportedSecurityAlgo:  []string{"EAP-TLS", "EAP-TTLS"},
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
		if r.Header.Get("Accept") != mediaType && r.Header.Get("Accept") != "*/*" {
			respondNotAcceptable(w, r)
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
// GET /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId}
func handleGetAlarm(alarmMgr AlarmManagerProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			respondMethodNotAllowed(w, r)
			return
		}
		if r.Header.Get("Accept") != mediaType && r.Header.Get("Accept") != "*/*" {
			respondNotAcceptable(w, r)
			return
		}

		alarmID := chi.URLParam(r, "alarmId")
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
// POST /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId}/ack
func handleAckAlarm(alarmMgr AlarmManagerProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondMethodNotAllowed(w, r)
			return
		}

		alarmID := chi.URLParam(r, "alarmId")

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
					"name":         "3gpp-nssaaf-nrm",
					"revision":     "2025-01-01",
					"namespace":    "urn:3gpp:ts:ts_28_541",
					"conformance": "implement",
				},
				{
					"name":         "ietf-restconf-monitoring",
					"revision":     "2016-08-15",
					"namespace":    "urn:ietf:params:xml:ns:yang:ietf-restconf-monitoring",
					"conformance": "implement",
				},
			},
		},
	})
}

// respondMethodNotAllowed returns 405 with Allow header.
func respondMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	SetJSONHeaders(w)
	w.Header().Set("Allow", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	body := NewErrorResponse(http.StatusMethodNotAllowed, "Method not allowed")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_ = json.NewEncoder(w).Encode(body)
}

// respondNotAcceptable returns 406.
func respondNotAcceptable(w http.ResponseWriter, r *http.Request) {
	SetJSONHeaders(w)
	body := NewErrorResponse(http.StatusNotAcceptable, "Not Acceptable: only "+mediaType+" is supported")
	w.WriteHeader(http.StatusNotAcceptable)
	_ = json.NewEncoder(w).Encode(body)
}

// respondNotFound returns 404 with RFC 8040 error format.
func respondNotFound(w http.ResponseWriter, r *http.Request, resource, id string) {
	body := NewErrorResponse(http.StatusNotFound,
		"Resource "+strings.ToLower(resource)+" with id="+id+" does not exist")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(body)
}
