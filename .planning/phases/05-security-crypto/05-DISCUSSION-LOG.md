# Phase 5: Security & Crypto - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-27
**Phase:** 05-security-crypto
**Areas discussed:** JWT Validation Boundary, mTLS Approach, HSM/KMS Scope

---

## JWT Validation Boundary

|| Option | Description | Selected |
|--------|---------|-----------|----------|
| HTTP Gateway validates all inbound N58/N60 tokens | HTTP GW terminates TLS, validates issuer/audience/expiry/NF-type/scope before forwarding | ✓ |
| Biz Pod validates N58/N60 tokens in each handler | Each handler validates token independently | |
| Both layers validate | Gateway rejects structural issues; Biz Pod checks scope | |

**User's choice:** HTTP Gateway validates all inbound N58/N60 tokens
**Notes:** Biz Pod trusts gateway-validated tokens. Biz Pod still needs own token infrastructure for outbound NF calls (NRF discovery, UDM, AUSF).

---

## mTLS Approach

|| Option | Description | Selected |
|--------|---------|-----------|----------|
| Go stdlib throughout | HTTP GW TLS + Biz→AAA mTLS both use explicit cert loading | ✓ |
| Istio mTLS for HTTP GW (auto cert rotation), Go stdlib mTLS for Biz→AAA | Split approach | |

**User's choice:** Go stdlib throughout, config-driven enable/disable. HTTP Gateway can optionally use Istio mTLS when ISTIO_MTLS=1.
**Notes:** Standardized on stdlib across all mTLS boundaries. Istio mode is optional via env var.

### HTTP Gateway Istio mode

|| Option | Description | Selected |
|--------|---------|-----------|----------|
| Optional — stdlib TLS active unless ISTIO_MTLS=1 env var | Feature flag approach | ✓ |
| Required — always use Istio mTLS in prod, skip stdlib for HTTP GW in Istio mode | Production-enforced Istio | |

**User's choice:** Optional — stdlib TLS active unless ISTIO_MTLS=1 env var
**Notes:** Allows dev/test without Istio cluster while supporting Istio in production.

---

## HSM/KMS Scope

|| Option | Description | Selected |
|--------|---------|-----------|----------|
| Interface + soft mode (dev) + Vault transit engine (prod) | KeyManager backed by Vault API for wrapping/unwrapping | |
| Interface + soft mode + SoftHSM (dev) + Vault (prod) | Full dev/test parity | ✓ |
| Interface + soft mode only — defer Vault/HSM to Phase 7 | Minimum scope | |

**User's choice:** Interface + soft mode + SoftHSM (dev) + Vault transit engine (prod)
**Notes:** kubeadm deployment (not AWS EKS). HashiCorp Vault runs as Kubernetes deployment on self-managed infrastructure. Transit engine for KEK wrap/unwrapping. SoftHSM for local dev/test parity.

---

## Claude's Discretion

The following areas were delegated to implementation discretion:
- TLS cipher suite ordering
- Token cache TTL and eviction policy
- Vault transit engine endpoint path and auth method
- Exact PostgreSQL column types for encrypted fields
- SoftHSM token slot / object label conventions
- Whether to encrypt RADIUS shared secrets in Phase 5 or defer to Phase 6

## Deferred Ideas

None — all security requirements (REQ-20 through REQ-25) discussed within scope.
