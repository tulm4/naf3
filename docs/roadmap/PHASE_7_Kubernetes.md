# Phase 7: Kubernetes — 3-Component Deployment

## Overview

Phase 7 tạo Kubernetes deployment manifests cho 3-component architecture:
1. HTTP Gateway — Envoy, N replicas
2. Biz Pod — NSSAAF app, N replicas
3. AAA Gateway — Envoy + keepalived, 2 replicas (active-standby)

**Design Doc:** `docs/design/01_service_model.md` §5.4, `docs/design/25_kubeadm_setup.md`
**Phase R Dependency:** This phase is superseded by Phase R (3-Component Refactor). See `docs/roadmap/PHASE_Refactor_3Component.md` for the definitive Helm chart structure. Complete Phase R before Phase 7.

## Modules to Implement

### 1. `deployments/helm/nssaa-http-gateway/` — HTTP Gateway Chart

**Priority:** P0
**Design Doc:** `docs/design/01_service_model.md` §5.4.4

**Deliverables:**
- [ ] `Chart.yaml`
- [ ] `values.yaml`
- [ ] `templates/deployment.yaml` (N replicas, pod IP binding)
- [ ] `templates/service.yaml` (LoadBalancer)
- [ ] `templates/servicemonitor.yaml`

### 2. `deployments/helm/nssaa-biz/` — Biz Pod Chart

**Priority:** P0
**Design Doc:** `docs/design/01_service_model.md` §5.4

**Deliverables:**
- [ ] `Chart.yaml`
- [ ] `values.yaml`
- [ ] `templates/deployment.yaml` (N replicas)
- [ ] `templates/service.yaml` (Headless ClusterIP for Biz pod routing)
- [ ] `templates/hpa.yaml` (autoscaling)
- [ ] `templates/pdb.yaml` (maxUnavailable=1)
- [ ] `templates/configmap.yaml`
- [ ] `templates/secret.yaml`
- [ ] `templates/servicemonitor.yaml`

### 3. `deployments/helm/nssaa-aaa-gateway/` — AAA Gateway Chart

**Priority:** P0
**Design Doc:** `docs/design/01_service_model.md` §5.4.5

**Deliverables:**
- [ ] `Chart.yaml`
- [ ] `values.yaml`
- [ ] `templates/deployment.yaml` (replicas=2, strategy=Recreate)
- [ ] `templates/service.yaml` (Headless ClusterIP)
- [ ] `templates/configmap-keepalived.yaml`
- [ ] `templates/network-attachment.yaml` (Multus CNI)
- [ ] `templates/servicemonitor.yaml`

### 4. `deployments/kustomize/` — Kustomize Overlays

**Priority:** P1

**Deliverables:**
- [ ] `base/` — shared base for all components
- [ ] `overlays/development/` — single binary or minimal 3-component
- [ ] `overlays/staging/` — 3-component
- [ ] `overlays/production/` — full 3-component + Multus CNI + keepalived

### 5. `deployments/argo/` — ArgoCD Applications

**Priority:** P1

**Deliverables:**
- [ ] `application.yaml` (per component)
- [ ] `appproject.yaml`

## Validation Checklist

- [ ] `helm lint` passes for all three charts
- [ ] HTTP Gateway: replicas configurable, pod IP binding configured
- [ ] Biz Pod: HPA min 3, max 50; PDB maxUnavailable=1
- [ ] AAA Gateway: replicas=2, strategy=Recreate
- [ ] keepalived ConfigMap has correct VIP and virtualRouterId
- [ ] Multus CNI NetworkAttachmentDefinition for `aaa-bridge-vlan`
- [ ] ServiceMonitor for Prometheus
- [ ] ArgoCD Application synced
