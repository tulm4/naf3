# Rate Limit Gaps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Normalize and harden the existing rate-limit enforcement already present in NSSAA and AIW handler paths so that each protected scope uses the correct limiter policy, Redis failure behavior is explicit, and allow/limited/error outcomes are observable and tested on real request paths.

**Architecture:** Keep the existing Redis sorted-set sliding-window limiter in `internal/cache/redis/ratelimit.go` and preserve the current handler-level enforcement model. Replace the current single-limiter / mixed-scope wiring with explicit per-scope limiter instances, add a small shared decision helper only where it reduces duplication, and expand request-path tests plus decision metrics so throttling behavior is deterministic, visible, and aligned with the current codebase.

**Tech Stack:** Go 1.22+, `net/http`, `go-redis/v9`, Prometheus metrics, `miniredis`, `testify`

---

## Scope check

This is one plan, not multiple subsystems. The rate limiter already exists and is already invoked from live handlers, so this work is a focused gap-closure effort: refine existing enforcement, correct scope/policy mismatches, document failure semantics, and add observability and test coverage.

## Current codebase reality

Before implementing, keep these facts in mind:

- NSSAA already rate-limits in `internal/api/nssaa/handler.go`
  - create path currently checks `AllowAMF(...)`
  - confirm path currently checks `Allow("authctx:" + authCtxId)`
- AIW already rate-limits in `internal/api/aiw/handler.go`
  - create path currently checks `Allow("aiw:supi:" + string(body.Supi))`
- `cmd/biz/factory.go` currently builds **one** limiter with:
  - `window = 1*time.Minute`
  - `limit = f.cfg.RateLimit.PerGpsiPerMin`
- that single limiter is reused across AMF, GPSI, SUPI, and auth context scopes, which means the configured `PerAmfPerSec` policy is not actually honored
- current metrics only increment on denied requests, not allow/error outcomes
- current Redis failure behavior is effectively fail-open because handlers warn on limiter errors and continue processing

This plan therefore treats the work as **refinement of existing live behavior**, not first-time handler wiring.

## File map

### Existing files to modify

- `internal/cache/redis/ratelimit.go`
  - keep the sliding-window implementation
  - optionally add tiny exported key helpers only if needed for test stability
- `internal/api/nssaa/handler.go`
  - keep enforcement in the handler
  - refactor duplicated decision handling only as much as needed
  - ensure AMF/GPSI/auth-context scopes use explicit policy wiring and observability
- `internal/api/nssaa/handler_test.go`
  - expand current request-path tests to cover allow, limited, and Redis-error cases
- `internal/api/aiw/handler.go`
  - keep enforcement in the handler
  - ensure SUPI/auth-context scopes use explicit policy wiring and observability
- `internal/api/aiw/handler_test.go`
  - expand request-path tests to cover allow, limited, and Redis-error cases
- `internal/metrics/metrics.go`
  - expand rate-limit observability from deny-only to decision-based metrics
- `internal/config/config.go`
  - add explicit failure-mode config validation if kept configurable
- `cmd/biz/factory.go`
  - replace the current single-limiter wiring with explicit per-scope limiter wiring
- `docs/superpowers/specs/2026-06-03-rate-limit-gaps-spec.md`
  - update the spec to reflect the chosen failure mode and the fact that handlers now enforce the limiter on real paths

### Existing files to inspect while implementing

- `internal/logging/gpsi.go`
  - use the log-specific GPSI hashing helper for structured logs
- `internal/crypto/hash.go`
  - keep using the stable deterministic hash for cache/storage keys if needed
- `internal/api/common/middleware.go`
  - follow request ID and logging conventions
- `cmd/biz/factory.go`
  - align construction with the existing dependency injection pattern
- `docs/quickref.md`
  - keep config naming/documentation consistent

### Files to update only if implementation proves they are truly tracked by roadmap state

- `docs/roadmap/module_index.md`
- `docs/roadmap/README.md`
- `.planning/STATE.md`
- `.planning/PROJECT.md`
- `.planning/REQUIREMENTS.md`

Do **not** edit these by default. Update them only if this gap-fix is already represented as tracked roadmap or planning work.

---

## Design decisions locked by this plan

