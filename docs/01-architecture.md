# NSSAAF Detail Design - Part 1: System Architecture

**Document Version:** 1.0.0
**Date:** 2026-04-13
**Project:** NSSAAF (Network Slice-Specific Authentication and Authorization Function)
**Technology Stack:** Golang, PostgreSQL, Redis, Kubernetes (Kubeadm)
**Compliance:** 3GPP Release 17/18

---

## 1. Tổng quan Project

### 1.1 Mục đích và Phạm vi

NSSAAF là Network Function trong kiến trúc 5G Core (5GC)，负责执行 Network Slice-Specific Authentication and Authorization (NSSAA)。该功能支持基于 EAP 的切片特定身份验证和授权流程。

**Core Responsibilities:**
- 提供基于 EAP 的切片认证和授权服务
- 与外部 AAA 服务器 (RADIUS/Diameter) 互操作
- 支持切片授权的撤销和重新认证
- 管理切片认证上下文生命周期

### 1.2 关键文档映射

| 3GPP Spec | 用途 | 关键章节 |
|-----------|------|----------|
| TS 29.526 | NSSAAF 服务定义 | Nnssaaf_NSSAA API |
| TS 29.561 | 外部 DN 互操作 | N58/N60 接口 |
| TS 33.501 | 安全架构 | EAP-TLS, AKA' 流程 |
| TS 29.500 | SBA 技术实现 | HTTP/2, OAuth2.0 |
| TS 29.510 | NRF 服务 | NF 注册/发现 |

---

## 2. Kiến trúc Hệ thống

### 2.1 High-Level Architecture

```mermaid
graph TB
    subgraph "5G Core Network"
        AMF["AMF<br/>(Access and Mobility<br/>Management Function)"]
        NRF["NRF<br/>(Network Function<br/>Repository)"]
        UDM["UDM<br/>(Unified Data<br/>Management)"]
    end

    subgraph "NSSAAF Service (Our System)"
        subgraph "NSSAAF Cluster"
            GW["API Gateway<br/>(OAuth2.0 + TLS)"]
            NSSAAF["NSSAAF Core<br/>(Business Logic)"]
            EAP["EAP Handler<br/>(EAP-TLS/AKA')"]
            AAA["AAA Proxy<br/>(RADIUS/Diameter)"]
        end
        
        subgraph "Data Layer"
            PG["PostgreSQL<br/>(Slice Auth Context)"]
            RD["Redis Cluster<br/>(Session Cache)"]
        end
    end

    subgraph "External AAA"
        RADIUS["RADIUS Server"]
        DIAMETER["Diameter Server"]
    end

    AMF -->|"Nnssaaf SBI<br/>HTTP/2"| GW
    GW -->|"OAuth2 Token<br/>Validation"| NRF
    NSSAAF -->|"NF Registration<br/>Heartbeat"| NRF
    NSSAAF -->|"Query<br/>Subscription"| UDM
    GW --> NSSAAF
    NSSAAF --> EAP
    EAP --> AAA
    AAA <-->|"RADIUS/Diameter<br/>Protocol"| RADIUS
    AAA <-->|"Diameter<br/>Protocol"| DIAMETER
    NSSAAF <--> PG
    NSSAAF <--> RD
```

### 2.2 Microservice Architecture

```mermaid
graph LR
    subgraph "Kubernetes Cluster"
        subgraph "Control Plane"
            API["Kubernetes API Server"]
            ETCD["etcd"]
            CM["Cluster Manager"]
        end

        subgraph "NSSAAF Namespace"
            ING["Ingress Controller<br/>(nginx-ingress)"]
            SVC["Service Mesh<br/>(Istio)"]
            
            subgraph "NSSAAF Core Services"
                API_GW["api-gateway<br/>:8080"]
                NSSAA["nssaa-service<br/>:8081"]
                AIW["aiw-service<br/>:8082"]
                EAP_MGR["eap-manager<br/>:8083"]
            end
            
            subgraph "Infrastructure Services"
                REDIS["redis-cluster<br/>:6379"]
                PG["postgres-primary<br/>:5432"]
                PG_RP["postgres-replica<br/>:5432"]
            end
        end
    end

    API --> CM
    CM --> ING
    ING --> SVC
    SVC --> API_GW
    API_GW --> NSSAA
    API_GW --> AIW
    NSSAA --> EAP_MGR
    NSSAA --> PG
    NSSAA --> REDIS
    AIW --> PG
    AIW --> REDIS
```

### 2.3 Component Architecture

