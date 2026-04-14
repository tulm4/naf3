---
spec: GitOps / ArgoCD / Flux / Kubernetes / Kustomize
section: Configuration Management
interface: N/A
service: Configuration
---

# NSSAAF Configuration Management Design

## 1. Overview

Thiết kế configuration management cho NSSAAF với GitOps workflow — đảm bảo:
- **Versioned configuration** trong Git
- **Environment separation** (dev, staging, production)
- **Zero-downtime updates** với dynamic reload
- **Drift detection** và automatic rollback

---

## 2. GitOps Architecture

### 2.1 Repository Structure

```
nssAAF-config/
├── base/
│   ├── kustomization.yaml
│   ├── configmap.yaml           # Default config
│   ├── secrets.yaml            # Encrypted secrets (Sealed Secrets)
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── hpa.yaml
│   ├── pdb.yaml
│   └── servicemonitor.yaml
│
├── overlays/
│   ├── development/
│   │   ├── kustomization.yaml
│   │   ├── config-patch.yaml    # Dev-specific values
│   │   └── replicas-patch.yaml
│   │
│   ├── staging/
│   │   ├── kustomization.yaml
│   │   ├── config-patch.yaml    # Staging-specific
│   │   ├── replicas-patch.yaml
│   │   └── resources-limits.yaml
│   │
│   └── production/
│       ├── kustomization.yaml
│       ├── config-patch.yaml    # Production values
│       ├── replicas-patch.yaml
│       ├── pdb-patch.yaml
│       └── alert-routing.yaml
│
├── secrets/
│   ├── .sops.yaml              # SOPS configuration
│   ├── aaa-shared-secrets.enc.yaml
│   └── tls-certs.enc.yaml
│
└── scripts/
    ├── pre-commit-validate.sh
    └── render.sh
```

### 2.2 Kustomize Base

```yaml
# base/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - deployment.yaml
  - service.yaml
  - service-headless.yaml
  - hpa.yaml
  - pdb.yaml
  - configmap.yaml
  - secret.yaml
  - servicemonitor.yaml
  - serviceaccount.yaml
  - networkpolicy.yaml

commonLabels:
  app.kubernetes.io/name: nssAAF
  app.kubernetes.io/part-of: 5g-core

configMapGenerator:
  - name: nssAAF-config
    behavior: create
    literals:
      - LOG_LEVEL=INFO
      - LOG_FORMAT=json
      - METRICS_ENABLED=true
```

### 2.3 Environment Overlays

```yaml
# overlays/production/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: nssAAF

resources:
  - ../../base

patchesStrategicMerge:
  - config-patch.yaml
  - replicas-patch.yaml

replicas:
  - name: nssAAF
    count: 9  # 3 per AZ

images:
  - name: operator/nssAAF
    newTag: "v1.0.0-prod"
```

```yaml
# overlays/production/config-patch.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nssAAF-config
data:
  LOG_LEVEL: "info"
  METRICS_ENABLED: "true"
  EAP_MAX_ROUNDS: "20"
  EAP_ROUND_TIMEOUT_SECONDS: "30"
  SESSION_TTL_SECONDS: "300"
  AAA_RESPONSE_TIMEOUT_MS: "10000"
  AAA_MAX_RETRIES: "3"
  RATE_LIMIT_PER_GPSI_PER_MIN: "10"
  RATE_LIMIT_PER_AMF_PER_SEC: "1000"
  RATE_LIMIT_GLOBAL_PER_SEC: "100000"
```

---

## 3. Secret Management

### 3.1 SOPS + Age Encryption

```bash
# Install SOPS and age
brew install sops age

# Generate age key
age-keygen -o age.key

# .sops.yaml configuration
cat > .sops.yaml << 'EOF'
creation_rules:
  - path_regex: secrets/.*\.yaml$
    encrypted_regex: "^(data|stringData)$"
    age: >-
      age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq
EOF
```

```yaml
# secrets/aaa-shared-secrets.enc.yaml
apiVersion: v1
kind: Secret
metadata:
  name: nssAAF-aaa-secrets
  namespace: nssAAF
type: Opaque
sops:
  encrypted: >-
    ENC[AES256_GCM,data:xxx,iv:xxx]
    age1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq
stringData:
  radius-shared-secret: decrypted-value
```