1. **Redis failure policy:** fail open.
   - Reason: these are telecom control-plane request paths, and the current code already behaves fail-open. The work should make that behavior explicit, documented, and tested instead of accidental.

2. **Primary implementation style:** preserve existing handler-level enforcement.
   - Do not introduce a large new orchestration package unless implementation proves it is necessary.
   - Prefer a small shared helper in an existing package, or tiny handler-local helpers, over a new subsystem.

3. **Limiter topology:** use explicit per-scope limiter instances as the primary design.
   - AMF scope: second-based policy using `PerAmfPerSec`
   - GPSI scope: minute-based policy using `PerGpsiPerMin`
   - SUPI scope: explicit decision required; unless a separate config is added now, reuse the subscriber-scoped minute policy intentionally and document it
   - auth context scope: explicit decision required; unless a dedicated config is added now, keep current behavior but make its policy explicit
   - `GlobalPerSec`: either wire it intentionally in this change or explicitly document it as not yet enforced; do not leave it ambiguous

4. **Redis key naming:** preserve existing key namespace conventions unless a migration is explicitly justified.
   - Keep the `nssaa:ratelimit:*` naming style used by the current limiter package.
   - Do not silently rename Redis keys to a new namespace.

5. **Observability contract:** every decision must record one of `allowed`, `limited`, or `error` with service and scope labels.
   - GPSI must always be hashed in logs using the **logging** hash helper.
   - SUPI logging policy must be explicit: either hash it, omit it, or log only a safer correlated identifier. Do not leave this to implementer judgment.

6. **Hashing distinction:**
   - `internal/crypto.HashGPSI` is for stable deterministic keying where needed
   - `internal/logging.HashGPSI` is for log-safe observability
   - do not mix them without intent

---

## Task 1: Lock policy decisions and align config/model with real scope behavior

**Files:**
- Modify: `internal/config/config.go`
- Modify: `docs/superpowers/specs/2026-06-03-rate-limit-gaps-spec.md`
- Test: `internal/config/config_test.go` or the nearest existing config validation test file

- [ ] **Step 1: Write the failing config validation test for rate-limit failure mode**

```go
func TestConfigValidate_RateLimitFailureMode(t *testing.T) {
    cfg := validConfigForTest()

    cfg.RateLimit.FailureMode = "open"
    require.NoError(t, cfg.Validate())

    cfg.RateLimit.FailureMode = ""
    require.NoError(t, cfg.Validate())

    cfg.RateLimit.FailureMode = "invalid"
    require.ErrorContains(t, cfg.Validate(), "rateLimit.failureMode")
}
```

If there is no nearby config test file with a stable setup helper, create the smallest possible table-driven test in the existing config test package.

- [ ] **Step 2: Run the focused config validation test and confirm it fails first**

Run: `go test ./internal/config -run 'RateLimitFailureMode' -count=1`

Expected: FAIL because `FailureMode` is not yet modeled or validated.

- [ ] **Step 3: Add the explicit failure-mode field to config and validate it**

```go
// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
    PerGpsiPerMin int    `yaml:"perGpsiPerMin"`
    PerAmfPerSec  int    `yaml:"perAmfPerSec"`
    GlobalPerSec  int    `yaml:"globalPerSec"`
    FailureMode   string `yaml:"failureMode"`
}
```

```go
switch c.RateLimit.FailureMode {
case "", "open", "closed":
default:
    return fmt.Errorf("rateLimit.failureMode must be one of: open, closed")
}
```

- [ ] **Step 4: Update the spec document with explicit policy decisions before code wiring spreads them further**

Add concrete text like:

```md
### 6.2 Policy scope

Current implementation direction:
- AMF requests use `perAmfPerSec`
- subscriber-scoped traffic uses `perGpsiPerMin`
- auth-context confirmation traffic uses the subscriber-scoped minute policy unless a dedicated limit is introduced later
- `globalPerSec` remains documented but is not enforced in this change unless explicitly wired during implementation
```

```md
### 6.3 Redis failure semantics

Selected behavior: fail open.

Rationale: these NSSAAF and AIW control-plane request paths preserve availability during transient Redis failures. Limiter failures are logged and counted as `error` decisions while request processing continues.
```

- [ ] **Step 5: Re-run the focused config test**

