---
spec: Kubernetes / kubeadm / Helm / ETSI NFV-IFA 031
section: Kubernetes Deployment
interface: N/A
service: Deployment
---

# NSSAAF Kubernetes Deployment Design

## 1. kubeadm Cluster Setup

> **Note (Phase R):** After the 3-component refactor, the Helm chart deploys three separate components: HTTP Gateway (N replicas), Biz Pod (N replicas), and AAA Gateway (2 replicas active-standby). See `docs/design/01_service_model.md` §5.4 for the architecture overview.

### 1.1 Control Plane

```bash
# Initialize control plane
kubeadm init \
  --control-plane-endpoint=k8s.operator.com \
  --pod-network-cidr=10.244.0.0/16 \
  --service-cidr=10.96.0.0/12 \
  --apiserver-advertise-address=10.0.0.10 \
  --tls-min-version=TLS1.3 \
  --tls-cipher-suites=TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256

# Install CNI (Calico BGP mode)
kubectl apply -f https://docs.projectcalico.org/v3.26/manifests/calico.yaml

# Node labels for AZ topology
kubectl label node k8s-az1 operator.com/zone=az1
kubectl label node k8s-az2 operator.com/zone=az2
kubectl label node k8s-az3 operator.com/zone=az3

# Node pools
kubectl label node k8s-az1 node-pool=system
kubectl label node k8s-az1 node-pool=nssAAF

# Install etcd for external etcd (or stacked)
# For production: external etcd with 3 nodes across 3 AZs
```

### 1.2 Node Pools

```yaml
# NodePool for NSSAAF workloads
apiVersion: v1
kind: Node
metadata:
  labels:
    node-pool: nssAAF
    topology.kubernetes.io/zone: az1
  name: nssAAF-az1-node-1
spec:
  taints:
    - key: "node-role"
      operator: "Equal"
      value: "network-function"
      effect: "NoSchedule"
  unschedulable: false
```

### 1.3 Storage (Local Path or Managed)

```yaml
# Local PV for PostgreSQL and Redis
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: nssAAF-local-storage
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Retain

---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pg-az1-pv
spec:
  capacity:
    storage: 500Gi
  volumeMode: Filesystem
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  storageClassName: nssAAF-local-storage
  local:
    path: /data/pg
  nodeAffinity:
    required:
      nodeSelectorTerms:
        - matchExpressions:
            - key: topology.kubernetes.io/zone
              operator: In
              values:
                - az1
```

---

## 2. Helm Chart Structure

> **Note (Phase R):** The Helm chart has separate directories for each component: `http-gateway/`, `biz/`, and `aaa-gateway/`. The values.yaml reflects the 3-component deployment model with per-component replica counts.

```
nssAAF/
├── Chart.yaml
├── values.yaml
├── values-production.yaml
├── values-staging.yaml
├── templates/
│   ├── NOTES.txt
│   ├── _helpers.tpl
│   ├── shared/
│   │   ├── configmap.yaml          # Shared config (logging, metrics)
│   │   ├── secret.yaml           # Encrypted secrets
│   │   ├── networkpolicy.yaml    # Shared network policies
│   │   └── servicemonitor.yaml   # Prometheus scraping
│   ├── http-gateway/
│   │   ├── deployment.yaml         # HTTP Gateway (N replicas)
│   │   ├── service.yaml          # LoadBalancer / ClusterIP
│   │   ├── hpa.yaml             # HPA
│   │   └── pdb.yaml             # PodDisruptionBudget
│   ├── biz/
│   │   ├── deployment.yaml         # Biz Pod (N replicas, stateless)
│   │   ├── service.yaml          # ClusterIP for internal SBI
│   │   ├── hpa.yaml             # HPA
│   │   └── pdb.yaml
│   ├── aaa-gateway/
│   │   ├── deployment.yaml        # AAA Gateway (2 replicas, Recreate)
│   │   ├── service.yaml         # ClusterIP for biz pods
│   │   ├── pdb.yaml             # PDB (maxUnavailable=1)
│   │   ├── keepalived-configmap.yaml  # keepalived VRRP config
│   │   └── network-attachments.yaml   # Multus CNI CRD
│   └── tests/
│       └── test-connection.yaml
└── .helmignore
```

### 2.1 Chart.yaml

