# Task 7: Per-Shard Scheduler Placement

**Status:** Draft
**Kind:** runtime code
**Depends on:** Task 6

## Context

`struct rt_scheduler` (`rt_async_internal.h:129-137`) already lives inside
`rt_shard` (`:150-160`, field `scheduler`), with its own `inject` deque,
`local_queues`, `worker_ctxs`, `worker_count`, `running_count`, and seeded-mode
fields. This means the per-shard ready-queue structure the target
architecture wants already exists — it is simply unused for `k>0` before
Task 6. What is genuinely missing:

- **Task ownership metadata.** `rt_task` (`rt_async_internal.h:167-202`) has
  no shard/owner field at all. There is currently no way to ask "which shard
  does this task belong to."
- **A steal boundary.** `SCHED_TRACE steal`
  (`docs/RUNTIME.md:326`, `rt_async_state.c:454`) proves the existing
  scheduler can steal ready work from another worker's local queue;
  `TestMTWorkStealing`/`TestMTSeededScheduler`
  (`internal/vm/mt_executor_test.go:1350,1428`) assert this happens. That
  code has no concept of "shard" today — all current stealing is
  intra-shard-only in the new terms, because there has only ever been one
  shard. This task must add the concept of "is this steal crossing shard
  ownership" and block it only in that case, while leaving intra-shard
  stealing (within `shards[0]`, or within any single shard's own
  `worker_ctxs`) exactly as it is, per the Epic 6 Boundary Decisions
  paragraph: *"No-steal is shard-relative. With `SURGE_SHARDS=1`, there is no
  non-owner shard, so the no-steal rule is vacuous and current intra-shard
  worker stealing remains a compatibility path. Existing MT gates such as
  `TestMTWorkStealing` and `TestMTSeededScheduler` must keep passing for the
  `SURGE_SHARDS=1` path."*
- **The parked-with-work invariant.** `LIVENESS_PROBES.md`'s Missing
  Mandatory Probes table names this explicitly: *"Catch lost wakeups where a
  worker or shard sleeps while runnable work is queued... blocks any task
  that changes wake elision, ready queue sleep, shard park state, or
  cross-shard inbound queues."* Disabling cross-shard steal is exactly such a
  change (a shard whose own queue is empty can no longer look at another
  shard's queue and decide to keep running instead of parking), so this
  probe stops being optional the moment this task lands its no-steal
  boundary.
- **Real worker-to-shard binding.** Task 6 only sizes and initializes each
  shard's `scheduler` *structure* (`local_queues`/`worker_count` via
  `rt_shard_scheduler_init`); it deliberately does not touch worker OS
  threads. Today, exactly one function spawns worker threads at all:
  `rt_start_workers` (`rt_async_state.c:278-319`), which is fully
  executor-global — one `ex->workers` array, one `rt_io_main` I/O thread,
  one loop spawning `rt_worker_main` threads into `scheduler->worker_ctxs`
  (itself resolved through the shard-0-only `rt_executor_scheduler`).
  `rt_worker_ctx` (`rt_async_state.c:17`) has no shard field. This task must
  make `rt_start_workers` shard-aware: spawn each shard's own worker
  thread(s) against that shard's own `scheduler.worker_ctxs` (allocated
  here, not in Task 6), with each `rt_worker_ctx` carrying the owning
  shard's id so the steal-boundary check has something concrete to test.
  This is the one piece of "per-shard scheduler placement" that is actually
  about OS threads, not just data structures — do not assume Task 6 already
  did it.

## Boundary With Task 6

Task 6 initializes shard *structures* only (`rt_shard_scheduler_init` per
shard, sizing `local_queues`/`worker_count`) and explicitly does not spawn
any worker OS thread or allocate `worker_ctxs` beyond what today's single
`rt_start_workers` call already does. This task is where `rt_start_workers`
itself becomes shard-aware: it allocates each shard's own `worker_ctxs`,
spawns the actual worker thread(s) for `SURGE_SHARDS>1` (one per shard, per
the Epic 6 Boundary Decisions rule), and tags each `rt_worker_ctx`/task with
the shard it belongs to. If any part of this reads as ambiguous between the
two task documents, this section is authoritative: worker OS-thread spawning
and shard binding is Task 7's scope, not Task 6's.

Per the Epic 6 Boundary Decisions paragraph, only net accept/readiness
ownership is moving in this epic; task placement for connection tasks is
what makes that ownership meaningful (a connection task must run on the
shard whose `fd_registry`/`waiter_store` actually holds its rows, or every
read/write it does will need to reach across shards under the lock, which
defeats the locality goal even though correctness is preserved by the shared
lock).

