# Architecture Deepening Plan

**Date:** 2026-05-04
**Source:** `/mattpocock-improve-codebase-architecture` skill
**Scope:** `internal/api/`, `internal/nrf/`, `internal/udm/`, `internal/amf/`, `internal/ausf/`, `internal/cache/`, `internal/eap/`, `internal/radius/`, `internal/diameter/`, `internal/storage/`, `internal/crypto/`, `internal/session/`, `internal/aaa/`, `internal/biz/`, `cmd/`

---

## Methodology

Per the mattpocock-improve-codebase-architecture skill:

- **Deletion test**: if a module is deleted, does complexity vanish (pass-through) or reappear across N callers (earning its keep)?
- **The interface is the test surface**
- **One adapter = hypothetical seam. Two adapters = real seam.**
- Vocabulary: module, interface, seam, adapter, depth, shallow, leverage, locality

---

## Candidates (numbered by priority)

### Candidate 1: NF Client Base Pattern — Collapse Duplication Across AMF/UDM/AUSF/NRF

**Files involved:**
- `internal/nrf/client.go`
- `internal/udm/client.go`
- `internal/amf/client.go`
- `internal/ausf/client.go`
- `internal/resilience/resilience.go`
- `internal/cache/redis/dlq.go`

**Problem:** Four NF clients each independently implement the same 25-line HTTP transport init block, three different retry patterns, asymmetric circuit breaker coverage, and inconsistent DLQ wiring. AMF has fault isolation (circuit breakers + DLQ + resilience.Do); UDM and AUSF have zero fault isolation. RADIUS client also has inline retry (different from all three).

**Solution:** Extract a `NFClient` base struct with a shared `http.Client` factory, single retry helper (replacing three implementations), circuit breaker wiring, and DLQ support. Wire AUSF and UDM through the same resilience path as AMF.

**Benefits:**
- *Leverage*: All NF clients gain consistent fault isolation with one change.
- *Locality*: The retry/cb/DLQ logic lives in one place. Bugs fixed once, not four times.
- *Tests*: One set of tests covers retry, circuit breaking, DLQ for all NF clients.

**ADR conflict:** None. Consistent with the resilience architecture in Phase 4 (05-CONTEXT.md).

---

### Candidate 2: Storage Module — Fix Circular Dependency and Split Interfaces

**Files involved:**
- `internal/storage/postgres/session.go`
- `internal/session/store.go`
- `internal/api/nssaa/handler.go`
- `internal/api/aiw/handler.go`
- `internal/session/adapter.go`
- `internal/storage/storage.go` (empty)

**Problem:** `internal/storage/postgres/` imports `internal/api/nssaa/` and `internal/api/aiw/` to access their concrete `AuthCtx` types. This creates a circular dependency: the storage module (domain) depends on API packages (consumers). The `session.SessionStore` interface in `session/store.go` is the right abstraction but is bypassed by the postgres Store which returns `*Store` implementing the API-level `AuthCtxStore` directly. The `session/adapter.go` adapters are dead code for the postgres path.

**Solution:** Reverse the dependency: define `SessionStore` in `internal/storage/` as the canonical interface. Make the postgres `*Store` return this interface. Delete `session/adapter.go` (no longer needed). Make `internal/api/nssaa/` and `internal/api/aiw/` import `internal/storage/` — not the other way around.

**Benefits:**
- *Locality*: Storage concerns live in one module. Circular dependency is eliminated.
- *Leverage*: API handlers depend on a stable interface. Storage implementation can be swapped.
- *Tests*: Handlers can be tested with an in-memory `SessionStore` mock without touching the database.

**ADR conflict:** None.

---

### Candidate 3: EAP Engine — Deepen the AAARouter Interface and Add NssaaStatus Integration Tests

**Files involved:**
- `internal/eap/engine_client.go`
- `internal/eap/engine.go`
- `internal/eap/fragment.go`
- `internal/eap/engine_test.go`
- `internal/types/types.go`
- `internal/types/types_test.go`
- `internal/aaa/router.go`
- `internal/radius/client.go`
- `internal/diameter/client.go`

