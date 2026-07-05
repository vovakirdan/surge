# Epic 8 Task 10: Await / Runner / Blocking Compatibility

Task 10 narrows the external-await, single-worker-runner, and sync-channel
blocking compatibility paths so they cannot reintroduce hot control-lane traffic
onto the worker steady path, and it peels the P10 static gate: `done_cv` (and the
sync `compat_cv`) are external/main-thread only and are **counted separately**
from the worker-lane join path (spike rule 5). This document is self-contained:
it restates the current runtime state with `file:line` evidence, records the
decisions and their named guardians, and reports the evidence, so the reviewer
can verify every claim without re-reading the whole epic.

Baseline commit: `b9a420c0` (Task 9 landing, scope owner lane). All line numbers
were re-verified against this tree.

## Scope And Boundary

In scope (spike rule 5, S6-Q1's `done_waiters` survivor, and the epic acceptance
"external/main-thread await compatibility ... counted separately from worker
steady state"):

- keep `done_cv` external-only and prove it with a static gate (P10) and a trace
  guardian;
- **count the external-await-forced completions separately** from worker
  steady-state completion (honest attribution, the one behavior-neutral code
  change);
- document the single-worker runner (`run_until_done`/`run_ready_one`) and the
  sync-channel `compat_cv` lane as counted-separate compatibility lanes that stay
  control-lane by design (honest split, no migration).

Out of scope / non-goals:

- the net `ctrl_completion` / `wait_keys` removal (S6-Q1 keeps net-key removal
  out of this epic) — but see the **correction** below: the 8x1024 bench's
  `ctrl_completion` was **not** a net residual;
- `ctrl_scope` = 19464 cross-owner fallback (Task 9 / net-placement work);
- the select slow lane (named epic non-goal);
- any Phase 4 surface; any `SURGE_SHARDS=1` behavior change;
- the wake-primitive gate (Task 8) and the `mark_done` net `wait_keys` residual —
  untouched.

No control lane is dropped in this task. The additive-then-peel rule still
applies to the P10 gate: P10 turns green in the same commit that makes the
attribution honest.

## Current State (restated, `file:line` at `b9a420c0`)

The heavy lifting was already done by Tasks 7/8/9, so the tree already satisfies
the structural half of rule 5. Verified in place:

1. **Worker-lane join never touches `done_cv`.** `rt_task_poll`
   (`rt_async_task.c:165-238`) is the owner-lane join path (P7); it references
   neither `done_cv` nor `done_waiters`.
2. **`done_cv` has exactly one waiter and one broadcaster.**
   - Waiter: `rt_task_await`'s `rt_worker_count() > 1` branch
     (`rt_async_task.c:337-357`) takes the control lane, tags
     `RT_CTRL_SITE_AWAIT_COMPAT` (`:339`), increments `ex->done_waiters`
     (`:343`), and `pthread_cond_wait(&ex->done_cv, &ex->lock)` (`:345`).
   - Broadcaster: `mark_done` (`rt_async_state.c:1567-1569`) broadcasts `done_cv`
     **only** under an `ex->done_waiters > 0` guard, so a plain worker completion
     (no external awaiter) never signals it. (`rt_shutdown.c:34` also broadcasts
     on teardown; that is the shutdown lane, not steady state.)
3. **`done_waiters` is incremented only by the external-await path**
   (`rt_async_task.c:343`, the `workers > 1` branch). It gates both the broadcast
   (`rt_async_state.c:1567`) and one `mark_done_needs_control` reason
   (`rt_async_state.c:1496`).
4. **`mark_done_needs_control` final form** (`rt_async_state.c:1488-1500`):
   net park_key `||` `wait_keys_len/select_timers_len` residual `||`
   `done_waiters > 0`. The scope and `WAKER_JOIN` reasons are gone (Tasks 7-9).

So Task 10 has no lane to migrate. Its work is (a) make the attribution honest
and (b) prove/gate the external-only invariant.

## Decision 1 — Honest counting of external-await-forced completions (rule 5)

**Verdict: attribute a completion forced onto the control lane *solely* by a
parked external awaiter (`done_waiters > 0`, with no net park_key and no residual
multi-key work) to `RT_CTRL_SITE_AWAIT_COMPAT`, not `RT_CTRL_SITE_COMPLETION`.**

`mark_done` decides `need_control` from `mark_done_needs_control`
(`rt_async_state.c:1522-1524`). When it takes the lock it now splits the tag
(`rt_async_state.c`, the `if (need_control)` block):

```c
int completion_reason =
    task->wait_keys_len > 0 || task->select_timers_len > 0 || park_needs_control;
if (completion_reason) {
    rt_trace_control_lock_site(RT_CTRL_SITE_COMPLETION);
} else {
    rt_trace_control_lock_site(RT_CTRL_SITE_AWAIT_COMPAT);
}
```

`completion_reason` is the exact complement of `mark_done_needs_control`'s
non-`done_waiters` reasons, so the `else` fires iff the *only* reason the
completion took control is `done_waiters > 0` — i.e. iff an external awaiter is
parked. That serialization exists only because of the external await, so
`AWAIT_COMPAT` is the honest lane (rule 5: counted separately from worker steady
state). This is **behavior-neutral**: the control lock is taken identically; only
the trace tag changes. `G6` still passes (`mark_done` still contains the
`RT_CTRL_SITE_COMPLETION` call in the `if` branch).

Guardian: the strengthened P10 static gate asserts `mark_done` contains the
`AWAIT_COMPAT` tag, and the trace guardian
(`TestRuntimeV2LifecycleTraceAwaitCompatCountedSeparately`) asserts an external
await produces a non-zero `ctrl_await_compat`.

### Correction to the recorded 8x1024 `ctrl_completion` attribution

This split produced a provable correction to Task 9's `ctrl_completion` finding
(and to `RV2-DEBT-016`). On the 8x1024 direct/seq bench the whole
`ctrl_completion` = 28673 population moved to `ctrl_await_compat` (measured
below). Because `completion_reason` was false for every one of them, none had
`wait_keys`, select timers, or a net park_key — so the *only* reason they took
control was `done_waiters > 0`. The net benchmark's `@entrypoint main` calls
`serve_many(...).await()` (`benchmarks/native/net_request_reply/main.sg:309`) on
the non-worker main thread with `workers > 1`, so it parks on `done_cv` and holds
`done_waiters = 1` for the entire run. Every net-wrapper child completion
therefore serializes on control **because of the benchmark's own main-thread
external await**, not because of a net `wait_keys` residual.

This overturns Task 9's Proof-2 interpretation ("net wait_keys residual owned by
future net-handle work"): Proof 2 only split net-park vs not, so its `else`
conflated `wait_keys` with `done_waiters`. The finer split here (which also
checks `wait_keys`/`select_timers`) shows the driver is `done_waiters`.
Net-wrapper child completions are shard-local (control-free) whenever
`done_waiters == 0` — the net wake clears the child's `park_key` before
completion (Task 8's wake gate), and these children never register `wait_keys[]`.

Consequence recorded for Task 12 (net re-baseline): every multi-worker Surge
program parks an external awaiter on the root task for its whole lifetime
(`@entrypoint main` awaits the root), so `done_waiters = 1` in steady state and a
large share of the net bench's control traffic (28673 of 105351, ~27%) is that
harness artifact, not worker steady-state completion. The net bench is not a
clean measurement of steady-state completion cost. `RV2-DEBT-016` is updated
accordingly.

## Decision 2 — Single-worker runner stays a counted-separate compat lane (honest split)

**Verdict: no migration.** `run_until_done` (`rt_async_poll.c:237-279`) and
`run_ready_one` (`rt_async_poll.c:155-235`) are the `N=1` legacy executor loop:
`run_ready_one` takes the control lane around the *entire* scheduler turn
(`:160`, `:206/210`, `:233`), which is the whole point of the single-worker
runner — it has no shards to lane against. `rt_task_await`'s `workers == 1` branch
(`rt_async_task.c:358-363`) drives it and takes control once for the final
`task_release`.

These are compatibility lanes, not worker steady-path: on the 8-shard net steady
state they do not run at all (workers > 1 uses the `done_cv` external-await branch
and owner-lane worker turns). They stay untagged (`RT_CTRL_SITE_OTHER`) — tagging
the whole `N=1` executor loop as a lifecycle site would misrepresent it as
per-request lifecycle traffic. This matches the Task 1 census ("Named
compatibility (stays, counted separately): ... `run_until_done` x2 +
`run_ready_one` x2 + `poll_task`"). Rule 5 explicitly keeps the single-worker
runner "counted-separately compatibility."

## Decision 3 — Sync-channel `compat_cv` lane stays a counted-separate compat lane (honest split)

**Verdict: no migration.** `rt_wait_current_worker_wakeup`
(`rt_async_compat.c:119-161`) is the deprecated sync-channel thread-blocking
path: it takes the control lane (`:132`), moves the worker's local queue to
inject, parks the OS worker on the self-refreshing `compat_cv` slice
(`compat_cv_timedwait_slice`, `:104-117`, `:149`), and may spawn a compensation
worker. It is reached only from `rt_channel_sync.c:177` (sync channel helpers),
never from the async fast lanes. Its latency envelope is the recorded
`RV2-DEBT-017`. Rule 5 names it a counted-separate compat lane; it stays
control-lane by design and is retired only when the sync-channel compat lane is
retired (`RV2-DEBT-017` owner). No change.

## Guardians (what proves each claim)

| Claim | Guardian | Kind |
| --- | --- | --- |
| worker-lane join never touches `done_cv` | P10 static gate (i) | static |
| `done_cv`'s only waiter is `rt_task_await` (workers>1), tagged `AWAIT_COMPAT` | P10 (ii) | static |
| `done_cv` broadcast is `done_waiters`-guarded (plain completions skip it) | P10 (iii) | static |
| external-await-forced completion is counted `AWAIT_COMPAT` | P10 (iv) | static |
| `done_cv` confined to `mark_done`'s guarded broadcast in the completion module | P10 (v: one broadcast, no wait in `rt_async_state.c`) | static |
| external await produces non-zero `ctrl_await_compat` | `TestRuntimeV2LifecycleTraceAwaitCompatCountedSeparately` | trace |
| worker-side join and external await both observe correct results concurrently | `TestRuntimeV2LifecycleWorkerAwaitVsExternalAwait` (Task 4, retained) | behavior |
| `G6` census tags unchanged (`mark_done` still `COMPLETION`, `rt_task_await` `AWAIT_COMPAT`) | `TestRuntimeV2LifecycleStaticCensusSitesTagged` | static |

Behavior guardian = **Plan A** (a real `workers>1` external await driven
end-to-end): `TestRuntimeV2LifecycleTraceAwaitCompatCountedSeparately` runs a
`SURGE_SHARDS=2` program whose non-worker `@entrypoint main` externally awaits
(`done_cv` branch) while inner spawned tasks join worker-side, and asserts
`ctrl_await_compat > 0`. The other half of Plan A — "worker-lane join contributes
0 to the await-compat lane" — is delegated to the **static** gate (P10 (i):
`rt_task_poll` never references `done_cv`, a structural zero) rather than a
numeric assertion, because every Surge program's `main` parks a root external
awaiter, so an "external-await-free" program cannot be constructed to isolate a
numeric zero. The structural proof is stronger than a numeric zero would be, and
is recorded here as the reason Plan B (a C-harness counter read) was not needed.

`TestRuntimeV2LifecycleStaticAwaitCompatCountedSeparately` (P10) and
`TestRuntimeV2LifecycleTraceAwaitCompatCountedSeparately` are wired into
`make runtime-v2-lifecycle-check` (Makefile lifecycle regex).

## Open Correctness Note — `done_cv` broadcast ordering (RV2-DEBT-022)

Per Global Rule 2 (explainable wakeups) the external-await wakeup was traced and a
narrow, pre-existing latent lost-wakeup window was found and recorded as
`RV2-DEBT-022` (not fixed in this task — see below). Mechanism:

`mark_done` reads `done_waiters` locklessly in `mark_done_needs_control`
(`rt_async_state.c:1496`) to decide whether to take the control lane, and the
`done_cv` broadcast (`:1567-1569`) only runs (under the lock) when that decision
took control. The external awaiter increments `done_waiters` under the control
lane (`rt_async_task.c:343`) then re-checks status and parks
(`:344-345`). A completion that reads `done_waiters == 0` in the window before the
awaiter's increment — while the awaited target completes on a worker that has not
re-synchronized with that increment — can skip both the control acquisition and
the broadcast, stranding the awaiter (`done_cv` has no other steady-state wakeup
source). This is the classic StoreLoad / condvar-with-lockless-signaler-check
gap: the awaiter's `increment; load status` and the signaler's `store DONE; load
done_waiters` need seq-cst on both sides plus a post-`DONE` re-check that
broadcasts under the lock.

Why not fixed here: the simple "increment `done_waiters` before `wake_task`"
reorder is **insufficient** — it does not cover an already-`RUNNING` awaited
target (whose worker never re-synchronizes to observe the increment), and the
real defect is the StoreLoad/seq-cst gap. A correct fix is a genuine protocol
change (seq-cst fences + late-broadcast-under-lock) that is too heavy and
destabilizing for this narrow count/gate task, and the window is empirically
unreachable (the external-await path is `main` awaiting a long-lived task; the
behavior test's quick target has never flaked across the epic). Recorded as
`RV2-DEBT-022` for a focused external-await/compat fix or Epic 8 closeout. No
behavior change to `done_cv` is made in this task.

## Files Touched

| Path | Change |
| --- | --- |
| `runtime/native/rt_async_state.c` | `mark_done`: split the `need_control` tag (COMPLETION vs AWAIT_COMPAT); +6 effective LOC (1377→1383, ≤1580) |
| `internal/vm/runtime_v2_lifecycle_static_test.go` | P10 activated + strengthened (5 assertions) |
| `internal/vm/runtime_v2_lifecycle_trace_test.go` | new `TestRuntimeV2LifecycleTraceAwaitCompatCountedSeparately` |
| `Makefile` | lifecycle regex + `StaticAwaitCompatCountedSeparately`, `TraceAwaitCompatCountedSeparately` |
| `docs/runtime-v2-epics/08-tasks/10-await-runner-blocking-compat.md` | this doc |
| `docs/runtime-v2-epics/08-evidence.md` | Task 10 section |
| `docs/runtime-v2-epics/08-tasks/README.md` | Task 10 status → Complete |
| `docs/runtime-v2-epics/NOTES.md` | Task 10 handoff |
| `docs/runtime-v2-epics/DEBT.md` | RV2-DEBT-016 correction; RV2-DEBT-022 raised |

No `rt_async_task.c`, `rt_async_poll.c`, or `rt_async_compat.c` runtime change
(Decisions 2 and 3 are honest splits, no migration).

## Measurement (8x1024 direct/seq, 8192 req, `SURGE_TRACE_EXEC=1`)

Before = Task 9 anchor (`b9a420c0`). After = this task. Total control is
unchanged (behavior-neutral re-tag); only the completion/await-compat split flips.

| Site | Before (Task 9) | After (Task 10) |
| --- | ---: | ---: |
| `control_lock_acquired` | 105285 | 105351 (noise) |
| `ctrl_completion` | 28673 | **0** |
| `ctrl_await_compat` | 1 | **28674** |
| `ctrl_scope` | 19464 | 19465 |
| `ctrl_handle` | 29696 | 29696 |
| `ctrl_join_poll` | 2039 | 2039 |
| `ctrl_create` | 8 | 10 |

The 28673 completions that were tagged `COMPLETION` are now correctly tagged
`AWAIT_COMPAT` — they take control only because the benchmark's `main` externally
awaits `serve_many` (`done_waiters = 1` throughout), which is external-await
compat, counted separately (Decision 1 + correction). Worker steady-state
completion control is ≈ 0 on this bench.

## Gates

(Filled at commit time — see `08-evidence.md` Task 10 section for the recorded
results: `git diff --check`, `make c-check`, `make cppcheck`,
`make runtime-v2-check` incl. `runtime-v2-lifecycle-check` with P10 wired,
`make check`, `./check_file_sizes.sh -a`, the no-keepalive
`CompletionPinInterleavingTSan` @ shards 1/2/8, Sentrux scoped rescan + session,
and the before/after bench above.)
