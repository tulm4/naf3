---
spec: Kubernetes / kubeadm / Helm / ETSI NFV-IFA 031
section: Kubernetes Deployment
interface: N/A
service: Deployment
---

# NSSAAF Kubernetes Deployment Design

## 1. kubeadm Cluster Setup

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

```
nssAAF/
├── Chart.yaml
├── values.yaml
├── values-production.yaml
├── values-staging.yaml
├── templates/
│   ├── NOTES.txt
│   ├── _helpers.tpl
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── service-headless.yaml
│   ├── hpa.yaml
│   ├── pdb.yaml
│   ├── serviceaccount.yaml
│   ├── secret.yaml
│   ├── configmap.yaml
│   ├── horizontalpodautoscaler.yaml
│   ├── servicemonitor.yaml
│   ├── poddisruptionbudget.yaml
│   ├── networkpolicy.yaml
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

```yaml
# Default values for NSSAAF

replicaCount: 3

image:
  repository: operator/nssAAF
  tag: "1.0.0"
  pullPolicy: IfNotPresent
  pullSecrets: []

imagePullSecrets: []

service:
  type: ClusterIP
  port: 8080
  grpcPort: 9090
  metricsPort: 9091

serviceAccount:
  create: true
  name: nssAAF-sa
  annotations: {}

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

# Istio integration
istio:
  enabled: true
  mtls:
    mode: STRICT
  host: nssAAF.operator.com
  gateway: nssAAF-gateway
```

---

## 3. Deployment Template

```yaml
# templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "nssAAF.fullname" . }}
  labels:
    {{- include "nssAAF.labels" . | nindent 4 }}
    app.kubernetes.io/component: nssAAF
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "nssAAF.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      annotations:
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
        {{- toYaml .Values.podAnnotations | nindent 8 }}
      labels:
        {{- include "nssAAF.labels" . | nindent 8 }}
        app: {{ include "nssAAF.name" . }}
        app.kubernetes.io/component: nssAAF
        sidecar.istio.io/inject: "true"
    spec:
      {{- with .Values.priorityClassName }}
      priorityClassName: {{ . | quote }}
      {{- end }}
      serviceAccountName: {{ include "nssAAF.serviceAccountName" . }}
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      {{- with .Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      terminationGracePeriodSeconds: 60
      containers:
        - name: nssAAF
          securityContext:
            {{- toYaml .Values.securityContext | nindent 12 }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
            - name: grpc
              containerPort: 9090
              protocol: TCP
            - name: metrics
              containerPort: 9091
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /healthz/live
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
            failureThreshold: 3
            timeoutSeconds: 5
          readinessProbe:
            httpGet:
              path: /healthz/ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 2
            timeoutSeconds: 3
          startupProbe:
            httpGet:
              path: /healthz/startup
              port: 8080
            failureThreshold: 30
            periodSeconds: 10
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          volumeMounts:
            - name: tmp
              mountPath: /tmp
            - name: config
              mountPath: /etc/nssAAF/config
              readOnly: true
            - name: secrets
              mountPath: /etc/nssAAF/secrets
              readOnly: true
      volumes:
        - name: tmp
          emptyDir: {}
        - name: config
          configMap:
            name: {{ include "nssAAF.fullname" . }}-config
        - name: secrets
          secret:
            secretName: {{ include "nssAAF.fullname" . }}-secrets
```

---

## 4. GitOps with ArgoCD

```yaml
# ArgoCD Application
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nssAAF
  namespace: argocd
spec:
  project: network-functions
  source:
    repoURL: https://github.com/operator/nssAAF-helm.git
    targetRevision: main
    path: nssAAF
    helm:
      valueFiles:
        - values-production.yaml
      parameters:
        - name: image.tag
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

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | kubeadm cluster với 3 AZ nodes | Multi-AZ node pools |
| AC2 | HPA: 3-50 replicas, CPU/memory/session metrics | HorizontalPodAutoscaler v2 |
| AC3 | PDB: maxUnavailable=1 | PodDisruptionBudget |
| AC4 | Helm chart với production values | values-production.yaml |
| AC5 | ArgoCD GitOps deployment | ArgoCD Application |
| AC6 | Prometheus ServiceMonitor | Prometheus Operator CRD |
| AC7 | Istio sidecar injection enabled | sidecar.istio.io/inject: "true" |
