# Architecture Vocabulary

This file defines the precise meaning of architecture terms used in `ARCHITECTURE_DEEPENING_PLAN.md` and throughout the codebase architecture discussions.

## Core Terms

### Module

Anything with an interface and an implementation. A single function, a package, a struct with methods — all are modules.

Examples:
- The `NRFClient` in `internal/nrf/client.go` is a module
- The `Repository` in `internal/storage/postgres/session.go` is a module
- The `AAARouter` interface in `internal/eap/engine_client.go` is a module

### Interface

Everything a caller must know to use a module: types, invariants, error modes, ordering, config. Not just the type signature.

Example: `NRFClient.DiscoverNFInstances(ctx, nfType, queryParams)` has an interface that includes:
- Input: NF type, query parameters
- Output: slice of NFProfile or error
- Invariant: returns within 30s or circuit breaker opens
- Error modes: 404, 502, 503, timeout

### Implementation

The code inside a module. The things that callers do not need to know.

### Depth

Leverage at the interface. A **deep** module has a simple interface that controls a lot of behavior. A **shallow** module has an interface nearly as complex as the implementation.

Example of deep: `KeyManager.Encrypt(plaintext)` → handles KEK rotation, envelope encryption, versioning transparently.

Example of shallow: A pass-through that validates input and calls another function with no additional behavior.

### Seam

Where an interface lives. A place where behavior can be altered without editing in place.

Examples:
- `AAARouter` is a seam between the EAP engine and the protocol client
- `SessionStore` is a seam between API handlers and the storage implementation
- `KeyManager` is a seam between the encryptor and the KMS backend

### Adapter

A concrete thing satisfying an interface at a seam.

Examples:
- `radius.Client` is an adapter at the `AAARouter` seam
- `diameter.Client` is an adapter at the `AAARouter` seam
- `SoftKeyManager` is an adapter at the `KeyManager` seam
- `VaultKeyManager` is an adapter at the `KeyManager` seam
- `postgres.Store` is an adapter at the `SessionStore` seam

### Leverage

What callers get from depth. High leverage = small interface, large behavior.

### Locality

What maintainers get from depth. Change, bugs, and knowledge concentrated in one place.

### Pass-through

A module that adds no behavior beyond delegating to another module. Pass-throughs fail the deletion test — deleting them makes the codebase simpler, not more complex.

## Key Principles

### Deletion Test

Imagine deleting a module. If all the complexity vanishes, it was a pass-through. If the complexity reappears spread across N callers, it was earning its keep.

### Interface is the Test Surface

Tests should exercise the interface, not the implementation. If you cannot test a module through its interface, the interface is too narrow.

### Two Adapters = Real Seam

One adapter = hypothetical seam. Two adapters = real seam. If there is only one implementation of an interface, the seam may not be worth the indirection.

## NSSAAF-Specific Terms

### EAP Tunnel

The logical channel through which EAP messages flow between AMF and AAA-S. Implemented by `AAARouter` with adapters for RADIUS and Diameter.

### NssaaStatus

The API-facing slice authorization status, tracked per S-NSSAI per UE. State machine: `NOT_EXECUTED` → `PENDING` → `EAP_SUCCESS | EAP_FAILURE`.

### SessionState

The EAP session state machine inside the EAP engine. State machine: `SessionStateInit` → `SessionStateEapExchange` → `SessionStateDone | SessionStateFailed | SessionStateTimeout`.

### AuthCtx

The slice authentication context, stored per-UE per-S-NSSAI. Contains NssaaStatus, EAP session state, and metadata.

### NF Client

A client that calls another 5G Network Function (NRF, UDM, AMF, AUSF) via HTTP/2SBI.

### Crypto Scheme

A method for encrypting data. Two schemes exist in the codebase:
- **Structured**: `EncryptedData{ciphertext, nonce, tag}` with AAD (used by envelope encryption)
- **Concat**: `nonce||ciphertext||tag` concatenated bytes (used by postgres encryptor and passphrase encryptor)