Run: `go test ./internal/config -run 'RateLimitFailureMode' -count=1`

Expected: PASS

- [ ] **Step 6: Commit the policy/config lock-in**

```bash
git add internal/config/config.go internal/config docs/superpowers/specs/2026-06-03-rate-limit-gaps-spec.md
git commit -m "$(cat <<'EOF'
feat: define explicit rate-limit failure policy

Make the limiter failure mode explicit in config and document how scope policies map to current handler enforcement.
EOF
)"
```

## Task 2: Expand limiter observability without adding a heavy new subsystem

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/api/nssaa/handler.go`
- Modify: `internal/api/aiw/handler.go`
- Inspect: `internal/logging/gpsi.go`
- Test: `internal/api/nssaa/handler_test.go`
- Test: `internal/api/aiw/handler_test.go`

- [ ] **Step 1: Write a focused failing test that proves allowed/limited/error decisions are all observable through the request path**

Use the smallest stable handler test already present and assert only what the current test harness can realistically observe first. For example, if metric state is hard to inspect directly in request-path tests, start by adding a helper-level assertion around the decision metric labels in a narrow metrics/unit test.

```go
func TestRateLimitMetrics_RecordsDecisionLabels(t *testing.T) {
    // Arrange metrics state reset using the project's existing metric test pattern.
    // Record one allowed, one limited, and one error decision.
    // Assert counter values for:
    // service=nssaa, scope=amf, result=allowed
    // service=nssaa, scope=amf, result=limited
    // service=nssaa, scope=amf, result=error
}
```

If there is no current metrics test pattern, add a small unit test near `internal/metrics/metrics.go` instead of forcing this into handler tests.

- [ ] **Step 2: Run the focused observability test first**

Run: `go test ./internal/metrics -run 'RateLimitMetrics' -count=1`

Expected: FAIL because the metric shape is currently deny-only.

- [ ] **Step 3: Expand the metric labels from deny-only to decision-based labels**

```go
// RateLimitRequests tracks rate-limit decisions by service, scope, and result.
RateLimitRequests = newCounterVec(prometheus.CounterOpts{
    Name: "nssAAF_ratelimit_requests_total",
    Help: "Total rate-limit decisions by service, scope, and result",
}, []string{"service", "scope", "result"})
```

- [ ] **Step 4: Add the smallest possible shared helper for recording decisions if duplication becomes noisy**

Do not create a new subsystem unless necessary. A tiny helper is enough:

```go
func observeRateLimitDecision(service, scope, result string) {
    metrics.RateLimitRequests.WithLabelValues(service, scope, result).Inc()
}
```

Place this where it best matches the existing codebase style:
- either near handler rate-limit code, or
- in a very small existing package-level helper file

- [ ] **Step 5: Make logging policy explicit in code changes**

For NSSAA GPSI-scoped logging:

```go
slog.Warn("ratelimit decision error",
    "service", "nssaa",
    "scope", "gpsi",
    "gpsi_hash", logging.HashGPSI(string(body.Gpsi)),
    "error", rlErr,
    "request_id", reqID,
)
```

For AIW SUPI-scoped logging, choose one explicit rule and keep it consistent in all AIW handler changes. Preferred minimal policy for this plan:
- do **not** log raw SUPI
- log only `scope`, `request_id`, and the error unless the codebase already has a safe SUPI hashing convention

- [ ] **Step 6: Re-run the focused observability tests**

Run: `go test ./internal/metrics -run 'RateLimitMetrics' -count=1`

Expected: PASS

- [ ] **Step 7: Commit the observability changes**

```bash
git add internal/metrics/metrics.go internal/api/nssaa/handler.go internal/api/aiw/handler.go
git commit -m "$(cat <<'EOF'
feat: record rate-limit decisions across outcomes

