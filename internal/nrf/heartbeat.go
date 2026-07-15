package nrf

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/operator/nssAAF/internal/config"
)

// HeartbeatManager handles NRF registration and periodic heartbeat
// with self-healing via re-registration after consecutive failures.
//
// Spec: TS 29.510 §6.4.2 (NFHeartBeat), §6.4.1 (NFRegistration).
type HeartbeatManager struct {
	client     HeartbeatClient
	cfg        config.HeartbeatConfig
	instanceID string

	mu                  sync.RWMutex
	registered          bool
	heartbeatInterval   time.Duration
	consecutiveFailures int
	etag                string

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// HeartbeatClient defines the operations the manager needs against NRF.
// Defining it here (consumer side) keeps the manager decoupled from any
// particular client implementation and makes it trivially mockable.
type HeartbeatClient interface {
	Register(ctx context.Context, profile *NFProfile) (time.Duration, string, error)
	Heartbeat(ctx context.Context, instanceID, etag string) (string, error)
	Deregister(ctx context.Context, instanceID string) error
}

// NewHeartbeatManager constructs a manager. instanceID is the NF instance ID
// used to address heartbeat/deregister calls in the NRF.
func NewHeartbeatManager(client HeartbeatClient, instanceID string, cfg config.HeartbeatConfig) *HeartbeatManager {
	return &HeartbeatManager{
		client:            client,
		cfg:               cfg,
		heartbeatInterval: cfg.InitialInterval,
		instanceID:        instanceID,
		stopCh:            make(chan struct{}),
	}
}

// Start performs an initial registration (without blocking on failure) and
// starts the heartbeat loop. If the initial registration fails the manager
// retries in the background and returns nil so that startup is not blocked
// by an NRF outage.
func (m *HeartbeatManager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.stopCh = make(chan struct{})
	m.mu.Unlock()

	// Best-effort initial registration; failures trigger background retry.
	if err := m.register(ctx); err != nil {
		slog.Warn("nrf initial registration failed, retrying in background",
			"error", err,
		)
		go m.reRegisterLoop(context.Background())
	}

	m.wg.Add(1)
	go m.run(ctx)

	return nil
}

// run is the main heartbeat loop.
func (m *HeartbeatManager) run(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.deregister(context.Background())
			return
		case <-m.stopCh:
			m.deregister(context.Background())
			return
		case <-ticker.C:
			if err := m.heartbeat(ctx); err != nil {
				m.handleFailure(ctx, err)
			} else {
				m.mu.Lock()
				m.consecutiveFailures = 0
				m.mu.Unlock()
			}
		}
	}
}

// register performs NF registration with NRF and updates internal state.
func (m *HeartbeatManager) register(ctx context.Context) error {
	profile := &NFProfile{
		NFInstanceID:   m.instanceID,
		NFType:         NFTypeNSSAAF,
		NFStatus:       NFStatusRegistered,
		HeartBeatTimer: int(m.cfg.InitialInterval.Seconds()),
	}

	interval, etag, err := m.client.Register(ctx, profile)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.registered = true
	m.etag = etag
	if m.cfg.AcceptNegotiatedInterval && interval > 0 {
		m.heartbeatInterval = interval
	}
	m.mu.Unlock()

	slog.Info("nrf registration successful",
		"interval", m.heartbeatInterval,
		"etag", etag,
	)

	return nil
}

// heartbeat sends a heartbeat PATCH against the last known etag.
func (m *HeartbeatManager) heartbeat(ctx context.Context) error {
	m.mu.RLock()
	etag := m.etag
	m.mu.RUnlock()

	newEtag, err := m.client.Heartbeat(ctx, m.instanceID, etag)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.etag = newEtag
	m.mu.Unlock()

	return nil
}

// handleFailure records a heartbeat failure and triggers re-registration
// when the configured tolerance is exceeded.
func (m *HeartbeatManager) handleFailure(ctx context.Context, err error) {
	m.mu.Lock()
	m.consecutiveFailures++
	failures := m.consecutiveFailures
	m.mu.Unlock()

	slog.Warn("nrf heartbeat failed",
		"attempt", failures,
		"max_failures", m.cfg.MaxConsecutiveFailures,
		"error", err,
	)

	if failures >= m.cfg.MaxConsecutiveFailures {
		slog.Error("nrf heartbeat degraded, initiating re-registration")

		m.mu.Lock()
		m.registered = false
		m.mu.Unlock()

		go m.reRegisterLoop(context.Background())
	}
}

// reRegisterLoop retries registration with exponential backoff and jitter.
// It runs until it succeeds or the manager is stopped (best-effort recovery).
func (m *HeartbeatManager) reRegisterLoop(ctx context.Context) {
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := m.register(ctx); err != nil {
			attempt++
			delay := exponentialBackoff(attempt)
			slog.Warn("re-registration failed, retrying",
				"attempt", attempt,
				"delay", delay,
				"error", err,
			)
			time.Sleep(delay)
			continue
		}

		slog.Info("re-registration successful, resuming heartbeat")
		m.mu.Lock()
		m.consecutiveFailures = 0
		m.mu.Unlock()
		return
	}
}

// deregister removes the NF instance from NRF. Safe to call when not
// registered.
func (m *HeartbeatManager) deregister(ctx context.Context) {
	m.mu.RLock()
	registered := m.registered
	m.mu.RUnlock()

	if !registered {
		return
	}

	if err := m.client.Deregister(ctx, m.instanceID); err != nil {
		slog.Warn("nrf deregistration failed", "error", err)
		return
	}

	m.mu.Lock()
	m.registered = false
	m.mu.Unlock()
	slog.Info("nrf deregistration successful")
}

// Stop halts the heartbeat manager and waits for the loop to exit.
func (m *HeartbeatManager) Stop() {
	m.mu.Lock()
	close(m.stopCh)
	m.mu.Unlock()
	m.wg.Wait()
}

// IsRegistered returns true if currently registered with NRF.
func (m *HeartbeatManager) IsRegistered() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.registered
}

// exponentialBackoff computes the retry delay with full jitter.
// Base 5s, capped at 5 minutes; ±10% random jitter reduces thundering-herd.
func exponentialBackoff(attempt int) time.Duration {
	const (
		base = 5 * time.Second
		max  = 5 * time.Minute
	)

	delay := base * time.Duration(1<<uint(attempt))
	if delay > max || delay <= 0 {
		delay = max
	}

	jitter := time.Duration(rand.Int63n(int64(delay / 5)))

	return delay + jitter
}

// parseHeartbeatInterval extracts a heartbeat interval in seconds from a
// JSON body, returning 0 if absent or invalid.
func parseHeartbeatInterval(body []byte) time.Duration {
	var resp struct {
		HeartBeatTimer int `json:"heartBeatTimer"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.HeartBeatTimer > 0 {
		return time.Duration(resp.HeartBeatTimer) * time.Second
	}
	return 0
}

// parseETag extracts an ETag value from a JSON body, returning "" if absent.
func parseETag(body []byte) string {
	var resp struct {
		ETag string `json:"etag"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.ETag != "" {
		return resp.ETag
	}
	return ""
}
