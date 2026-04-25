# Phase 7: Kubernetes Deployment

## Overview

Phase 7 creates production-grade Kubernetes manifests for the 3-component NSSAAF architecture. Each component has its own Helm chart with appropriate scaling, high availability, and operational requirements. The target is Ericsson/Nokia-class availability (>99.999%).

**Design Docs:** `docs/design/01_service_model.md` §5.4, `docs/design/25_kubeadm_setup.md`

---

## Helm Chart Structure

```
deployments/
├── helm/
│   ├── nssaa-http-gateway/     # HTTP Gateway chart
│   ├── nssaa-biz/              # Biz Pod chart
│   └── nssaa-aaa-gateway/      # AAA Gateway chart (active-standby)
├── kustomize/
│   ├── base/                   # Shared base manifests
│   └── overlays/
│       ├── development/
│       ├── staging/
│       └── production/
└── argo/
    └── applications.yaml       # ArgoCD Application manifests
```

---

## 1. HTTP Gateway Chart (`nssaa-http-gateway/`)

**Purpose:** TLS terminator and request router. Stateless, scales horizontally.

### 1.1 Chart Structure

```
nssaa-http-gateway/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── service-monitor.yaml
│   ├── horizontalpodautoscaler.yaml
│   ├── poddisruptionbudget.yaml
│   ├── network-policy.yaml
│   └── _helpers.tpl
└── .helmignore
```

### 1.2 `Chart.yaml`

```yaml
apiVersion: v2
name: nssaa-http-gateway
description: NSSAAF HTTP Gateway - TLS terminator and request router
type: application
version: 1.0.0
appVersion: "1.0.0"
kubeVersion: ">=1.25.0"
keywords:
  - 5g
  - nssAAF
  - http-gateway
maintainers:
  - name: NSSAAF Team
    email: nssaa-team@operator.com
dependencies:
  - name: common
    repository: https://charts.bitnami.com/bitnami
    version: 1.x.x
```

### 1.3 `values.yaml`

```yaml
# Default values for nssaa-http-gateway

replicaCount: 3  # Start with 3 replicas for HA

image:
  repository: operator-registry/nssaa-http-gateway
  tag: "latest"
  pullPolicy: IfNotPresent
  pullSecrets: []

service:
  type: LoadBalancer
  port: 443
  annotations:
    # AWS: Enable NLB
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
    # GCP: Reserve static IP
    networking.gke.io/load-balancer-type: "Internal"
    # Azure: Enable ILB
    service.beta.kubernetes.io/azure-load-balancer-internal: "true"

# TLS configuration
tls:
  enabled: true
  secretName: nssaa-http-gw-tls
  # Or cert-manager integration
  certManager:
    enabled: false
    issuerRef:
      name: letsencrypt-prod
      kind: ClusterIssuer

# Health endpoints
health:
  livenessPath: /healthz/live
  readinessPath: /healthz/ready

# Autoscaling
autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 20
  targetCPUUtilizationPercentage: 70
  targetMemoryUtilizationPercentage: 80
  # Custom metrics for scaling decisions
  customMetrics:
    - type: Pods
      pods:
        metric:
          name: nssaa_request_duration_p99_seconds
        target:
          type: AverageValue
          averageValue: "500m"  # P99 < 500ms

# Pod Disruption Budget
podDisruptionBudget:
  enabled: true
  minAvailable: 2  # Always keep at least 2 available

# Resource limits (telecom-grade)
resources:
  limits:
    cpu: "2"
    memory: "2Gi"
  requests:
    cpu: "500m"
    memory: "512Mi"

# Security context
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 1000
  fsGroup: 1000

containerSecurityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL

# ServiceMonitor for Prometheus
serviceMonitor:
  enabled: true
  interval: 15s
  scrapeTimeout: 10s
  namespace: monitoring

# Network policy
networkPolicy:
  enabled: true
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              name: amf  # Allow AMF namespace
      ports:
        - port: 443
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: nssaa-biz
      ports:
        - port: 8080
    - to:  # DNS
        - namespaceSelector:
            matchLabels:
              name: kube-system
      ports:
        - port: 53
          protocol: UDP

# Node affinity/anti-affinity
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app: nssaa-http-gateway
          topologyKey: topology.kubernetes.io/zone

# Topology spread
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: topology.kubernetes.io/zone
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        app: nssaa-http-gateway

# Pod annotations for proxy/intercept
podAnnotations:
  proxy.amazonaws.com/config: |
    {
      "kind": "proxyAuthorization",
      "authorizationTTLSeconds": 3600
    }
```

