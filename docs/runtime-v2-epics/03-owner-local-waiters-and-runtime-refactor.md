# Epic 3: Owner-Local Waiters And Runtime Refactor

**Goal:** move waiter bookkeeping toward owner-local Runtime V2 structures under
the existing `N=1` boundary, while reducing large-file runtime debt through
behavior-preserving refactoring.

**Approach:** this epic changes waiter ownership and module boundaries before it
changes concurrency. It must first prove the current waiter contract, then move
or wrap waiter storage behind owner-named APIs. Refactoring is part of the epic,
not cleanup after it: every extraction must start from a dependency map and a
behavior proof.

**Status:** complete. Epic 3 closed the owner-local waiter and
dependency-aware refactor slice under the existing `N=1` runtime/shard
boundary. Epic 4 starts from persistent fd registry and net lifecycle proof,
not from `N>1` scheduling or crossing syntax.

**Task documents:** brief task scopes live under `03-tasks/`. Expand only the
next task before execution.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`
- `docs/runtime-v2-epics/02-evidence.md`
- `docs/runtime-v2-epics/02-field-ownership-map.md`
- `docs/runtime-v2-epics/02-ci-test-contract.md`
- `docs/runtime-v2-epics/NOTES.md`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_async_channel.c`
- `runtime/native/rt_async_task.c`
- `runtime/native/rt_async_scope.c`
- `runtime/native/rt_async_blocking.c`
- `runtime/native/rt_async_poll.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_runtime.c`
- `internal/vm/mt_executor_test.go`
- `internal/vm/mt_correctness_test.go`

## Starting State

The current runtime has an internal `N=1` `rt_runtime` / `rt_shard` skeleton.
Waiters still use executor-global storage:

- `rt_executor.waiters`, `waiters_len`, and `waiters_cap`;
- `rt_executor.net_waiters_len`;
- `add_waiter`, `remove_waiter`, `pop_waiter`, `wake_key_all`, and
  `park_current` scan or mutate the global waiter list;
- channel, task/select, timer, scope, blocking, and net paths all depend on the
  same list and key model.

Current large native files, measured at Epic 3 drafting time:

- `runtime/native/rt_async_state.c`: 2431 lines;
- `runtime/native/rt_term.c`: 1091 lines;
- `runtime/native/rt_net.c`: 1040 lines;
- `runtime/native/rt_fs.c`: 978 lines;
- `runtime/native/rt_async_task.c`: 768 lines;
- `runtime/native/rt_async_channel.c`: 549 lines;
- `runtime/native/rt_async_internal.h`: 460 lines, close to the limit.

The over-limit files are accepted starting debt. Epic 3 tasks may touch them
only with a recorded line-count outcome: reduced, flat, or justified follow-up.
Non-waiter large files such as `rt_term.c` and `rt_fs.c` are not Epic 3
refactor targets unless waiter work touches them.

Final Epic 3 line-count outcome:

- `runtime/native/rt_async_state.c`: 2431 -> 1731 lines, still over the
  500-line target;
- `runtime/native/rt_net.c`: 1040 -> 1024 lines, still over the 500-line
  target;
- `runtime/native/rt_async_trace.c`: new trace split, 497 lines;
- `runtime/native/rt_async_internal.h`: 460 -> 499 lines.

## Accepted Baseline Debt

The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted backend-test
debt. Do not add it as a required green gate in this epic.

Missing Sentrux rule files remain debt, not compliance. Every implementation
task must record root and scoped scans honestly and must not treat missing
rules as a passing rule gate.

Existing large runtime files are debt. They do not block Epic 3, but Epic 3 must
stop growing them by default and must create real refactoring tasks where the
waiter work exposes cohesive module boundaries.

Final Epic 3 debt carried forward:

- the broad focused VM command remains accepted backend-test debt;
- missing Sentrux rule files remain debt for root, `runtime/`, and
  `runtime/native/`;
- `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` remain known
  timeout-sensitive test debt outside the Epic 3 green gate;
- `runtime/native/rt_async_state.c` and `runtime/native/rt_net.c` remain over
  500 lines;
- persistent fd registry and net lifecycle proof remain Epic 4 work.

## Scope

Included:

- map every waiter producer, consumer, cleanup path, and wake path;
- define the waiter behavior contract before moving storage;
- preserve or explicitly reclassify current FIFO-by-key behavior;
- introduce owner-local waiter structures for `N=1`;
- move channel, task/select, timer, scope, blocking, and net waiter users in
  small slices;
- keep public native ABI stable;
- keep the existing poll-set rebuild model for net waiters;
- extract cohesive waiter and runtime helper modules when the dependency map
  proves a stable boundary;
- identify dead-code candidates, then delete only with reference, build, test,
  and Sentrux evidence;