```bash
# Encrypt secrets
sops --encrypt secrets/aaa-shared-secrets.yaml > secrets/aaa-shared-secrets.enc.yaml
rm secrets/aaa-shared-secrets.yaml

# Decrypt for viewing
sops --decrypt secrets/aaa-shared-secrets.enc.yaml
```

### 3.2 External Secrets Operator

```yaml
# ExternalSecret to fetch from AWS Secrets Manager
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: nssAAF-aaa-secrets
  namespace: nssAAF
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secretsmanager
    kind: ClusterSecretStore
  target:
    name: nssAAF-aaa-secrets
    creationPolicy: Owner
  data:
    - secretKey: radius-shared-secret
      remoteRef:
        key: production/nssAAF/aaa
        property: shared-secret
    - secretKey: diameter-tls-cert
      remoteRef:
        key: production/nssAAF/diameter
        property: cert
```

### 3.3 Sealed Secrets

```yaml
# SealedSecret (encrypted by controller, decryptable only by cluster)
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: nssAAF-tls-cert
  namespace: nssAAF
spec:
  encryptedData:
    tls.crt: AgAixxx...encrypted...==
    tls.key: AgBiyyy...encrypted...==
```

---

## 4. Dynamic Configuration Reload

### 4.1 ConfigMap as Config Source

```yaml
# ConfigMap updated by ArgoCD/Flux, triggers pod reload
apiVersion: v1
kind: ConfigMap
metadata:
  name: nssAAF-config
  namespace: nssAAF
  annotations:
    argocd.argoproj.io/refresh: "true"
data:
  LOG_LEVEL: "info"
  EAP_MAX_ROUNDS: "20"
  AAA_RESPONSE_TIMEOUT_MS: "10000"
```

### 4.2 Config Reload Controller

```go
// In-application config watcher
type ConfigReloader struct {
    filePath    string
    watcher    *fsnotify.Watcher
    onReload   func(*Config) error
}

func (r *ConfigReloader) Start(ctx context.Context) error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }

    // Watch ConfigMap mount
    if err := watcher.Add(r.filePath); err != nil {
        return err
    }

    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            case event := <-watcher.Events:
                if event.Op&fsnotify.Write == fsnotify.Write {
                    log.Info("Config changed, reloading...")
                    if err := r.reload(); err != nil {
                        log.Error("Reload failed: %v", err)
                    }
                }
            case err := <-watcher.Errors:
                log.Error("Watcher error: %v", err)
            }
        }
    }()

    return nil
}

// Kubernetes configmap reload via sidecar
// Or: Kubernetes API watch on ConfigMap
func (r *ConfigReloader) WatchConfigMap(ctx context.Context, cmName, ns string) error {
    clientset, _ := kubernetes.NewForConfig(nil)
    watcher, _ := clientset.CoreV1().ConfigMaps(ns).Watch(ctx, metav1.ListOptions{
        FieldSelector: "metadata.name=" + cmName,
    })

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case event := <-watcher.ResultChan():
            if event.Type == watch.Modified {
                log.Info("ConfigMap modified, reloading config...")
                r.reload()
            }
        }
    }
}
```

### 4.3 Graceful Reload Without Restart

```go
// Atomic config swap
type AtomicConfig struct {
    mu     sync.RWMutex
    config atomic.Value  // Stores *Config
}

func (c *AtomicConfig) Reload(newCfg *Config) error {
    // Validate new config
    if err := validateConfig(newCfg); err != nil {
        return fmt.Errorf("invalid config: %w", err)
    }

    // Atomic swap
    c.mu.Lock()
    c.config.Store(newCfg)
    c.mu.Unlock()

    log.Info("Config reloaded successfully")
    return nil
}

func (c *AtomicConfig) Get() *Config {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.config.Load().(*Config)
}

// Usage in request handler:
// No restart needed, next request uses new config
func (s *Server) HandleAuth(w http.ResponseWriter, r *http.Request) {
    cfg := s.config.Get()  // Always gets current config
    timeout := cfg.AAA.Timeout
    // ...
}
```

