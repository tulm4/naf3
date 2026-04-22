---
spec: Redis Cluster / Redis Sentinel / Redis 7.x
section: High Availability Cache
interface: N/A (infrastructure)
service: Cache HA
---

# NSSAAF Redis High Availability Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, Redis is used for **cross-component session correlation** in addition to session caching. The AAA Gateway writes session correlation keys (`nssaa:session:{sessionId}`) to Redis; Biz Pods subscribe to `nssaa:aaa-response` pub/sub to receive AAA responses. See `docs/design/01_service_model.md` §5.4.6 for the internal communication design.

Thiết kế Redis Cluster cho NSSAAF session caching và rate limiting — đảm bảo:
- **Sub-millisecond latency** (<1ms P99)
- **Automatic failover** với cluster mode
- **Horizontal scaling** với data sharding
- **>500,000 ops/sec** per shard

---

## 2. Redis Cluster Architecture

### 2.1 Cluster Topology

```
Redis Cluster: 3 shards × 2 replicas = 6 nodes minimum

Shard 0:
  10.0.1.10:6379 (master, AZ1) ←→ 10.0.2.10:6379 (replica, AZ2)

Shard 1:
  10.0.2.11:6379 (master, AZ2) ←→ 10.0.3.11:6379 (replica, AZ3)

Shard 2:
  10.0.3.10:6379 (master, AZ3) ←→ 10.0.1.11:6379 (replica, AZ1)

Quorum: 2/3 nodes per shard must be available
Automatic failover: Yes
Read scaling: Yes (replicas)
Write consistency: ONE (configurable: ONE/QUORUM/MAJORITY)
```

### 2.2 Cluster Slots

```
16384 total slots distributed across 3 shards:
  Shard 0: slots 0-5460      (0.0% - 33.3%)
  Shard 1: slots 5461-10922  (33.3% - 66.6%)
  Shard 2: slots 10923-16383 (66.6% - 100%)

Key distribution:
  Key = "nssaa:session:{authCtxId}"
  Slot = CRC16(key) mod 16384
```

---

## 3. Redis Configuration

### 3.1 Master Node Configuration

```conf
# redis.conf for NSSAAF master node
bind 0.0.0.0
port 6379

# Memory
maxmemory 8gb
maxmemory-policy allkeys-lru
maxmemory-samples 5

# Persistence
save 300 1
save 60 10000
save 30 100000
stop-writes-on-bgsave-error yes
rdbcompression yes
rdbchecksum yes

# AOF (append-only file)
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec
no-appendfsync-on-rewrite no
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 64mb

# Replication
replica-read-only yes
replica-serve-stale-data yes
repl-diskless-sync yes
repl-diskless-sync-delay 5
repl-disable-tcp-nodelay no

# Cluster
cluster-enabled yes
cluster-config-file nodes.conf
cluster-node-timeout 15000
cluster-replica-validity-factor 10
cluster-migration-barrier 1
cluster-require-full-coverage yes

# Performance
tcp-backlog 511
timeout 0
tcp-keepalive 300
hz 10
dynamic-hz yes
lazyfree-lazy-eviction yes
lazyfree-lazy-expire yes
lazyfree-lazy-server-del yes
replica-lazy-flush yes

# Security
protected-mode yes
requirepass "${REDIS_PASSWORD}"
rename-command CONFIG ""
rename-command FLUSHDB ""
rename-command FLUSHALL ""
```

### 3.2 Cluster Node Initialization

```bash
# Initialize cluster with 3 masters and replicas
redis-cli --cluster create \
  10.0.1.10:6379 \
  10.0.2.11:6379 \
  10.0.3.10:6379 \
  10.0.2.10:6379 \
  10.0.3.11:6379 \
  10.0.1.11:6379 \
  --cluster-replicas 1 \
  --cluster-yes

# Verify cluster
redis-cli -h 10.0.1.10 cluster info
redis-cli -h 10.0.1.10 cluster nodes
redis-cli -h 10.0.1.10 --cluster check-all-nodes
```

---

## 4. Data Structures for NSSAAF

### 4.1 Session Cache

