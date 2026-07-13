package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/operator/nssAAF/internal/logging"
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

// TestCLI_Trace_PopulatedStream verifies that seeded events are rendered.
func TestCLI_Trace_PopulatedStream(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	hash := logging.HashGPSI("msisdn-X")
	stream := "nssaa:debug:stream:" + hash
	now := time.Now().UnixMilli()
	for i, op := range []string{"http.request", "pg.session.save", "aaa.radius.forward"} {
		_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
			Stream: stream,
			Values: map[string]interface{}{
				"ts":     strconv.FormatInt(now-int64(i), 10),
				"pod":    "biz-1",
				"svc":    "biz",
				"trace":  "deadbeefcafebabe",
				"op":     op,
				"status": "ok",
				"dur":    "3",
				"detail": `{"table":"sessions"}`,
			},
		}).Err()
	}

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-X",
		Since:     1 * time.Hour,
	}, rdb); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "http.request") {
		t.Fatalf("expected http.request in output, got: %s", out)
	}
	if !strings.Contains(out, "pg.session.save") {
		t.Fatalf("expected pg.session.save in output, got: %s", out)
	}
	if !strings.Contains(out, "aaa.radius.forward") {
		t.Fatalf("expected aaa.radius.forward in output, got: %s", out)
	}
}

// TestCLI_Trace_OpFilter verifies op substring filtering.
func TestCLI_Trace_OpFilter(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	hash := logging.HashGPSI("msisdn-OpFilter")
	stream := "nssaa:debug:stream:" + hash
	now := time.Now().UnixMilli()
	_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"ts": strconv.FormatInt(now, 10), "pod": "biz-1", "svc": "biz",
			"trace": "abc", "op": "pg.session.save", "status": "ok", "dur": "1",
		},
	}).Err()
	_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"ts": strconv.FormatInt(now, 10), "pod": "biz-1", "svc": "biz",
			"trace": "abc", "op": "redis.rate_limit.allow", "status": "ok", "dur": "1",
		},
	}).Err()

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-OpFilter",
		Since:     1 * time.Hour,
		Op:        "pg.",
	}, rdb); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "redis.rate_limit.allow") {
		t.Fatalf("op filter failed: %s", out)
	}
	if !strings.Contains(out, "pg.session.save") {
		t.Fatalf("op filter dropped the matching event: %s", out)
	}
}

// TestCLI_Trace_SinceFilter verifies --since windowing.
func TestCLI_Trace_SinceFilter(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	hash := logging.HashGPSI("msisdn-SinceFilter")
	stream := "nssaa:debug:stream:" + hash
	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	new := time.Now().UnixMilli()
	_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"ts": strconv.FormatInt(old, 10), "pod": "biz-1", "svc": "biz",
			"trace": "abc", "op": "old.op", "status": "ok", "dur": "1",
		},
	}).Err()
	_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"ts": strconv.FormatInt(new, 10), "pod": "biz-1", "svc": "biz",
			"trace": "abc", "op": "new.op", "status": "ok", "dur": "1",
		},
	}).Err()

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-SinceFilter",
		Since:     1 * time.Hour,
	}, rdb); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "old.op") {
		t.Fatalf("since filter did not drop old event: %s", out)
	}
	if !strings.Contains(out, "new.op") {
		t.Fatalf("since filter dropped recent event: %s", out)
	}
}

// TestCLI_Trace_LimitFilter verifies --limit truncates output.
func TestCLI_Trace_LimitFilter(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	hash := logging.HashGPSI("msisdn-Limit")
	stream := "nssaa:debug:stream:" + hash
	now := time.Now().UnixMilli()
	for i := 0; i < 5; i++ {
		_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
			Stream: stream,
			Values: map[string]interface{}{
				"ts": strconv.FormatInt(now+int64(i), 10),
				"pod": "biz-1", "svc": "biz", "trace": "abc",
				"op": fmt.Sprintf("op.%d", i), "status": "ok", "dur": "1",
			},
		}).Err()
	}

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-Limit",
		Since:     1 * time.Hour,
		Limit:     3,
	}, rdb); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	count := strings.Count(out, "\n")
	if count > 4 {
		t.Fatalf("limit filter did not constrain output, lines=%d, output=%s", count, out)
	}
}
