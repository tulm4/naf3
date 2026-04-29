// Package restconf provides a RESTCONF server (RFC 8040) for the NRM.
package restconf

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"
)

// panicRecovery is middleware that recovers from panics and returns a proper
// RFC 8040 error response instead of closing the connection.
func panicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("handler panic recovered", "error", err, "stack", debug.Stack())
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{
						"type":  "urn:ietf:params:restconf:errors:server-error",
						"title": "Internal Server Error",
						"detail": "An unexpected error occurred",
					},
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// AlarmManagerProvider abstracts the AlarmManager methods used by RESTCONF handlers.
// This interface breaks the import cycle between nrm and restconf.
type AlarmManagerProvider interface {
	ListAlarmInfos() []*AlarmInfo
	GetAlarmInfo(id string) *AlarmInfo
	AckAlarmInfo(id string, ackedBy string) bool
}

// RouterConfig holds the dependencies for the RESTCONF router.
type RouterConfig struct {
	AlarmMgr AlarmManagerProvider
}

// NewRouter creates a new chi router with all RESTCONF routes registered.
// RFC 8040 §3: RESTCONF uses GET, POST, PUT, PATCH, DELETE on YANG-defined resources.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// ─── Panic recovery middleware (RFC 8040 §3) ─────────────────────────────────
	// Wrap all handlers with panic recovery to prevent connection drops.
	r.Use(panicRecovery)

	// ─── RESTCONF data endpoints (RFC 8040 §3) ─────────────────────────────────

	// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function — list all NSSAAF function entries.
	r.Get("/data/3gpp-nssaaf-nrm:nssaa-function", handleGetNssaaFunction)

	// GET /restconf/data/3gpp-nssaaf-nrm:nssaa-function={id} — get single entry.
	r.Get("/data/3gpp-nssaaf-nrm:nssaa-function/{id}", handleGetNssaaFunctionByID)

	// GET /restconf/data/3gpp-nssaaf-nrm:alarms — list all active alarms.
	r.Get("/data/3gpp-nssaaf-nrm:alarms", handleGetAlarms(cfg.AlarmMgr))

	// GET /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId} — get single alarm.
	r.Get("/data/3gpp-nssaaf-nrm:alarms/{alarmId}", handleGetAlarm(cfg.AlarmMgr))

	// POST /restconf/data/3gpp-nssaaf-nrm:alarms={alarmId}/ack — acknowledge alarm.
	r.Post("/data/3gpp-nssaaf-nrm:alarms/{alarmId}/ack", handleAckAlarm(cfg.AlarmMgr))

	// ─── RFC 8040 §3.1: OPTIONS pre-flight for /restconf/data and subpaths ─────────
	// Use wildcard pattern to catch OPTIONS requests to any /data/* subpath.
	r.Options("/data/{path:.*}", handleOptionsData)

	// ─── RFC 8040 §3.8: YANG module capability ──────────────────────────────
	r.Get("/modules", handleModules)

	return r
}