```go
// Key: nssaa:session:{authCtxId}
// Type: Hash
// TTL: 300s (5 minutes)

type SessionCache struct {
    key   string
    field string
}

// HSET nssaa:session:auth123 gpsi "5-208046000000001" snssai "1:000001" status "PENDING"
func (r *RedisCluster) SetSession(ctx context.Context, session *Session) error {
    key := fmt.Sprintf("nssaa:session:%s", session.AuthCtxId)

    pipe := r.client.Pipeline()
    pipe.HSet(ctx, key,
        "gpsi", session.Gpsi,
        "snssai", fmt.Sprintf("%d:%s", session.Snssai.Sst, session.Snssai.Sd),
        "status", string(session.Status),
        "aaaConfigId", session.AaaConfigId.String(),
        "eapRounds", strconv.Itoa(session.EapRounds),
        "updatedAt", time.Now().Format(time.RFC3339),
    )
    pipe.Expire(ctx, key, 5*time.Minute)

    _, err := pipe.Exec(ctx)
    return err
}

// HGETALL nssaa:session:auth123
func (r *RedisCluster) GetSession(ctx context.Context, authCtxId string) (*Session, error) {
    key := fmt.Sprintf("nssaa:session:%s", authCtxId)

    result, err := r.client.HGetAll(ctx, key).Result()
    if err != nil {
        return nil, err
    }

    if len(result) == 0 {
        return nil, ErrSessionNotFound
    }

    return parseSession(result)
}
```

### 4.2 Idempotency Cache

```go
// Key: nssaa:idempotency:{authCtxId}:{msgHash}
// Type: String (JSON)
// TTL: 3600s (1 hour)

func (r *RedisCluster) CheckIdempotency(ctx context.Context, authCtxId, msgHash string) (*CachedResponse, error) {
    key := fmt.Sprintf("nssaa:idempotency:%s:%s", authCtxId, msgHash)

    data, err := r.client.Get(ctx, key).Result()
    if err == redis.Nil {
        return nil, nil  // Not found = not duplicate
    }
    if err != nil {
        return nil, err
    }

    var resp CachedResponse
    json.Unmarshal([]byte(data), &resp)
    return &resp, nil
}

func (r *RedisCluster) SetIdempotency(ctx context.Context, authCtxId, msgHash string, resp *CachedResponse) error {
    key := fmt.Sprintf("nssaa:idempotency:%s:%s", authCtxId, msgHash)

    data, _ := json.Marshal(resp)
    return r.client.Set(ctx, key, data, time.Hour).Err()
}
```

### 4.3 Rate Limiting

```go
// Sliding window rate limiting
// Key: nssaa:ratelimit:{type}:{id}
// Type: String (counter)
// TTL: window duration

func (r *RedisCluster) RateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
    lua := `
        local current = redis.call('INCR', KEYS[1])
        if current == 1 then
            redis.call('EXPIRE', KEYS[1], ARGV[1])
        end
        return current
    `

    result, err := r.client.Eval(ctx, lua, []string{key},
        int(window.Seconds())).Int()

    if err != nil {
        return false, 0, err
    }

    allowed := result <= limit
    remaining := limit - result
    if remaining < 0 {
        remaining = 0
    }

    return allowed, remaining, nil
}

// Usage:
allowed, remaining, err := redis.RateLimit(
    ctx,
    "nssaa:ratelimit:gpsi:abc123",
    10,              // 10 requests
    time.Minute,     // per minute
)
```

### 4.4 Distributed Locking

```go
// For EAP round coordination
// Key: nssaa:lock:session:{authCtxId}
// Type: String with NX + EX

func (r *RedisCluster) AcquireLock(ctx context.Context, authCtxId string, ttl time.Duration) (bool, error) {
    key := fmt.Sprintf("nssaa:lock:session:%s", authCtxId)

    // SET nssaa:lock:session:auth123 {holder} NX EX 30
    success, err := r.client.SetNX(ctx, key, os.Getenv("POD_NAME"), ttl).Result()
    if err != nil {
        return false, err
    }

    return success, nil
}

func (r *RedisCluster) ReleaseLock(ctx context.Context, authCtxId string) error {
    key := fmt.Sprintf("nssaa:lock:session:%s", authCtxId)

    // Lua script: only delete if we own the lock
    lua := `
        if redis.call('GET', KEYS[1]) == ARGV[1] then
            return redis.call('DEL', KEYS[1])
        else
            return 0
        end
    `

    _, err := r.client.Eval(ctx, lua, []string{key}, os.Getenv("POD_NAME")).Result()
    return err
}
```

