# NSSAAF Project Design Roadmap
## Mục tiêu: Telecom-Grade NSSAAF (Ericsson/Nokia Class)

**Phạm vi:** Thiết kế toàn bộ hệ thống NSSAAF cho 5G Network Slice-Specific Authentication & Authorization Function.

**Tiêu chuẩn áp dụng:**
- 3GPP Release 18 (TS 23.502, 29.526, 33.501, 29.561, 29.571, 28.541)
- 3GPP SA3 Security (TS 33.501)
- ETSI NFV SEC (SEC 021, SEC 013)
- IETF EAP (RFC 3748), EAP-TLS (RFC 5216), RADIUS (RFC 2865/3579), Diameter (RFC 4072/7155)
- Kubernetes电信级部署标准 (高可用、性能、故障隔离)

---

## Tổng quan kiến trúc

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            OPERATOR DOMAIN                               │
│                                                                          │
│  ┌──────────┐    ┌──────────────┐    ┌─────────────┐                   │
│  │  OAM/OSS │    │     NRF      │    │    UDM      │                   │
│  │(FCAPS)   │    │(Discovery)   │    │(UECM_Get)   │                   │
│  └────┬─────┘    └──────┬───────┘    └──────┬──────┘                   │
│       │                  │                   │                          │
│       └──────────────────┼───────────────────┘                          │
│                          │                                              │
│                          ▼                                              │
│  ┌─────────────────────────────────────────────────────┐               │
│  │              NSSAAF 3-COMPONENT CLUSTER               │               │
│  │                                                        │               │
│  │  ┌───────────────────────────────────────────────┐    │               │
│  │  │    HTTP Gateway (N replicas, Envoy)           │    │               │
│  │  │    TLS 1.3 termination, routes to Biz Pods  │    │               │
│  │  │    Binds to pod IP on external interface     │    │               │
│  │  └───────────────────────────────────────────────┘    │               │
│  │                          │                               │               │
│  │                          ▼                               │               │
│  │  ┌───────────────────────────────────────────────┐    │               │
│  │  │    Biz Pods (N replicas, stateless)          │    │               │
│  │  │    SBI Handlers · EAP Engine · Session State │    │               │
│  │  │    RADIUS/Diameter encode/decode              │    │               │
│  │  │    Communicates with AAA GW via HTTP          │    │               │
│  │  └───────────────────────────────────────────────┘    │               │
│  │                          │                               │               │
│  │                          ▼                               │               │
│  │  ┌───────────────────────────────────────────────┐    │               │
│  │  │    AAA Gateway (2 replicas: active+standby)  │    │               │
│  │  │    keepalived VIP (Multus CNI bridge VLAN)   │    │               │
│  │  │    FORWARDS raw transport — no encode/decode│    │               │
│  │  └───────────────────────────────────────────────┘    │               │
│  │                          │                               │               │
│  └──────────────────────────┼───────────────────────────────┘               │
│                             │                                              │
│                             ▼                                              │
│  ┌─────────────────────────────────────────────────────┐               │
│  │              THIRD-PARTY / H-PLMN                   │               │
│  │   ┌───────────┐  ┌───────────┐  ┌───────────┐      │               │
│  │   │  AAA-S    │  │  AAA-S    │  │ NSS-AAA   │      │               │
│  │   │(Enterprise │  │(Enterprise │  │ (Third-   │      │               │
│  │   │ Operator)  │  │ Operator)  │  │  Party)   │      │               │
│  │   └───────────┘  └───────────┘  └───────────┘      │               │
│  └─────────────────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────────────────┘
See docs/design/01_service_model.md §5.4 for full deployment details.
```

---

## PHASE 1: Foundation & Core API

### 1.1 NSSAAF Service Model Design
```
Mục tiêu: Xác định NSSAAF như một microservice trong 5G SBA