```yaml
apiVersion: v2
name: nssAAF
description: NSSAAF - Network Slice-Specific Authentication and Authorization Function
type: application
version: 1.0.0
appVersion: "1.0.0"
kubeVersion: ">=1.26.0"
keywords:
  - 5G
  - NSSAAF
  - network-slice
  - authentication
maintainers:
  - name: Operator Team
    email: ops@operator.com
dependencies:
  - name: postgresql-ha
    version: "11.x.x"
    repository: "https://charts.bitnami.com/bitnami"
    condition: postgresql-ha.enabled
  - name: redis
    version: "17.x.x"
    repository: "https://charts.bitnami.com/bitnami"
    condition: redis.enabled
```

### 2.2 values.yaml

> **Note (Phase R):** Each component has its own replica count. The AAA Gateway replica count is hard-coded to 2 (active-standby).

```yaml
# Default values for NSSAAF (3-component model)

# HTTP Gateway (receives AMF/AUSF SBI traffic)
httpGateway:
  replicaCount: 3
  image:
    repository: operator/nssaa-http-gw
    tag: "1.0.0"
  service:
    type: LoadBalancer  # Stable FQDN for AMF/AUSF
    port: 443
  metricsPort: 9091
  resources:
    requests:
      cpu: "500m"
      memory: "256Mi"
    limits:
      cpu: "1"
      memory: "512Mi"

# Biz Pod (NSSAAF application logic)
biz:
  replicaCount: 5
  image:
    repository: operator/nssaa-biz
    tag: "1.0.0"
  service:
    port: 8080  # ClusterIP, internal only
  metricsPort: 9091
  resources:
    requests:
      cpu: "2"
      memory: "4Gi"
    limits:
      cpu: "4"
      memory: "8Gi"

# AAA Gateway (raw RADIUS/Diameter sockets, active-standby)
# HARD LIMIT: Never exceed 2 replicas
aaaGateway:
  replicaCount: 2  # hard maximum — 1 active, 1 standby
  image:
    repository: operator/nssaa-aaa-gw
    tag: "1.0.0"
  service:
    port: 9090  # ClusterIP for biz pods
    radiusPort: 1812  # UDP, via Multus CNI
    diameterPort: 3868  # TCP/SCTP, via Multus CNI
  metricsPort: 9091
  keepalived:
    enabled: true
    vip: "10.1.100.200"  # Stable IP seen by AAA-S
    interface: "net0"      # Multus CNI bridge VLAN interface
    virtualRouterId: 60
  resources:
    requests:
      cpu: "1"
      memory: "1Gi"
    limits:
      cpu: "2"
      memory: "2Gi"

podAnnotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "9091"
  prometheus.io/path: "/metrics"

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 10000
  fsGroup: 10000
  seccompProfile:
    type: RuntimeDefault

securityContext:
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false

resources:
  requests:
    cpu: "2"
    memory: "4Gi"
  limits:
    cpu: "4"
    memory: "8Gi"

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 50
  targetCPUUtilizationPercentage: 60
  targetMemoryUtilizationPercentage: 70
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 30
      policies:
        - type: Pods
          value: 5
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300

affinity:
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app: nssAAF
        topologyKey: topology.kubernetes.io/zone
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app: nssAAF
          topologyKey: kubernetes.io/hostname

nodeSelector: {}

tolerations: []

# Configuration
config:
  eap:
    maxRounds: 20
    roundTimeoutSeconds: 30
    sessionTtlSeconds: 300
  aaa:
    responseTimeoutMs: 10000
    maxRetries: 3
  rateLimit:
    perGpsiPerMin: 10
    perAmfPerSec: 1000
    globalPerSec: 100000
  logging:
    level: INFO
    format: json

# Secrets (externalized)
secrets:
  aaaSharedSecrets:
    enabled: true
    existingSecret: nssAAF-aaa-secrets

# PostgreSQL HA
postgresql-ha:
  enabled: true
  postgresql:
    replicaCount: 3
    resources:
      requests:
        cpu: "1"
        memory: "2Gi"
      limits:
        cpu: "2"
        memory: "4Gi"
  persistence:
    enabled: true
    size: 100Gi
    storageClass: nssAAF-local-storage

# Redis Cluster
redis:
  enabled: true
  architecture: replication
  auth:
    enabled: false  # Internal cluster, no auth
  master:
    resources:
      requests:
        cpu: "500m"
        memory: "1Gi"
      limits:
        cpu: "1"
        memory: "2Gi"
  replica:
    replicaCount: 3
    resources:
      requests:
        cpu: "500m"
        memory: "1Gi"
      limits:
        cpu: "1"
        memory: "2Gi"
  persistence:
    enabled: true
    size: 10Gi
    storageClass: nssAAF-local-storage

# HTTP Gateway (separate Deployment, N replicas)
httpGateway:
  enabled: true
  replicas: 3
  bindPodIP: true  # Each replica binds its own pod IP for external interface

# Biz Pod (separate Deployment, N replicas — stateless)
biz:
  enabled: true
  replicas: 5

# AAA Gateway (separate Deployment, 2 replicas, active-standby)
aaaGateway:
  enabled: true
  replicas: 2  # hard maximum — 1 active, 1 standby
  strategy: Recreate
  keepalived:
    enabled: true
    vip: "10.1.100.200"    # Stable VIP seen by AAA-S
    interface: "net0"       # Multus CNI bridge VLAN interface
    virtualRouterId: 60
  multus:
    enabled: true
    networkAttachment: "aaa-bridge-vlan"

# Internal communication
internal:
  bizServiceURL: "http://svc-nssaa-biz:8080"
  aaaGatewayURL: "http://svc-nssaa-aaa:9090"
```

