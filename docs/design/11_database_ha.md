---
spec: Patroni / etcd / Consul / PostgreSQL 16
section: High Availability Database
interface: N/A (infrastructure)
service: Database HA
---

# NSSAAF Database High Availability Design

## 1. Overview

> **Note (Phase R):** After the 3-component refactor, only **Biz Pods** access the database directly. The HTTP Gateway has no database access (it routes requests to Biz Pods). The AAA Gateway uses Redis for session correlation, not PostgreSQL. See `docs/design/01_service_model.md` §5.4 for the architecture overview.

Thiết kế PostgreSQL High Availability với Patroni cho NSSAAF — đảm bảo:
- **RPO = 0** cho synchronous replication (sync standby)
- **RTO < 30s** cho automatic failover
- **3 AZ deployment** cho zone-level resilience

---

## 2. Patroni Architecture

### 2.1 System Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Control Plane (Consul/etcd)                       │
│                                                                      │
│  ┌─────────┐     ┌─────────┐     ┌─────────┐                      │
│  │ Consul  │     │ Consul  │     │ Consul  │                      │
│  │ Node 1  │     │ Node 2  │     │ Node 3  │                      │
│  │  (AZ1)  │     │  (AZ2)  │     │  (AZ3)  │                      │
│  └────┬────┘     └────┬────┘     └────┬────┘                      │
│       │                 │                 │                          │
│       └─────────────────┼─────────────────┘                          │
│                         │ (Raft consensus)                           │
└─────────────────────────┼───────────────────────────────────────────┘
                          │
       ┌──────────────────┼──────────────────┐
       │                  │                  │
       ▼                  ▼                  ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│  PostgreSQL │    │  PostgreSQL │    │  PostgreSQL │
│    AZ1      │    │    AZ2      │    │    AZ3      │
│ (Leader)   │◄──►│ (Sync Rep) │◄──►│ (Async Rep) │
│  Patroni    │    │  Patroni    │    │  Patroni    │
└─────────────┘    └─────────────┘    └─────────────┘
       │                  │                  │
       │  Streaming Repl  │  Streaming Repl │
       │──────────────────│─────────────────│
                          ▼
              WAL Archives → S3 (WAL-E/Barman)
```

### 2.2 Patroni Configuration

```yaml
# patroni.yml - Patroni configuration for NSSAAF PostgreSQL
scope: nssAAF-postgres
namespace: /service/
datadir: /data/postgresql

# Name must be unique
name: nssAAF-pg-az1

# Consul for distributed consensus
consul:
  register_service: true
  service_port: 5432
  service_name: nssAAF-postgres
  service_tags: ['primary', 'postgres']
  host: 127.0.0.1
  port: 8500
  token: "${CONSUL_TOKEN}"
  verify: true
  cacert: /etc/consul/ca.crt

# etcd alternative
# etcd:
#   host: 127.0.0.1
#   port: 2379
#   protocol: http
#   username: "${ETCD_USER}"
#   password: "${ETCD_PASSWORD}"

# PostgreSQL configuration
postgresql:
  listen: 0.0.0.0:5432
  connect_address: nssAAF-pg-az1.operator.com:5432
  data_dir: /data/postgresql
  pgpass: /tmp/pgpass

  authentication:
    replication:
      username: repl_user
      password: "${REPL_PASSWORD}"
    superuser:
      username: postgres
      password: "${POSTGRES_PASSWORD}"

  create_replica_methods:
    - wal_e
  wal_e:
    command: /usr/local/bin/wal-e replication create %r
    keep_history: 10

  parameters:
    # Connections
    max_connections: 500
    max_worker_processes: 8
    max_parallel_workers_per_gather: 4

    # Memory
    shared_buffers: 8GB                  # 25% of RAM
    effective_cache_size: 24GB           # 75% of RAM
    maintenance_work_mem: 2GB
    work_mem: 16MB

    # Write performance
    wal_buffers: 64MB
    min_wal_size: 2GB
    max_wal_size: 8GB
    checkpoint_timeout: 10min
    checkpoint_completion_target: 0.9

    # Query optimization
    default_statistics_target: 100
    random_page_cost: 1.1              # SSD
    effective_io_concurrency: 200        # SSD

    # Replication
    wal_level: replica
    max_wal_senders: 10
    max_replication_slots: 10
    hot_standby: on
    synchronous_commit: on                # For sync replication
    synchronous_standby_names: 'ANY 1 (nssAAF-pg-az2)'

    # Logging
    log_destination: 'stderr'
    logging_collector: on
    log_directory: /var/log/postgresql
    log_filename: 'postgresql-%Y-%m-%d.log'
    log_statement: 'none'
    log_min_duration_statement: 1000

    # Security
    ssl: on
    ssl_cert_file: /etc/ssl/certs/postgresql.crt
    ssl_key_file: /etc/ssl/private/postgresql.key
    ssl_ca_file: /etc/ssl/certs/ca.crt