## Goal

Add owner-shard placement metadata usable by connection tasks, prevent
non-owner shards from stealing a task once it is marked shard-owned, and
implement (or add an equivalent focused proof of) the parked-with-work
invariant for the now-meaningful shard boundary.

## Why This Task Exists

`RUNTIME_V2.md` §3 ("No Hot-Path Stealing") is the core Tier 1 property this
whole epic is building toward: connection tasks are shard-local, and a shard
may not steal another shard's connection task just because it is idle. This
is also the task the Accept Ownership Contract cites directly: *"Tier 1
connection tasks are not stolen by non-owner shards. If the current scheduler
cannot prove task class or ownership yet, the task must add that metadata
before disabling steals."*

## Scope

- Make `rt_start_workers` (`rt_async_state.c:278-319`) shard-aware: for
  `SURGE_SHARDS>1`, allocate each shard's own `scheduler.worker_ctxs` and
  spawn that shard's configured worker thread(s) against it (one worker per
  shard per the Epic 6 Boundary Decisions rule), instead of the current
  single executor-global allocation and spawn loop. Tag each
  `rt_worker_ctx` with the shard it runs for. For `SURGE_SHARDS=1`, this
  must reduce to exactly today's behavior (one call, shard 0's
  `worker_ctxs`, same thread count) — this is the compatibility floor to
  check first.