```mermaid
classDiagram
    class APIGateway {
        +OAuth2ClientValidator
        +TLSTerminator
        +RequestRouter
        +RateLimiter
        +Logger
        +ValidateToken()
        +RouteRequest()
        +ApplyRateLimit()
    }

    class NSSAAService {
        +SliceAuthManager
        +ContextRegistry
        +NotificationHandler
        +AuthResultHandler
        +CreateSliceAuthContext()
        +ConfirmSliceAuth()
        +HandleRevocation()
        +HandleReauth()
    }

    class AIWService {
        +AuthInterworkingHandler
        +TTLSProcessor
        +AuthContextManager
        +CreateAuthContext()
        +ConfirmAuth()
    }

    class EAPHandler {
        +EAPMessageParser
        +EAPTLSEngine
        +EAPAKAProcessor
        +KeyDerivation
        +ParseEAPMessage()
        +ProcessEAPTLS()
        +ProcessEAKA()
        +DeriveMSK()
    }

    class AAAProxy {
        +RADIUSClient
        +DiameterClient
        +ProtocolConverter
        +MessageAuthenticator
        +SendRADIUSAuth()
        +SendDiameterAuth()
        +ConvertProtocol()
    }

    class DataLayer {
        +PostgresRepository
        +RedisCache
        +TransactionManager
        +SaveContext()
        +GetContext()
        +UpdateStatus()
    }

    APIGateway --> NSSAAService
    APIGateway --> AIWService
    NSSAAService --> EAPHandler
    AIWService --> EAPHandler
    EAPHandler --> AAAProxy
    NSSAAService --> DataLayer
    AIWService --> DataLayer
```

---

## 3. Giao diện Dịch vụ (Service Interfaces)

### 3.1 SBI (Service Based Interface) - Nnssaaf

**API Root:** `https://nssaaf.operator.com/nnssaaf-nssaa/v1`

#### 3.1.1 Nnssaaf_NSSAA Service

```mermaid
graph LR
    subgraph "AMF (Consumer)"
        AMF_REG["Registration<br/>Request"]
        AMF_AUTH["Auth<br/>Response"]
    end

    subgraph "NSSAAF (Producer)"
        POST["POST<br/>/slice-authentications"]
        PUT["PUT<br/>/slice-authentications/{id}"]
        CALLBACK["Callback<br/>Notifications"]
    end

    AMF_REG -->|"1. Create<br/>SliceAuthContext"| POST
    POST -->|"2. EAP Challenge<br/>Response"| AMF_AUTH
    AMF_AUTH -->|"3. Confirm<br/>SliceAuth"| PUT
    PUT -->|"4. Auth Result<br/>Success/Failure"| AMF_AUTH
    
    POST -.->|"5. Reauth<br/>Notification"| CALLBACK
    POST -.->|"6. Revocation<br/>Notification"| CALLBACK
```

#### 3.1.2 Nnssaaf_AIW Service (AAA Interworking)

**API Root:** `https://nssaaf.operator.com/nnssaaf-aiw/v1`

```mermaid
graph LR
    subgraph "NSSAAF Internal"
        AIW_POST["POST<br/>/authentications"]
        AIW_PUT["PUT<br/>/authentications/{id}"]
    end

    subgraph "AAA Proxy"
        RADIUS["RADIUS Server"]
        DIAMETER["Diameter Server"]
    end

    AIW_POST -->|"DER/DEA"| RADIUS
    AIW_POST -->|"DER/DEA"| DIAMETER
    AIW_PUT -->|"RAR/RAA"| RADIUS
    AIW_PUT -->|"RAR/RAA"| DIAMETER
```

### 3.2 External Interface (N6 Reference Point)

```mermaid
graph TB
    subgraph "NSSAAF - NSS-AAA Server"
        NSSAAF_EXT["NSSAAF<br/>External Interface"]
        RADIUS_AAP["RADIUS<br/>AVP Mapping"]
        DIAMETER_AAP["Diameter<br/>AVP Mapping"]
    end

    subgraph "NSS-AAA Servers"
        RADIUS_SRV["NSS-RADIUS<br/>Server"]
        DIAMETER_SRV["NSS-Diameter<br/>Server"]
    end

    NSSAAF_EXT --> RADIUS_AAP
    NSSAAF_EXT --> DIAMETER_AAP
    RADIUS_AAP -->|"UDP 1812/1813"| RADIUS_SRV
    DIAMETER_AAP -->|"SCTP/TLS"| DIAMETER_SRV
```

---

## 4. Data Models

