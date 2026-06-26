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

**Status:** draft.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/01-contract-rules-harness.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
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

## Brief Task List

### Task 1: Epic 2 Kickoff Evidence

Record the current commit, worktree status, Sentrux root/runtime scans, accepted
focused VM debt, and the exact Sentrux rule-file decision for this epic. This is
a docs/evidence task. It updates `NOTES.md` and an Epic 2 evidence section.

Done means:

- accepted VM debt is named and not treated as an Epic 2 start blocker;
- missing Sentrux rules are either resolved or explicitly deferred for the first
  structural task;
- the first structural task has a clean baseline and knows which probes it must
  run.

### Task 2: Field Ownership Map

Inventory `rt_executor` fields and classify each as runtime-level, shard-local,
blocking-pool compatibility, trace/debug, or later-epic state. The map must say
which fields are safe to move in Epic 2 and which must wait for waiters, fd
registry, allocator, or multi-shard epics.

Done means:

- each field group has an owner and preserved behavior statement;
- over-limit file risks are recorded;
- the first code task has an exact field group, not a broad rewrite.

### Task 3: Introduce N=1 Runtime/Shard Skeleton

Add internal V2-shaped structures and initialization helpers without moving hot
behavior yet. Prefer small new internal files or tightly scoped header changes.
The existing runtime should still behave as a single executor with one shard.

Done means:

- the public native ABI is unchanged;
- new helpers use explicit status codes for recoverable failures;
- initialization and shutdown still pass the existing default gates;
- line-count notes are recorded for every touched C file.

### Task 4: Move Scheduler Container Shape

Move or wrap ready-queue, worker, running-count, timer-clock, and trace-facing
scheduler fields into the `N=1` shard shape while preserving current scheduling
behavior. Do not remove current global synchronization as part of this task.

Done means:

- no observable scheduler semantics change;
- direct channel and net smoke/liveness probes relevant to moved fields pass or
  have recorded accepted blockers;
- trace counters remain readable and comparable to the baseline.

### Task 5: Move Net Poll Scratch Shape Without FD Registry Rewrite

Move or wrap current net poll scratch buffers and net waiter counters behind the
single shard. Keep the existing poll-set rebuild and global waiter scan.

Done means:

- native net benchmark still runs against a current-checkout binary;
- net wakeup/SIGUSR1 trace evidence is recorded;
- no persistent fd registry behavior appears in this epic.

### Task 6: Move Channel/Blocking Compatibility Shape

Move or wrap channel-blocked worker counters, compensation counters, and blocking
fallback integration into the runtime/shard shape without changing direct async
channel semantics or sync helper fallback behavior.

Done means:

- direct async channel wakeup tests stay equivalent;
- sync fallback compensation evidence stays explainable;
- this task does not turn sync fallback into the target hot path.

### Task 7: Consolidate Accessors And Remove Ambiguous Ownership

Replace ad hoc field access introduced during migration with owner-named helpers
or local variables. This is cleanup, not a second refactor. Do not add new
abstractions unless they remove actual ambiguity created by Tasks 3-6.

Done means:

- ownership reads as runtime-level or shard-level at call sites;
- no touched file grows without a recorded reason;
- Sentrux scoped quality does not drop without an accepted recovery task.

### Task 8: Epic 2 Closeout

Consolidate evidence, update `NOTES.md`, update this epic with actual results,
and decide whether Epic 3 can start with owner-local waiters. Do not close Epic
2 with hidden blockers.

Done means:

- every task has an evidence entry;
- Sentrux root/runtime results are recorded;
- current benchmark rows are linked;
- accepted VM test debt is still named and remains assigned to the later
  test/backend epic;
- Epic 3 starts from a clear field ownership map and liveness checklist.

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