restapi:
  listen: 0.0.0.0:8008
  connect_address: nssAAF-pg-az1.operator.com:8008
  auth: 'admin:password'
```

### 2.3 Node-Specific Configuration

```bash
# AZ1 - Leader candidate
patroni:
  name: nssAAF-pg-az1
  scope: nssAAF-postgres
  postgresql:
    connect_address: nssAAF-pg-az1.operator.com:5432
    synchronous_standby_names: 'ANY 1 (nssAAF-pg-az2)'

# AZ2 - Sync standby
patroni:
  name: nssAAF-pg-az2
  scope: nssAAF-postgres
  postgresql:
    connect_address: nssAAF-pg-az2.operator.com:5432
    synchronous_standby_names: 'ANY 1 (nssAAF-pg-az1)'

# AZ3 - Async standby (offsite DR)
patroni:
  name: nssAAF-pg-az3
  scope: nssAAF-postgres
  postgresql:
    connect_address: nssAAF-pg-az3.operator.com:5432
    # No synchronous - async replication to offsite
```

---

## 3. Streaming Replication

### 3.1 Replication Configuration

```sql
-- On leader (AZ1)
ALTER SYSTEM SET wal_level = replica;
ALTER SYSTEM SET max_wal_senders = 10;
ALTER SYSTEM SET max_replication_slots = 10;
ALTER SYSTEM SET hot_standby = on;
ALTER SYSTEM SET synchronous_commit = on;
ALTER SYSTEM SET synchronous_standby_names = 'ANY 1 (nssAAF-pg-az2)';

-- Create replication user
CREATE USER repl_user WITH REPLICATION ENCRYPTED PASSWORD '${REPL_PASSWORD}';

-- Create replication slot
SELECT pg_create_physical_replication_slot('nssAAF-pg-az2_slot');
SELECT pg_create_physical_replication_slot('nssAAF-pg-az3_slot');

-- pg_hba.conf for replication
host     replication     repl_user     10.0.1.0/24     scram-sha-256
host     replication     repl_user     10.0.2.0/24     scram-sha-256
host     replication     repl_user     10.0.3.0/24     scram-sha-256

-- Application connections
host     nssAAF         nssAAF_app    10.0.1.0/24     scram-sha-256
host     nssAAF         nssAAF_app    10.0.2.0/24     scram-sha-256
host     nssAAF         nssAAF_app    10.0.3.0/24     scram-sha-256
```

### 3.2 Replication Monitoring

```sql
-- Check replication status
SELECT
    client_addr,
    state,
    sent_lsn,
    write_lsn,
    flush_lsn,
    replay_lsn,
    (sent_lsn - replay_lsn) AS replication_lag_bytes
FROM pg_stat_replication;

-- Check replication slots
SELECT slot_name, slot_type, active, restart_lsn
FROM pg_replication_slots
WHERE slot_type = 'physical';

-- Check timeline
SELECT pg_current_wal_lsn();
SELECT pg_last_wal_receive_lsn();
SELECT pg_last_wal_replay_lsn();
```

```go
// Replication lag monitoring
type ReplicationMonitor struct {
    db *pgxpool.Pool
}

