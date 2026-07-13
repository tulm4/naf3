package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestCLI_Trace_EmptyStream verifies that an empty stream produces a "no events" message.
func TestCLI_Trace_EmptyStream(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-208046000000001",
		Since:     1 * time.Hour,
	}, rdb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no events") {
		t.Fatalf("expected empty-stream message, got: %s", buf.String())
	}
}

// TestCLI_Trace_SUPIAlsoWorks verifies that --supi also produces a "no events" message.
func TestCLI_Trace_SUPIAlsoWorks(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		SUPI:      "imsi-208046000000001",
		Since:     1 * time.Hour,
	}, rdb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no events") {
		t.Fatalf("expected empty-stream message, got: %s", buf.String())
	}
}

// TestCLI_Trace_RejectsBothGpsiAndSupi verifies mutual exclusion of --gpsi and --supi.
func TestCLI_Trace_RejectsBothGpsiAndSupi(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	var buf bytes.Buffer
	err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-1",
		SUPI:      "imsi-1",
		Since:     1 * time.Hour,
	}, rdb)
	if err == nil {
		t.Fatal("expected error when both --gpsi and --supi are set")
	}
	if !errors.Is(err, errBothGpsiAndSupi) {
		t.Fatalf("expected errBothGpsiAndSupi, got: %v", err)
	}
}

// TestCLI_Trace_RequiresEitherGpsiOrSupi verifies that neither produces an error.
func TestCLI_Trace_RequiresEitherGpsiOrSupi(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	var buf bytes.Buffer
	err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		Since:     1 * time.Hour,
	}, rdb)
	if err == nil {
		t.Fatal("expected error when neither --gpsi nor --supi is set")
	}
	if !errors.Is(err, errMissingSubscriber) {
		t.Fatalf("expected errMissingSubscriber, got: %v", err)
	}
}