- Add owner-shard metadata reachable from a task — either a field directly
  on `rt_task`, or a lookup through the connection object it is bound to
  (decide based on Task 2's map and Task 8's planned connection-metadata
  shape; if Task 8 has not landed yet, add the field to `rt_task` now and
  let Task 8 attach the connection-to-shard link separately, since a task
  can exist before Task 8's listener/connection metadata is fully wired).
- Add a task-class distinction sufficient to answer "is this a Tier 1
  connection task with a shard owner, or a class of task steal rules do not
  yet apply to" (CPU-bound/non-connection work may keep using the current
  compatibility scheduler per the Accept Ownership Contract: *"CPU-bound
  non-connection work may keep using the current compatibility scheduler
  while Tier 2 is still future work, but it must not justify stealing
  connection tasks."*).
- Modify the steal path (wherever `SCHED_TRACE steal` currently fires) to
  check shard ownership before stealing a marked connection task; block the
  steal if the stealing worker is not on the task's owner shard. Do not
  touch the steal path for tasks without owner-shard metadata — those keep
  today's behavior exactly.
- With `SURGE_SHARDS=1`, confirm this check is provably a no-op (there is
  only one shard, so "not on the task's owner shard" can never be true) —
  this is what keeps `TestMTWorkStealing`/`TestMTSeededScheduler` green.
- Implement the parked-with-work invariant per `LIVENESS_PROBES.md`'s
  candidate: at minimum, a per-shard form — "a shard worker does not sleep
  while its own ready queue, local fd-ready batch, or required wake work is
  non-empty" — as a debug assertion plus a focused test that deliberately
  tries to create the violation (e.g. force a race between a worker parking
  and a connection task being enqueued to its shard) and fails loudly if it
  occurs.
- Update the Task 4 pending contract tests and Task 5 static gates that
  target this task's scope; some Task 4 cases should flip from
  pending-fail to passing here (specifically: "connection tasks are not
  reported through `SCHED_TRACE steal`" and the parked-with-work case).

## Out Of Scope

- Actually routing accepted connections to specific shards (Task 9) — this
  task only builds the placement/no-steal mechanism; Task 9 is what
  populates it with real accepted connections.
- Per-shard poller/wake mechanism (Task 10) — this task's parked-with-work
  invariant covers the scheduler's own ready-queue sleep, not the net
  poller's sleep/wake cycle, which Task 10 owns separately.
- Migration (moving an already-placed task to a different shard) — explicitly
  out of scope for the whole epic.
- Tier 2 CPU-pool stealing — unaffected by this task; it is a different
  destination and keeps stealing internally by design (`RUNTIME_V2.md` §10).

## Approach / Steps

1. Confirm Task 6 has landed (`N` shard *structures* correctly sized —
   `local_queues`/`worker_count` per shard via `rt_shard_scheduler_init`).
   Task 6 does not spawn worker threads or allocate `worker_ctxs` beyond
   shard 0; this task needs real, running worker threads on at least 2
   shards to test the no-steal boundary meaningfully, so step 2 below (make
   `rt_start_workers` shard-aware) must land before the no-steal proof in
   step 5 can run for real.
2. Make `rt_start_workers` shard-aware: allocate `worker_ctxs` and spawn
   worker thread(s) per shard instead of once for the executor; confirm
   `SURGE_SHARDS=1` reduces to today's exact behavior first.
3. Decide and record the owner-shard metadata location (task field vs.
   connection-object lookup) with the tradeoff written down (Global Rule 5:
   "why the existing primitive is wrong or insufficient... which invariant
   the new primitive owns").
4. Add the task-class/ownership field(s); wire the steal path's ownership
   check.
5. Write and run the deliberate no-steal proof: spawn connection tasks
   pinned to shard `k`, keep shard `k`'s worker busy, keep another shard's
   worker idle, and confirm `SCHED_TRACE steal` never fires for the pinned
   task across the idle shard.
6. Implement the parked-with-work invariant and its adversarial test.
7. Run `TestMTWorkStealing`/`TestMTSeededScheduler` under `SURGE_SHARDS=1`
   and confirm they are unaffected.
8. Flip the relevant Task 4 pending tests to passing; confirm Task 5's
   static gates still pass.
9. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch:

- `runtime/native/rt_async_internal.h` (task/ownership metadata fields)
- `runtime/native/rt_async_state.c` (`rt_start_workers` shard-aware
  rewrite, `rt_worker_ctx` shard tagging, steal path, parked-with-work
  invariant)
- `runtime/native/rt_async_task.c`, if the steal path or task-class check
  lives there instead

Read:

- `docs/RUNTIME_V2.md` §3 ("No Hot-Path Stealing"), §10 (Execution Tiers)
- `docs/runtime-v2-epics/LIVENESS_PROBES.md` (Missing Mandatory Probes:
  "Parked-with-work invariant" row; Mandatory Gate By Change Type: "Scheduler
  ready queues, worker sleep/wake, work stealing, or seeded mode" row)
- `internal/vm/mt_executor_test.go:1350,1428` (existing steal/seeded tests)
- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` (Task 2)

## Skills & Working Practice

- Full Global Rule 9 plan gate: state the owner-metadata location decision
  and the exact steal-path check before writing code.
- This is a scheduler/wakeup change, so `LIVENESS_PROBES.md`'s Mandatory Gate
  By Change Type row for "Scheduler ready queues, worker sleep/wake, work
  stealing, or seeded mode" applies in full: MT process timeout wrapper,
  scheduler source trace, parked-with-work invariant, SIGUSR1 snapshot if
  the process runs long enough to sample.
- Sequenced strictly after Task 6; Task 8 can proceed in parallel with this
  task if their write sets stay disjoint (owner metadata on the task vs. on
  the listener/connection object), but reconcile the shared "how does a task
  find its connection's shard" question between the two before Task 9
  starts.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- `go test ./internal/vm -run 'TestMT(WorkStealing|SeededScheduler)'` (must
  stay green under `SURGE_SHARDS=1`)
- The new deliberate no-steal proof test
- The new parked-with-work invariant test
- `git diff --check`
- Sentrux root and scoped scans

## Definition Of Done

- [ ] `rt_start_workers` is shard-aware: under `SURGE_SHARDS>1`, each shard
      has its own real, running worker thread(s) with its own
      `worker_ctxs`, each tagged with its owning shard; under
      `SURGE_SHARDS=1`, behavior is byte-for-byte identical to before this
      task.
- [ ] Owner-shard metadata exists and is reachable from a connection task.
- [ ] A non-owner shard cannot steal a marked connection task; proven by a
      deliberate adversarial test, not just absence of failure.
- [ ] `SURGE_SHARDS=1` keeps `TestMTWorkStealing`/`TestMTSeededScheduler`
      green unchanged.
- [ ] CPU-bound/non-connection tasks are unaffected by the new steal check.
- [ ] The parked-with-work invariant (at least the per-shard form) is
      implemented with an adversarial test that fails on a deliberately
      injected violation and passes at steady state.
- [ ] Relevant Task 4 pending tests now pass; Task 5 static gates unaffected.

## Evidence To Record

- `06-evidence.md`: owner-metadata design decision and tradeoff, Contracts
  Touched (no-steal boundary, parked-with-work invariant), Commands/Checks,
  Trace Counters/Liveness Proof (`SCHED_TRACE` rows for the no-steal proof).
- `NOTES.md`: the metadata location decision, so Task 8/9 know exactly how
  to look up a task's owner shard.