- update Runtime V2 CI gates with stable waiter liveness tests;
- keep `NOTES.md` and `03-evidence.md` current after every task.

Not included:

- no persistent fd registry;
- no `N>1` shard scheduling;
- no accept ownership changes;
- no cross-shard wake-fd protocol;
- no `far`, `submit_to`, `crosses`, move-only capture, or compiler lowering
  work;
- no heap-counter or allocator-pool work unless a tiny compatibility edit is
  needed for touched waiter code;
- no `io_uring` or backend I/O rewrite;
- no broad VM/native/LLVM test-matrix rewrite.

## Refactor Safety Contract

Refactoring in this epic is allowed only when it satisfies this contract:

- write or select the behavior proof before moving code;
- record the dependency cluster and owning module before extraction;
- keep behavior changes out of refactor commits;
- move one responsibility at a time;
- do not create catch-all files such as `common`, `misc`, or vague `helpers`;
- keep new or heavily rewritten runtime files at or below 500 lines;
- reduce or keep flat every touched over-limit file unless the task is a
  documented proving spike;
- use owner-first APIs and explicit status codes for new V2 primitives;
- delete code only after proving the symbol is unreachable or obsolete through
  references, build, tests, and Sentrux evidence;
- record rejected paths in `NOTES.md` so they are not rediscovered later.

Dependency-aware refactoring means the task can answer:

- which state owns this data after the move;
- which callers still mutate it;
- which cleanup path removes stale work;
- which tests prove cancellation, timeout, close, and shutdown behavior;
- which file loses responsibility and which file gains it.

## Waiter Behavior To Preserve Or Decide

Epic 3 must prove these before implementation:

- wake-before-park is still closed by `wake_token` or a documented replacement;
- stale, done, and cancelled waiter entries do not wake completed tasks;
- per-key FIFO either remains a contract or is explicitly demoted with tests;
- channel close wakes both send and receive waiters correctly;
- task select and timeout cleanup cannot leave orphan waiters or timer tasks;
- scope cancellation wakes owners and children without double wake;
- blocking-job completion and cancellation wake the parked task once;
- net waiters wake through the existing poll rebuild path without introducing
  a persistent fd registry;
- shutdown cannot strand a waiter behind an owner that no longer runs.

## Refactor Targets

Primary targets:

- waiter key/list operations currently in `rt_async_state.c`;
- waiter storage and net-waiter counters currently in `rt_executor`;
- task wait-key and select-timer cleanup around `rt_async_task.c`;
- channel waiter handoff paths in `rt_async_channel.c`;
- net waiter scanning and completion in `rt_net.c`;
- declaration pressure in `rt_async_internal.h`.

Secondary targets are only allowed after the primary waiter work exposes a
cohesive boundary. Do not spend this epic refactoring unrelated bignum, string,
filesystem, terminal, or VM code.

Initial dead-code audit seed: `rt_select_poll_tasks` is a suspect only. It has
native, ABI, and LLVM builtin references, while current select emission appears
to call `rt_select_poll`. Do not delete it without generated-IR search, ABI
review, focused tests, and Sentrux evidence.

## Brief Task List

| Task | Document | Purpose |
| --- | --- | --- |
| 1 | `03-tasks/01-kickoff-baseline-and-sentrux.md` | Record current checkout, line counts, accepted debt, Sentrux state, and final Epic 3 gate plan. |
| 2 | `03-tasks/02-waiter-dependency-map.md` | Map every waiter key, producer, consumer, cleanup path, and wake path. |
| 3 | `03-tasks/03-refactor-dependency-and-dead-code-audit.md` | Build the dependency-aware refactor plan and mark only proven or suspected dead code. |
| 4 | `03-tasks/04-waiter-behavior-contract-tests.md` | Define and add focused tests for the current waiter contract before moving storage. |
| 5 | `03-tasks/05-waiter-module-extraction-tests.md` | Add compile/static checks that protect extraction of waiter helpers. |
| 6 | `03-tasks/06-extract-waiter-key-list-helpers.md` | Extract cohesive key/list helper code without changing behavior. |
| 7 | `03-tasks/07-owner-local-waiter-skeleton-tests.md` | Prove the `N=1` owner-local waiter skeleton before implementation. |
| 8 | `03-tasks/08-owner-local-waiter-skeleton.md` | Add the owner-local waiter container behind compatibility APIs. |
| 9 | `03-tasks/09-channel-waiter-tests.md` | Add channel send/receive/close/cancel waiter probes. |
| 10 | `03-tasks/10-channel-waiter-migration.md` | Move channel waiter users to the owner-local container. |
| 11 | `03-tasks/11-task-scope-blocking-waiter-tests.md` | Add task await, scope, and blocking wake/cancel probes. |
| 12 | `03-tasks/12-task-scope-blocking-waiter-migration.md` | Move task, scope, and blocking waiter users. |
| 13 | `03-tasks/13-timer-select-cancellation-tests.md` | Add timeout/select/cancellation cleanup probes. |
| 14 | `03-tasks/14-timer-select-cancellation-migration.md` | Move timer/select/cancellation waiter cleanup paths. |
| 15 | `03-tasks/15-net-waiter-tests-and-trace-contract.md` | Add net waiter liveness and trace probes without fd-registry work. |
| 16 | `03-tasks/16-net-waiter-migration.md` | Move net waiter users while preserving poll rebuild behavior. |
| 17 | `03-tasks/17-large-file-refactor-tranche.md` | Reduce or split the largest touched runtime files with behavior proof. |
| 18 | `03-tasks/18-runtime-v2-waiter-ci-gates.md` | Add stable waiter probes to local and CI gates. |
| 19 | `03-tasks/19-epic-closeout-and-static-gates.md` | Consolidate evidence, run full gates, and hand off to Epic 4. |