**Problem (shallow interface):** The `AAARouter` interface passes `*Session` into the protocol layer, exposing EAP internal state to callers. The interface should be: "here is an EAP payload, here is the GPSI, here is the S-NSSAI, give me back an EAP response" — no `Session` knowledge needed.

**Problem (wrong test level):** `NssaaStatus` state transitions are only tested at the types level (`TestNssaaStatusValidate`, `TestNssaaStatusHelpers`). The actual transitions happen in `advanceState()` in `engine.go` but are not tested in integration with the storage layer.

**Problem (dead code):** `internal/aaa/router.go` is the deprecated Biz Pod router with correctness bugs: uses `authCtxID` as GPSI instead of the real GPSI, hardcodes SNSSAI to zero.

**Solution (shallow fix):** Refactor `AAARouter` to take `(gpsi, eapPayload, snssaiSst, snssaiSd)` instead of `*Session`. Move session state management fully inside the engine.

**Solution (test fix):** Add integration tests in `engine_test.go` that verify `NssaaStatus` transitions through a complete EAP flow with a mock `SessionStore`.

**Solution (cleanup):** Delete `internal/aaa/router.go` (deprecated) or fix the correctness bugs if it is still in use.

**Benefits:**
- *Leverage*: The engine becomes protocol-agnostic. New AAA protocols need only implement the narrow `AAARouter` interface.
- *Locality*: `NssaaStatus` state machine tested in one place, not scattered across types + engine.
- *Tests*: A mock `AAARouter` can exercise all engine state transitions without a real AAA server.

---

### Candidate 4: Protocol Clients — Add Shared Error Abstraction and Test RADIUS/Diameter Swappability

**Files involved:**
- `internal/radius/client.go`
- `internal/diameter/client.go`
- `internal/eap/engine_client.go`
- `internal/proto/transport.go`
- `internal/aaa/gateway/radius_forward.go`
- `internal/aaa/gateway/diameter_forward.go`

**Problem:** RADIUS and Diameter clients have incompatible APIs (`SendEAP` vs `SendDER`) with no common interface. Protocol-specific errors (timeout, invalid response, ID mismatch) are not mapped to 3GPP HTTP status codes (502, 503, 504). GPSI hashing, EAP fragmentation, and S-NSSAI encoding are duplicated across both clients. The deprecated gateway (`radius_forward.go`, `diameter_forward.go`) has code that duplicates the protocol clients (`FragmentEAPMessage` called in both places).

**Solution:** Define a `AAAClient` interface in `internal/eap/engine_client.go` that both clients implement. Extract shared `AAAError` type with 3GPP cause codes. Move `FragmentEAPMessage` to `internal/eap/fragment.go` as the single source of truth. Define `SnssaiCodec` interface for S-NSSAI encoding, implemented separately by RADIUS and Diameter.

**Benefits:**
- *Leverage*: The EAP engine can call either client through the same interface. Swapping RADIUS for Diameter requires one type assertion.
- *Locality*: Protocol-specific encoding lives in one place per protocol.
- *Tests*: A fake `AAAClient` can test the EAP engine without a real protocol stack.

---

### Candidate 5: Encryption — Add Pool Interface and Fix Silent Error Suppression in Repository

**Files involved:**
- `internal/storage/postgres/session.go`
- `internal/storage/postgres/pool.go`
- `internal/storage/postgres/session_store.go`
- `internal/storage/postgres/session_test.go`
- `internal/crypto/aead.go`
- `internal/crypto/encrypt.go`
- `internal/crypto/kms.go`

**Problem (tight coupling):** The `Repository` struct holds a concrete `*pgxpool.Pool`. There is no `Pool` interface. Every repository method calls `pool.Exec/Query/QueryRow` directly, making the Repository untestable without a real database or invasive mocks.

**Problem (silent errors):** `decryptField` (`session.go:402-403`) discards `fmt.Errorf` on decryption failure silently, returning empty bytes. Callers (`scanSession`, `scanSessionFromRows`) inherit this behavior without knowing data is missing.

