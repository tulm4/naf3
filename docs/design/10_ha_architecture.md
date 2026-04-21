---
spec: ETSI GS NFV-TST 012 / ETSI GS NFV-REL 001 / 3GPP TS 23.501 §5
section: High Availability Architecture
interface: N/A (infrastructure)
service: N/A (HA design)
---

# NSSAAF High Availability Architecture

## 1. Overview

Thiết kế kiến trúc High Availability cho NSSAAF đạt telecom-grade availability (>99.999% uptime / 5 nines). Mô hình stateless microservices với multi-AZ deployment, đảm bảo:
- Horizontal scalability không cần session affinity
- Zone-level failure isolation
- Zero RPO (Recovery Point Objective)
- <30s RTO (Recovery Time Objective)

---

## 2. Architecture Overview

### 2.1 Multi-Layer HA Design

> **Note (Phase R):** After the 3-component refactor, NSSAAF pods are split into three distinct deployments: HTTP Gateway, Biz Pods, and AAA Gateway. HA characteristics differ per component. See `docs/design/01_service_model.md` §5.4 for the full 3-component architecture.

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          LOAD BALANCER (NLB/CLB)                          │
│                    Health check, SSL termination, routing                 │
└──────────────────────────────┬───────────────────────────────────────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
        ▼                      ▼                      ▼
┌───────────────┐      ┌───────────────┐      ┌───────────────┐
│     AZ1       │      │     AZ2       │      │     AZ3       │
│               │      │               │      │               │
│ ┌───────────┐ │      │ ┌───────────┐ │      │ ┌───────────┐ │
│ │ NSSAAF    │ │      │ │ NSSAAF    │ │      │ │ NSSAAF    │ │
│ │ Pod-1     │ │      │ │ Pod-4     │ │      │ │ Pod-7     │ │
│ └───────────┘ │      │ └───────────┘ │      │ └───────────┘ │
│ ┌───────────┐ │      │ ┌───────────┐ │      │ ┌───────────┐ │
│ │ NSSAAF    │ │      │ │ NSSAAF    │ │      │ │ NSSAAF    │ │
│ │ Pod-2     │ │      │ │ Pod-5     │ │      │ │ Pod-8     │ │
│ └───────────┘ │      │ └───────────┘ │      │ └───────────┘ │
│ ┌───────────┐ │      │ ┌───────────┐ │      │ ┌───────────┐ │
│ │ NSSAAF    │ │      │ │ NSSAAF    │ │      │ │ NSSAAF    │ │
│ │ Pod-3     │ │      │ │ Pod-6     │ │      │ │ Pod-9     │ │
│ └───────────┘ │      │ └───────────┘ │      │ └───────────┘ │
└───────────────┘      └───────────────┘      └───────────────┘
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
        ▼                      ▼                      ▼
   ┌─────────┐           ┌─────────┐           ┌─────────┐
   │  PG AZ1 │           │  PG AZ2 │           │  PG AZ3 │
   │ (Leader)│◄──────────►│ (Sync)  │◄──────────►│ (Async) │
   └─────────┘           └─────────┘           └─────────┘
   Patroni/Consul HA     Streaming Repl         Streaming Repl

        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
        ▼                      ▼                      ▼
   ┌─────────┐           ┌─────────┐           ┌─────────┐
   │ Redis-1 │           │ Redis-2 │           │ Redis-3 │
   │(Master) │◄──────────►│(Replica)│◄──────────►│(Replica)│
   └─────────┘           └─────────┘           └─────────┘
   (Shard 0)            (Shard 0)             (Shard 1)
