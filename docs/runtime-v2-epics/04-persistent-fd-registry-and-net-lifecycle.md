# Epic 4: Persistent FD Registry And Net Lifecycle

**Goal:** replace per-poll fd-set rebuilds from waiter scans with a
shard-local persistent fd registry under the existing `N=1` boundary, and prove
listener/connection readiness, close, cancellation, re-registration, wake-fd,
and shutdown ownership.

**Approach:** this epic changes net readiness ownership before it changes
concurrency or backend type. Keep the current `poll()` backend first. The
registry must make fd lifetime explicit and testable; `epoll`, `kqueue`,
`io_uring`, multi-shard accept ownership, and crossing language work stay out
until the lifecycle contract is stable.

**Status:** draft. Expand only the next task before execution.

**Task documents:** brief task scopes live under `04-tasks/`.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/03-owner-local-waiters-and-runtime-refactor.md`
- `docs/runtime-v2-epics/03-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_net.c`
- `runtime/native/rt_runtime.c`
- `runtime/native/rt_async_waiter.c`
- `runtime/native/rt_async_state.c`
- `internal/vm/mt_net_test.go`
- `internal/vm/runtime_v2_*_test.go`
- `.github/workflows/*`

## Starting State

Epic 3 moved net waiters into owner-local waiter storage, but net polling still
rebuilds the fd set from waiter state on every poll cycle.

Current net polling shape:

- `runtime/native/rt_net.c` builds temporary `NetPollFd` rows from owner-local
  net waiters through `rt_executor_visit_net_waiters`.
- `poll_net_waiters` derives poll capacity from
  `rt_executor_net_waiter_len(ex)`, fills scratch arrays, calls `poll()`, then
  wakes waiters by fd/kind key.
- `rt_net_poll_scratch` stores temporary fd and `pollfd` arrays under the
  shard/executor compatibility state.
- listener and connection close paths close raw fds, but there is no durable
  fd entry that owns readiness registration, stale waiter cleanup, generation,
  or re-registration state.

Final Epic 3 large-file state carried into this epic:

- `runtime/native/rt_async_state.c`: 1731 lines;
- `runtime/native/rt_net.c`: 1024 lines;
- `runtime/native/rt_async_trace.c`: 497 lines;
- `runtime/native/rt_async_internal.h`: 499 lines.

`rt_net.c` and `rt_async_state.c` remain over the 500-line Runtime V2 target.
Epic 4 may touch `rt_net.c`, but every implementation task must state whether
the file grew, stayed flat, or shrank. A refactor tranche is part of the epic
because the registry work will otherwise keep adding pressure to `rt_net.c`.

## Accepted Baseline Debt

The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted backend-test
debt. Do not add it as a required green gate in this epic.

Sentrux rule files exist for root, `runtime/`, and `runtime/native`. Every
implementation task must record root and scoped scans honestly. A missing-rules
result is now a blocker, not an accepted Runtime V2 state.

Timeout-sensitive VM/native/LLVM tests remain outside this epic's green gate
unless a task explicitly changes that path and adds a focused proof.

Large runtime files remain debt. Touching an over-limit file is allowed only
with a recorded line-count outcome and a clear reason tied to the fd registry
or net lifecycle work.

## Scope

Included:

- map the current net readiness, close, cancellation, wake-fd, and shutdown
  paths before changing code;
- define the fd lifecycle contract before replacing the poll rebuild path;
- introduce a shard-local persistent fd registry for `N=1`;
- keep each listener or connection fd represented by one durable registry
  entry while it is live;
- attach read, write, and accept readiness interest to the owning fd entry;
- preserve current public native ABI and current `poll()` backend behavior;
- replace global waiter-derived fd-set rebuilds with registry-derived polling;
- prove duplicate readiness interest, cancellation, close, stale wake, and
  re-registration behavior;
- prove wake-fd behavior for registration, close, and shutdown transitions;
- add trace counters that distinguish registry updates from poll waits;
- add CI coverage for stable fd-registry liveness tests;
- reduce or split cohesive `rt_net.c` responsibility after behavior is proven;
- keep `NOTES.md` and `04-evidence.md` current after every task.

Not included:

- no `N>1` shard scheduling;
- no accept distribution or `SO_REUSEPORT` ownership change;
- no cross-shard wake-fd protocol;
- no `far`, `submit_to`, `crosses`, move-only capture, or compiler lowering
  work;
- no heap-counter or hot-pool migration;
- no `epoll`, `kqueue`, or `io_uring` migration;
- no broad VM/native/LLVM test-matrix rewrite;
- no semantic changes to channel, timer, task, scope, or blocking waiters
  unless needed for a narrowly documented net-waiter integration fix.

## FD Registry Contract

Epic 4 must make these properties true and testable:

- each live listener or connection fd has exactly one registry entry in the
  owning shard;
- an fd entry records the fd number, generation or equivalent stale-wake guard,
  close state, and active readiness interests;
- read, write, and accept readiness interests are attached to the fd entry, not
  discovered by scanning all waiters during poll construction;
- adding or changing interest wakes the I/O thread when it may be blocked;
- close removes future readiness interest, wakes or cancels affected waiters
  exactly once, and prevents stale completions from the old generation;
- cancellation removes only the cancelled waiter or interest and leaves other
  interests on the same fd intact;
- closing and then reusing the same numeric fd cannot wake waiters from the
  previous lifetime;
- shutdown wakes the poller and drains registry state without leaving parked
  net waiters behind;
- poll scratch may still be rebuilt from the registry for the `poll()` backend,
  but it must not be rebuilt from the full waiter store.

## Trace And Quality Contract

The epic must add or update counters so evidence can answer:

- how many live fd entries the registry owns;
- how many fd entries are registered, updated, closed, cancelled, and reused;
- how many poll cycles run over registry entries;
- whether any legacy global-waiter fd rebuild path still executes;
- how many stale readiness completions are ignored by generation or close
  checks.

Counter names should be chosen during implementation, but the proof must be
machine-checkable through trace output or focused tests.

Every runtime-code task must run:

- `make c-check`;
- `make cppcheck`;
- `make runtime-v2-check`;
- `make check`, unless the task document records a narrower approved gate;
- `git diff --check`;
- root and scoped Sentrux scans plus rule checks.

Every net-lifecycle task must also run the exact focused net probes named in
the task, selected from `LIVENESS_PROBES.md` or introduced by this epic.

## Refactor Safety Contract

Refactoring in this epic is allowed only when it satisfies this contract:

- write or select the behavior proof before moving code;
- record the net dependency cluster and owning module before extraction;
- keep behavior changes out of refactor commits;
- move one responsibility at a time;
- do not create catch-all files such as `common`, `misc`, or vague `helpers`;
- keep new or heavily rewritten runtime files at or below 500 lines;
- reduce or keep flat every touched over-limit file unless the task records a
  specific proving-spike exception;
- use owner-first APIs and explicit status codes for new V2 primitives;
- delete code only after proving the symbol is unreachable or obsolete through
  references, build, tests, and Sentrux evidence;
- record rejected paths in `NOTES.md` so they are not rediscovered later.

## Parallelization Model

After Task 1, Tasks 2-4 may be planned in parallel if their write sets stay
separate:

- Task 2 maps runtime/native dependencies and should not edit tests.
- Task 3 writes behavior contract tests and should not edit runtime C.
- Task 4 writes static shape tests and should not edit runtime C.

Implementation tasks are mostly sequential because they change one shared net
lifecycle. Review subagents may run after each implementation task. Every
subagent must return a plan and wait for approval before edits, test-writing,
or review work starts.

## Brief Task List

| Task | Document | Purpose |
| --- | --- | --- |
| 1 | `04-tasks/01-kickoff-baseline-and-sentrux.md` | Record checkout, line counts, accepted debt, Sentrux state, and final Epic 4 gate plan. |
| 2 | `04-tasks/02-fd-registry-dependency-map.md` | Map current net readiness, close, cancellation, wake-fd, shutdown, and poll rebuild paths. |
| 3 | `04-tasks/03-fd-lifecycle-contract-tests.md` | Add focused behavior tests for fd lifecycle before replacing the rebuild path. |
| 4 | `04-tasks/04-registry-static-shape-tests.md` | Add compile/static checks for the registry skeleton and owner boundary. |
| 5 | `04-tasks/05-registry-container-skeleton.md` | Add the shard-local fd registry container without changing net behavior. |
| 6 | `04-tasks/06-net-wait-registration.md` | Route net wait registration through registry-owned fd entries while preserving wake behavior. |
| 7 | `04-tasks/07-poll-from-registry.md` | Build the `poll()` input from registry entries instead of scanning the waiter store. |
| 8 | `04-tasks/08-close-cancel-and-reregister-tests.md` | Add focused tests for close, cancellation, stale wake, and numeric fd reuse. |
| 9 | `04-tasks/09-close-cancel-and-reregister-migration.md` | Move close/cancel/re-register cleanup into the fd registry lifecycle. |
| 10 | `04-tasks/10-wake-fd-and-shutdown-tests.md` | Add tests for wake-fd notification and shutdown registry drain behavior. |
| 11 | `04-tasks/11-wake-fd-and-shutdown-migration.md` | Migrate wake-fd and shutdown paths to the registry contract. |
| 12 | `04-tasks/12-trace-counters-and-benchmark-contract.md` | Add registry counters, trace evidence, and before/after net benchmark reporting. |
| 13 | `04-tasks/13-runtime-v2-fd-registry-ci-gates.md` | Add stable fd-registry liveness tests to Runtime V2 CI gates. |
| 14 | `04-tasks/14-large-file-refactor-tranche.md` | Split cohesive net-registry code and reduce touched over-limit files after behavior is proven. |
| 15 | `04-tasks/15-epic-closeout-and-static-gates.md` | Run closeout gates, update durable docs, and state the Epic 5 handoff. |

## Epic Acceptance

Epic 4 is complete only when:

- the current `poll()` backend receives fd readiness from the persistent
  registry, not from a full waiter-store fd rebuild;
- fd registration, duplicate interest, cancellation, close, stale wake,
  re-registration, wake-fd, and shutdown behavior have focused tests;
- stable fd-registry liveness tests run in `make runtime-v2-check` and CI;
- trace or test evidence proves bounded registry ownership and no legacy
  waiter-derived poll rebuild path on the covered probes;
- native net benchmarks have before/after evidence and no unexplained
  regression;
- touched over-limit files have recorded line-count outcomes and at least one
  cohesive refactor tranche has been attempted;
- Sentrux root, `runtime/`, and `runtime/native/` scans and rule checks are
  recorded as pass/fail evidence;
- `04-evidence.md`, `NOTES.md`, this document, and `README.md` are updated with
  the final state and the exact next epic handoff.

## Epic 5 Handoff Candidate

If Epic 4 closes cleanly, Epic 5 should start from heap and hot accounting
ownership. It should not start `N>1` accept distribution until fd lifecycle and
per-shard net registry evidence are stable.