func (m *ReplicationMonitor) CheckLag(ctx context.Context) (map[string]int64, error) {
    query := `
        SELECT client_addr::text,
               (sent_lsn - replay_lsn) AS lag_bytes
        FROM pg_stat_replication`

    rows, err := m.db.Query(ctx, query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    lag := make(map[string]int64)
    for rows.Next() {
        var addr string
        var bytes int64
        rows.Scan(&addr, &bytes)
        lag[addr] = bytes
    }

    return lag, nil
}

func (m *ReplicationMonitor) AlertOnLag(ctx context.Context, thresholdBytes int64) {
    lag, err := m.CheckLag(ctx)
    if err != nil {
        log.Error(err)
        return
    }

    for addr, bytes := range lag {
        if bytes > thresholdBytes {
            alerts.Raise("REPLICATION_LAG_HIGH",
                map[string]string{
                    "peer": addr,
                    "lag_bytes": fmt.Sprintf("%d", bytes),
                })
        }
    }
}
```

---

## 4. Backup & Recovery

### 4.1 WAL Archival to S3

```bash
# WAL-E / WAL-G configuration
# Continuous WAL archiving to S3

# Environment variables
WAL_S3_BUCKET=s3://operator-nssAAF-backups/wal
WAL_E_AWS_REGION=eu-west-1
WALE_S3_PREFIX=s3://operator-nssAAF-backups/wal

# In patroni.yml
postgresql:
  recovery_conf:
    restore_command: /usr/local/bin/wal-e wal-fetch "%f" "%p"

# Cron job for base backups
# Daily base backup at 2 AM UTC
0 2 * * * /usr/local/bin/wal-e backup-push /data/postgresql
```

### 4.2 Point-in-Time Recovery (PITR)

```bash
# Restore to specific point in time
# Example: Restore to 2025-01-01 12:00:00 UTC

# Stop PostgreSQL
pg_ctl stop -m fast

# Clean data directory
rm -rf /data/postgresql/*

# Restore base backup + WAL
WAL_E_S3_PREFIX=s3://operator-nssAAF-backups/wal \
/usr/local/bin/wal-e backup-fetch /data/postgresql LATEST

# Create recovery signal
touch /data/postgresql/recovery.signal

# Create recovery configuration
cat > /data/postgresql/postgresql.auto.conf << 'EOF'
restore_command = '/usr/local/bin/wal-e wal-fetch "%f" "%p"'
recovery_target_time = '2025-01-01 12:00:00 UTC'
recovery_target_action = 'promote'
EOF

# Start PostgreSQL
pg_ctl start
```

### 4.3 Backup Schedule

| Type | Frequency | Retention | Destination |
|------|-----------|-----------|-------------|
| WAL | Continuous | 30 days | S3 Standard |
| Base backup | Daily 02:00 UTC | 30 days | S3 Standard-IA |
| Weekly full | Sunday 03:00 UTC | 90 days | S3 Glacier |
| Monthly full | 1st of month | 1 year | S3 Glacier Deep Archive |
| DR offsite | Daily | 7 days | Cross-region S3 |

### 4.4 Backup Validation

```go
// Automated backup validation
type BackupValidator struct {
    s3Client *s3.Client
    pgPool   *pgxpool.Pool
}

func (v *BackupValidator) Validate(ctx context.Context) error {
    // 1. Check latest backup exists and is not empty
    latest, err := v.s3Client.GetLatestBackup(ctx)
    if err != nil {
        return fmt.Errorf("no backup found: %w", err)
    }

    if latest.Size < 1GB {
        return fmt.Errorf("backup suspiciously small: %d bytes", latest.Size)
    }

    // 2. Verify backup is not corrupt (checksum)
    if !v.validateChecksum(latest) {
        return fmt.Errorf("backup checksum mismatch")
    }

    // 3. Test restore to isolated environment
    if err := v.testRestore(ctx, latest); err != nil {
        return fmt.Errorf("restore test failed: %w", err)
    }

    // 4. Verify data integrity
    if err := v.verifyDataIntegrity(ctx); err != nil {
        return fmt.Errorf("data integrity check failed: %w", err)
    }

    return nil
}
```

---

## 5. Failover Mechanism

### 5.1 Patroni Automatic Failover

```
Normal state:
  Leader (AZ1) ←→ Sync Standby (AZ2)
      ↑                  ↑
      │                  │
      └──────────────────┘
           Consensus via Consul

Failure: AZ1 (Leader) dies
         │
         ▼
Consul detects leader lost
         │
         ▼
Patroni elects new leader (AZ2 - sync standby)
         │
         ▼
AZ2 promotes to leader
         │
         ▼
Async standby (AZ3) reconnects to new leader
         │
         ▼
Application reconnects via new leader address
```

### 5.2 Patroni API for Failover

```bash
# Manual switchover (controlled)
curl -X POST http://nssAAF-pg-az1:8008/switchover \
  -d '{"leader":"nssAAF-pg-az1","candidate":"nssAAF-pg-az2"}'

# Manual failover (uncontrolled)
curl -X POST http://nssAAF-pg-az2:8008/failover \
  -d '{"candidate":"nssAAF-pg-az2"}'

# Get cluster status
curl http://nssAAF-pg-az1:8008/cluster
```

### 5.3 Application Reconnection

```go
// PgBouncer / connection pool handles reconnection
// NSSAAF application uses PgBouncer:

# pgbouncer.ini
[databases]
nssAAF = host=nssAAF-pg-lb.operator.com port=5432 dbname=nssAAF

# Load balancer DNS resolves to current leader
# DNS TTL: 60 seconds
# Health checks: Patroni updates DNS on failover

# Reconnection strategy in application:
type ReconnectingPool struct {
    inner   *pgxpool.Pool
    mux    sync.RWMutex
    leader string
}

func (p *ReconnectingPool) Query(ctx context.Context, sql string, args ...interface{}) error {
    for attempt := 0; attempt < 3; attempt++ {
        p.mux.RLock()
        pool := p.inner
        p.mux.RUnlock()

        _, err := pool.Exec(ctx, sql, args...)
        if err == nil {
            return nil
        }

        // Check if connection error
        if !isConnectionError(err) {
            return err
        }

        // Refresh DNS and reconnect
        leader, err := p.discoverLeader(ctx)
        if err != nil {
            return err
        }

        p.refreshPool(ctx, leader)
        time.Sleep(time.Duration(attempt) * time.Second)
    }
    return ErrMaxRetriesExceeded
}
```

---

## 6. PgBouncer Connection Pooling

### 6.1 PgBouncer Configuration

```ini
; pgbouncer.ini
[databases]
nssAAF = host=nssAAF-pg-lb.operator.com port=5432 dbname=nssAAF

[pgbouncer]
listen_addr = 0.0.0.0
listen_port = 6432
auth_type = scram-sha-256
auth_file = /etc/pgbouncer/userlist.txt

; Pool mode: transaction (recommended for NSSAAF)
pool_mode = transaction

; Pool sizes
max_client_conn = 5000
default_pool_size = 100
min_pool_size = 20
reserve_pool_size = 20
reserve_pool_timeout = 5

; Server lifetime
server_lifetime = 3600
server_idle_timeout = 600

; Query timeout
query_timeout = 5
idle_transaction_timeout = 60

; Logging
log_connections = 0
log_disconnections = 0
log_pooler_errors = 1

; Performance
pkt_buf = 4096
max_packet_size = 2147483647
sbuf_loopcnt = 5
tcp_defer_accept = 5
tcp_socket_buffer = 65536
```

### 6.2 PgBouncer HA

```yaml
# Kubernetes PgBouncer deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pgbouncer
spec:
  replicas: 2
  selector:
    matchLabels:
      app: pgbouncer
  template:
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: pgbouncer
              topologyKey: topology.kubernetes.io/zone
      containers:
        - name: pgbouncer
          image: pgbouncer/pgbouncer:latest
          ports:
            - containerPort: 6432
          env:
            - name: DATABASE_URL
              value: "pgbouncer://nssAAF@nssAAF-pg-lb.operator.com:5432/nssAAF"
          resources:
            requests:
              cpu: "500m"
              memory: "512Mi"
            limits:
              cpu: "1"
              memory: "1Gi"
---
apiVersion: v1
kind: Service
metadata:
  name: pgbouncer
spec:
  type: ClusterIP
  ports:
    - port: 6432
      targetPort: 6432
  selector:
    app: pgbouncer
```

---

## 7. Disaster Recovery

### 7.1 DR Architecture

```
Primary Region (EU-WEST):
  AZ1 ── Sync ── AZ2 ── Async ── AZ3 (offsite)
  │                        │
  └────────────────────────┘
           WAL to S3

DR Region (EU-NORTH):
  DR-AZ1 ── Async from Primary AZ3
  │
  └── Read replicas for DR
```

### 7.2 RTO/RPO Matrix

| Scenario | RPO | RTO | Recovery Action |
|----------|-----|-----|----------------|
| Single PG instance crash | 0 (sync repl) | <30s | Auto failover |
| AZ1 failure (leader) | 0 (sync to AZ2) | <30s | Patroni elects AZ2 |
| AZ1+AZ2 failure | <1s (async to AZ3) | <5min | Manual promote AZ3 |
| Region failure | ~1s (async offsite) | <15min | Promote offsite DR |
| Data corruption | Depends on backup | <1h | PITR from S3 |

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | Patroni manages failover automatically | DCS via Consul, <30s RTO |
| AC2 | Synchronous replication to AZ2 | synchronous_standby_names = AZ2 |
| AC3 | Asynchronous replication to AZ3 (DR) | WAL streaming to offsite |
| AC4 | Continuous WAL archival to S3 | WAL-E / WAL-G |
| AC5 | Daily base backups with PITR | wal-e backup-push daily |
| AC6 | PgBouncer transaction mode pooling | max_client_conn=5000, pool=100 |
| AC7 | Backup validation automated | BackupValidator test restore |
| AC8 | DR offsite replication | Cross-region async replica |
