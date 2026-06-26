# Epic 2: Runtime V2 N=1 Structure

**Goal:** introduce V2-shaped `rt_runtime` and `rt_shard` ownership boundaries
inside the native runtime while keeping exactly one shard and preserving current
source-visible behavior.

**Approach:** this epic changes structure before changing concurrency. The
runtime may gain new internal containers, helper APIs, and accessors, but it
must not enable `N>1`, owner-local waiter semantics, persistent fd registry,
cross-shard messaging, compiler crossing syntax, or public ABI changes. Every
code task must name the old behavior it preserves and prove that the new shape
is still the same runtime from the program's point of view.

**Status:** draft; Tasks 1-5 evidence is recorded in `02-evidence.md`.

**Task documents:** detailed tasks live under `02-tasks/`. Each runtime-code
task has a separate testing task where a meaningful test can be written before
or beside the implementation.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/01-contract-rules-harness.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/02-evidence.md`
- `docs/runtime-v2-epics/02-field-ownership-map.md`
- `docs/runtime-v2-epics/02-ci-test-contract.md`
- `docs/runtime-v2-epics/01-baseline-evidence.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/OPEN_DECISIONS_BEFORE_EPIC_2.md`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`
- `internal/vm/mt_executor_test.go`
- `internal/vm/mt_correctness_test.go`

## Accepted Baseline Debt

The focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails in the current checkout
when timeout-sensitive tests are not skipped. This is accepted backend-test
debt, not an Epic 2 start blocker. A later test/backend epic will rewrite the
VM/native/LLVM test matrix around stable Runtime V2 contracts.

Epic 2 tasks must still record this debt in evidence. They must not attribute
new runtime failures to the accepted debt unless the failure matches the
recorded classes in `01-baseline-evidence.md`.

## Scope

Included:

- introduce internal `rt_runtime` and `rt_shard` concepts with `N=1`;
- classify current `rt_executor` fields as process-level runtime state,
  shard-local hot state, compatibility state, or later-epic state;
- move or wrap fields behind owner-oriented accessors where that can be done
  without changing behavior;
- keep the existing global lock discipline until a task explicitly proves a
  narrower owner boundary;
- keep the public native ABI stable;
- keep current direct channel, net, timer, cancellation, shutdown, and blocking
  fallback behavior unchanged;
- record Sentrux root/runtime scans and missing-rules status on every task;
- add stable Runtime V2 tests to CI before closing the epic;
- keep notes and evidence current after each task.

Not included:

- no owner-local waiter rewrite;
- no persistent fd registry;
- no `N>1` shards;
- no accept ownership changes;
- no cross-shard wake-fd protocol;
- no `far`, `submit_to`, `crosses`, move-only capture, or compiler lowering
  work;
- no heap-counter or allocator pool work unless a touched file requires a small
  compatibility move;
- no VM/native/LLVM test-matrix rewrite.

## Preserved Behavior Boundary

Epic 2 may change internal structure only if these stay equivalent:

- channel FIFO and direct async channel parking behavior;
- cooperative cancellation, timeout, structured join, and failfast outcomes;
- `spawn` and `@local spawn` source-visible rules;
- native `threads=1` semantic behavior where current tests can prove it;
- current MT liveness behavior for touched scheduler, channel, and net paths;
- shutdown halt/drop behavior;
- public native ABI and exported runtime function names;
- debug-facing trace counters unless the task explicitly records a renamed or
  moved counter as equivalent.

Current implementation artifacts may move behind the new shape:

- global waiter storage;
- global inject and worker-local queue storage;
- net poll scratch buffers;
- timer state;
- blocking fallback counters;
- compensation counters.

Moving storage is allowed in this epic. Changing owner semantics is not.

## C API Policy

New V2 internal APIs return explicit status codes for recoverable failures. Do
not introduce new `panic_msg`-based control flow for allocation, initialization,
field migration, or lifecycle helpers. A task may keep existing legacy panic
paths unchanged when it is only moving structure.

Every new or heavily rewritten runtime/compiler code file must stay at or below
500 lines. Existing over-limit files must not grow unless the task records why
the edit is a proving spike or why a split would make the migration less safe.

## Sentrux Policy

Every Epic 2 task records:

- root scan: `/home/zov/projects/surge/surge`;
- scoped scan: `/home/zov/projects/surge/surge/runtime`;
- `health` and `check_rules` after each scan;
- whether missing rules are still an explicit temporary deferral.