Nội dung:
- NSSAAF trong 5G Service-Based Architecture (TS 23.501 §6.3.17)
- NSSAAF vs các NF khác: AMF, AUSF, UDM, NRF
- SBA Interface: N58 (Nnssaaf_NSSAA), N60 (Nnssaaf_AIW), N59 (Nudm_UECM_Get)
- NF Profile đăng ký với NRF
- 3-component architecture: HTTP Gateway + Biz Pods + AAA Gateway (see docs/design/01_service_model.md §5.4)
- Multi-tenancy: PLMN isolation, slice isolation
- Release version: 18.7.0
```

### 1.2 Nnssaaf_NSSAA API Design
```
Mục tiêu: Thiết kế RESTful SBI API cho NSSAA service theo TS 29.526

Nội dung:
- OpenAPI 3.0 spec (dựa trên TS29526_Nnssaaf_NSSAA.yaml)
  · POST /slice-authentications — tạo context
  · PUT  /slice-authentications/{authCtxId} — confirm/advance
- Schema design:
  · SliceAuthInfo, SliceAuthContext
  · SliceAuthConfirmationData, SliceAuthConfirmationResponse
  · SliceAuthReauthNotification, SliceAuthRevocNotification
- Error model: ProblemDetails (TS 29.501)
  · 400, 403, 404, 502, 503, 504
- Pagination: không cần (resource-oriented, per-session)
- Idempotency: authCtxId as idempotency key
- Versioning: URI-based (/v1/)
```

### 1.3 Nnssaaf_AIW API Design
```
Mục tiêu: Thiết kế API cho SNPN Credentials Holder authentication

Nội dung:
- TS 29.526 §7.3 — Nnssaaf_AIW service
- POST /authentications — create context (SUPI-based, not GPSI)
- PUT  /authentications/{authCtxId} — confirm
- Schema: AuthInfo, AuthContext, AuthConfirmationData, AuthConfirmationResponse
- Khác biệt với NSSAA:
  · Dùng SUPI thay vì GPSI
  · Consumer là AUSF thay vì AMF
  · Có thêm MSK output (Master Session Key)
  · pvsInfo (Privacy Violating Servers) response
```

### 1.4 Data Model & Persistence Design
```
Mục tiêu: Thiết kế database schema cho NSSAAF state

Nội dung:
- Entity: SliceAuthContext
  · authCtxId (PK, UUID v7 cho sortability)
  · gpsi, snssai, amfInstanceId
  · eapSessionState (serialized)
  · nssaaStatus (NOT_EXECUTED/PENDING/EAP_SUCCESS/EAP_FAILURE)
  · aaaServerAddress (per-S-NSSAI config)
  · createdAt, updatedAt, expiresAt
- Entity: AaaServerConfig
  · snssai (PK compound)
  · protocol (RADIUS/DIAMETER)
  · host, port, sharedSecret (encrypted at rest)
  · aaaProxyAddress (optional)
  · priority, weight (load balancing)
- Entity: AuthSessionAuditLog
  · Immutable append-only log
  · Full EAP message trace (encrypted payload)
- Database: PostgreSQL 16+
  · Partitioning: by snssai (range) for horizontal scaling
  · Index: (gpsi, snssai), (authCtxId), (nssaaStatus)
  · Connection pooling: PgBouncer (transaction mode)
  · High availability: Patroni (consul-based) + streaming replication
- Cache: Redis Cluster
  · Short-lived session state cache
  · AAA server health status
  · TTL: 5 phút (configurable)
```

---

## PHASE 2: EAP & AAA Protocol Handling

### 2.1 EAP Framework Design
```
Mục tiêu: Thiết kế EAP message handling engine (multi-method)

Nội dung:
- EAP Framework (RFC 3748):
  · EAP Request/Response relay engine
  · Multi-round support (stateful session)
  · EAP method abstraction layer
- Supported EAP Methods:
  · EAP-TLS (RFC 5216) — primary cho enterprise slices
    - TLS 1.3 required
    - Client certificate auth
    - MSK derivation per RFC 5216
  · EAP-TTLS (RFC 5281) — legacy compatibility
  · EAP-AKA' (RFC 5448) — 3GPP native