### 4.5 Cross-Component Session Correlation (Phase R)

> **Note (Phase R):** These keys are used in the 3-component model for communication between AAA Gateway and Biz Pods. See `01_service_model.md` §5.4.6.

In the 3-component model, the AAA Gateway needs to correlate raw RADIUS/Diameter transactions with Biz Pod sessions, and Biz Pods need to receive AAA responses asynchronously:

```go
// 4.5.1 Session Correlation (written by AAA Gateway, read by Biz Pods)
// Key: nssaa:session:{transactionId}  (transactionId from RADIUS/Diameter)
// Type: Hash
// TTL: 300s (5 minutes)
// Written by: AAA Gateway (before forwarding to AAA-S)
// Read by: Biz Pods (via pub/sub event)

func (r *RedisCluster) SetSessionCorrelation(ctx context.Context, txId, authCtxId string, configId string) error {
    key := fmt.Sprintf("nssaa:session:%s", txId)
    pipe := r.client.Pipeline()
    pipe.HSet(ctx, key,
        "authCtxId", authCtxId,
        "configId", configId,
        "createdAt", time.Now().Format(time.RFC3339),
    )
    pipe.Expire(ctx, key, 5*time.Minute)
    _, err := pipe.Exec(ctx)
    return err
}

// 4.5.2 Biz Pod Registry (written/read by Biz Pods + HTTP Gateway)
// Key: nssaa:pods
// Type: Set
// TTL: none (persistent, updated on heartbeat)
// Written by: Biz Pods (on startup, on heartbeat)
// Used by: AAA Gateway (to know which pods to send server-initiated messages to)

func (r *RedisCluster) RegisterBizPod(ctx context.Context, podName, podIP string) error {
    key := "nssaa:pods"
    member := fmt.Sprintf("%s:%s", podName, podIP)
    pipe := r.client.Pipeline()
    pipe.SAdd(ctx, key, member)
    // Expire old entries that haven't heartbeat'd in 2 minutes
    pipe.ZAdd(ctx, "nssaa:pods:heartbeat", redis.Z{
        Score:  float64(time.Now().Unix()),
        Member: member,
    })
    _, err := pipe.Exec(ctx)
    return err
}

// 4.5.3 AAA Response Pub/Sub (published by AAA Gateway, subscribed by Biz Pods)
// Channel: nssaa:aaa-response
// Published by: AAA Gateway (on receiving response from AAA-S)
// Subscribed by: All Biz Pods (each discards events not matching its in-flight sessions)

func (r *RedisCluster) PublishAAAResponse(ctx context.Context, txId string, rawPacket []byte) error {
    channel := "nssaa:aaa-response"
    event := map[string]interface{}{
        "txId":      txId,
        "rawPacket": rawPacket,
        "receivedAt": time.Now().Format(time.RFC3339Nano),
    }
    data, _ := json.Marshal(event)
    return r.client.Publish(ctx, channel, data).Err()
}

func (r *RedisCluster) SubscribeAAAResponses(ctx context.Context) *redis.PubSub {
    return r.client.Subscribe(ctx, "nssaa:aaa-response")
}

// 4.5.4 Server-Initiated Queue (written by AAA Gateway, read by Biz Pods)
// Key: nssaa:server-initiated:{podName}
// Type: List (FIFO queue)
// Written by: AAA Gateway (on receiving server-initiated request from AAA-S)
// Read by: Target Biz Pod (dequeues via BLPOP)

func (r *RedisCluster) EnqueueServerInitiated(ctx context.Context, podName string, event *ServerInitiatedEvent) error {
    key := fmt.Sprintf("nssaa:server-initiated:%s", podName)
    data, _ := json.Marshal(event)
    return r.client.RPush(ctx, key, data).Err()
}
```

---

## 5. Redis Sentinel (Alternative)

### 5.1 Sentinel Configuration

```conf
# sentinel.conf
port 26379
bind 0.0.0.0

# Master monitoring
sentinel monitor nssAAF-cache 10.0.1.10 6379 2
sentinel down-after-milliseconds nssAAF-cache 5000
sentinel parallel-syncs nssAAF-cache 1
sentinel failover-timeout nssAAF-cache 60000

# Authentication
sentinel auth-pass nssAAF-cache "${REDIS_PASSWORD}"

# Sentinel quorum
# 2 of 3 sentinels must agree for failover

# Auto-discovery
sentinel known-replica nssAAF-cache 10.0.2.10 6379
sentinel known-replica nssAAF-cache 10.0.3.10 6379
```