---

## 5. ArgoCD Integration

### 5.1 ArgoCD Application

```yaml
# argocd/application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nssAAF
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: network-functions
  source:
    repoURL: https://github.com/operator/nssAAF-config.git
    targetRevision: main
    path: overlays/production
    kustomize:
      images:
        - operator/nssAAF=v1.0.0
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

### 5.2 ArgoCD Sync Waves

```yaml
# Use annotations for ordering
# Wave 0: Namespaces and CRDs
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "0"

---
# Wave 1: Secrets and ConfigMaps
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "1"

---
# Wave 2: Deployments and Services
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "2"
```

### 5.3 Drift Detection & Rollback

```bash
# ArgoCD automatically detects drift (unmanaged changes)
argocd app diff nssAAF

# Manual sync
argocd app sync nssAAF

# Rollback to previous commit
argocd app rollback nssAAF <rollback-id>

# Rollback to specific revision
argocd app set nssAAF --revision <commit-hash>
argocd app sync nssAAF
```

---

## 6. Validation & Testing

### 6.1 Pre-commit Validation

```bash
#!/bin/bash
# scripts/pre-commit-validate.sh

set -e

echo "Validating YAML..."
yamllint overlays/

echo "Validating JSON..."
cat base/configmap.yaml | jq .

echo "Validating Kustomize..."
kustomize build overlays/production > /dev/null

echo "Checking for secrets in plain text..."
if grep -r "password\|secret\|token" base/*.yaml | grep -v "secretKey\|SecretName"; then
    echo "ERROR: Potential secret in plain text!"
    exit 1
fi

echo "Validating Helm values..."
helm lint charts/nssAAF

echo "All validations passed!"
```

### 6.2 Config Validation in Application

```go
// Config validation on startup and reload
type Config struct {
    EAP     EAPConfig
    AAA     AAAConfig
    RateLimit RateLimitConfig
}

func validateConfig(cfg *Config) error {
    if cfg.EAP.MaxRounds < 1 || cfg.EAP.MaxRounds > 100 {
        return fmt.Errorf("EAP.MaxRounds must be 1-100, got %d", cfg.EAP.MaxRounds)
    }

    if cfg.EAP.RoundTimeoutSeconds < 1 {
        return fmt.Errorf("EAP.RoundTimeoutSeconds must be > 0")
    }

    if cfg.AAA.ResponseTimeoutMs < 100 {
        return fmt.Errorf("AAA.ResponseTimeoutMs must be >= 100")
    }

    if cfg.RateLimit.PerGpsiPerMin < 1 {
        return fmt.Errorf("RateLimit.PerGpsiPerMin must be >= 1")
    }

    return nil
}
```

---

## 7. Environment Promotion

```
Feature branch → Development → Staging → Production

┌─────────────────────────────────────────────────────────────────┐
│                        Git Flow                                     │
│                                                                   │
│  feature/NSSAA-xxx ──► main ──► release/v1.0                 │
│         │                 │               │                       │
│         ▼                 ▼               ▼                       │
│   ┌───────────┐    ┌───────────┐   ┌───────────┐              │
│   │ Development│    │  Staging  │   │ Production │              │
│   │  Overlay  │    │  Overlay  │   │  Overlay   │              │
│   └───────────┘    └───────────┘   └───────────┘              │
│                                                                   │
│  Auto-deploy:     Manual deploy:    Manual deploy with approval │
│  on PR merge      on tag            on release                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 8. Acceptance Criteria

| # | Criteria | Implementation |
|---|----------|----------------|
| AC1 | GitOps workflow: all config in Git | ArgoCD + Kustomize overlays |
| AC2 | Environment separation (dev/staging/prod) | Separate overlay directories |
| AC3 | Secrets encrypted with SOPS/Age | .sops.yaml, age encryption |
| AC4 | Dynamic reload without pod restart | ConfigReloader, atomic swap |
| AC5 | Drift detection | ArgoCD auto-sync with selfHeal |
| AC6 | Config validation on reload | validateConfig() |
| AC7 | Pre-commit validation | yamllint, kustomize build |
| AC8 | Rollback capability | ArgoCD rollback to previous revision |
