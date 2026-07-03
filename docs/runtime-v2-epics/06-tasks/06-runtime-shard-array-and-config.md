# Task 6: Runtime Shard Array And Config

**Status:** Draft
**Kind:** runtime code
**Depends on:** Task 1, Task 2, Task 3, Task 5

## Context

Today (`runtime/native/rt_async_internal.h:127,162-165`):

```c
#define RT_RUNTIME_SHARD_COUNT 1U
...
struct rt_runtime {
    size_t shard_count;
    rt_shard shards[RT_RUNTIME_SHARD_COUNT];
};
```

`shard_count` already exists as a runtime field, but the array behind it is
sized by the fixed macro, and `rt_runtime_init_n1`
(`runtime/native/rt_runtime.c:19-40`) only ever initializes `shards[0]`. Every
other accessor in that file (`rt_runtime_shard0`, and everything that calls
it: `rt_executor_scheduler`, `rt_executor_net_poll_scratch`,
`rt_executor_channel_blocking_compat`, `rt_executor_waiter_store`,
`rt_executor_fd_registry`) hardcodes `runtime->shard_count !=
RT_RUNTIME_SHARD_COUNT` as its validity guard (lines 51, 86, 117, 138, 159).
`rt_shard_scheduler_init` (`rt_runtime.c:165-187`) is already a general
per-shard initializer that takes an explicit `worker_count` — it does not
need to change to support a second shard, it just needs to be called more
than once.

`rt_env_worker_count` (`runtime/native/rt_async_state.c:109-110`) reads
`SURGE_THREADS` and feeds `exec_init_once` (`:201`), which today sizes the
single executor's one worker pool. This task must add the `SURGE_SHARDS`
read and the interaction rule the epic requires (Epic 6 Boundary Decisions:
one Tier 1 worker per shard when `SURGE_SHARDS>1`; a conflicting
`SURGE_THREADS` is an explicit configuration error).