### 5.2 Sentinel HA Deployment

```yaml
# Kubernetes Sentinel deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis-sentinel
spec:
  replicas: 3  # One per AZ
  selector:
    matchLabels:
      app: redis-sentinel
  template:
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: redis-sentinel
              topologyKey: topology.kubernetes.io/zone
      containers:
        - name: sentinel
          image: redis:7-alpine
          command:
            - redis-sentinel
            - /usr/local/etc/redis/sentinel.conf
          ports:
            - containerPort: 26379
          volumeMounts:
            - name: config
              mountPath: /usr/local/etc/redis
      volumes:
        - name: config
          configMap:
            name: redis-sentinel-config
```

---

## 6. Cluster Management

### 6.1 Health Monitoring

```go
// Cluster health check
func (r *RedisCluster) HealthCheck(ctx context.Context) error {
    // Check all masters
    masters, err := r.client.ClusterMasters(ctx).Result()
    if err != nil {
        return fmt.Errorf("cluster masters failed: %w", err)
    }

    for _, master := range masters {
        // Check replica lag
        info, err := r.client.ClusterInfo(ctx).Result()
        if err != nil {
            return fmt.Errorf("cluster info failed: %w", err)
        }

        // Parse state
        if strings.Contains(info, "cluster_state:fail") {
            return fmt.Errorf("cluster in failed state")
        }
    }

    return nil
}

// Redis Exporter metrics for Prometheus
// scrape config:
# - job_name: redis
#   static_configs:
#     - targets:
#       - redis-cluster-0:9121
#       - redis-cluster-1:9121
#       - redis-cluster-2:9121
```

### 6.2 Failover Handling

```go
// Application handles Redis failover gracefully
type RedisClient struct {
    cluster *redis.ClusterClient
    pool    *redis.Pool  // Fallback to single node
}

func (c *RedisClient) Get(ctx context.Context, key string) (string, error) {
    result, err := c.cluster.Get(ctx, key).Result()
    if err == nil {
        return result, nil
    }

    // Try pool fallback
    if err == redis.Nil {
        return "", redis.Nil
    }

    // Cluster unavailable, try pool
    result, err = c.pool.Get(ctx, key).Result()
    if err != nil {
        return "", fmt.Errorf("cluster and pool failed: %w", err)
    }

    // Refresh cluster slots
    go c.cluster.RefreshCluster()
    return result, nil
}
```

---

## 7. Performance Tuning

### 7.1 Benchmarks

```bash
# redis-benchmark results target
redis-benchmark -h 10.0.1.10 -p 6379 -t set,get -c 100 -n 1000000

# Expected results:
# SET: >200,000 ops/sec
# GET: >250,000 ops/sec
# MSET: >150,000 ops/sec
# MGET: >200,000 ops/sec
```

### 7.2 Connection Pool

```go
// Connection pool configuration
clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
    Addrs: []string{
        "10.0.1.10:6379",
        "10.0.2.11:6379",
        "10.0.3.10:6379",
    },

    // Pool config per node
    PoolSize:     50,        // connections per node
    MinIdleConns: 10,        // keep-alive connections
    MaxRetries:   3,
    PoolTimeout:  5 * time.Second,

    // Timeouts
    ReadTimeout:  1 * time.Millisecond,
    WriteTimeout: 1 * time.Millisecond,

    // TLS (if needed)
    TLSConfig: &tls.Config{
        MinVersion: tls.VersionTLS13,
    },

    // Route
    RouteRandomly:  false,
    RouteByLatency: true,  // Route to nearest node
})
```

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | Redis Cluster with 3 shards × 2 replicas | Shards across 3 AZs |
| AC2 | Automatic failover on master failure | Cluster node-timeout=15s |
| AC3 | Session cache with 5-min TTL | nssaa:session:{id} hash |
| AC4 | Idempotency cache with 1-hour TTL | nssaa:idempotency:{id}:{hash} |
| AC5 | Sliding window rate limiting | Lua script with INCR+EXPIRE |
| AC6 | Distributed locking for EAP coordination | SETNX with owner check |
| AC7 | >500,000 ops/sec per shard | redis-benchmark validation |
| AC8 | <1ms P99 latency | Connection pool, local AZ routing |
