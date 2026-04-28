// Package nrm implements the Network Resource Model (NRM) for NSSAAF.
package nrm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// BizPodNRMClient pushes alarm-relevant events to the NRM RESTCONF server.
// It is used by the Biz Pod to report authentication outcomes, circuit breaker
// state changes, and infrastructure health events.
//
// Spec: docs/design/18_nrm_fcaps.md §9.1 (Biz Pod → NRM push model, Option A).
type BizPodNRMClient struct {
	nrmURL  string
	client  *http.Client
	logger  *slog.Logger
}

// NewBizPodNRMClient creates a new BizPodNRMClient that pushes events to
// the given NRM server URL.
func NewBizPodNRMClient(nrmURL string, logger *slog.Logger) *BizPodNRMClient {
	if logger == nil {
		logger = slog.Default()
	}
	if nrmURL == "" {
		nrmURL = "http://localhost:8081"
	}
	return &BizPodNRMClient{
		nrmURL: nrmURL,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger: logger,
	}
}

// push sends an AlarmEvent to the NRM server's /internal/events endpoint.
func (c *BizPodNRMClient) push(event *AlarmEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	url := c.nrmURL + "/internal/events"
	resp, err := c.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		c.logger.Warn("failed to push event to NRM",
			"url", url,
			"event_type", event.EventType,
			"error", err,
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		c.logger.Warn("NRM returned non-success status",
			"url", url,
			"status", resp.StatusCode,
		)
		return fmt.Errorf("NRM returned status %d", resp.StatusCode)
	}

	return nil
}

// PushAuthSuccess reports a successful authentication to the NRM for metrics.
func (c *BizPodNRMClient) PushAuthSuccess() error {
	return c.push(&AlarmEvent{
		EventType: "AUTH_SUCCESS",
	})
}

// PushAuthFailure reports a failed authentication to the NRM, triggering alarm
// evaluation. failureRate is the current failure rate percentage for context.
func (c *BizPodNRMClient) PushAuthFailure(failureRate float64) error {
	return c.push(&AlarmEvent{
		EventType: "AUTH_FAILURE",
		Metrics: map[string]float64{
			"failureRate": failureRate,
		},
	})
}

// PushCircuitBreakerOpen reports that the circuit breaker for a given AAA server
// has opened. This triggers the REQ-34 alarm.
func (c *BizPodNRMClient) PushCircuitBreakerOpen(aaaServer string) error {
	return c.push(&AlarmEvent{
		EventType: "CIRCUIT_BREAKER_OPEN",
		Target:    aaaServer,
	})
}

// PushCircuitBreakerClosed reports that the circuit breaker for a given AAA server
// has closed. This triggers alarm clearing.
func (c *BizPodNRMClient) PushCircuitBreakerClosed(aaaServer string) error {
	return c.push(&AlarmEvent{
		EventType: "CIRCUIT_BREAKER_CLOSED",
		Target:    aaaServer,
	})
}

// PushAAAUnreachable reports that an AAA server is unreachable.
func (c *BizPodNRMClient) PushAAAUnreachable(aaaServer string) error {
	return c.push(&AlarmEvent{
		EventType: "AAA_UNREACHABLE",
		Target:    aaaServer,
	})
}

// PushDBUnreachable reports that the database is unreachable.
func (c *BizPodNRMClient) PushDBUnreachable(details string) error {
	return c.push(&AlarmEvent{
		EventType: "DB_UNREACHABLE",
		Target:    details,
	})
}

// PushRedisUnreachable reports that Redis is unreachable.
func (c *BizPodNRMClient) PushRedisUnreachable(details string) error {
	return c.push(&AlarmEvent{
		EventType: "REDIS_UNREACHABLE",
		Target:    details,
	})
}

// PushNRFUnreachable reports that the NRF is unreachable.
func (c *BizPodNRMClient) PushNRFUnreachable(details string) error {
	return c.push(&AlarmEvent{
		EventType: "NRF_UNREACHABLE",
		Target:    details,
	})
}