---

## 3. Deployment Templates

> **Note (Phase R):** Each of the three components has its own Deployment manifest. The monolithic single-binary deployment has been replaced with separate templates for HTTP Gateway, Biz Pod, and AAA Gateway.

### 3.1 HTTP Gateway Deployment

```yaml
# templates/http-gateway/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "nssAAF.httpGateway.fullname" . }}
  labels:
    {{- include "nssAAF.labels" . | nindent 4 }}
    app.kubernetes.io/component: http-gateway
spec:
  replicas: {{ .Values.httpGateway.replicaCount }}
  selector:
    matchLabels:
      {{- include "nssAAF.httpGateway.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        app.kubernetes.io/component: http-gateway
        app: nssaa-http-gw
    spec:
      serviceAccountName: {{ include "nssAAF.serviceAccountName" . }}
      containers:
        - name: http-gw
          image: "{{ .Values.httpGateway.image.repository }}:{{ .Values.httpGateway.image.tag }}"
          imagePullPolicy: {{ .Values.httpGateway.image.pullPolicy | default "IfNotPresent" }}
          ports:
            - name: https
              containerPort: 443
              protocol: TCP
            - name: metrics
              containerPort: {{ .Values.httpGateway.metricsPort }}
          env:
            - name: BIND_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: BIZ_SERVICE_URL
              value: "http://svc-nssaa-biz:8080"
          resources:
            {{- toYaml .Values.httpGateway.resources | nindent 12 }}
```

### 3.2 Biz Pod Deployment

```yaml
# templates/biz/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "nssAAF.biz.fullname" . }}
  labels:
    {{- include "nssAAF.labels" . | nindent 4 }}
    app.kubernetes.io/component: biz
spec:
  replicas: {{ .Values.biz.replicaCount }}
  selector:
    matchLabels:
      {{- include "nssAAF.biz.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        app.kubernetes.io/component: biz
        app: nssaa-biz
    spec:
      serviceAccountName: {{ include "nssAAF.serviceAccountName" . }}
      containers:
        - name: biz
          image: "{{ .Values.biz.image.repository }}:{{ .Values.biz.image.tag }}"
          imagePullPolicy: {{ .Values.biz.image.pullPolicy | default "IfNotPresent" }}
          ports:
            - name: http
              containerPort: 8080  # ClusterIP for internal SBI
            - name: metrics
              containerPort: {{ .Values.biz.metricsPort }}
          env:
            - name: AAA_GATEWAY_URL
              value: "http://svc-nssaa-aaa:9090"
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
          resources:
            {{- toYaml .Values.biz.resources | nindent 12 }}
```

### 3.3 AAA Gateway Deployment (Active-Standby)