### 4.1 Core Data Types (theo TS 29.571)

```yaml
# Snssai - Single Network Slice Selection Assistance Information
Snssai:
  sst: uint8          # Slice/Service Type (1-255)
  sd: string          # Slice Differentiator (6 hex digits, optional)

# Supi - Subscription Permanent Identifier
Supi:
  type: enum          # "imsi", "nai", "gpsi"
  value: string       # Format depends on type

# Gpsi - Generic Public Subscription Identifier
Gpsi:
  type: enum          # "msisdn", "externalId", "bcdid"
  value: string

# UserLocation
UserLocation:
 utra: UtraLocation  # For 3G
  eutra: EutraLocation # For 4G
  nr: NrLocation      # For 5G

# NssaaStatus
NssaaStatus:
  status: enum        # "AUTHORIZED", "NOT_AUTHORIZED", "AUTHENTICATION_REQUIRED"
  failureReason: string (optional)
  nssaaAvail: bool
```

### 4.2 NSSAAF Specific Data Types

```yaml
# SliceAuthInfo - Request để tạo authentication context
SliceAuthInfo:
  gpsi: Gpsi              # UE Identifier
  snssai: Snssai          # Slice identifier
  eapIdRsp: EapMessage    # EAP Response from UE
  amfInstanceId: string   # AMF Instance ID
  reauthNotifUri: Uri     # URI for re-auth notification
  revocNotifUri: Uri      # URI for revocation notification

# SliceAuthContext - Authentication Context Resource
SliceAuthContext:
  gpsi: Gpsi
  snssai: Snssai
  authCtxId: string       # UUID
  eapMessage: EapMessage  # EAP Challenge/Response

# SliceAuthConfirmationData - Confirm authentication result
SliceAuthConfirmationData:
  gpsi: Gpsi
  snssai: Snssai
  eapMessage: EapMessage

# SliceAuthConfirmationResponse
SliceAuthConfirmationResponse:
  gpsi: Gpsi
  snssai: Snssai
  eapMessage: EapMessage
  authResult: AuthStatus  # "SUCCESS" or "FAILURE"

# SliceAuthReauthNotification
SliceAuthReauthNotification:
  notifType: enum         # "SLICE_RE_AUTH"
  gpsi: Gpsi
  snssai: Snssai
  supi: Supi

# SliceAuthRevocNotification
SliceAuthRevocNotification:
  notifType: enum         # "SLICE_REVOCATION"
  gpsi: Gpsi
  snssai: Snssai
  supi: Supi
```

### 4.3 EAP Message Structure

```yaml
# EapMessage - EAP Packet (RFC 3748)
EapMessage:
  format: byte           # Base64 encoded EAP packet
  # EAP Packet Structure:
  # Code: 1=Request, 2=Response, 3=Success, 4=Failure
  # Type: 1=Identity, 13=EAP-TLS, 50=EAP-AKA', 51=EAP-SIM

# EAP-TLS Packet (RFC 5216)
EAPTLS:
  code: 2               # Response
  type: 13              # EAP-TLS
  flags: bitfield
    - 0: Reserved
    - 1: Integrity=1
    - 2: Reserved
    - 3: MRU
    - 4: Fragmentation
    - 5-7:TLVs
  mtu: uint16           # If MRU flag set
  fragment: byte        # If fragmentation flag set
```

---

## 5. Security Architecture

### 5.1 Security Domain

```mermaid
graph TB
    subgraph "Security Domains"
        SEC_UE["UE Domain<br/>- USIM<br/>- ME"]
        SEC_RAN["RAN Domain<br/>- gNB<br/>- E1/Ng/Xn"]
        SEC_5GC["5GC Domain<br/>- NSSAAF<br/>- AMF/NRF"]
        SEC_EXT["External Domain<br/>- NSS-AAA<br/>- DN"]
    end

    SEC_UE -->|"NAS Security"| SEC_RAN
    SEC_RAN -->|"DTLS/TLS"| SEC_5GC
    SEC_5GC -->|"OAuth2.0<br/>TLS 1.3"| SEC_EXT
```

### 5.2 Authentication Flow Security