```

**HA characteristics by component (Phase R):**

| Component | Replicas | HA Mechanism | Scalable? |
|---|---|---|---|
| HTTP Gateway | N (stateless) | LoadBalancer, any pod handles any request | Yes, horizontally |
| Biz Pod | N (stateless) | No session affinity; state in Redis/PG | Yes, horizontally |
| AAA Gateway | 2 (active-standby) | keepalived VRRP + VIP | **No — hard limit of 2** |

### 2.2 Stateless Design Principle

> **Note (Phase R):** In the 3-component model, both HTTP Gateway and Biz Pod are stateless. Only the AAA Gateway maintains in-process state (pending response channels), but all state needed for routing is in Redis.

**Biz Pod — fully stateless:**
```
┌──────────────────────────────────────────────────────────────┐
│                    Biz Pod                                      │
│                                                              │
│  ┌─────────────┐                                            │
│  │  HTTP      │ ─── mTLS inbound (from HTTP Gateway)       │
│  │  Handler    │                                            │
│  └──────┬──────┘                                            │
│         │                                                     │
│  ┌──────▼──────┐                                            │
│  │ EAP Engine  │ ─── No local state                        │
│  │             │     No connection pools                     │
│  │             │     No session cache                        │
│  └──────┬──────┘                                            │
│         │                                                     │
│  ┌──────▼──────┐                                            │
│  │ httpAAAClient│ ─── HTTP to AAA Gateway (internal only)   │
│  │             │     No direct external connectivity        │
│  └─────────────┘                                            │
└──────────────────────────────────────────────────────────────┘
```

**HTTP Gateway — stateless TLS terminator:**
```
┌──────────────────────────────────────────────────────────────┐
│                    HTTP Gateway                                │
│                                                              │
│  ┌─────────────┐                                            │
│  │  TLS 1.3   │ ─── TLS termination                        │
│  │  Terminator │                                            │
│  └──────┬──────┘                                            │
│         │                                                     │
│  ┌──────▼──────┐                                            │
│  │ HTTP Router │ ─── No session state                       │
│  │             │     Routes to Biz Pods via ClusterIP        │
│  └─────────────┘                                            │
└──────────────────────────────────────────────────────────────┘
```

**Principle:** All persistent state resides in PostgreSQL and Redis external to pods. Any Biz Pod or HTTP Gateway pod can handle any request.

**Nguyên tắc:** Mọi trạng thái nằm trong PostgreSQL và Redis bên ngoài pod. NSSAAF pods hoàn toàn stateless — bất kỳ pod nào có thể xử lý bất kỳ request nào.

---

## 3. Kubernetes Deployment Design

### 3.1 Pod Distribution

> **Note (Phase R):** The monolithic NSSAAF deployment is replaced by three separate Deployments. The YAML below applies to Biz Pods only. HTTP Gateway and AAA Gateway each have their own Deployment manifests. See `docs/design/01_service_model.md` §5.4.7.

```yaml
# Biz Pod Deployment with anti-affinity across AZs
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nssAAF-biz
  labels:
    app: nssAAF-biz
    tier: network-function
    component: biz  # distinguishes from http-gateway and aaa-gateway
spec:
  replicas: 9  # 3 pods per AZ minimum
  selector:
    matchLabels:
      app: nssAAF-biz
  template:
    metadata:
      labels:
        app: nssAAF-biz
        tier: network-function
        component: biz
      # Pod anti-affinity: spread across AZs
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: nssAAF-biz
              topologyKey: topology.kubernetes.io/zone
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app: nssAAF
                topologyKey: kubernetes.io/hostname

      # Security
      securityContext:
        runAsNonRoot: true
        runAsUser: 10000
        fsGroup: 10000
        seccompProfile:
          type: RuntimeDefault

      # Resource limits
      containers:
        - name: nssAAF
          image: nssAAF:1.0.0
          resources:
            requests:
              cpu: "2"
              memory: "4Gi"
            limits:
              cpu: "4"
              memory: "8Gi"

          # Health checks
          livenessProbe:
            httpGet:
              path: /healthz/live
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: /healthz/ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 2
          startupProbe:
            httpGet:
              path: /healthz/startup
              port: 8080
            failureThreshold: 30
            periodSeconds: 10

          ports:
            - name: http
              containerPort: 8080
            - name: grpc
              containerPort: 9090
            - name: metrics
              containerPort: 9091
```

### 3.2 Horizontal Pod Autoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: nssAAF-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: nssAAF
  minReplicas: 3
  maxReplicas: 50
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 60
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 70
    - type: Pods
      pods:
        metric:
          name: nssAAF_eap_active_sessions
        target:
          type: AverageValue
          averageValue: "50000"
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 30
      policies:
        - type: Pods
          value: 5
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300  # 5 min, prevent flapping
      policies:
        - type: Pods
          value: 2
          periodSeconds: 60
```

### 3.3 Pod Disruption Budget

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: nssAAF-pdb
spec:
  minAvailable: 2   # At least 2 pods must remain available
  selector:
    matchLabels:
      app: nssAAF
```

---

## 4. Database HA (Patroni PostgreSQL)

### 4.1 Patroni Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Consul    │     │   Consul    │     │   Consul    │
│   Node 1    │     │   Node 2    │     │   Node 3    │
│   (AZ1)     │     │   (AZ2)     │     │   (AZ3)     │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           │ DCS (Consul)
                           ▼
       ┌───────────────────────────────────────────┐
       │           Patroni Cluster                   │
       │                                             │
       │  ┌─────────┐  ┌─────────┐  ┌─────────┐    │
       │  │  PG AZ1 │  │  PG AZ2 │  │  PG AZ3 │    │
       │  │(Leader) │◄─┤(Sync)  │◄─┤(Async) │    │
       │  │         │──┤Replica │──┤Replica │    │
       │  └─────────┘  └─────────┘  └─────────┘    │
       │                                             │
       │  Patroni manages failover                   │
       │  Consul provides distributed consensus      │
       └───────────────────────────────────────────┘
```

