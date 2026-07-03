# Task 4: Multishard Accept Contract Tests

**Status:** Complete
**Kind:** test writing
**Depends on:** Task 1, Task 2, Task 3

## Context

Task 3 has chosen and proven a listener model. This task writes the durable
behavior tests for the Accept Ownership Contract *before* Tasks 6-11
implement it, matching the pattern Epic 4 used (`04-tasks/03-fd-lifecycle-contract-tests.md`
before `05-registry-container-skeleton.md`/`06-net-wait-registration.md`) and
the epic's own Refactor Safety Contract ("write or select the behavior proof
before moving code").

These tests describe intended behavior against code that does not exist yet
(`SURGE_SHARDS`, per-shard fd registries actually populated for shard
indices `>0`, owner-shard placement). Follow the Epic 4 precedent: tests that
describe future behavior go behind the `runtime_v2_pending` build tag (see
`make runtime-v2-fd-registry-check` in `Makefile:113-115`, which runs
`-tags runtime_v2_pending` against `TestRuntimeV2FDRegistry*`), so they do
not fail default `go test ./...` runs while still existing as an executable
contract.

## Goal

Add focused behavior tests that describe the desired multi-shard accept,
fd-readiness, close, cancellation, and shutdown behavior, so Tasks 6-11 have
a concrete target and Task 13 has a stable CI gate to promote later.

## Why This Task Exists

`RULES.md` Global Rule 2 requires every accepted change to answer who owns
the state, who wakes the task, who cleans up on cancellation, and which
test/trace exposes a violation — these tests are that exposure mechanism for
Epic 6. Writing them before implementation is also what makes Task 6-11
reviewable: a reviewer can check "does this task's diff make the pending test
in Task 4 pass" instead of trusting a prose description of intent.

## Scope

Cover, at minimum, one test (or table-driven case) per property in the
epic's Accept Ownership Contract that is testable at the Go/native-probe
level:

- `SURGE_SHARDS=1` preserves current observable native net behavior
  (regression case — this must already pass without any Epic 6 code change,
  proving the compatibility floor before anything else is built).
- `SURGE_SHARDS=N` (`N>1`) initializes exactly `N` shards; an invalid value
  (`0`, non-numeric, or above whatever bound Task 6 defines for
  `RT_RUNTIME_MAX_SHARDS`) fails with an explicit status and diagnostic, not
  a crash or silent clamp.
