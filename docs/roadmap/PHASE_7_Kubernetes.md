# Phase 7: Kubernetes — Deployment

## Overview

Phase 7 tạo Kubernetes deployment manifests.

## Modules to Implement

### 1. `deployments/helm/nssAAF/` — Helm Chart

**Priority:** P0
**Design Doc:** `docs/design/25_kubeadm_setup.md`

**Deliverables:**
- [ ] `Chart.yaml`
- [ ] `values.yaml`
- [ ] `templates/deployment.yaml`
- [ ] `templates/service.yaml`
- [ ] `templates/hpa.yaml`
- [ ] `templates/pdb.yaml`
- [ ] `templates/configmap.yaml`
- [ ] `templates/secret.yaml`
- [ ] `templates/servicemonitor.yaml`

### 2. `deployments/kustomize/` — Kustomize Overlays

**Priority:** P1

**Deliverables:**
- [ ] `base/kustomization.yaml`
- [ ] `overlays/development/`
- [ ] `overlays/staging/`
- [ ] `overlays/production/`

### 3. `deployments/argo/` — ArgoCD Applications

**Priority:** P1

**Deliverables:**
- [ ] `application.yaml`
- [ ] `appproject.yaml`

## Validation Checklist

- [ ] Helm chart lints successfully
- [ ] HPA configured: min 3, max 50 replicas
- [ ] PDB: maxUnavailable=1
- [ ] ServiceMonitor for Prometheus
- [ ] ArgoCD Application synced