Epic 2 may start with missing Sentrux rules only if the first task records the
deferral. A runtime-code task must not claim rule compliance until the relevant
rules file exists and `check_rules` passes for the active scan path.

## Liveness Policy

Use `LIVENESS_PROBES.md` by changed surface. A structural move that touches a
path must run the corresponding existing probe or record the exact missing probe
and owner.

Minimum liveness evidence for this epic:

- scheduler/queue moves: scheduler source trace and parked-with-work invariant
  status;
- channel state moves: direct async channel wakeups and sync fallback evidence;
- net state moves: net wakeup/SIGUSR1 trace and native net benchmark trace;
- timer/cancellation/shutdown-adjacent moves: cancellation/join/timeout smoke
  and current shutdown-probe gap;
- performance-sensitive moves: native net/channel benchmark rows with a
  current-checkout `SURGE` binary.

CI liveness coverage is defined in `02-ci-test-contract.md`. Task 12 wires the
target and workflow job; Task 03 only records the required gate shape and
candidate exact tests.

## Brief Task List

| Task | Document | Purpose |
| --- | --- | --- |
| 1 | `02-tasks/01-kickoff-evidence.md` | Record current baseline, accepted VM debt, and Sentrux state. |
| 2 | `02-tasks/02-field-ownership-map.md` | Classify `rt_executor` fields and pick safe Epic 2 moves. Output: `02-field-ownership-map.md`. |
| 3 | `02-tasks/03-runtime-v2-ci-test-contract.md` | Define stable Runtime V2 test and CI policy before code work. Output: `02-ci-test-contract.md`. |
| 4 | `02-tasks/04-runtime-shard-skeleton-tests.md` | Add or select tests/static checks for the skeleton. |
| 5 | `02-tasks/05-runtime-shard-skeleton.md` | Introduce `N=1` `rt_runtime` and `rt_shard`. |
| 6 | `02-tasks/06-scheduler-shape-tests.md` | Add or select scheduler behavior tests before moving scheduler fields. |
| 7 | `02-tasks/07-scheduler-shape-migration.md` | Move scheduler container shape without semantic changes. |
| 8 | `02-tasks/08-net-poll-scratch-tests.md` | Add or select net scratch/readiness tests before moving net state. |
| 9 | `02-tasks/09-net-poll-scratch-migration.md` | Move net poll scratch shape without fd-registry rewrite. |
| 10 | `02-tasks/10-channel-blocking-compat-tests.md` | Add or select channel and blocking fallback tests. |
| 11 | `02-tasks/11-channel-blocking-compat-migration.md` | Move channel/blocking compatibility shape. |
| 12 | `02-tasks/12-ci-runtime-v2-gates.md` | Add stable Runtime V2 liveness gates to CI. |
| 13 | `02-tasks/13-accessor-cleanup-and-static-gates.md` | Clean ambiguous ownership accessors and run static gates. |
| 14 | `02-tasks/14-epic-closeout.md` | Consolidate evidence and hand off to Epic 3. |

## Acceptance Gates

Epic 2 is complete when:

- `rt_runtime` and `rt_shard` exist as internal concepts with exactly one shard;
- public native ABI remains stable;
- behavior-preserving field movement is proven for the touched scheduler, net,
  channel, timer, cancellation, and shutdown-adjacent paths;
- no owner-local waiter, persistent fd registry, `N>1`, or compiler crossing
  work has slipped into this epic;
- all new V2 internal APIs use explicit status codes for recoverable failures;
- Sentrux root and runtime scans are recorded before and after code changes;
- missing Sentrux rules are resolved or explicitly deferred without claiming
  rule compliance;
- stable Runtime V2 tests are added to CI through the named target or job
  defined by `02-ci-test-contract.md`;
- CI does not require the broad accepted-debt focused VM command as a green
  gate;
- `make check`, `make c-check`, and `make cppcheck` pass or have recorded
  blockers unrelated to Epic 2 changes;
- native net and channel benchmark reports are regenerated with a
  current-checkout compiler binary when touched paths can affect them;
- liveness evidence from `LIVENESS_PROBES.md` is recorded for every touched
  surface;
- `NOTES.md` is current enough to start Epic 3 without chat context.

## Non-Goals To Recheck During Review

- Do not turn this epic into a waiter rewrite.
- Do not introduce cross-shard language syntax.
- Do not enable multiple shards.
- Do not hide remote or future cross-shard cost behind local-looking helpers.
- Do not spend this epic rewriting the semi-broken VM/native/LLVM test matrix.
- Do not claim Sentrux rule compliance from a missing-rules result.