Track allowed, limited, and error outcomes so handler throttling behavior is visible in metrics and structured logs.
EOF
)"
```

## Task 3: Fix NSSAA scope wiring to use explicit policy-backed limiters

**Files:**
- Modify: `cmd/biz/factory.go`
- Modify: `internal/api/nssaa/handler.go`
- Modify: `internal/api/nssaa/handler_test.go`
- Inspect: `internal/cache/redis/ratelimit.go`

- [ ] **Step 1: Write failing NSSAA request-path tests for the current policy gaps**

Keep the tests grounded in the existing package API. Extend the current tests that already hit the real request path.

Add at least these cases:

```go
func TestNSSAAHandler_Create_RateLimit_AMFLimited_Returns429(t *testing.T) {
    // Use miniredis and the existing handler construction pattern.
    // Pre-exhaust the AMF limiter key for the same AMF instance.
    // POST create request.
    // Expect 429 and Retry-After header.
}

func TestNSSAAHandler_Create_RateLimit_RedisError_FailsOpen(t *testing.T) {
    // Inject the smallest possible failing limiter dependency using the real constructor pattern.
    // POST create request.
    // Expect business-path success (201 Created), not 429.
}

func TestNSSAAHandler_Confirm_RateLimit_Limited_Returns429(t *testing.T) {
    // Pre-exhaust the auth-context or subscriber-scoped limiter used by confirm.
    // PUT confirm request.
    // Expect 429.
}
```

- [ ] **Step 2: Run only the NSSAA rate-limit tests first**

Run: `go test ./internal/api/nssaa -run 'RateLimit' -count=1`

Expected: FAIL because the current wiring uses one shared limiter policy and incomplete observability.

- [ ] **Step 3: Keep handler-level enforcement but reduce duplication with a tiny local helper if useful**

If repeated blocks become noisy, use a small helper inside the package rather than a new cross-cutting subsystem:

```go
func handleRateLimitDecision(w http.ResponseWriter, service, scope string, allowed bool, err error, retryAfter int) bool {
    // record metrics
    // log error case
    // write 429 when blocked
    // return true when the request handler should stop
}
```

Only keep this helper if it genuinely reduces duplication without obscuring handler readability.

- [ ] **Step 4: Ensure NSSAA create path uses explicit AMF and GPSI logic intentionally**

The code should make the order explicit:
- check AMF-scoped limiter using the per-second AMF policy
- check GPSI-scoped limiter using the subscriber minute policy if GPSI-scope limiting is part of the chosen design for the create path
- continue to validation and business logic only when the request is allowed or a limiter error is handled fail-open

When logging GPSI-related limiter errors, always use `logging.HashGPSI(...)` rather than raw GPSI.

- [ ] **Step 5: Ensure confirm path uses an explicitly documented policy**

Pick one and make it explicit in code and tests:
- keep auth-context rate limiting using the subscriber minute policy, or
- remap confirm requests to subscriber scope after loading the context

Do not leave this as an implicit string key with unexplained policy inheritance.

- [ ] **Step 6: Re-run NSSAA package tests**

Run: `go test ./internal/api/nssaa -count=1`

Expected: PASS

- [ ] **Step 7: Commit the NSSAA scope/policy wiring**

```bash
git add cmd/biz/factory.go internal/api/nssaa/handler.go internal/api/nssaa/handler_test.go
git commit -m "$(cat <<'EOF'
feat: align NSSAA throttling with explicit scope policies

Use policy-backed limiter wiring for NSSAA request scopes so AMF and confirmation throttling behavior matches configured intent.
EOF
)"
```

## Task 4: Fix AIW scope wiring and make SUPI handling explicit

**Files:**
- Modify: `cmd/biz/factory.go`
- Modify: `internal/api/aiw/handler.go`
- Modify: `internal/api/aiw/handler_test.go`

- [ ] **Step 1: Write failing AIW request-path tests for limit and Redis failure behavior**

Extend the current AIW handler tests using the existing package construction pattern.

Add at least:

```go
func TestAIWHandler_Create_RateLimit_Limited_Returns429(t *testing.T) {
    // Pre-exhaust the subscriber-scoped limiter used by AIW.
    // POST create request.
    // Expect 429.
}

func TestAIWHandler_Create_RateLimit_RedisError_FailsOpen(t *testing.T) {
    // Inject a failing limiter dependency.
    // POST create request.
    // Expect 201 Created.
}