### 4.2 Patroni Configuration

```yaml
# patroni.yml
scope: nssAAF-postgres
namespace: /service/
name: nssAAF-pg-az1

consul:
  host: consul.operator.com:8500
  register_service: true
  service_port: 5432
  service_name: nssAAF-postgres

postgresql:
  listen: 0.0.0.0:5432
  connect_address: pg-az1.operator.com:5432
  data_dir: /data/postgresql
  pgpass: /tmp/pgpass

  authentication:
    replication:
      username: repl_user
      password: "***"
    superuser:
      username: postgres
      password: "***"

  create_replica_methods:
    - wal_e
  wal_e:
    command: /usr/local/bin/wal-e replication create %r
    keep_history: 10

  recovery_conf:
    restore_command: /usr/local/bin/wal-e wal-fetch "%f" "%p"

  parameters:
    max_connections: 500
    max_worker_processes: 8
    shared_buffers: 8GB
    effective_cache_size: 24GB
    maintenance_work_mem: 2GB
    checkpoint_timeout: 10min
    wal_buffers: 64MB
    default_statistics_target: 100
    random_page_cost: 1.1
    effective_io_concurrency: 200
    work_mem: 16MB
    min_wal_size: 2GB
    max_wal_size: 8GB

  replication:
    maximum_backup_epoch: 10
    basebackup_parallel: 4

restapi:
  listen: 0.0.0.0:8008
  connect_address: pg-az1.operator.com:8008
```

### 4.3 Replication Configuration

```sql
-- Streaming replication for high availability
-- Synchronous on AZ1-AZ2, asynchronous to AZ3

-- On primary:
ALTER SYSTEM SET synchronous_commit = on;
ALTER SYSTEM SET synchronous_standby_names = 'ANY 1 (nssAAF-pg-az2)';

-- Recovery configuration for async replica (AZ3)
ALTER SYSTEM SET synchronous_commit = 'remote_write';
ALTER SYSTEM SET max_wal_senders = 10;
ALTER SYSTEM SET max_replication_slots = 10;
ALTER SYSTEM SET wal_level = replica;
ALTER SYSTEM SET hot_standby = on;
```

---

## 5. Redis Cluster HA

### 5.1 Cluster Architecture

```
Redis Cluster: 3 shards × 2 replicas = 6 nodes minimum

Shard 0: 10.0.1.10:6379 (master, AZ1) ←→ 10.0.2.10:6379 (replica, AZ2)
Shard 1: 10.0.2.11:6379 (master, AZ2) ←→ 10.0.3.10:6379 (replica, AZ3)
Shard 2: 10.0.3.11:6379 (master, AZ3) ←→ 10.0.1.11:6379 (replica, AZ1)

Quorum: 2/3 nodes per shard
Automatic failover: yes
Read scaling: yes (replicas)
```

### 5.2 Redis Sentinel (alternative to Cluster)

```yaml
# sentinel.conf for NSSAAF cache
sentinel monitor nssAAF-cache pg1.operator.com 6379 2
sentinel down-after-milliseconds nssAAF-cache 5000
sentinel parallel-syncs nssAAF-cache 1
sentinel failover-timeout nssAAF-cache 60000
sentinel auth-pass nssAAF-cache "***"
```

---

## 6. Network HA

### 6.1 Istio Service Mesh

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: nssAAF-dr
spec:
  host: nssAAF
  trafficPolicy:
    connectionPool:
      tcp:
        maxConnections: 1000
      http:
        h2UpgradePolicy: UPGRADE
        http1MaxPendingRequests: 1000
        http2MaxRequests: 1000
        idleTimeout: 3600s
    loadBalancer:
      simple: LEAST_REQUEST
      consistentHash:
        httpHeaderName: X-Request-ID
    outlierDetection:
      consecutive5xxErrors: 5
      interval: 30s
      baseEjectionTime: 30s
      maxEjectionPercent: 50
      minHealthPercent: 50
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: nssAAF-vs
spec:
  hosts:
    - nssAAF
  http:
    - route:
        - destination:
            host: nssAAF
          weight: 100
      retries:
        attempts: 3
        perTryTimeout: 5s
        retryOn: 5xx,reset,connect-failure
      timeout: 30s
