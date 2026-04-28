# Phase 6: Integration Testing & NRM - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-28 (supplemental: AAA-S Diameter library + SCTP)
**Phase:** 06-integration-testing-nrm
**Areas discussed:** 2 (AAA-S Diameter library choice, AAA-S SCTP support)

---

## Area 1: AAA-S Diameter Library

|| Option | Description | Selected |
|--------|---------|-------------|----------|
| Manual parsing (current) | No new dependency in test/; hand-written CER/CEA and AVP encoding | |
| go-diameter/v4 | Uses `sm.Client`/`sm.Listener` for RFC 6733-compliant state machine; mirrors production code | ✓ |
| Hybrid | go-diameter for CER/CEA handshake, manual for DER/DEA EAP handling | |
| Defer to Phase 8 | Leave TCP-only, add go-diameter when load testing requires full fidelity | |

**User's choice:** go-diameter/v4
**Notes:** go-diameter/v4 is already in `go.mod` (used by `internal/diameter/` production code). Using it in the simulator gives E2E tests fidelity matching production behavior — CER/CEA handshake, DWR/DWA watchdog, connection state management. This was flagged as missing from the initial context (which only described TCP-only).

---

## Area 2: AAA-S SCTP Support

|| Option | Description | Selected |
|--------|---------|-------------|----------|
| Add SCTP support | Update `mode.go` + `diameter.go` to support both SCTP and TCP; `AAA_SIM_DIAMETER_TRANSPORT` env var | ✓ |
| TCP only (current) | Keep `net.Listen("tcp", ...)` — SCTP is Phase 8 concern | |
| SCTP not needed | E2E tests don't need SCTP fidelity | |

**User's choice:** Add SCTP support
**Notes:** TS 29.561 Ch.17 specifies SCTP as the preferred Diameter transport in 3GPP deployments. The context (D-06, D-07) only mentioned TCP. The user wants the AAA-S simulator to support both transports. `test/aaa_sim/diameter.go` already accepts `net.Listener` from the caller, making transport pluggable. go-diameter/v4/sm natively supports SCTP via `sm.Listen` with an SCTP listener.

---

## Claude's Discretion

No items delegated to Claude in this supplemental discussion.

## Deferred Ideas

None — both items were within phase scope and resolved.

