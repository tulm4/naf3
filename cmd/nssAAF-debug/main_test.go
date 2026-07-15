package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestRunTrace_JSON verifies that --json emits one JSON object per line
// with all stream fields and that grouping by trace_id is preserved via
// the "trace" field on each object.
func TestRunTrace_JSON(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	hash := logging.HashGPSI("msisdn-JSON")
	stream := "nssaa:debug:stream:" + hash
	now := time.Now().UnixMilli()

	// Forward trace: two events sharing trace "traceForward"
	_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"ts":  strconv.FormatInt(now-100, 10),
			"pod": "biz-1", "svc": "biz",
			"trace": "traceForward", "span": "span-a",
			"auth": "AUTH1", "gpsi_h": "abcdef0123",
			"op": "biz:http.request", "kind": "http", "status": "ok", "dur": "5",
			"detail": `{"table":"sessions"}`,
		},
	}).Err()
	_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"ts":  strconv.FormatInt(now-50, 10),
			"pod": "aaa-gw-1", "svc": "aaa-gw",
			"trace": "traceForward", "span": "span-b",
			"auth": "AUTH1", "gpsi_h": "abcdef0123",
			"op": "aaa-gw:aaa.radius.forward", "kind": "aaa", "status": "ok", "dur": "12",
			"detail": `{"session_id":"abc"}`,
		},
	}).Err()

	// Callback trace: one event with a different trace_id.
	_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: stream,
		Values: map[string]interface{}{
			"ts":  strconv.FormatInt(now, 10),
			"pod": "aaa-gw-1", "svc": "aaa-gw",
			"trace": "traceCallback", "span": "span-c",
			"auth": "AUTH1", "gpsi_h": "abcdef0123",
			"op": "aaa-gw:aaa.radius.recv", "kind": "aaa", "status": "ok", "dur": "3",
			"detail": `{"rar":true}`,
		},
	}).Err()

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-JSON",
		Since:     1 * time.Hour,
		JSON:      true,
	}, rdb); err != nil {
		t.Fatal(err)
	}

	out := strings.TrimRight(buf.String(), "\n")
	if out == "" {
		t.Fatal("expected JSON output, got empty")
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON lines (one per event), got %d:\n%s", len(lines), out)
	}

	type rec struct {
		TS     int64           `json:"ts"`
		Pod    string          `json:"pod"`
		Svc    string          `json:"svc"`
		Trace  string          `json:"trace"`
		Span   string          `json:"span"`
		SubH   string          `json:"sub_h"`
		GpsiH  string          `json:"gpsi_h"`
		Auth   string          `json:"auth"`
		Op     string          `json:"op"`
		Kind   string          `json:"kind"`
		Status string          `json:"status"`
		Detail json.RawMessage `json:"detail"`
	}
	var records []rec
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var r rec
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("decode JSON line: %v\nline was: %s", err, out)
		}
		records = append(records, r)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 JSON records, got %d", len(records))
	}

	// Sorted ascending by ts: forward-100, forward-50, callback-now.
	if !(records[0].TS < records[1].TS && records[1].TS < records[2].TS) {
		t.Fatalf("expected ascending ts order, got %d, %d, %d",
			records[0].TS, records[1].TS, records[2].TS)
	}

	// Grouping: trace_id must be carried on each record so post-processors
	// can reconstruct the grouping the CLI used.
	wantTraces := []string{"traceForward", "traceForward", "traceCallback"}
	for i, want := range wantTraces {
		if records[i].Trace != want {
			t.Fatalf("record %d trace: want %q, got %q", i, want, records[i].Trace)
		}
	}

	// Spot-check a non-trivial field.
	if records[0].Auth != "AUTH1" {
		t.Fatalf("record 0 auth: want AUTH1, got %q", records[0].Auth)
	}
	if records[0].GpsiH != "abcdef0123" {
		t.Fatalf("record 0 gpsi_h: want abcdef0123, got %q", records[0].GpsiH)
	}
	if records[0].Op != "biz:http.request" {
		t.Fatalf("record 0 op: want biz:http.request, got %q", records[0].Op)
	}
}

// TestRunTrace_TableGrouping verifies that the table mode emits a blank
// line between consecutive events whose trace_id changes.
func TestRunTrace_TableGrouping(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer rdb.Close()

	hash := logging.HashGPSI("msisdn-Group")
	stream := "nssaa:debug:stream:" + hash
	now := time.Now().UnixMilli()

	for i, trace := range []string{"traceA", "traceA", "traceB"} {
		_ = rdb.XAdd(context.Background(), &redis.XAddArgs{
			Stream: stream,
			Values: map[string]interface{}{
				"ts":  strconv.FormatInt(now+int64(i), 10),
				"pod": "biz-1", "svc": "biz", "trace": trace,
				"auth": "AUTH1", "gpsi_h": "deadbeef",
				"op": "biz:http.request", "status": "ok", "dur": "1",
			},
		}).Err()
	}

	var buf bytes.Buffer
	if err := runTrace(&buf, traceOpts{
		RedisAddr: s.Addr(),
		GPSI:      "msisdn-Group",
		Since:     1 * time.Hour,
	}, rdb); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// Header has AUTH and GPSI_H columns; tabwriter renders them as
	// whitespace-aligned, so check for the literal column words.
	if !strings.Contains(out, " AUTH ") && !strings.Contains(out, "\nAUTH\n") {
		t.Fatalf("expected AUTH column in header, got: %s", out)
	}
	if !strings.Contains(out, "GPSI_H") {
		t.Fatalf("expected GPSI_H column in header, got: %s", out)
	}
	if !strings.Contains(out, "deadbeef") {
		t.Fatalf("expected full gpsi_h in body, got: %s", out)
	}
	// Count blank lines (lines containing only whitespace) — should be at
	// least one between the two groups.
	blankCount := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			blankCount++
		}
	}
	if blankCount < 1 {
		t.Fatalf("expected at least one blank line between trace groups, got %d in:\n%s", blankCount, out)
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
				"ts":  strconv.FormatInt(now+int64(i), 10),
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