```

### 6.2 Circuit Breaker per Pod

Outlier detection tách biệt các pod có vấn đề:
- 5xx liên tục → đẩy pod ra khỏi load balancer pool
- Recovery sau 30s base ejection time
- Tối đa 50% pods bị eject

---

## 7. Failure Scenarios

### 7.1 Failure Matrix

> **Note (Phase R):** Failures are scoped per component. Biz Pod and HTTP Gateway failures are recovered by Kubernetes (HPA/PDB). AAA Gateway failures require keepalived failover within seconds.

| Failure | Impact | Recovery | RTO |
|---------|--------|----------|-----|
| HTTP Gateway pod dies | Traffic routed to remaining HTTP GW replicas | HPA spawns replacement | ~30s |
| Biz Pod dies | In-flight EAP sessions may fail; HPA spawns replacement | HPA spawns replacement; in-flight sessions retried by AMF | ~30s |
| 1 AZ down (HTTP GW/Biz) | 1/3 capacity lost | HPA scales up remaining AZs | <2 min |
| DB leader fails | Patroni promotes sync replica | Automatic failover | <30s |
| Redis master fails | Cluster promotes replica | Automatic failover | <10s |
| AAA Gateway active dies | AAA-S connection drops; keepalived promotes standby | keepalived VRRP failover | ~1-3s |
| AAA Gateway standby dies | No HA until replaced | Manual intervention or node-level recovery | N/A |
| All Biz Pods fail | Total service outage (SBI unavailable) | Cluster recovery | <5 min |
| All HTTP GW pods fail | Total service outage (SBI unreachable) | Cluster recovery | <5 min |

### 7.2 Graceful Degradation

```go
// When system is degraded, prioritize critical paths
func (h *HealthManager) EvaluateHealth() HealthState {
    dbHealthy := h.checkDB()
    redisHealthy := h.checkRedis()
    nrfHealthy := h.checkNRF()

    switch {
    case !dbHealthy:
        return HEALTH_CRITICAL  // Reject all new requests
    case !redisHealthy:
        return HEALTH_DEGRADED // Serve from DB only (slower)
    case !nrfHealthy:
        return HEALTH_DEGRADED // Use cached NRF data
    default:
        return HEALTH_HEALTHY
    }
}

// Health-aware request handling
func (s *Server) HandleRequest(ctx *Request) *Response {
    health := s.healthManager.EvaluateHealth()

    switch health {
    case HEALTH_CRITICAL:
        return &Response{
            Status: 503,
            Body:   "Service temporarily unavailable",
            Retry:  false,
        }
    case HEALTH_DEGRADED:
        // Allow with warnings
        ctx.SetWarning("System operating in degraded mode")
    }
    // ... normal processing
}
```

---

## 8. Acceptance Criteria

> **Note (Phase R):** These criteria apply to the 3-component model. Biz Pod and HTTP Gateway are stateless and scale horizontally. AAA Gateway is stateful (active-standby) with a hard limit of 2 replicas.

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | Stateless Biz Pod: no local session state | All state in PostgreSQL + Redis |
| AC2 | Stateless HTTP Gateway: no session state | Routes all requests to Biz Pods |
| AC3 | 3 AZ deployment with anti-affinity | PodAntiAffinity, requiredDuringScheduling |
| AC4 | HPA for Biz Pods: 3-50 replicas, scale on CPU/memory/sessions | HorizontalPodAutoscaler v2 |
| AC5 | HPA for HTTP Gateway: N replicas, scale on CPU/memory | HorizontalPodAutoscaler v2 |
| AC6 | PDB: maxUnavailable=1, minAvailable=2 per deployment | PodDisruptionBudget |
| AC7 | AAA Gateway: exactly 2 replicas, active-standby | keepalived VRRP, strategy=Recreate |
| AC8 | Patroni HA: leader + 2 standbys, automatic failover | Consul DCS, streaming replication |
| AC9 | Redis Cluster: 3 shards, automatic failover | Cluster mode, quorum 2/3 |
| AC10 | Circuit breaker per AAA server | CLOSED/OPEN/HALF_OPEN |
| AC11 | RTO < 30s for single AZ failure (Biz/HTTP GW) | Patroni automatic failover + HPA |
| AC12 | RTO < 3s for AAA Gateway active failure | keepalived VRRP failover |
| AC13 | RPO = 0 (sync replication to 1 standby) | Synchronous streaming replication |
| AC14 | Graceful degradation: reject when DB unhealthy | HealthManager with HealthState |