```mermaid
sequenceDiagram
    participant UE
    participant AMF
    participant NSSAAF
    participant AAA
    participant UDM

    Note over NSSAAF: TLS 1.3 Mutual Authentication
    
    AMF->>+NSSAAF: POST /slice-authentications<br/>Bearer Token (OAuth2.0)
    NSSAAF->>NRF: Validate Token
    NRF-->>NSSAAF: Token Valid
    
    NSSAAF->>NSSAAF: Parse EAP Message
    NSSAAF->>AAA: Forward to AAA Server<br/>(RADIUS/Diameter)
    
    AAA->>AAA: Authenticate with NSS-AAA
    
    alt EAP-TLS
        Note over AAA: TLS Handshake<br/>Certificate Validation<br/>MSK Derivation
    end
    
    alt EAP-AKA'
        Note over AAA: AKA' Milenage<br/>CK', IK' Derivation
    end
    
    AAA-->>NSSAAF: Authentication Result
    NSSAAF-->>AMF: EAP Challenge/Response
    AMF->>UE: NAS Security Mode Command
    
    UE->>AMF: NAS Security Mode Complete
    AMF->>+NSSAAF: PUT /slice-authentications/{id}<br/>EAP Result
    
    NSSAAF->>NSSAAF: Verify Result<br/>Update Context
    NSSAAF-->>AMF: Final Auth Status
```

---

## 6. Deployment Architecture

### 6.1 Kubernetes Deployment Model

```yaml
# Kubernetes Resources for NSSAAF
apiVersion: v1
kind: Namespace
metadata:
  name: nssaaf
  labels:
    app.kubernetes.io/part-of: 5gc
    topo.domain: core
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nssaa-service
  namespace: nssaaf
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nssaa-service
  template:
    metadata:
      labels:
        app: nssaa-service
        version: v1
    spec:
      containers:
      - name: nssaa-service
        image: nssaaf/nssaa-service:1.0.0
        ports:
        - containerPort: 8081
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "2000m"
        env:
        - name: CONFIG_FILE
          value: "/config/nssaaf.yaml"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        volumeMounts:
        - name: config
          mountPath: /config
      volumes:
      - name: config
        configMap:
          name: nssaaf-config
---
apiVersion: v1
kind: Service
metadata:
  name: nssaa-service
  namespace: nssaaf
spec:
  type: ClusterIP
  ports:
  - port: 8081
    targetPort: 8081
    protocol: TCP
  selector:
    app: nssaa-service
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: nssaa-service-hpa
  namespace: nssaaf
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: nssaa-service
  minReplicas: 3
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

### 6.2 High Availability Topology

```mermaid
graph TB
    subgraph "Kubernetes Cluster - Primary DC"
        subgraph "nssaaf Namespace"
            NSSAA_P1["nssaa-service-1"]
            NSSAA_P2["nssaa-service-2"]
            NSSAA_P3["nssaa-service-3"]
        end
        
        subgraph "Storage"
            PG_P["postgres-primary"]
            PG_R1["postgres-replica-1"]
            PG_R2["postgres-replica-2"]
        end
        
        subgraph "Cache"
            RD_M["redis-master"]
            RD_S1["redis-replica-1"]
            RD_S2["redis-replica-2"]
        end
    end

    subgraph "Kubernetes Cluster - Secondary DC"
        subgraph "nssaaf-dr Namespace"
            NSSAA_D1["nssaa-service-dr1"]
            NSSAA_D2["nssaa-service-dr2"]
        end
        
        subgraph "Storage DR"
            PG_DR["postgres-standby"]
        end
        
        subgraph "Cache DR"
            RD_DR["redis-standby"]
        end
    end

    subgraph "Service Mesh"
        ISTIO["Istio Control Plane"]
    end

    NSSAA_P1 --> ISTIO
    NSSAA_P2 --> ISTIO
    NSSAA_P3 --> ISTIO
    NSSAA_D1 --> ISTIO
    NSSAA_D2 --> ISTIO

    NSSAA_P1 <-->|"Sync"| PG_P
    NSSAA_P2 <-->|"Sync"| PG_P
    NSSAA_P3 <-->|"Sync"| PG_P
    PG_P -->|"Async<br/>Replicate"| PG_R1
    PG_P -->|"Async<br/>Replicate"| PG_R2
    PG_P -.->|"Sync<br/>Replication"| PG_DR

    RD_M -->|"Sync"| RD_S1
    RD_M -->|"Sync"| RD_S2
    RD_M -.->|"Async"| RD_DR
