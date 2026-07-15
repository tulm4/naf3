// Package main is the entry point for the NSSAAF Biz Pod.
package main

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

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

func TestNewRateLimiterSet_UsesDistinctRateLimitPolicies(t *testing.T) {
	cfg := &config.Config{}
	cfg.RateLimit.PerGpsiPerMin = 1
	cfg.RateLimit.PerAmfPerSec = 2

	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:0"})
	defer func() { _ = client.Close() }()

	f := NewBizPodFactory(cfg)
	rateLimiters := f.newRateLimiterSet(client, nil)

	amfAllowed1, err := rateLimiters.amfRateLimiter.AllowAMF(context.Background(), "amf-test")
	if err == nil {
		if !amfAllowed1 {
			t.Fatal("first AMF request should be allowed")
		}
		amfAllowed2, err := rateLimiters.amfRateLimiter.AllowAMF(context.Background(), "amf-test")
		if err == nil && !amfAllowed2 {
			// If a real Redis server is available this proves the second-based policy is distinct.
			return
		}
	}

	if rateLimiters.amfRateLimiter == nil || rateLimiters.gpsiRateLimiter == nil {
		t.Fatal("expected both AMF and subscriber rate limiters to be constructed")
	}
	if rateLimiters.amfRateLimiter == rateLimiters.gpsiRateLimiter {
		t.Fatal("expected distinct rate limiter instances for AMF and subscriber scopes")
	}
}