**This task stops at structural/config initialization — it does not spawn
worker OS threads or bind them to a shard.** Concretely, today
`exec_init_once` calls `rt_shard_scheduler_init(rt_runtime_shard0(...), threads, ...)`
(`rt_async_state.c:216-220`), which only allocates `scheduler->local_queues`
and sets `worker_count`/`sched_mode`/`sched_seed` — it does not touch
`worker_ctxs` and never calls `pthread_create`. The actual OS-thread
spawning, `rt_worker_ctx` allocation, and `worker_id` assignment happen
separately in `rt_start_workers` (`rt_async_state.c:278-319`), which is
*itself* fully executor-global today: one `ex->workers` array, one call to
`rt_io_main` for the single I/O thread, one loop spawning `rt_worker_main`
threads — none of it shard-aware, and `rt_worker_ctx` has no shard field.
This task calls `rt_shard_scheduler_init` once per configured shard (so each
shard's `local_queues`/`worker_count` are correctly sized), but it must
**not** attempt to make `rt_start_workers` shard-aware, must not populate
`worker_ctxs` for shard indices beyond what today's single call already
does, and must not spawn any new OS thread. Making `rt_start_workers`
shard-aware — one worker (or configured `worker_count`) actually running per
shard, with each `rt_worker_ctx` carrying its owning shard's id — is Task 7's
job, because Task 7 owns the placement metadata (`ctx`/`rt_task` shard
field) that this binding needs to be meaningful. If this task's own testing
needs to observe more than one shard's scheduler existing, it is enough to
confirm the *structures* are correctly sized and initialized for `N` shards;
it does not need real multi-shard worker threads running to do that.

Task 5 has already updated the two `N=1` static pins
(`runtime_v2_skeleton_static_test.go`, `runtime_v2_fd_registry_static_test.go`)
to assert the dynamic-shard shape instead of a fixed `1`, so this task's
macro/struct change should make those tests pass, not break them.

## Goal

Replace the fixed `N=1` runtime skeleton with an internal `N>=1` shard
configuration driven by `SURGE_SHARDS`, using `RT_RUNTIME_MAX_SHARDS` plus a
runtime `shard_count` bound, while keeping `SURGE_SHARDS=1` behavior
byte-for-byte compatible with today.

## Why This Task Exists

This is the structural precondition for every other implementation task.
Task 7 (scheduler placement) needs more than one shard to place a task on.
Task 8 (owner metadata) needs a real shard index to attach to a listener/
connection. Task 9 (accept distribution) and Task 11 (net lifecycle
migration) need real per-shard `fd_registry`/`waiter_store` instances to
route into. Nothing downstream can be exercised for real until this lands.

## Scope

- Add `RT_RUNTIME_MAX_SHARDS` (a compile-time upper bound; pick a
  conservative bound and record why, e.g. matching a reasonable core-count
  ceiling — do not make it unbounded/dynamically allocated unless Task 6
  itself proves during implementation that the fixed-bound `rt_shard
  shards[RT_RUNTIME_MAX_SHARDS]` shape is materially harder to get right
  than a heap-allocated array; the epic states a preference for the fixed
  bound "unless Task 6 proves a different shape is simpler and equally
  testable" — if you deviate, write down the proof).
- Change `struct rt_runtime` to size its `shards[]` array by
  `RT_RUNTIME_MAX_SHARDS` and use the existing `shard_count` field as the
  actual configured count (`1 <= shard_count <= RT_RUNTIME_MAX_SHARDS`).
- Add an internal shard-count configuration path, most likely
  `SURGE_SHARDS`, defaulting to `1` (the compatibility default named in the
  epic Scope) until a later task/epic promotes a multi-shard CI gate.
- Implement the `SURGE_SHARDS`/`SURGE_THREADS` interaction exactly as the
  epic requires: `SURGE_SHARDS=1` keeps `SURGE_THREADS` as the existing
  compatibility worker-count control (no behavior change); `SURGE_SHARDS>1`
  means one Tier 1 worker per shard, and a `SURGE_THREADS` value that is set
  and does not equal `SURGE_SHARDS` is an explicit configuration error (not
  a silent override) — return a status through the existing
  `rt_runtime_status` enum (`rt_async_internal.h:115-119`), do not
  `panic_msg` for this recoverable configuration error (`RULES.md` Global
  Rule 8).
- Initialize `N` shards' *structures* — their own `scheduler` (via
  `rt_shard_scheduler_init`, which sizes `local_queues`/`worker_count` only,
  not `worker_ctxs`), `waiter_store`, `fd_registry`, `net_poll_scratch`,
  `heap_accounting`, and (per Task 12) trace-counter storage — all fields
  already exist on `rt_shard` (`rt_async_internal.h:150-160`); this task's
  job is calling their existing per-field `init` functions
  (`rt_heap_accounting_init`, `rt_fd_registry_init`,
  `rt_shard_scheduler_init`) once per shard instead of once for `shards[0]`
  only, and setting each shard's `shard_id`. This does **not** include
  spawning any worker OS thread or allocating `worker_ctxs` for shard
  indices beyond shard 0 — see the boundary note in Context. Task 7 owns
  making `rt_start_workers` shard-aware and actually running a worker per
  shard.
- Add a genuinely shard-indexed accessor family alongside (not instead of)
  the existing shard-0 compatibility accessors — e.g. `rt_runtime_shard(rt_runtime*, size_t index)`
  next to `rt_runtime_shard0`, and shard-indexed variants of
  `rt_executor_fd_registry`/`rt_executor_net_poll_scratch` for the
  net-ownership call sites Task 2 classified as needing to become
  shard-aware. Leave the stays-global-compat call sites (channel/timer/join
  waiter routing) on the existing shard-0 accessors unchanged, per the Epic
  6 Boundary Decisions invariant.
- Update the two static pins Task 5 rewrote to actually exercise the new
  macro/struct shape (they were written against the intended shape; confirm
  they pass against the real one now).
- Preserve the current public ABI; this is purely internal runtime
  structure.

## Out Of Scope

- Making `rt_start_workers` (`rt_async_state.c:278-319`) shard-aware,
  spawning any worker OS thread bound to a specific shard, or populating
  `scheduler->worker_ctxs` for shard indices beyond what today's single
  executor-global call already produces. This task only sizes and
  initializes each shard's structures (`local_queues`/`worker_count` via
  `rt_shard_scheduler_init`, plus the other per-shard fields); real
  worker-to-shard binding is Task 7's job (see Context boundary note).
- Scheduler placement/no-steal logic (Task 7).
- Listener/connection owner metadata (Task 8).
- Accept distribution using the new shards (Task 9) — this task only makes
  `N` shards exist and initialize correctly; it does not yet route accepted
  connections to them.
- Per-shard poller/wake ownership (Task 10).
- Trace counters (Task 12).

## Approach / Steps

1. Confirm Task 5's static-test updates and Task 2's dependency map are both
   in hand; if either is still pending, do not start this task's C changes.
2. Pick `RT_RUNTIME_MAX_SHARDS`'s value and write the one-paragraph
   justification into `06-evidence.md` before writing code.