```

---

## 7. Network Configuration

### 7.1 Service Discovery

```yaml
# NSSAAF NF Profile for NRF Registration
nfProfile:
  nfInstanceId: "<auto-generated-uuid>"
  nfType: NSSAAF
  nfStatus: REGISTERED
  apiVersion: v1
  serviceName: nnssaaf-nssaa
  services:
    - serviceName: nnssaaf-nssaa
      versions:
        - apiVersionInUri: v1
          apiFullVersion: "1.2.1"
      scheme: https
      nfServiceStatus: SERVED
    - serviceName: nnssaaf-aiw
      versions:
        - apiVersionInUri: v1
          apiFullVersion: "1.1.0"
      scheme: https
      nfServiceStatus: SERVED
  fqdn: nssaaf.operator.com
  interPlmnFqdn: nssaaf.homenet.operator.com
  ipv4Addresses:
    - 10.100.1.10
  port: 443
  priority: 100
  capacity: 10000
  loadControlWeight: 1.0
  recoveryTime: "2026-04-13T00:00:00Z"
  supportedFeatures: "NSSAA-EAP-TLS,NSSAA-EAP-AKA"
```

### 7.2 Network Policies

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: nssaaf-network-policy
  namespace: nssaaf
spec:
  podSelector:
    matchLabels:
      app: nssaa-service
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: 5gc
    ports:
    - protocol: TCP
      port: 8081
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: postgres
    ports:
    - protocol: TCP
      port: 5432
  - to:
    - podSelector:
        matchLabels:
          app: redis
    ports:
    - protocol: TCP
      port: 6379
  - to:
    - namespaceSelector: {}
      podSelector:
        matchLabels:
          app: nrf
    ports:
    - protocol: TCP
      port: 8080
```

---

## 8. Monitoring and Observability

### 8.1 Metrics Collection

```yaml
# Prometheus Metrics for NSSAAF
metrics:
  # Request Metrics
  - name: nssaaf_http_requests_total
    type: counter
    labels: [method, endpoint, status_code]
  - name: nssaaf_http_request_duration_seconds
    type: histogram
    labels: [method, endpoint]
  
  # Business Metrics
  - name: nssaaf_auth_requests_total
    type: counter
    labels: [result, snssai_sst]
  - name: nssaaf_auth_duration_seconds
    type: histogram
    labels: [auth_type]
  - name: nssaaf_active_contexts
    type: gauge
  
  # AAA Proxy Metrics
  - name: nssaaf_radius_requests_total
    type: counter
    labels: [server, result]
  - name: nssaaf_diameter_requests_total
    type: counter
    labels: [server, result]
  
  # Infrastructure Metrics
  - name: nssaaf_db_connections_active
    type: gauge
  - name: nssaaf_redis_operations_total
    type: counter
    labels: [operation, result]
```

### 8.2 Logging Structure

```json
{
  "timestamp": "2026-04-13T10:30:00.123Z",
  "level": "INFO",
  "service": "nssaa-service",
  "trace_id": "abc123def456",
  "span_id": "span789",
  "message": "Slice authentication context created",
  "context": {
    "auth_ctx_id": "ctx-uuid-123",
    "gpsi": "msisdn-84-1234567890",
    "snssai_sst": 1,
    "amf_instance_id": "amf-001",
    "procedure_type": "NSSAA_INITIAL_AUTH"
  }
}
```

---

## 9. Performance Requirements

### 9.1 SLA Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| Auth Request Latency (p99) | < 100ms | End-to-end |
| Throughput | 10,000 req/sec | Per instance |
| Availability | 99.999% | Per year |
| Context Recovery | < 30s | After failure |
| Max Active Contexts | 1,000,000 | Per cluster |

### 9.2 Resource Scaling

```yaml
# Scaling Configuration
scaling:
  # Horizontal Pod Autoscaler
  hpa:
    min_replicas: 3
    max_replicas: 20
    target_cpu_utilization: 70
    target_memory_utilization: 80
  
  # Vertical Pod Autoscaler (for memory optimization)
  vpa:
    memory:
      target_avg_utilization: 70
  
  # Pod Disruption Budget
  pdb:
    min_available: 2
  
  # Resource Quotas
  resource_quota:
    cpu_limit: "40 cores"
    memory_limit: "80 Gi"
```

---

## 10. Compliance Checklist

- [x] TS 29.526 NSSAAF Service Implementation
- [x] TS 29.571 Common Data Types
- [x] TS 29.500 SBA Technical Realization
- [x] TS 29.501 SBI Design Principles
- [x] TS 29.561 External DN Interworking (N58/N60)
- [x] TS 33.501 Security Architecture
- [x] TS 28.532 Performance Metrics
- [x] RFC 3748 EAP Protocol
- [x] RFC 5216 EAP-TLS
- [x] OAuth2.0 Client Credentials Flow

---

**Document Author:** NSSAAF Design Team
**Next Document:** Part 2 - API Specification & Data Models
