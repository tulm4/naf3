// Command nssAAF-debug is the operator CLI for per-UE debug timelines.
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/redis/go-redis/v9"

	"github.com/operator/nssAAF/internal/logging"
)

// Sentinel errors for CLI argument validation.
var (
	errMissingSubscriber = errors.New("exactly one of --gpsi or --supi is required")
	errBothGpsiAndSupi   = errors.New("--gpsi and --supi are mutually exclusive")
)

// traceOpts captures the operator-selected filters for a timeline query.
type traceOpts struct {
	RedisAddr string
	GPSI      string
	SUPI      string
	Trace     string
	Pod       string
	Op        string
	Since     time.Duration
	Limit     int
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "trace":
		traceCmd(os.Args[2:])
	case "stream-list":
		streamListCmd(os.Args[2:])
	case "stream-clear":
		streamClearCmd(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  nssAAF-debug trace --redis ADDR (--gpsi GPSI | --supi SUPI) [--trace ID] [--pod ID] [--op PATTERN] [--since DUR] [--limit N]
  nssAAF-debug stream-list --redis ADDR (--gpsi GPSI | --supi SUPI)
  nssAAF-debug stream-clear --redis ADDR (--gpsi GPSI | --supi SUPI)`)
}

func traceCmd(args []string) {
	fs := flag.NewFlagSet("trace", flag.ExitOnError)
	redisAddr := fs.String("redis", "127.0.0.1:6379", "Redis address")
	gpsi := fs.String("gpsi", "", "GPSI (N58 flow; mutually exclusive with --supi)")
	supi := fs.String("supi", "", "SUPI (N60 AIW flow; mutually exclusive with --gpsi)")
	traceID := fs.String("trace", "", "Filter to one trace_id")
	pod := fs.String("pod", "", "Filter to one pod")
	op := fs.String("op", "", "Filter ops (substring match)")
	since := fs.Duration("since", 1*time.Hour, "Time window")
	limit := fs.Int("limit", 0, "Max events to show")
	_ = fs.Parse(args)

	rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
	defer rdb.Close()

	if err := runTrace(os.Stdout, traceOpts{
		RedisAddr: *redisAddr, GPSI: *gpsi, SUPI: *supi,
		Trace: *traceID, Pod: *pod, Op: *op, Since: *since, Limit: *limit,
	}, rdb); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func streamListCmd(args []string) {
	fs := flag.NewFlagSet("stream-list", flag.ExitOnError)
	redisAddr := fs.String("redis", "127.0.0.1:6379", "")
	gpsi := fs.String("gpsi", "", "")
	supi := fs.String("supi", "", "")
	_ = fs.Parse(args)
	hash, err := requireSubscriber(*gpsi, *supi)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
	defer rdb.Close()
	key := "nssaa:debug:stream:" + hash
	length, err := rdb.XLen(context.Background(), key).Result()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	ttl, _ := rdb.TTL(context.Background(), key).Result()
	fmt.Printf("stream: %s\nlength: %d\nttl: %s\n", key, length, ttl)
}

func streamClearCmd(args []string) {
	fs := flag.NewFlagSet("stream-clear", flag.ExitOnError)
	redisAddr := fs.String("redis", "127.0.0.1:6379", "")
	gpsi := fs.String("gpsi", "", "")
	supi := fs.String("supi", "", "")
	_ = fs.Parse(args)
	hash, err := requireSubscriber(*gpsi, *supi)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
	defer rdb.Close()
	key := "nssaa:debug:stream:" + hash
	if err := rdb.Del(context.Background(), key).Err(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("cleared:", key)
}

// requireSubscriber validates the identifier pair and returns the hashed subscriber key.
func requireSubscriber(gpsi, supi string) (string, error) {
	switch {
	case gpsi != "" && supi != "":
		return "", errBothGpsiAndSupi
	case gpsi == "" && supi == "":
		return "", errMissingSubscriber
	case gpsi != "":
		return logging.HashGPSI(gpsi), nil
	default:
		return logging.HashGPSI(supi), nil
	}
}

// runTrace is the testable inner function for the trace subcommand.
func runTrace(w io.Writer, opts traceOpts, rdb *redis.Client) error {
	subHash, err := requireSubscriber(opts.GPSI, opts.SUPI)
	if err != nil {
		return err
	}
	key := "nssaa:debug:stream:" + subHash
	cutoff := time.Now().Add(-opts.Since).UnixMilli()
	msgs, err := rdb.XRange(context.Background(), key, "-", "+").Result()
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		fmt.Fprintln(w, "no events for this subscriber in the last", opts.Since)
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tPOD\tSVC\tTRACE\tOP\tSTATUS\tDUR\tDETAIL")
	count := 0
	for _, m := range msgs {
		ts, _ := m.Values["ts"].(string)
		tsMs, _ := parseInt64(ts)
		if tsMs < cutoff {
			continue
		}
		if opts.Trace != "" && m.Values["trace"] != opts.Trace {
			continue
		}
		if opts.Pod != "" && m.Values["pod"] != opts.Pod {
			continue
		}
		if opts.Op != "" && !strings.Contains(asString(m.Values["op"]), opts.Op) {
			continue
		}
		if opts.Limit > 0 {
			count++
			if count > opts.Limit {
				continue
			}
		}
		t := time.UnixMilli(tsMs).Format("2006-01-02T15:04:05")
		svc := colorSvc(m.Values["svc"])
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			t, asString(m.Values["pod"]), svc, shortTrace(m.Values["trace"]),
			asString(m.Values["op"]), asString(m.Values["status"]), asString(m.Values["dur"]),
			asString(m.Values["detail"]))
	}
	return tw.Flush()
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func parseInt64(s string) (int64, bool) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

func shortTrace(s interface{}) string {
	t, _ := s.(string)
	if len(t) > 8 {
		return t[:8]
	}
	return t
}

func colorSvc(s interface{}) string {
	str, _ := s.(string)
	switch str {
	case "http-gw":
		return color.New(color.FgCyan).Sprint(str)
	case "biz":
		return color.New(color.FgGreen).Sprint(str)
	case "aaa-gw":
		return color.New(color.FgYellow).Sprint(str)
	}
	return str
}