func TestAIWHandler_Confirm_RateLimit_Limited_Returns429(t *testing.T) {
    // Pre-exhaust the explicitly chosen confirm limiter scope.
    // PUT confirm request.
    // Expect 429.
}
```

- [ ] **Step 2: Run AIW rate-limit tests first**

Run: `go test ./internal/api/aiw -run 'RateLimit' -count=1`

Expected: FAIL until scope wiring and decision observability are aligned.

- [ ] **Step 3: Keep AIW enforcement in the handler and make the SUPI logging policy explicit**

Preferred minimal behavior for this plan:
- subscriber-scoped rate limiting may still use the SUPI-derived key currently used by AIW
- do not log raw SUPI
- for limiter errors, log only service, scope, error, and request ID unless a safe SUPI hash helper is intentionally introduced

Example pattern:

```go
if rlErr != nil {
    slog.Warn("ratelimit decision error",
        "service", "aiw",
        "scope", "supi",
        "error", rlErr,
        "request_id", reqID,
    )
}
```

- [ ] **Step 4: Re-run the AIW package tests**

Run: `go test ./internal/api/aiw -count=1`

Expected: PASS

- [ ] **Step 5: Commit the AIW scope/policy wiring**

```bash
git add cmd/biz/factory.go internal/api/aiw/handler.go internal/api/aiw/handler_test.go
git commit -m "$(cat <<'EOF'
feat: align AIW throttling with explicit subscriber policies

Make AIW rate-limit behavior explicit for subscriber and confirmation paths while preserving fail-open handling for Redis errors.
EOF
)"
```

## Task 5: Replace the current single-limiter factory wiring with explicit per-scope limiters

**Files:**
- Modify: `cmd/biz/factory.go`
- Test: `cmd/biz/factory_test.go` or nearest existing build/config wiring test
- Inspect: runtime config fixtures only if they are already used by tests

- [ ] **Step 1: Write a focused failing factory/config test that proves the current single-limiter mismatch**

Pick the lightest stable test shape available in the repo. If full Biz factory tests are too expensive, validate constructor output or a tiny extracted helper instead.

```go
func TestBuildBizPod_UsesDistinctRateLimitPolicies(t *testing.T) {
    cfg := minimalBizConfig(t)
    cfg.RateLimit.PerGpsiPerMin = 10
    cfg.RateLimit.PerAmfPerSec = 1000

    // Build or call a tiny extracted wiring helper.
    // Assert that AMF and subscriber scopes do not share the exact same limiter configuration.
}
```

If that assertion is too indirect for the current code structure, first extract a tiny helper that returns the scope limiter set and test that directly.

- [ ] **Step 2: Run the targeted factory/config test first**

Run: `go test ./cmd/biz -run 'RateLimit' -count=1`

Expected: FAIL because factory wiring still constructs only one limiter.

- [ ] **Step 3: Introduce explicit per-scope limiter construction as the main implementation path**

At minimum:

```go
amfLimiter := redis.NewRateLimiter(
    redisPool.Client(),
    time.Second,
    f.cfg.RateLimit.PerAmfPerSec,
)

gpsiLimiter := redis.NewRateLimiter(
    redisPool.Client(),
    time.Minute,
    f.cfg.RateLimit.PerGpsiPerMin,
)
```

Then wire handlers so they intentionally receive or access the correct limiter for:
- AMF scope
- subscriber scope
- confirm/auth-context scope according to the decision from Task 3 / Task 4

If `GlobalPerSec` is implemented now, add a dedicated limiter and enforce it explicitly. If not, document in code review notes and spec updates that it remains configured but not enforced by this change.

- [ ] **Step 4: Keep the dependency shape as small as possible**

Prefer one of these over a big new package:
- pass multiple limiter dependencies into handlers directly
- pass a tiny struct of limiter references
- extract one small builder/helper local to Biz factory and handlers

Do **not** introduce broad new types unless the existing code becomes unmanageable without them.

- [ ] **Step 5: Update config fixtures only if they are already used by tests or local runtime**

Example snippet:

```yaml
rateLimit:
  perGpsiPerMin: 10
  perAmfPerSec: 1000
  globalPerSec: 100000
  failureMode: open
```

- [ ] **Step 6: Re-run the targeted Biz factory tests**

Run: `go test ./cmd/biz -count=1`

Expected: PASS

- [ ] **Step 7: Commit the explicit limiter wiring**

```bash
git add cmd/biz/factory.go cmd/biz
 git commit -m "$(cat <<'EOF'
