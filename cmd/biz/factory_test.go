// Package main is the entry point for the NSSAAF Biz Pod.
package main

import (
	"testing"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/resilience"
)

// TestBuild_UsesInternalCommNativeCBConfig proves the factory wires the circuit
// breaker registry from the canonical InternalComm.Native.CB config path, not
// from any AAA config path.
func TestBuild_UsesInternalCommNativeCBConfig(t *testing.T) {
	cfg := &config.Config{
		InternalComm: config.InternalCommConfig{
			Mode: "native",
			Native: config.NativeCommConfig{
				CB: config.CircuitBreakerConfig{
					FailureThreshold: 7,
					RecoveryTimeout:  45 * time.Second,
					SuccessThreshold: 4,
				},
			},
		},
	}

	f := NewBizPodFactory(cfg)
	registry := f.newNFRegistry()

	cb := registry.Get("any-host:1234")

	expected := resilience.NewCircuitBreaker(7, 45*time.Second, 4)
	if cb.FailureThreshold() != expected.FailureThreshold() {
		t.Errorf("FailureThreshold: got %d, want %d (from InternalComm.Native.CB)",
			cb.FailureThreshold(), expected.FailureThreshold())
	}
	if cb.RecoveryTimeout() != expected.RecoveryTimeout() {
		t.Errorf("RecoveryTimeout: got %v, want %v (from InternalComm.Native.CB)",
			cb.RecoveryTimeout(), expected.RecoveryTimeout())
	}
	if cb.SuccessThreshold() != expected.SuccessThreshold() {
		t.Errorf("SuccessThreshold: got %d, want %d (from InternalComm.Native.CB)",
			cb.SuccessThreshold(), expected.SuccessThreshold())
	}
}

// TestBuild_UsesInternalCommNativeCBConfig_Defaults verifies the defaults are
// applied when zero values are provided.
func TestBuild_UsesInternalCommNativeCBConfig_Defaults(t *testing.T) {
	cfg := &config.Config{
		InternalComm: config.InternalCommConfig{
			Mode: "native",
			Native: config.NativeCommConfig{
				CB: config.CircuitBreakerConfig{
					// All zero — defaults should apply
				},
			},
		},
	}

	f := NewBizPodFactory(cfg)
	registry := f.newNFRegistry()

	cb := registry.Get("test-host:9999")

	// Expected defaults from factory.go
	if cb.FailureThreshold() != 3 {
		t.Errorf("FailureThreshold default: got %d, want 3", cb.FailureThreshold())
	}
	if cb.RecoveryTimeout() != 10*time.Second {
		t.Errorf("RecoveryTimeout default: got %v, want 10s", cb.RecoveryTimeout())
	}
	if cb.SuccessThreshold() != 2 {
		t.Errorf("SuccessThreshold default: got %d, want 2", cb.SuccessThreshold())
	}
}
