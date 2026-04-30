// Package nrm implements the Network Resource Model (NRM) for NSSAAF.
package nrm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/operator/nssAAF/internal/restconf"
)

// Server wraps the HTTP server for the NRM RESTCONF API.
type Server struct {
	httpServer *http.Server
	listenAddr string
	logger     *slog.Logger
}

// NewServer creates a new NRM RESTCONF server.
func NewServer(
	cfg *NRMConfig,
	alarmMgr *AlarmManager,
	alarmStore *AlarmStore,
	logger *slog.Logger,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		cfg = DefaultNRMConfig()
	}

	addr := cfg.ListenAddr
	if addr == "" {
		addr = ":8081"
	}

	// Build the HTTP handler.
	mux := http.NewServeMux()

	// RESTCONF routes — register chi handler for /restconf prefix.
	restconfHandler := restconf.NewRouter(restconf.RouterConfig{AlarmMgr: alarmMgr})
	// Strip "/restconf" prefix so chi routes like "/data/..." match correctly.
	mux.Handle("/restconf/", http.StripPrefix("/restconf", restconfHandler))

	// Alarm acknowledgment: chi's {path:.+} pattern doesn't match multi-segment
	// paths with POST (chi#704), so we register the ack handler directly on the mux.
	// RFC 8040 uses "=" as the list key separator: /restconf/data/3gpp-nssaaf-nrm:alarms={id}/ack
	ackHandler := restconf.NewAlarmAckHandler(alarmMgr)
	mux.HandleFunc("POST /restconf/data/", func(w http.ResponseWriter, r *http.Request) {
		// Only handle /restconf/data/3gpp-nssaaf-nrm:alarms={id}/ack patterns
		path := r.URL.Path
		const alarmsPrefix = "/restconf/data/3gpp-nssaaf-nrm:alarms="
		if strings.HasPrefix(path, alarmsPrefix) {
			rest := path[len(alarmsPrefix):] // strip prefix to get "{alarmId}[/more]"
			if strings.HasSuffix(rest, "/ack") {
				alarmID := rest[:len(rest)-4] // strip "/ack"
				ackHandler.HandleAck(w, r, alarmID)
				return
			}
		}
		http.NotFound(w, r)
	})

	// Internal event endpoint for Biz Pod push.
	mux.HandleFunc("POST /internal/events", handleEvents(alarmMgr))

	// Health check.
	mux.HandleFunc("GET /healthz", handleHealthz)

	readTimeout := 10 * time.Second
	writeTimeout := 30 * time.Second
	idleTimeout := 120 * time.Second
	if cfg.ReadTimeout > 0 {
		readTimeout = time.Duration(cfg.ReadTimeout) * time.Second
	}
	if cfg.WriteTimeout > 0 {
		writeTimeout = time.Duration(cfg.WriteTimeout) * time.Second
	}
	if cfg.IdleTimeout > 0 {
		idleTimeout = time.Duration(cfg.IdleTimeout) * time.Second
	}

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
		},
		listenAddr: addr,
		logger:     logger,
	}
}

// Start starts the NRM HTTP server in a goroutine and returns immediately.
// It logs any startup errors via the standard logger.
func (s *Server) Start() error {
	s.logger.Info("NRM RESTCONF server starting", "addr", s.listenAddr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		s.logger.Error("NRM server error", "error", err)
		return err
	}
	return nil
}

// Shutdown gracefully shuts down the NRM server with the given context timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("NRM RESTCONF server shutting down")
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the listen address of the server.
func (s *Server) Addr() string {
	return s.listenAddr
}

// handleEvents processes incoming AlarmEvent from the Biz Pod.
func handleEvents(alarmMgr *AlarmManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}

		var event AlarmEvent
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if alarmMgr == nil {
			http.Error(w, "alarm manager not initialized", http.StatusServiceUnavailable)
			return
		}
		alarmMgr.Evaluate(&event)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// handleHealthz returns the health check response.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