**Problem (two schemes):** `Encrypt`/`Decrypt` (structured `EncryptedData` with AAD) and `EncryptConcat`/`DecryptConcat` (concatenated nonce||ct||tag) are two separate schemes with no shared interface. The postgres encryptor uses the concat scheme exclusively. The envelope encryption uses the structured scheme exclusively.

**Solution:** Define a `Pool` interface in `storage/postgres/`. Update all repository methods to accept the interface. Add a mock `Pool` for testing. Fix `decryptField` to return an error instead of silently discarding it. Consider a shared `CryptoScheme` interface to unify the two encryption approaches.

**Benefits:**
- *Leverage*: Repository tests can use an in-memory mock Pool, eliminating the need for a live database.
- *Locality*: Decryption failures surface immediately instead of producing silent data loss.
- *Tests*: All repository methods become testable at the unit level.

---

### Candidate 6: AAA Config Encryption — Shared Encryptor for AAA Secrets and Session Data

**Files involved:**
- `internal/storage/postgres/aaa_config.go`
- `internal/storage/postgres/session.go`
- `internal/storage/postgres/secret.go`
- `internal/crypto/crypto.go`

**Problem:** `encryptSecret`/`decryptSecret` in `aaa_config.go` use `crypto.FromPassphrase` + `crypto.EncryptConcat` directly, duplicating the encryption logic from `session.go`'s `encryptor`. If the encryption scheme changes, both places must be updated. Additionally, `FromPassphrase` is deterministic (no salt/KDF beyond Blake2b), which may not be production-appropriate for AAA secrets that need PBKDF2 or Argon2.

**Solution:** Extract a `SecretEncryptor` interface shared by both AAA config and session storage. Use the same encryptor for both workloads, with a shared key derived from a master secret (stored in Vault/SoftHSM via the KeyManager, not derived from a passphrase in plaintext).

**Benefits:**
- *Locality*: One encryption implementation for all secrets.
- *Leverage*: Changing the encryption scheme requires one change.
- *Tests*: One test suite covers all secret encryption.

---

### Candidate 7: Global Crypto State — Replace `crypto.Init` Singleton with Constructor Injection

**Files involved:**
- `internal/crypto/config.go`
- `internal/crypto/crypto.go`
- `internal/crypto/kms.go`
- All test files that call `crypto.Init()` or reset `globalKM`

**Problem:** `crypto.Init()` sets a package-level `globalKM` global. `crypto.KM()` panics if not initialized. Every test that touches `SoftKeyManager` or envelope encryption must manage this global state. There is no `NewKeyManager()` constructor — instantiation always goes through `Init`, which is global and imperative.

**Solution:** Replace `globalKM` with a functional options pattern: `NewKeyManager(opts ...Option) KeyManager`. Tests create a fresh `SoftKeyManager` directly without touching global state. The `crypto.KM()` getter becomes a no-op or is removed.

**Benefits:**
- *Locality*: Each test creates its own `KeyManager` in `beforeFn`.
- *Leverage*: The crypto package is usable without initialization ceremony.
- *Tests*: No global state pollution between tests.

---

### Candidate 8: Session TTL — Extract Constants and Make Configurable

**Files involved:**
- `internal/session/adapter.go`
- `internal/storage/postgres/session_store.go`
- `internal/storage/postgres/session.go`

**Problem:** Two magic numbers for session TTLs are hardcoded inline:
- AIW: `24 * time.Hour` in `session/adapter.go:175`
- NSSAA: `5 * time.Minute` in `storage/postgres/session_store.go:80`

These are not configurable, not extracted as constants, and not documented.

**Solution:** Define `const DefaultAIWSessionTTL = 24 * time.Hour` and `const DefaultNSSAASessionTTL = 5 * time.Minute` in the respective packages. Make them configurable via the config struct passed to constructors.

**Benefits:**
- *Locality*: TTL is documented in one place.
- *Leverage*: Operations can change TTL without code changes.
- *Tests*: Different TTLs can be tested without patching time.

---

## Which of these would you like to explore?

For each candidate, the deepening involves:
1. Walking the design tree — constraints, dependencies, the shape of the deepened module
2. What sits behind the seam (the concrete adapters)
3. What tests survive

**Enter a number (1-8) or "all" to proceed.**