## Parallelization Plan

Tasks 2 and 3 can run in parallel after Task 1 because one maps waiter behavior
and the other maps refactor/dead-code risk. Task 4 depends on both.

After Task 8, the test-writing tasks for channel, task/scope/blocking,
timer/select/cancellation, and net waiters can be planned in parallel if their
write sets are disjoint. Implementation tasks must remain sequenced by shared
waiter API changes unless the task plans prove no overlap.

Every subagent must start with a plan-only pass and wait for main-agent
approval before edits or long-running checks.

## Acceptance Gates

Epic 3 is complete when:

- waiter ownership is owner-local for the included `N=1` surfaces;
- no persistent fd registry, `N>1`, accept ownership, or crossing syntax has
  slipped into the epic;
- current waiter behavior is preserved or every intentional behavior change is
  documented with tests;
- stale/done/cancelled waiter cleanup is proven;
- channel close, task select, timeout, cancellation, scope cancellation,
  blocking completion, net waiter wakeup, and shutdown-adjacent paths have
  focused evidence;
- new V2 waiter APIs use owner-first arguments and explicit status codes for
  recoverable failures;
- touched over-limit files do not grow without a documented proving-spike
  reason, and at least one large-file refactor tranche lands with tests;
- dead code is deleted only with recorded proof, or remains marked as suspect;
- `make runtime-v2-check`, `make check`, `make c-check`, `make cppcheck`, and
  `git diff --check` pass or have recorded blockers unrelated to Epic 3;
- native net and channel benchmarks are regenerated when touched paths can
  affect performance;
- root and scoped Sentrux scans are recorded before and after implementation;
- stable waiter liveness tests are in local and CI gates;
- durable decisions are consolidated out of `NOTES.md` before closeout.

## Non-Goals To Recheck During Review

- Do not turn owner-local waiters into a persistent fd registry.
- Do not hide future cross-shard cost behind local-looking helper names.
- Do not add magic ownership inference; make the owner explicit in data and API.
- Do not refactor unrelated runtime modules to improve line counts in the
  abstract.
- Do not treat a code move as safe unless the behavior proof ran before and
  after the move.
- Do not claim Sentrux rule compliance from missing rules.

## Closeout Summary

Epic 3 moved waiter storage behind owner-local Runtime V2 structures for the
included `N=1` surfaces and kept the public native ABI stable. Channel, task,
scope, blocking, timer/select, and net waiter users were covered by focused
probes before or during migration. Task 18 promoted the stable waiter liveness
set into the local Runtime V2 gate path, and the existing CI workflow reaches
that set through `make runtime-v2-check`.

The epic also landed two dependency-aware refactor tranches. Waiter key/list
operations moved out of the legacy state file, and trace/SIGUSR1 dump
responsibility moved into `rt_async_trace.c`. These moves reduced
`rt_async_state.c` and slightly reduced `rt_net.c`, but both files remain
accepted large-file debt.

Epic 3 did not implement a persistent fd registry, accept ownership changes,
`N>1` shard scheduling, crossing syntax, `eventfd`, `epoll`, `kqueue`,
`io_uring`, heap-counter work, or backend test-matrix cleanup.

Local closeout evidence belongs in `03-evidence.md`. Do not describe this epic
as CI-green unless a fresh CI run is recorded. The durable claim is narrower:
local gates passed where recorded, and the CI workflow reaches
`make runtime-v2-check`.

## Epic 4 Handoff

Epic 4 should start with a shard-local persistent fd registry and net lifecycle
proof. The first proof should define fd registration, readiness lifetime,
close/cancel cleanup, wake-fd ownership, and shutdown behavior before changing
the poll backend. Do not start Epic 4 with `N>1`, crossing syntax, or
cross-shard wake protocol work; those depend on stable fd ownership and net
lifecycle semantics.
