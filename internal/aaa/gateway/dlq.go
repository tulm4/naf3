// Package gateway provides the AAA Gateway component.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/operator/nssAAF/internal/proto"
	"github.com/redis/go-redis/v9"
)

const (
	dlqRetryBaseDelay = 1 * time.Second
)

// DLQMessage represents a message in the server-initiated DLQ.
type DLQMessage struct {
	SessionID     string `json:"sessionID"`
	TransportType string `json:"transportType"`
	MessageType   string `json:"messageType"`
	Payload       []byte `json:"payload"`
	AttemptCount  int    `json:"attemptCount"`
	QueuedAt      int64  `json:"queuedAt"`
}

// runDLQConsumer processes messages from the DLQ list.
// It polls every cfg.DLQ.PollInterval, retries up to cfg.DLQ.MaxRetries, and discards after exhaustion.
func (g *Gateway) runDLQConsumer(ctx context.Context) {
	ticker := time.NewTicker(g.cfg.DLQ.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.processDLQOne(ctx)
		}
	}
}

// processDLQOne pops and processes one DLQ message. Non-blocking.
func (g *Gateway) processDLQOne(ctx context.Context) {
	result, err := g.redis.BRPop(ctx, 1*time.Second, proto.DLQKey).Result()
	if err != nil {
		if err == redis.Nil {
			return // No message available
		}
		g.logger.Warn("DLQ BRPOP failed", "error", err)
		return
	}
	if len(result) < 2 {
		return
	}

	var msg DLQMessage
	if err := json.Unmarshal([]byte(result[1]), &msg); err != nil {
		g.logger.Error("failed to unmarshal DLQ message", "error", err)
		return
	}

	g.processDLQMessage(ctx, &msg)
}

// processDLQMessage retries a single DLQ message.
func (g *Gateway) processDLQMessage(ctx context.Context, msg *DLQMessage) {
	if msg.AttemptCount >= g.cfg.DLQ.MaxRetries {
		g.logger.Error("server_initiated_dlq_exhausted",
			"session_id", msg.SessionID,
			"message_type", msg.MessageType,
			"attempts", msg.AttemptCount,
			"queued_at", msg.QueuedAt,
		)
		// TODO: fire alert metric here
		return
	}

	targetURL, err := g.selectTargetBizURL(ctx, "")
	if err != nil || targetURL == "" {
		g.logger.Warn("DLQ: selectTargetBizURL failed, requeueing", "error", err)
		g.requeueDLQ(ctx, msg)
		return
	}

	body := append([]byte(nil), msg.Payload...)
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		targetURL+"/aaa/server-initiated", bytes.NewReader(body))
	if err != nil {
		g.requeueDLQ(ctx, msg)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(proto.HeaderName, g.version)

	resp, err := g.bizHTTPClient.Do(httpReq)
	if err != nil {
		g.logger.Warn("DLQ: HTTP call failed, requeueing",
			"error", err, "session_id", msg.SessionID, "target_url", targetURL)
		g.requeueDLQ(ctx, msg)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		g.logger.Info("DLQ: message delivered successfully",
			"session_id", msg.SessionID, "target_url", targetURL)
		return
	}
	g.logger.Warn("DLQ: non-OK response, requeueing",
		"status", resp.StatusCode, "session_id", msg.SessionID)
	g.requeueDLQ(ctx, msg)
}

// requeueDLQ pushes the message back to the DLQ with incremented attempt count.
func (g *Gateway) requeueDLQ(ctx context.Context, msg *DLQMessage) {
	msg.AttemptCount++
	data, err := json.Marshal(msg)
	if err != nil {
		g.logger.Error("failed to marshal DLQ message for requeue", "error", err)
		return
	}
	if err := g.redis.RPush(ctx, proto.DLQKey, data).Err(); err != nil {
		g.logger.Error("failed to requeue DLQ message", "error", err)
	}
}