### 1.4 `templates/deployment.yaml`

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "nssaa-http-gateway.fullname" . }}
  labels:
    {{- include "nssaa-http-gateway.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "nssaa-http-gateway.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
      labels:
        {{- include "nssaa-http-gateway.selectorLabels" . | nindent 8 }}
    spec:
      {{- with .Values.image.pullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      serviceAccountName: {{ include "nssaa-http-gateway.serviceAccountName" . }}
      terminationGracePeriodSeconds: 60
      {{- if .Values.affinity }}
      affinity:
        {{- toYaml .Values.affinity | nindent 8 }}
      {{- end }}
      {{- if .Values.topologySpreadConstraints }}
      topologySpreadConstraints:
        {{- toYaml .Values.topologySpreadConstraints | nindent 8 }}
      {{- end }}
      containers:
        - name: http-gateway
          securityContext:
            {{- toYaml .Values.containerSecurityContext | nindent 12 }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: https
              containerPort: 443
              protocol: TCP
            - name: metrics
              containerPort: 9090
              protocol: TCP
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          livenessProbe:
            httpGet:
              path: {{ .Values.health.livenessPath }}
              port: metrics
            initialDelaySeconds: 10
            periodSeconds: 10
            failureThreshold: 3
          readinessProbe:
            httpGet:
              path: {{ .Values.health.readinessPath }}
              port: metrics
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 3
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          volumeMounts:
            - name: tls-cert
              mountPath: /etc/tls
              readOnly: true
      volumes:
        - name: tls-cert
          secret:
            secretName: {{ .Values.tls.secretName }}
```

---

## 2. Biz Pod Chart (`nssaa-biz/`)

**Purpose:** NSSAAF business logic. Stateless, scales horizontally with HPA.

### 2.1 Chart Structure

```
nssaa-biz/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── deployment.yaml
│   ├── service.yaml              # Headless ClusterIP
│   ├── service-headless.yaml
│   ├── horizontalpodautoscaler.yaml
│   ├── poddisruptionbudget.yaml
│   ├── configmap.yaml
│   ├── secret.yaml               # KEK reference
│   ├── service-monitor.yaml
│   └── network-policy.yaml
└── .helmignore
```

### 2.2 `values.yaml`

```yaml
replicaCount: 3

image:
  repository: operator-registry/nssaa-biz
  tag: "latest"
  pullPolicy: IfNotPresent

service:
  # Headless service for Biz pod routing
  type: ClusterIP
  port: 8080
  headless:
    enabled: true
    # Used by HTTP Gateway to discover Biz pods
    publishNotReadyAddresses: true

# Internal communication (to AAA Gateway)
aaaGateway:
  url: "http://nssaa-aaa-gateway:8080"

# Database configuration
database:
  host: "nssaa-postgres.secondary.svc.cluster.local"
  port: 5432
  name: "nssaa"
  sslMode: "require"
  secretName: "nssaa-biz-db-secret"

# Redis configuration
redis:
  host: "nssaa-redis-master.secondary.svc.cluster.local"
  port: 6379
  passwordSecret: "nssaa-biz-redis-secret"

# NRF configuration
nrf:
  baseURL: "https://nrf.operator.com"

# Autoscaling - aggressive scaling for telecom workload
autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 50  # Scale up to 50 for high load
  targetCPUUtilizationPercentage: 60
  targetMemoryUtilizationPercentage: 70
  # Scale based on active EAP sessions
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
        - type: Percent
          value: 100
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300  # Wait 5 min before scaling down

podDisruptionBudget:
  enabled: true
  maxUnavailable: 1  # Only lose 1 pod during updates

resources:
  limits:
    cpu: "4"          # Higher CPU for EAP processing
    memory: "4Gi"
  requests:
    cpu: "1"
    memory: "1Gi"

# Configuration
config:
  # Circuit breaker settings
  circuitBreaker:
    failureThreshold: 5
    recoveryTimeout: "30s"
    halfOpenMax: 3
  # Retry settings
  retry:
    maxAttempts: 3
    baseDelay: "1s"
    maxDelay: "30s"
  # EAP session settings
  eap:
    roundTimeout: "30s"
    maxRounds: 20

# KEK reference (encrypted KEK stored in Vault)
kek:
  enabled: true
  secretName: "nssaa-biz-kek"
  # Or Vault annotation
  # vault.secretName: "secret/data/nssaa/kek"

serviceMonitor:
  enabled: true
  interval: 15s

networkPolicy:
  enabled: true
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: nssaa-http-gateway
      ports:
        - port: 8080
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: nssaa-aaa-gateway
      ports:
        - port: 8080
    - to:
        - namespaceSelector: {}
      ports:
        - port: 5432
          protocol: TCP
        - port: 6379
          protocol: TCP
```

---

## 3. AAA Gateway Chart (`nssaa-aaa-gateway/`)

**Purpose:** RADIUS/Diameter transport. Stateful, active-standby with keepalived.

### 3.1 Chart Structure

```
nssaa-aaa-gateway/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── statefulset.yaml         # replicas=2, strategy=Recreate
│   ├── service.yaml             # Headless ClusterIP
│   ├── service-vip.yaml         # Keepalived VIP
│   ├── configmap-keepalived.yaml
│   ├── network-attachment.yaml  # Multus CNI for VLAN
│   ├── service-monitor.yaml
│   └── poddisruptionbudget.yaml
└── .helmignore
```

### 3.2 `values.yaml`

```yaml
# Active-standby: exactly 2 replicas
replicaCount: 2

# IMPORTANT: strategy must be Recreate for stateful protocol
updateStrategy:
  type: Recreate

image:
  repository: operator-registry/nssaa-aaa-gateway
  tag: "latest"
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  ports:
    radius-auth: 1812
    radius-acct: 1813
    diameter: 3868
    metrics: 9090
  headless:
    enabled: true

# Keepalived configuration for VIP
keepalived:
  enabled: true
  image: registry.k8s.io/keepalived:v2.3.1
  vip: "10.100.50.100"  # VIP for RADIUS/Diameter
  interface: "aaa-bridge"  # Multus network interface
  virtualRouterId: 51
  priorityBase: 100
  authentication:
    type: PASS
    pass: "secret123"

# Multus CNI for VLAN attachment
multus:
  enabled: true
  networkAttachmentDefinition:
    name: "aaa-bridge-vlan"
    cniVersion: "0.3.1"
    type: "bridge"
    master: "eth0"
    ipam:
      type: "host-local"
      subnet: "10.100.50.0/24"
      gateway: "10.100.50.1"

# AAA server configuration (for testing)
aaaServers:
  - name: "primary"
    host: "aaa-server-1.operator.com"
    port: 1812
    protocol: "RADIUS"
    sharedSecretSecret: "aaa-server-1-secret"
    priority: 100
  - name: "secondary"
    host: "aaa-server-2.operator.com"
    port: 1812
    protocol: "RADIUS"
    sharedSecretSecret: "aaa-server-2-secret"
    priority: 50

# Biz Pod callback URL (for AAA responses)
bizPod:
  callbackURL: "http://nssaa-biz:8080/aaa/callback"

# Pod disruption - can't have more than 1 unavailable
podDisruptionBudget:
  enabled: true
  maxUnavailable: 1

resources:
  limits:
    cpu: "2"
    memory: "2Gi"
  requests:
    cpu: "500m"
    memory: "512Mi"

# Anti-affinity: spread across zones
affinity:
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            app: nssaa-aaa-gateway
        topologyKey: topology.kubernetes.io/zone

serviceMonitor:
  enabled: true
  interval: 15s

networkPolicy:
  enabled: true
  # Allow traffic from Biz Pods
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: nssaa-biz
      ports:
        - port: 8080
    - from:
        - namespaceSelector: {}
      ports:
        - port: 1812  # RADIUS auth
          protocol: UDP
        - port: 1813  # RADIUS acct
          protocol: UDP
        - port: 3868  # Diameter
          protocol: TCP
  egress:
    - to:
        - namespaceSelector: {}
      ports:
        - port: 1812
          protocol: UDP
        - port: 1813
          protocol: UDP
        - port: 3868
          protocol: TCP
```

### 3.3 `templates/statefulset.yaml`

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ include "nssaa-aaa-gateway.fullname" . }}
  labels:
    {{- include "nssaa-aaa-gateway.labels" . | nindent 4 }}
spec:
  serviceName: {{ include "nssaa-aaa-gateway.fullname" . }}-headless
  replicas: {{ .Values.replicaCount }}
  podManagementPolicy: Parallel  # Both pods start simultaneously
  updateStrategy:
    type: Recreate  # CRITICAL: Recreate, not RollingUpdate
  selector:
    matchLabels:
      {{- include "nssaa-aaa-gateway.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: |
          [{
            "name": "{{ .Values.multus.networkAttachmentDefinition.name }}",
            "namespace": "{{ .Release.Namespace }}",
            "interface": "{{ .Values.multus.interface }}"
          }]
      labels:
        {{- include "nssaa-aaa-gateway.selectorLabels" . | nindent 8 }}
    spec:
      terminationGracePeriodSeconds: 120  # Longer for graceful protocol shutdown
      {{- if .Values.affinity }}
      affinity:
        {{- toYaml .Values.affinity | nindent 8 }}
      {{- end }}
      initContainers:
        - name: init
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          command:
            - sh
            - -c
            - |
              # Wait for keepalived to establish VIP
              until ip addr show {{ .Values.multus.interface }} | grep {{ .Values.keepalived.vip }}; do
                echo "Waiting for VIP {{ .Values.keepalived.vip }}..."
                sleep 1
              done
              echo "VIP ready"
          securityContext:
            capabilities:
              add:
                - NET_ADMIN  # Required for ip addr
      containers:
        # Main AAA Gateway container
        - name: aaa-gateway
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: radius-auth
              containerPort: 1812
              protocol: UDP
            - name: radius-acct
              containerPort: 1813
              protocol: UDP
            - name: diameter
              containerPort: 3868
              protocol: TCP
            - name: metrics
              containerPort: 9090
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: VIP
              value: {{ .Values.keepalived.vip }}
            - name: BIZ_POD_CALLBACK_URL
              value: {{ .Values.bizPod.callbackURL }}
          livenessProbe:
            udpSocket:
              port: 1812
            initialDelaySeconds: 30
            periodSeconds: 10
          readinessProbe:
            exec:
              command:
                - /bin/grpc_health_probe
                - -addr=:9090
            initialDelaySeconds: 10
            periodSeconds: 5
          resources:
            {{- toYaml .Values.resources | nindent 12 }}

        # Keepalived sidecar for VIP management
        - name: keepalived
          image: {{ .Values.keepalived.image }}
          securityContext:
            capabilities:
              add:
                - NET_ADMIN
                - NET_BROADCAST
                - NET_RAW
          volumeMounts:
            - name: keepalived-config
              mountPath: /etc/keepalived
          resources:
            limits:
              cpu: "100m"
              memory: "64Mi"
      volumes:
        - name: keepalived-config
          configMap:
            name: {{ include "nssaa-aaa-gateway.fullname" . }}-keepalived
```

---

## 4. Kustomize Overlays

### 4.1 `kustomize/base/kustomization.yaml`

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../helm/nssaa-http-gateway
  - ../../helm/nssaa-biz
  - ../../helm/nssaa-aaa-gateway

namespace: nssaa

commonLabels:
  app.kubernetes.io/part-of: nssaa
  app.kubernetes.io/managed-by: Helm
```

### 4.2 `kustomize/overlays/development/kustomization.yaml`

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

bases:
  - ../../base

namespace: nssaa-dev

patchesStrategicMerge:
  - patches/replicas.yaml
  - patches/images.yaml
  - patches/resources.yaml

patches:
  - patch: |-
      - op: replace
        path: /spec/replicaCount
        value: 1
    target:
      kind: Deployment
      name: nssaa-http-gateway
  - patch: |-
      - op: replace
        path: /spec/replicaCount
        value: 1
    target:
      kind: Deployment
      name: nssaa-biz
  - patch: |-
      - op: replace
        path: /spec/replicaCount
        value: 1
    target:
      kind: StatefulSet
      name: nssaa-aaa-gateway
  - patch: |-
      - op: replace
        path: /spec/updateStrategy/type
        value: RollingUpdate
    target:
      kind: StatefulSet
      name: nssaa-aaa-gateway
```

### 4.3 `kustomize/overlays/production/kustomization.yaml`

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

bases:
  - ../../base

namespace: nssaa

patchesStrategicMerge:
  - patches/replicas.yaml
  - patches/high-availability.yaml

commonAnnotations:
  prometheus.io/scrape: "true"
```

---

## 5. ArgoCD Applications

### 5.1 `argo/application-set.yaml`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: nssaa
  namespace: argocd
spec:
  generators:
    - matrix:
        generators:
          - clusters:
              selector:
                matchLabels:
                  environment: production
          - git:
              repoURL: https://github.com/operator/nssaa-deploy.git
              revision: HEAD
              directories:
                - path: kustomize/overlays/production
  template:
    metadata:
      name: nssaa-{{path.basename}}
    spec:
      project: nssaa
      source:
        repoURL: https://github.com/operator/nssaa-deploy.git
        targetRevision: HEAD
        path: "{{path}}/.."
        kustomize:
          images:
            - operator-registry/nssaa-http-gateway:v{{semver dist.ea.tag}}
            - operator-registry/nssaa-biz:v{{semver dist.ea.tag}}
            - operator-registry/nssaa-aaa-gateway:v{{semver dist.ea.tag}}
      destination:
        server: "{{server}}"
        namespace: nssaa
      syncPolicy:
        automated:
          prune: true
          selfHeal: true
          allowEmpty: false
        syncOptions:
          - CreateNamespace=true
          - PruneLast=true
        retry:
          limit: 5
          backoff:
            duration: 5s
            factor: 2
            maxDuration: 3m
```

---

## 6. Helm Dependencies

### 6.1 PostgreSQL (Bitnami Operator)

```yaml
# Chart.yaml dependencies section
dependencies:
  - name: postgresql
    version: "12.x.x"
    repository: "https://charts.bitnami.com/bitnami"
    condition: postgresql.enabled
  - name: redis
    version: "17.x.x"
    repository: "https://charts.bitnami.com/bitnami"
    condition: redis.enabled
```

### 6.2 Production Values

```yaml
postgresql:
  enabled: true
  architecture: replication  # Primary + 2 replicas
  auth:
    database: nssaa
    username: nssaa
    passwordSecret: nssaa-postgres-secret
  primary:
    persistence:
      size: 100Gi
      storageClass: ssd
    resources:
      limits:
        cpu: "4"
        memory: "8Gi"
  readReplicas:
    replicaCount: 2
    persistence:
      size: 100Gi
    resources:
      limits:
        cpu: "2"
        memory: "4Gi"

redis:
  enabled: true
  architecture: replication  # Master + 2 replicas
  auth:
    passwordSecret: nssaa-redis-secret
  master:
    persistence:
      size: 10Gi
      storageClass: ssd
    resources:
      limits:
        cpu: "2"
        memory: "2Gi"
  replica:
    replicaCount: 2
    persistence:
      size: 10Gi
```

---

## Validation Checklist

### HTTP Gateway

- [ ] `helm lint` passes
- [ ] replicas configurable (3 default)
- [ ] LoadBalancer service with correct annotations
- [ ] TLS secret mounted
- [ ] HPA configured (min 3, max 20)
- [ ] PDB (minAvailable: 2)
- [ ] ServiceMonitor for Prometheus
- [ ] Network policy restricts ingress

### Biz Pod

- [ ] `helm lint` passes
- [ ] HPA configured (min 3, max 50)
- [ ] PDB (maxUnavailable: 1)
- [ ] Headless ClusterIP for pod discovery
- [ ] Database connection with TLS
- [ ] Redis connection
- [ ] KEK reference from Vault
- [ ] Circuit breaker settings configurable
- [ ] ServiceMonitor for Prometheus

### AAA Gateway

- [ ] `helm lint` passes
- [ ] replicas=2 (exact)
- [ ] updateStrategy=Recreate
- [ ] keepalived ConfigMap with VIP
- [ ] Multus NetworkAttachmentDefinition for VLAN
- [ ] Anti-affinity across zones
- [ ] PDB (maxUnavailable: 1)
- [ ] ServiceMonitor for Prometheus

### Kustomize

- [ ] Base compiles with `kustomize build`
- [ ] Development overlay: single replica, RollingUpdate
- [ ] Staging overlay: 2 replicas
- [ ] Production overlay: full HA config

### ArgoCD

- [ ] ApplicationSet syncs to production cluster
- [ ] Automated sync with prune
- [ ] Retry on failure
- [ ] Image tag updates via kustomize

### Integration

- [ ] `helm dependency build` succeeds
- [ ] PostgreSQL operator installs
- [ ] Redis operator installs
- [ ] All 3 components deploy successfully
- [ ] E2E tests pass in K8s environment

---

## Success Criteria (What Must Be TRUE)

1. **HTTP Gateway scales horizontally** — HPA adds pods when load increases, max 20 replicas
2. **Biz Pod scales aggressively** — HPA scales to 50 replicas under load, P99 latency stays <500ms
3. **AAA Gateway fails over <5s** — keepalived detects primary failure, promotes standby within 5 seconds
4. **No single point of failure** — Every component has at least 2 replicas, PDB ensures availability during updates
5. **Secrets never in plaintext** — KEK from Vault, TLS certs from cert-manager
6. **Network policies enforce least privilege** — Only required traffic allowed between components
7. **ArgoCD manages deployments** — GitOps workflow with automated sync and rollback

---

## Dependencies

| Component | Status | Blocking |
|-----------|--------|----------|
| `cmd/http-gateway/` | READY (Phase R) | No |
| `cmd/biz/` | READY (Phase R) | No |
| `cmd/aaa-gateway/` | READY (Phase R) | No |
| `internal/resilience/` | Phase 4 | No |
| `internal/metrics/` | Phase 4 | No |
| `internal/auth/` | Phase 5 | No |
| Kubernetes cluster | External | No |
| cert-manager | External | No |
| Prometheus Operator | External | No |

---

## Next Phase

Phase 8: Performance & Load Testing — Final tuning, 5-nines validation