3. Change the macro/struct in `rt_async_internal.h`.
4. Rewrite `rt_runtime_init_n1`/`rt_runtime_init_global_n1`
   (`rt_runtime.c:19-44`) into a shard-count-aware initializer (rename if the
   `_n1` suffix becomes misleading — a stale name that no longer matches
   behavior is worse than a rename, per `RULES.md` Global Rule 7 on names
   exposing ownership/lifecycle). Keep a partial-init failure path clean
   (Global Rule 8: "clean up partial initialization through one documented
   failure path") — if shard `k` fails to initialize, free shards `0..k-1`
   before returning the error status.
5. Add the `SURGE_SHARDS` env read next to `rt_env_worker_count`
   (`rt_async_state.c:109-110`) and wire the conflict-detection rule into
   `exec_init_once` (`:201`).
6. Add the shard-indexed accessor family; update only the net-ownership call
   sites Task 2 named to use it; leave stays-global-compat call sites
   pointing at `rt_runtime_shard0` unchanged.
7. Run the full check list below, including Task 4's contract tests (some
   should now flip from pending-fail to passing) and Task 5's static gates.
8. Record line-count impact for every touched over-limit file
   (`rt_async_internal.h` is 499 lines already, at the edge of the Rule 4
   limit — watch this closely and split before it crosses 500 if this task's
   additions would push it over).
9. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch:

- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_runtime.c`
- `runtime/native/rt_async_state.c` (env/config reads and `exec_init_once`)

Read:

- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` (Task 2)
- `docs/runtime-v2-epics/06-listener-model-proving-spike.md` (Task 3)
- `internal/vm/runtime_v2_skeleton_static_test.go`,
  `runtime_v2_fd_registry_static_test.go` (Task 5 updates)
- `internal/vm/runtime_v2_accept_contract_test.go` (Task 4)

## Skills & Working Practice

- Full Global Rule 9 plan gate applies: the implementing subagent must state
  the exact `RT_RUNTIME_MAX_SHARDS` value and justification, the exact
  rename (if any) of `rt_runtime_init_n1`, and the exact new accessor names
  before writing any code, and wait for approval.
- This task is sequenced strictly after Tasks 1-3 and 5, and strictly before
  Tasks 7-11; do not attempt to parallelize it with them.
- Follow Global Rule 8 for the new `SURGE_SHARDS`/`SURGE_THREADS` conflict
  path: explicit status code, not `panic_msg`, and the caller (VM/CLI
  startup path) must be able to surface a diagnostic to the user rather than
  crash.
- Watch `rt_async_internal.h`'s line count (499 today) against Global Rule 4
  before adding fields/macros; if it would cross 500, split the new
  shard-count-config declarations into a small new header rather than
  pushing this file over the limit inside this task.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- `go test ./internal/vm -run 'TestRuntimeV2Skeleton'`
- `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Accept'`
  (confirm which Task-4 cases now pass)
- `git diff --check`
- Sentrux root and scoped scans

## Definition Of Done

- [ ] `SURGE_SHARDS=1` reproduces byte-identical observable behavior to
      before this task (confirm via Task 4's regression-floor test and the
      existing net/MT liveness probes).
- [ ] `SURGE_SHARDS=N>1` initializes exactly `N` shard *structures*, each
      with its own correctly-initialized `scheduler` (sized `local_queues`/
      `worker_count`), `waiter_store`, `fd_registry`, `net_poll_scratch`,
      `heap_accounting` — without spawning any worker OS thread beyond what
      today's single executor-global `rt_start_workers` call already spawns
      (that remains Task 7's job).
- [ ] An invalid `SURGE_SHARDS` value (`0`, non-numeric, `> RT_RUNTIME_MAX_SHARDS`)
      fails with an explicit status and diagnostic, not a crash.
- [ ] A conflicting `SURGE_THREADS` under `SURGE_SHARDS>1` fails explicitly.
- [ ] Partial shard-initialization failure cleans up already-initialized
      shards through one documented failure path.
- [ ] Task 5's static gates pass against the real struct/macro shape.
- [ ] The shard-indexed accessor family exists for net-owned call sites only;
      stays-global-compat call sites are unchanged.
- [ ] Line-count impact is recorded for every touched file; `rt_async_internal.h`
      stays at or below 500 lines or the overage is explicitly justified as
      an existing-file exception per Global Rule 4.

## Evidence To Record

- `06-evidence.md`: `RT_RUNTIME_MAX_SHARDS` justification, Files Touched with
  line-count deltas, Contracts Touched (shard-count config, `SURGE_SHARDS`/
  `SURGE_THREADS` interaction), Commands/Checks, Sentrux Root/Scoped Signals.
- `NOTES.md`: what changed, which Task 4 pending tests now pass, and any
  accessor naming decision Task 7-11 must follow.