- `SURGE_SHARDS>1` uses one Tier 1 worker per shard; a conflicting
  `SURGE_THREADS` value (per Task 2's mapped interaction) fails explicitly
  rather than silently overriding one or the other.
- Accepted connections are distributed across the intended shard owners
  under the chosen listener model, or the chosen fallback records explicit
  handoff rows (per Task 3's decision) — assert real distribution across at
  least 2 shards for a burst of connections, matching the empirical
  `SO_REUSEPORT` behavior Task 3 already observed.
- Each accepted connection's read/write waiters and fd registry entry live
  on the owner shard specifically (not shard 0 by default) — this needs a
  way to inspect which shard's `rt_fd_registry`/`rt_waiter_store` holds the
  row; if no such inspection hook exists yet, this test can assert on trace
  counters from Task 12 instead, but name that dependency explicitly rather
  than inventing an untracked internal hook.
- Close, cancellation, fd readiness, and shutdown for a connection use only
  its owner shard's state and do not silently touch shard 0 when the owner
  is a different shard.
- A non-owner task attempting to act on a shard-owned connection is either
  rejected by an Epic 6 guard, or the epic's closeout records it as explicit
  debt — write the test to assert whichever behavior Task 7/9 actually
  implements, once decided; if undecided when this task runs, write the test
  against the intended behavior and mark it `runtime_v2_pending` with a note
  that Task 7 may need to adjust it.
- Runtime shutdown wakes every shard poller and worker, and leaves no live
  connection waiters or leftover benchmark/test child processes across all
  shards (`N>1`), not just shard 0.
- A per-shard listener group closes as one logical handle: closing it closes
  every fd in the group and wakes/cancels waiters on every owning shard
  (only relevant if Task 3 chose the `SO_REUSEPORT` group model; otherwise
  adapt to the chosen fallback's close semantics).

## Out Of Scope

- Static/compile-time shape checks (Task 5's job, e.g. `_Static_assert` on
  struct layout, `#error` guards).
- Trace-counter-specific assertions beyond what is needed to observe shard
  distribution (Task 12 owns the full counter set).
- CI wiring (Task 13) — this task only needs the tests to exist and run
  locally under `runtime_v2_pending`.
- Making any of these tests pass — that is Tasks 6-11's job. This task must
  not implement runtime code to force green tests.

## Approach / Steps

1. Re-read the Accept Ownership Contract and Proof And Quality Contract
   sections of the epic document; enumerate every bullet that is testable
   at this layer.
2. For each, write a Go test (native-backend, `SURGE_BACKEND=llvm` style,
   matching existing `internal/vm/mt_*_test.go` and
   `internal/vm/runtime_v2_fd_registry_contract_test.go` conventions) in a
   new `internal/vm/runtime_v2_accept_contract_test.go`.
3. Gate tests that need code that does not exist yet behind
   `//go:build runtime_v2_pending`; tests that only need the `SURGE_SHARDS=1`
   regression floor should run by default (no tag), since they must already
   pass.
4. For each pending test, record in a comment or in `06-evidence.md` exactly
   which Task (6, 7, 8, 9, 10, or 11) is expected to make it pass, so nobody
   has to guess later.
5. Run the default (untagged) subset now and confirm it is green against the
   Task 1 baseline; run the tagged subset now and confirm it fails only in
   the expected "not implemented yet" way (e.g. shard count always reports
   `1`), not from an unrelated bug.
6. Update `06-evidence.md` and `NOTES.md`.

## Files

Read:

- `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md`
  (Accept Ownership Contract, Proof And Quality Contract)
- `docs/runtime-v2-epics/06-listener-model-proving-spike.md` (Task 3 output)
- `docs/runtime-v2-epics/04-tasks/03-fd-lifecycle-contract-tests.md` (prior
  pattern)
- `internal/vm/runtime_v2_fd_registry_contract_test.go` (conventions to
  follow: pending tags, native backend setup, assertions style)
- `internal/vm/mt_correctness_test.go`, `mt_executor_test.go` (net/scheduler
  test helpers already in use, e.g. `runBinaryWithTimeout`)

Create:

- `internal/vm/runtime_v2_accept_contract_test.go`
- Update `docs/runtime-v2-epics/06-evidence.md`, `NOTES.md`

## Skills & Working Practice

- Follow Global Rule 9: a subagent writing these tests must first present
  the list of test cases and which pending tag/build strategy it will use,
  then wait for approval before writing files.
- This task can be planned in parallel with Task 5 (static shape tests) —
  their write sets are disjoint (behavior tests here, static/`_Static_assert`
  tests there) — but both must agree with Task 3's chosen listener model
  before either is finalized.
- Do not silently change existing `SURGE_SHARDS=1` behavior to make a new
  test pass; if the regression floor test fails against current code, that
  is itself a Task 1 baseline bug to report, not something to work around
  here.
- Use `LIVENESS_PROBES.md`'s "Mandatory Gate By Change Type" table (row:
  "Net waiters, fd registry, I/O thread, or accept ownership") to select the
  matching existing liveness probes to run alongside these new tests.

## Checks

- Default (untagged) `go test ./internal/vm -run 'TestRuntimeV2Accept'`
- `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Accept'`
  (expected-fail subset, with failures matching the "not implemented yet"
  shape, not a crash or hang)
- Existing focused net liveness probe selected from `LIVENESS_PROBES.md`
  ("Net wakeup and live SIGUSR1 trace" row)
- `git diff --check`

## Definition Of Done

- [ ] Every testable Accept Ownership Contract bullet has a corresponding
      test case.
- [ ] The `SURGE_SHARDS=1` regression-floor subset passes today, without any
      Epic 6 runtime change.
- [ ] Every `runtime_v2_pending`-tagged test names which later task is
      expected to make it pass.
- [ ] Tests fail in the expected "not implemented" way today, not from an
      unrelated bug or crash.
- [ ] `06-evidence.md` records which subset is green now and which is
      pending, with the owning task for each pending case.

## Evidence To Record

- `06-evidence.md`: Task Identity And Scope, Files Touched, Contracts Touched
  (map each contract bullet to its test), Commands/Checks (exact test
  invocations and pass/expected-fail results).
- `NOTES.md`: which tests exist, which are pending on which task, and any
  contract bullet found untestable at this layer (and why).
