// Package main provides HTTP Gateway handlers including internal discovery API.
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 2
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/operator/nssAAF/internal/nrf"
)

// discoveryHandler handles internal NF discovery requests from Biz Pod.
type discoveryHandler struct {
	nrfClient *nrf.Client
	logger    *slog.Logger
}

// newDiscoveryHandler creates a discovery handler.
func newDiscoveryHandler(nrfClient *nrf.Client, logger *slog.Logger) *discoveryHandler {
	return &discoveryHandler{
		nrfClient: nrfClient,
		logger:    logger,
	}
}

// HandleNFFind handles GET /internal/nf-discovery/{nfType}.
// Discovers an NF instance by type via NRF and returns the NF profile.
// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §New Internal API
func (h *discoveryHandler) HandleNFFind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeProblemDetails(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
			"Only GET method is allowed", r.RequestURI)
		return
	}

	// Extract nfType from path: /internal/nf-discovery/{nfType}
	path := strings.TrimPrefix(r.URL.Path, "/internal/nf-discovery/")
	nfType := strings.TrimSuffix(path, "/")
	if nfType == "" || r.URL.Path == "/internal/nf-discovery/" {
		writeProblemDetails(w, http.StatusBadRequest, "INVALID_NF_TYPE",
			"NF type is required in path /internal/nf-discovery/{nfType}", r.RequestURI)
		return
	}

	// Normalize NF type to NRF format (uppercase)
	nfType = strings.ToUpper(nfType)

	h.logger.Info("NF discovery request",
		"nf_type", nfType,
		"request_id", r.Header.Get("X-Request-ID"),
	)

	// Discover the NF via NRF client
	// Spec: TS 29.510 §5.3 (NF discovery query)
	profile, err := h.nrfClient.FindNF(r.Context(), nfType)
	if err != nil {
		h.logger.Warn("NF discovery failed", "nf_type", nfType, "error", err)
		writeProblemDetails(w, http.StatusServiceUnavailable, "NRF_UNAVAILABLE",
			fmt.Sprintf("Failed to discover %s: %v", nfType, err), r.RequestURI)
		return
	}

	if profile == nil {
		h.logger.Info("NF not found in NRF", "nf_type", nfType)
		writeProblemDetails(w, http.StatusNotFound, "NF_NOT_FOUND",
			fmt.Sprintf("No serving %s found in NRF", nfType), r.RequestURI)
		return
	}

	// Return NF profile as JSON
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(profile); err != nil {
		h.logger.Error("failed to encode NF profile", "error", err)
	}
}

// writeProblemDetails writes a RFC 7807 Problem Details response.
func writeProblemDetails(w http.ResponseWriter, status int, cause, detail, instance string) {
	problem := struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}{
		Type:   fmt.Sprintf("https://nrf.operator.com/problem/%s", strings.ToLower(cause)),
		Title:  cause,
		Status: status,
		Detail: detail,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}