- EAP Session State Machine:
  · IDLE → INIT → EAP_EXCHANGE → COMPLETING → DONE
  · Timeout handling: 30s per round (configurable)
  · Max rounds: 20 (configurable)
- Performance:
  · Async EAP handling (event-driven, no blocking)
  · Connection pooling for TLS sessions
  · Zero-copy EAP payload forwarding
```

### 2.2 RADIUS Client Design
```
Mục tiêu: High-performance RADIUS client trong NSSAAF

Nội dung:
- RFC 2865/3162/3579 compliance:
  · Access-Request / Access-Challenge / Access-Accept / Access-Reject
  · Disconnect-Request / Disconnect-ACK/NAK (RFC 5176)
- RADIUS Attribute handling:
  · User-Name / Calling-Station-Id → GPSI mapping
  · 3GPP-S-NSSAI (VSA Sub-attr #200):
    - Type=200, Length=3(SST) or 6(SST+SD)
    - SST: 0-255, SD: 3-byte hex
  · EAP-Message (RFC 3579)
  · Message-Authenticator
- Transport:
  · Primary: UDP (RFC 2865) — low latency
  · DTLS (RFC 4818) — for untrusted networks
  · Connection: multi-threaded async I/O (io_uring / epoll)
- Reliability:
  · Retransmission: exponential backoff (1s, 2s, 4s, 8s, max 30s)
  · Client-side request timeout: 10s (configurable)
  · Duplicate detection via Request Authenticator
- Performance targets:
  · >50,000 RADIUS Access-Request/s per instance
  · <5ms P99 latency (NSSAAF → AAA-S)
  · Multi-home: source address binding per PLMN
```

### 2.3 Diameter Client Design
```
Mục tiêu: Diameter agent trong NSSAAF cho enterprise AAA

Nội dung:
- RFC 7155 (Diameter EAP Application) + RFC 6733 (Base)
- Application IDs:
  · NASREQ (1) + EAP (5) via Vendor-Specific-Application-Id (3GPP: 10415)
- Message handling:
  · DER (Diameter-EAP-Request) ↔ DEA (Diameter-EAP-Answer)
  · STR/STA for session termination
  · ASR/ASA for authorization revocation
- CER/CEA capabilities exchange:
  · Vendor-Id: 10415 (3GPP)
  · Auth-Application-Id: 1 (NASREQ), 5 (Diameter EAP)
- Transport:
  · SCTP (preferred) — ordered, multi-stream
  · TCP fallback
  · IP multi-homing (SCTP)
- State management:
  · Session-State: FULL (NSSAAF stores state)
  · Auth-Request-Type: AUTHORIZE_ONLY (initial) / AUTHORIZE_AUTHENTICATE
- Performance:
  · Connection pooling: 10 SCTP associations per Diameter peer
  · Watchdog: CCR/CCA keepalive (30s interval)
  · Failover: secondary peer configuration
```

### 2.4 AAA Proxy Design
```
Mục tiêu: NSSAAF ↔ AAA-P ↔ Third-party AAA-S routing

Nội dung:
- When AAA-P is required:
  · Third-party AAA-S deployment
  · Enterprise DMZ architecture
- Routing logic:
  · Per-S-NSSAI → AAA-S/AAA-P address mapping (config)
  · Optional S-NSSAI → ENSI mapping (per operator policy)
- Protocol passthrough:
  · RADIUS: relay Access-Request/Challenge/Accept/Reject
  · Diameter: relay DER/DEA with AVP translation
- Security:
  · TLS/DTLS termination at AAA-P
  · mTLS between NSSAAF ↔ AAA-P
  · Shared secret rotation (automated)
- Co-location option: NSSAAF + AAA-P same process (deploy mode)
```

---

## PHASE 3: High Availability & Resilience

### 3.1 Stateless Microservice Architecture
```
Mục tiêu: Horizontal scaling không cần session affinity

Nội dung:
- All session state in external store (PostgreSQL + Redis)
- NSSAAF Biz Pods are stateless:
  · No in-memory EAP session state (Redis)
  · No local connection pools (pooled connections to PostgreSQL)
  · All state via Redis pub/sub for cross-pod routing
- Horizontal Pod Autoscaler (HPA):
  · Metrics: CPU > 60%, memory > 70%, RPS > 10k
  · Min replicas: 3 per AZ
  · Max replicas: 50 (configurable)
- Pod Disruption Budget (PDB):
  · maxUnavailable: 1
  · Ensures at least 2 replicas available during updates
```

### 3.2 Multi-AZ Deployment Design
```
Mục tiêu: Zone-level failure isolation (telecom-grade)

Nội dung:
- Kubernetes topology:
  · 3 Availability Zones (AZ1, AZ2, AZ3)
  · Node pools: compute-optimized (e.g., c5.2xlarge equivalent)
  · Pod anti-affinity: spread across AZs
- Stateful component placement:
  · PostgreSQL Patroni: 1 leader + 2 sync standbys (3 AZs)
  · Redis Cluster: 6 nodes (2 per AZ, 3 shards)
- Network:
  · VPC-level cross-AZ latency: <2ms
  · Load balancer: NLB (AWS) / Cloud LB (GCP) with AZ health
- Failure scenarios:
  · 1 AZ down: automatic failover, zero downtime
  · 2 AZs down: degraded mode (read-only AAA config), reject new sessions
  · Database quorum lost: panic mode, reject all requests (fail-secure)
```

### 3.3 Database HA Design
```
Mục tiêu: Zero RPO, <30s RTO cho database layer

Nội dung:
- PostgreSQL 16 with Patroni:
  · Consensus: etcd (3-node) or Consul
  · Synchronous replication: quorum commit
  · Async replication: to offsite DR site
  · RPO: 0 (sync), <1s (async to DR)
  · RTO: <30s (automatic failover)
- Backup strategy:
  · Continuous WAL archiving (S3/GCS)
  · Daily base backup (pg_basebackup)
  · Point-in-time recovery (PITR): 30-day retention
  · Quarterly full snapshot to cold storage
- Connection management:
  · PgBouncer in transaction mode
  · Pool size: 100 connections per NSSAAF instance
  · Max client connections: 5000
  · Query timeout: 5s
- Sharding strategy (future):
  · By PLMN ID (hashed)
  · Per-shard connection pool
  · No cross-shard transactions in hot path
```

### 3.4 Redis HA Design
```
Mụi tiêu: Sub-millisecond cache với HA

Nội dung:
- Redis Cluster (6 nodes minimum):
  · 3 shards × 2 replicas
  · Quorum: 2/2 replicas per shard
  · Automatic failover + replica promotion
- Use cases:
  · EAP session state (short TTL, 5 min)
  · AAA server health (30s TTL)
  · Rate limiting counters (sliding window)
  · Distributed locking (EAP round coordination)
- Persistence:
  · RDB snapshots: every 5 min
  · AOF: every 1s (appendfsync)
- Performance:
  · >500,000 ops/s per shard
  · P99 latency: <1ms
```

### 3.5 Circuit Breaker & Rate Limiting Design
```
Mục tiêu: Isolate failures, prevent cascade

Nội dung:
- Circuit Breaker (per AAA server):
  · Closed: normal operation
  · Open: reject requests after 5 consecutive failures
  · Half-open: allow 1 test request after 30s
  · Thresholds: failure ratio > 50% in 10s window
- Rate Limiting:
  · Per GPSI: 10 auth attempts/minute
  · Per AMF: 1000 req/s
  · Per AAA server: 5000 req/s (token bucket)
  · Global: 100,000 req/s per NSSAAF cluster
- Bulkhead isolation:
  · Separate thread pools: SBI handler, RADIUS sender, Diameter sender
  · Memory isolation: cgroups per component
- Timeout management:
  · EAP round: 30s
  · AAA-S response: 10s
  · DB query: 5s
  · External NF (NRF/UDM): 5s
```

---

## PHASE 4: Security Design

### 4.1 SBI Security (HTTP/2 / TLS)
```
Mục tiêu: MUTLS giữa NSSAAF và consumer NFs

Nội dung:
- TLS 1.3 mandatory (RFC 8446)
- Certificate management:
  · O(1) bootstrap: NSSAAF certificate from operator CA
  · Certificate rotation: every 90 days (automated)
  · MTLS: all SBI consumers (AMF, AUSF, UDM, NRF)
- OAuth 2.0 / NRF-based discovery:
  · NSSAAF registers with NRF: NF-type = NSSAAF
  · Consumer obtains token via NRF OAuth2 server
  · Scope: nnssaaf-nssaa (Nnssaaf_NSSAA service)
- IP allowlist:
  · AMF CIDR ranges (from operator config)
  · AUSF CIDR ranges
  · Internal-only (no public exposure)
```

### 4.2 AAA Protocol Security
```
Mục tiêu: Secure AAA protocol transport

Nội dung:
- RADIUS security:
  · Shared secret: 256-bit, rotated quarterly
  · Message-Authenticator: HMAC-MD5 (RFC 2865)
  · DTLS (RFC 4818) for untrusted transport
  · IP allowlist for AAA-S/AAA-P addresses
- Diameter security:
  · IPSec (RFC 3588) or TLS/DTLS
  · Avp mandatory flags
  · Replay protection via Session-Id
- AAA credential storage:
  · Encrypted at rest (AES-256-GCM)
  · HSM integration (optional): AWS CloudHSM / Thales Luna
  · Audit log for credential access
```

### 4.3 Data Security & Privacy
```
Mục tiêu: Bảo vệ PII/GPSI/EAP-ID theo GDPR/3GPP SA3

Nội dung:
- Data minimization:
  · GPSI: stored encrypted, indexed by hash
  · EAP-ID: not stored persistently (relay only)
  · EAP payloads: stored encrypted (AES-256-GCM), TTL 24h
- Privacy-preserving EAP method:
  · Recommend EAP method with identity protection
  · ANI (Access Network Identity) handling
- Audit logging:
  · Immutable log for all NSSAA operations
  · Fields: timestamp, GPSI (hashed), S-NSSAI, result, AMF-ID
  · SIEM integration: Splunk/Elasticsearch
- Encryption at rest:
  · Database: AES-256
  · Redis: AES-256
  · Backups: AES-256
- Key management:
  · Operator KMS integration
  · Short-lived DEK + long-lived KEK
```

---

## PHASE 5: Operations & FCAPS

### 5.1 Network Resource Model (NRM) Design
```
Mục tiêu: FCAPS-compliant management interface theo TS 28.541

Nội dung:
- NSSAAFFunction IOC (TS 28.541 §5.3.145):
  · Attributes: pLMNInfoList, sBIFQDN, cNSIIdList, managedNFProfile, commModelList, nssaafInfo
- NssaafInfo (§5.3.146):
  · supiRanges
  · internalGroupIdentifiersRanges
- EP_N58 / EP_N59 endpoints (§5.3.147-148)
- ManagedElement integration:
  · vendorName, managedBy
- Notifications (TS 28.541 §5.5):
  · NSSAA_REAUTH_NOTIFICATION
  · NSSAA_REVOC_NOTIFICATION
- RESTCONF/YANG management API:
  · Northbound: OAM systems
  · YANG models: 3GPP managed data (per TS 28.541)
  · NETCONF for configuration push
```

### 5.2 Observability Design
```
Mục tiêu: Full observability — Metrics, Logs, Traces

Nội dung:
- Prometheus metrics:
  · Request rate, latency (P50/P95/P99), error rate
  · EAP session: active, completed, failed, timeout
  · AAA: RADIUS/Diameter request/response times
  · DB: query latency, connection pool utilization
  · Circuit breaker: state, failure rate
  · Per-AMF, per-GPSI, per-S-NSSAI labels
- Structured logging (JSON):
  · Correlation ID (x-request-id)
  · AMF ID, GPSI (hashed), S-NSSAI, authCtxId
  · Log levels: ERROR/WARN/INFO/DEBUG
  · Ship to: Elasticsearch (hot) → S3 (cold, 90d)
- Distributed tracing:
  · OpenTelemetry + Jaeger/Zipkin
  · Trace context propagation (W3C TraceContext)
  · End-to-end: AMF → NSSAAF → AAA-S
  · Span per: HTTP handler, EAP round, DB query, AAA message
- Alerting:
  · PagerDuty integration
  · Thresholds: error rate >1%, latency P99 >500ms, circuit open
```

### 5.3 Configuration Management
```
Mục tiêu: Centralized, versioned configuration

Nội dung:
- Kubernetes-native config:
  · ConfigMap: non-sensitive config
  · Secrets: credentials, keys (mounted as volumes)
- Dynamic config (without restart):
  · AAA server endpoints
  · Timeout values
  · Circuit breaker thresholds
  · Feature flags
- Configuration validation:
  · JSON Schema validation on startup
  · Dry-run mode for config changes
- Version control:
  · All config in GitOps (ArgoCD/Flux)
  · Config drift detection
  · Rollback capability
```

---

## PHASE 6: Integration & Testing

### 6.1 AMF Integration Design
```
Mục tiêu: NSSAAF ↔ AMF N58 interface

Nội dung:
- Nnssaaf_NSSAA_Authenticate service:
  · AMF as consumer, NSSAAF as producer
  · Subscription: implicit (AMF always subscribed)
  · Callback URI: AMF exposes callback endpoint
- Registration flow integration:
  · NSSAA triggered during 5G Registration (§4.2.9)
  · AMF sends H-PLMN S-NSSAI (not mapped)
- Re-auth notification:
  · AMF receives: Nnssaaf_NSSAA_Re-AuthenticationNotification
  · AMF triggers NSSAA procedure on same UE
- Revocation notification:
  · AMF receives: Nnssaaf_NSSAA_RevocationNotification
  · AMF updates Allowed NSSAI → UE Configuration Update
```

### 6.2 UDM Integration Design
```
Mục tiêu: NSSAAF ↔ UDM N59 interface

Nội dung:
- Nudm_UECM_Get service:
  · NSSAAF requests: AMF ID for given GPSI
  · UDM responds: list of AMF IDs (may be multiple for multi-registration)
- Triggers:
  · AAA-S triggered reauth (§4.2.9.3 step 3a)
  · AAA-S triggered revocation (§4.2.9.4 step 3a)
- Failure handling:
  · AMF not registered: stop procedure, ACK to AAA-S
  · UDM unreachable: 503, retry with backoff (3 attempts)
```

### 6.3 NRF Integration Design
```
Mục tiêu: NSSAAF self-registration và service discovery

Nội dung:
- NF registration:
  · NF-type: NSSAAF
  · NF services: Nnssaaf_NSSAA, Nnssaaf_AIW
  · NF profile: per TS 29.510
  · Heartbeat: 5 min (configurable)
- Service discovery:
  · NSSAAF discovers AMF callback URI via NRF
  · NSSAAF discovers UDM Nudm_UECM service
  · TTL-based cache: 5 min
```

### 6.4 End-to-End Testing Strategy
```
Mục tiêu: Comprehensive test coverage cho telecom-grade quality

Nội dung:
- Unit tests:
  · EAP message encoding/decoding
  · RADIUS/Diameter AVP serialization
  · State machine transitions
  · Circuit breaker logic
- Integration tests:
  · AMF simulator → NSSAAF → AAA-S simulator
  · Full EAP-TLS handshake end-to-end
  · Re-auth flow from AAA-S trigger to AMF notification
  · Revocation flow
- Conformance tests:
  · 3GPP TS 26.526 test cases (API)
  · RFC 3579/5216 compliance (RADIUS/EAP)
  · TS 29.561 Ch.16-17 compliance (AAA)
- Load tests:
  · 50,000 concurrent sessions per instance
  · 500,000 sessions/s peak load
  · 100 AMF simulators, 10 AAA-S simulators
  · Chaos: kill AZ, kill pod, network partition
- Security tests:
  · TLS/mTLS validation
  · Certificate expiry handling
  · RADIUS shared secret rotation
  · Fuzzing: EAP payloads, RADIUS AVPs
```

---

## PHASE 7: Kubernetes Deployment

### 7.1 kubeadm Cluster Design
```
Mục tiêu: Production-grade Kubernetes cluster trên kubeadm

Nội dung:
- Control plane (3 nodes, odd number for etcd quorum):
  · kubeadm init với --control-plane-endpoint
  · etcd: stacked (co-located) or external
  · API server: --tls-min-version=TLS1.3
  · Scheduler/ControllerManager: default settings
- Node pools:
  · system: kube-system pods (5-10 nodes)
  · nssAAF: NSSAAF application pods (10-50 nodes, autoscaling)
  · database: Patroni/PostgreSQL nodes (3 nodes)
  · monitoring: Prometheus/Grafana (3 nodes)
- Network:
  · CNI: Calico (BGP mode) or Cilium (eBPF)
  · Pod CIDR: 10.244.0.0/16
  · Service CIDR: 10.96.0.0/12
  · Node CIDR: per AZ subnet
- Storage:
  · PostgreSQL: PersistentVolume (local path or managed storage)
  · Redis: PersistentVolume (local SSD recommended)
- Cluster add-ons:
  · cert-manager: automated TLS certificate management
  · External DNS: route53/integration
  · Metrics server: HPA metrics source
  · Ingress: NGINX Ingress Controller (internal only)
```

### 7.2 NSSAAF Helm Chart Design
```
Mục tiêu: Production Helm chart cho NSSAAF deployment

Nội dung:
Chart structure:
nssAAF/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml         # NSSAAF pods
│   ├── service.yaml             # ClusterIP service
│   ├── hpa.yaml                # Horizontal Pod Autoscaler
│   ├── pdb.yaml                 # Pod Disruption Budget
│   ├── serviceaccount.yaml
│   ├── secret.yaml              # TLS certs, AAA secrets
│   ├── configmap.yaml           # App config
│   ├── ingress.yaml             # Internal ingress (optional)
│   ├── pvc.yaml                 # Persistence (if local storage)
│   └── servicemonitor.yaml     # Prometheus scraping
```

### 7.3 Service Mesh Design
```
Mục tiêu: Istio/Envoy service mesh cho SBI traffic

Nội dung:
- Sidecar injection:
  · NSSAAF pods: enabled
  · Consumer NFs (AMF, AUSF): assume already meshed
- mTLS (STRICT mode):
  · NSSAAF ↔ AMF: mTLS
  · NSSAAF ↔ AUSF: mTLS
  · NSSAAF ↔ UDM: mTLS
  · NSSAAF ↔ NRF: mTLS
- Traffic management:
  · VirtualService: weighted routing (canary deployment)
  · DestinationRule: connection pool, outlier detection
  · PeerAuthentication: mTLS STRICT
- Observability:
  · Automatic traces (Envoy access logs)
  · mTLS certificate rotation via Envoy secret discovery
- Performance overhead:
  · Target: <2ms P99 latency added per hop
  · Optimize: parallel prefetch, connection pooling
```

### 7.4 Deployment Strategy
```
Mục tiêu: Zero-downtime deployment với canary/blue-green

Nội dung:
- Canary deployment:
  · 5% traffic → new version
  · Monitor: error rate, latency P99
  · Auto-rollback if error rate > 1%
  · Full rollout: 30 min (gradual)
- Blue-green deployment:
  · Parallel blue/green clusters
  · Traffic switch via NRF weight or DNS
  · Instant rollback capability
- Rolling update:
  · maxUnavailable: 0 (maxSurge: 1)
  · ReadinessProbe: /healthz endpoint
  · StartupProbe: 60s timeout, 5s interval
- Upgrade sequence:
  · 1. Database migration (backward compatible)
  · 2. NSSAAF new version (canary)
  · 3. Validate metrics
  · 4. Full rollout
```

---

## Tổng hợp Design Documents — Tiến độ thực tế

### Phase 1: Foundation ✅ HOÀN THÀNH
- [x] `design/01_service_model.md` — SBA, NF placement, service mesh (636 dòng)
- [x] `design/02_nssaa_api.md` — Nnssaaf_NSSAA OpenAPI implementation (574 dòng)
- [x] `design/03_aiw_api.md` — Nnssaaf_AIW OpenAPI implementation (387 dòng)
- [x] `design/04_data_model.md` — PostgreSQL schema, Redis cache design (681 dòng)
- [x] `design/05_nf_profile.md` — NRF registration profile (532 dòng)

### Phase 2: Protocol ✅ HOÀN THÀNH
- [x] `design/06_eap_engine.md` — EAP framework, multi-method support (548 dòng)
- [x] `design/07_radius_client.md` — RADIUS client, VSA handling (601 dòng)
- [x] `design/08_diameter_client.md` — Diameter agent, AVP encoding (500 dòng)
- [x] `design/09_aaa_proxy.md` — AAA-P routing, protocol passthrough (119 dòng)

### Phase 3: HA & Resilience ✅ HOÀN THÀNH
- [x] `design/10_ha_architecture.md` — Stateless design, multi-AZ, Kubernetes HA (518 dòng)
- [x] `design/11_database_ha.md` — Patroni PostgreSQL HA, streaming replication (620 dòng)
- [x] `design/12_redis_ha.md` — Redis Cluster, sharding, failover (540 dòng)

### Phase 4: Security ✅ HOÀN THÀNH
- [x] `design/15_sbi_security.md` — TLS/mTLS, OAuth2, NRF, certificates (503 dòng)
- [x] `design/16_aaa_security.md` — RADIUS shared secret, DTLS, Diameter IPSec (470 dòng)

### Phase 5: Operations ✅ HOÀN THÀNH
- [x] `design/18_nrm_fcaps.md` — NRM, YANG models, FCAPS (326 dòng)
- [x] `design/19_observability.md` — Prometheus, logging, tracing (422 dòng)
- [x] `design/20_config_management.md` — GitOps, ArgoCD, SOPS, dynamic config (490 dòng)

### Phase 6: Integration ✅ HOÀN THÀNH
- [x] `design/21_amf_integration.md` — N58 AMF interface (135 dòng)
- [x] `design/22_udm_integration.md` — N59 UDM interface (146 dòng)
- [x] `design/23_ausf_integration.md` — N60 AUSF interface, MSK handling (380 dòng)

### Phase 7: Kubernetes ✅ HOÀN THÀNH
- [x] `design/25_kubeadm_setup.md` — Cluster design, networking, Helm chart (496 dòng)

---

## Timeline thực tế

| Phase | Docs | Status | Lines |
|-------|------|--------|-------|
| Phase 1: Foundation | 5/5 | ✅ Hoàn thành | 2,810 |
| Phase 2: Protocol | 4/4 | ✅ Hoàn thành | 1,768 |
| Phase 3: HA/Resilience | 3/3 | ✅ Hoàn thành | 1,678 |
| Phase 4: Security | 2/2 | ✅ Hoàn thành | 973 |
| Phase 5: Operations | 3/3 | ✅ Hoàn thành | 1,238 |
| Phase 6: Integration | 3/3 | ✅ Hoàn thành | 661 |
| Phase 7: Kubernetes | 1/1 | ✅ Hoàn thành | 496 |
| **Tổng** | **21/21** | **100% ✅** | **9,624 lines** |

> **Tổng cộng: 20 design documents + 2 Cursor rules + 5 chunk docs = ~10,000+ dòng tài liệu**