```yaml
# templates/aaa-gateway/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "nssAAF.aaaGateway.fullname" . }}
  labels:
    {{- include "nssAAF.labels" . | nindent 4 }}
    app.kubernetes.io/component: aaa-gateway
spec:
  # HARD LIMIT: Never exceed 2 replicas. Diameter requires single active connection.
  replicas: {{ .Values.aaaGateway.replicaCount }}
  # Recreate strategy prevents two active pods during rolling update
  strategy:
    type: Recreate
  selector:
    matchLabels:
      {{- include "nssAAF.aaaGateway.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        # Multus CNI secondary interface for AAA traffic
        k8s.v1.cni.cncf.io/networks: |
          [{
            "name": "aaa-bridge-vlan",
            "interface": "net0",
            "ips": ["$(POD_IP)/24"],
            "gateway": ["10.1.100.1"]
          }]
      labels:
        app.kubernetes.io/component: aaa-gateway
        app: nssaa-aaa-gw
    spec:
      serviceAccountName: {{ include "nssAAF.serviceAccountName" . }}
      # NET_ADMIN capability required for keepalived to manage VIP
      securityContext:
        capabilities:
          add: ["NET_ADMIN"]
      containers:
        - name: aaa-gw
          image: "{{ .Values.aaaGateway.image.repository }}:{{ .Values.aaaGateway.image.tag }}"
          imagePullPolicy: {{ .Values.aaaGateway.image.pullPolicy | default "IfNotPresent" }}
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: REDIS_URL
              value: "{{ .Values.redis.url }}"
            - name: REDIS_CHANNEL
              value: "nssaa:aaa-response"
          ports:
            - name: http-internal
              containerPort: 9090
            - name: radius
              containerPort: 1812
              protocol: UDP
            - name: diameter
              containerPort: 3868
              protocol: TCP
            - name: metrics
              containerPort: {{ .Values.aaaGateway.metricsPort }}
          volumeMounts:
            - name: keepalived-conf
              mountPath: /etc/keepalived/keepalived.conf
              readOnly: true
          resources:
            {{- toYaml .Values.aaaGateway.resources | nindent 12 }}
      volumes:
        - name: keepalived-conf
          configMap:
            name: {{ include "nssAAF.aaaGateway.fullname" . }}-keepalived
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: nssaa-aaa-gw
              topologyKey: topology.kubernetes.io/zone
```

---

## 4. GitOps with ArgoCD

> **Note (Phase R):** ArgoCD deploys three separate Helm charts (or one chart with 3-component structure). The sync policy applies to all components.

```yaml
# ArgoCD Application (multi-component)
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nssAAF
  namespace: argocd
spec:
  project: network-functions
  source:
    repoURL: https://github.com/operator/nssAAF-config.git
    targetRevision: main
    path: nssAAF
    helm:
      valueFiles:
        - values-production.yaml
      parameters:
        - name: httpGateway.image.tag
          value: "1.0.0"
        - name: biz.image.tag
          value: "1.0.0"
        - name: aaaGateway.image.tag
          value: "1.0.0"
  destination:
    server: https://kubernetes.default.svc
    namespace: nssAAF
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: false
    syncOptions:
      - CreateNamespace=true
      - PrunePropagation=foreground
      - PruneLast=true
    retry:
      limit: 5
      backoff:
        duration: 5s
        factor: 2
        maxDuration: 3m
```

---

## 5. Acceptance Criteria

> **Note (Phase R):** The acceptance criteria reflect the 3-component deployment model.

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | kubeadm cluster với 3 AZ nodes | Multi-AZ node pools |
| AC2 | HPA for HTTP Gateway (N replicas) and Biz Pod (N replicas) | HorizontalPodAutoscaler v2 |
| AC3 | HPA for AAA Gateway (2 replicas fixed, Recreate strategy) | Deployment.spec.strategy=Recreate |
| AC4 | PDB: maxUnavailable=1 per component | PodDisruptionBudget |
| AC5 | Helm chart với 3-component structure | Separate http-gateway/, biz/, aaa-gateway/ directories |
| AC6 | ArgoCD GitOps deployment | ArgoCD Application |
| AC7 | Prometheus ServiceMonitor per component | Separate monitors for HTTP GW, Biz, AAA GW |
| AC8 | keepalived VIP for AAA Gateway (Multus CNI) | keepalived.conf, network-attachments.yaml |