feat: use explicit per-scope rate-limit wiring

Replace the shared limiter setup with scope-specific limiter policies so handler enforcement matches configured AMF and subscriber limits.
EOF
)"
```

## Task 6: Final verification and minimal documentation/state updates

**Files:**
- Modify: `docs/superpowers/specs/2026-06-03-rate-limit-gaps-spec.md`
- Modify only if clearly justified by tracked project process:
  - `docs/roadmap/module_index.md`
  - `docs/roadmap/README.md`
  - `.planning/STATE.md`
  - `.planning/PROJECT.md`
  - `.planning/REQUIREMENTS.md`

- [ ] **Step 1: Re-read the spec and confirm the implementation matches the final decisions**

Check that the implemented code now makes these statements true:
- real handler paths enforce rate limiting
- Redis failure mode is explicit and tested
- AMF and subscriber scopes map to deterministic keys
- decision metrics distinguish allow, limited, and error outcomes
- no raw GPSI or raw SUPI is logged by new limiter code

- [ ] **Step 2: Run focused verification before broad verification**

Run: `go test ./internal/config ./internal/metrics ./internal/api/nssaa ./internal/api/aiw ./cmd/biz -count=1`

Expected: PASS

- [ ] **Step 3: Run broader regression verification**

Run: `go test ./...`

Expected: PASS

- [ ] **Step 4: Run the repo’s established lint command, or fall back to the broad default if none exists**

Preferred if already used in this repo:

```bash
golangci-lint run ./...
```

If the repo uses a different wrapper or make target, use that instead.

Expected: PASS

- [ ] **Step 5: Update only the docs/state files that are truly justified**

Always update:
- `docs/superpowers/specs/2026-06-03-rate-limit-gaps-spec.md`

Update roadmap/planning state files only if this work is formally tracked there. Use concise factual notes, for example:

```md
- rate-limit enforcement normalized across NSSAA and AIW request paths with explicit fail-open policy and decision metrics
```

Do not invent new roadmap milestones or status categories.

- [ ] **Step 6: Commit the documentation and verification updates**

```bash
git add docs/superpowers/specs/2026-06-03-rate-limit-gaps-spec.md docs/roadmap/module_index.md docs/roadmap/README.md .planning/STATE.md .planning/PROJECT.md .planning/REQUIREMENTS.md
git commit -m "$(cat <<'EOF'
docs: record normalized rate-limit enforcement behavior

Document the chosen failure mode and final scope policy so the spec matches the real handler enforcement and observability behavior.
EOF
)"
```

If only the spec changed, stage only the spec file in this commit.

---

## Spec coverage check

- **Wire rate limiting into real request entry points:** reframed as hardening existing handler enforcement; covered by Tasks 3, 4, and 5.
- **Make protected identity explicit for GPSI- and AMF-scoped limits:** covered by Tasks 1, 3, and 5.
- **Define Redis error behavior clearly:** covered by Tasks 1, 2, and 6.
- **Emit metrics and logs for allowed, limited, and error outcomes:** covered by Task 2 and exercised in Tasks 3 and 4.
- **Add tests proving real-path rejection:** covered by Tasks 3 and 4.
- **Deterministic Redis key mapping:** covered by Tasks 3 and 5 while preserving the current namespace.
- **No raw sensitive identity values in logs:** covered by Task 2 and reinforced in Tasks 3 and 4.
- **Resolve the current single-limiter policy mismatch:** covered explicitly by Task 5.

## Placeholder scan

Checked for `TBD`, `TODO`, invented types, and vague "handle appropriately" language. Removed the earlier made-up compatibility type, removed the heavy new package requirement, and narrowed optional doc/state updates so the implementation path matches the existing repository more closely.

## Type consistency check

This revised plan intentionally avoids locking in large new types unless implementation proves they are needed. It consistently assumes:
- existing Redis limiter remains in `internal/cache/redis`
- handler-level enforcement remains in `internal/api/nssaa` and `internal/api/aiw`
- config grows by `RateLimitConfig.FailureMode`
- observability uses decision labels `allowed`, `limited`, and `error`

If implementation extracts a tiny shared helper, keep its naming consistent across handlers and tests rather than mixing multiple competing helper styles.
