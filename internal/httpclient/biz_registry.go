package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/redis/go-redis/v9"
)

const BizPodsKeyPrefix = "nssaa:biz:pod:"

// BizPodEntry represents a registered Biz Pod in Redis.
// Source: docs/superpowers/specs/2026-06-14-http-gateway-biz-comms-full-gap-fix-design.md §Option B
type BizPodEntry struct {
	URL      string `json:"url"`
	LastSeen int64  `json:"lastSeen"`
}

// BizRegistry implements load balancing for HTTP Gateway → Biz Pod communication.
// It uses Redis to discover live Biz Pods and falls back to a static URL when Redis
// has no registered pods.
// Spec: Option B — Redis-based target selection with circuit breakers
type BizRegistry struct {
	redisAddr  string
	staticURL  string
	httpClient *http.Client
	cbRegistry *resilience.Registry
	retryCfg   resilience.RetryConfig
}

// Verify BizRegistry implements proto.BizServiceClient.
// The interface will be updated in Slice 1.3 to include requestID parameter.
var _ proto.BizServiceClient = (*BizRegistry)(nil)

// NewBizRegistry creates a new BizRegistry with the given configuration.
// redisAddr: Redis server address for pod discovery
// staticURL: fallback static URL when Redis has no live pods
// cfg: NativeCommConfig for timeout, retry, and circuit breaker settings
// transport: optional http.RoundTripper override for outbound HTTP. When nil,
// the client builds a default pooled transport from cfg.Pool.
//
// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §5.4
func NewBizRegistry(redisAddr, staticURL string, cfg config.NativeCommConfig, transport http.RoundTripper) *BizRegistry {
	retryCfg := resilience.RetryConfig{
		MaxAttempts: cfg.Retry.MaxAttempts,
		BaseDelay:   cfg.Retry.BaseDelay,
		MaxDelay:    cfg.Retry.MaxDelay,
	}
	if retryCfg.MaxAttempts == 0 {
		retryCfg.MaxAttempts = resilience.DefaultRetryConfig.MaxAttempts
	}
	if retryCfg.BaseDelay == 0 {
		retryCfg.BaseDelay = resilience.DefaultRetryConfig.BaseDelay
	}
	if retryCfg.MaxDelay == 0 {
		retryCfg.MaxDelay = resilience.DefaultRetryConfig.MaxDelay
	}

	cbCfg := cfg.CB
	if cbCfg.FailureThreshold == 0 {
		cbCfg.FailureThreshold = 5
	}
	if cbCfg.RecoveryTimeout == 0 {
		cbCfg.RecoveryTimeout = 30 * time.Second
	}
	if cbCfg.SuccessThreshold == 0 {
		cbCfg.SuccessThreshold = 3
	}

	if transport == nil {
		transport = &http.Transport{}
	}

	return &BizRegistry{
		redisAddr: redisAddr,
		staticURL: staticURL,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
		cbRegistry: resilience.NewRegistry(
			cbCfg.FailureThreshold,
			cbCfg.RecoveryTimeout,
			cbCfg.SuccessThreshold,
		),
		retryCfg: retryCfg,
	}
}

// livePod represents a discovered live Biz Pod.
type livePod struct {
	podID string
	url   string
}

// getLivePods scans Redis for live Biz Pods registered under nssaa:biz:pod:* keys.
// A pod is considered live if its LastSeen timestamp is within maxAge.
func (b *BizRegistry) getLivePods(ctx context.Context) ([]livePod, error) {
	rdb := redis.NewClient(&redis.Options{Addr: b.redisAddr})
	defer rdb.Close()

	const pattern = BizPodsKeyPrefix + "*"
	const maxAge = 60 * time.Second
	now := time.Now().Unix()
	var live []livePod
	var cursor uint64

	for {
		keys, nextCursor, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			data, err := rdb.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}

			var entry BizPodEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				continue
			}

			if now-entry.LastSeen < int64(maxAge.Seconds()) {
				podID := strings.TrimPrefix(key, BizPodsKeyPrefix)
				live = append(live, livePod{podID: podID, url: entry.URL})
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return live, nil
}

// selectRandomLivePod picks a random live pod from Redis.
func (b *BizRegistry) selectRandomLivePod(ctx context.Context) (string, error) {
	pods, err := b.getLivePods(ctx)
	if err != nil {
		return "", err
	}
	if len(pods) == 0 {
		return "", fmt.Errorf("no live pods")
	}
	return pods[rand.Intn(len(pods))].url, nil
}

// ForwardRequest implements proto.BizServiceClient with Redis-based pod discovery,
// circuit breakers, and retry logic.
// Spec: Option B — Redis-based target selection
func (b *BizRegistry) ForwardRequest(ctx context.Context, path, method string, body []byte, requestID string) ([]byte, int, error) {
	var lastErr error
	var lastStatus int
	var lastBody []byte

	err := resilience.Do(ctx, b.retryCfg, func() error {
		// Try to get live pod from Redis, fallback to static URL
		targetURL := b.staticURL
		if podURL, err := b.selectRandomLivePod(ctx); err == nil {
			targetURL = podURL
		}

		// Check circuit breaker
		cb := b.cbRegistry.Get(targetURL)
		if !cb.Allow() {
			// Try to find an alternative live pod
			if altURL, err := b.selectRandomLivePod(ctx); err == nil && b.cbRegistry.Get(altURL).Allow() {
				targetURL = altURL
			} else {
				lastErr = fmt.Errorf("circuit breaker open, no live pods")
				return lastErr
			}
		}

		// Execute request
		req, err := http.NewRequestWithContext(ctx, method, targetURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if requestID != "" {
			req.Header.Set("X-Request-ID", requestID)
		}

		resp, err := b.httpClient.Do(req)
		if err != nil {
			b.cbRegistry.Get(targetURL).RecordFailure()
			lastErr = err
			return err
		}
		defer resp.Body.Close()

		lastStatus = resp.StatusCode
		lastBody, _ = io.ReadAll(resp.Body)

		// Don't retry 4xx errors
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			cb.RecordSuccess()
			return nil
		}

		// Retry 5xx errors
		if resilience.IsRetryable(resp.StatusCode) {
			b.cbRegistry.Get(targetURL).RecordFailure()
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			return lastErr
		}

		cb.RecordSuccess()
		return nil
	})

	if err != nil {
		return lastBody, lastStatus, lastErr
	}
	return lastBody, lastStatus, nil
}
