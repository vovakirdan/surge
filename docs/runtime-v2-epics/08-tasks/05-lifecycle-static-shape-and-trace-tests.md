# Epic 8 Task 5: Lifecycle Static Shape And Trace Tests

Task 5 output. This document is self-contained: it restates the runtime state
it depends on (with `file:line` evidence at baseline `daeac51e`) and does not
assume the reader has memorized the epic. It delivers three things:

1. **Per-site control-lock trace counters** (C, additive, no behavior change):
   `rt_ctrl_site` attribution of `control_lock_acquired` across the lifecycle
   census sites, wired into the `TRACE_EXEC` dump and `bench_native_net.sh`.
2. **Static gate machinery** mirroring Epic 7's lock-split static tests: the
   gates for properties already true at `daeac51e`, plus documented pending
   gates for Tasks 6-10 with exact activation criteria.
3. **The 8x1024 per-site baseline** that decides the Task 6 escalation and
   re-verifies the 26.4 control-acq/request total.

The authoritative lane decisions live in
`../08-lifecycle-lane-proving-spike.md` (rules 1-6, S5-Q1..S9-Q7); this document
quotes the parts it gates and points back for the rest.

## A. Per-Site Control-Lock Counters

### Baseline state (what exists at `daeac51e`)

The runtime already has a single global counter: `rt_control_lock`
(`rt_lane.c:43-58`) calls `rt_trace_control_lock_acquired()` on every
acquisition (`rt_lane.c:57`), tallied in `rt_async_trace.c` and printed as
`control_lock_acquired=` in the `TRACE_EXEC` dump. That total cannot tell Tasks
6-10 which lifecycle slice a given acquisition belongs to, so the per-request
control traffic each task peels is invisible.

### Design (additive, near-zero cost when off)

A new enum `rt_ctrl_site` (`rt_async_internal.h`) names the census sites; a new
`rt_trace_control_lock_site(rt_ctrl_site)` (`rt_async_trace.c`) increments a
per-site `_Atomic uint64_t[RT_CTRL_SITE_COUNT]` array. Each census site calls it
**immediately after** its acquiring `rt_control_lock(ex)`. The global
`control_lock_acquired` total and `rt_lane.c` are unchanged, so the per-site
counters are a strict attribution **subset**: `sum(sites) <=
control_lock_acquired`, and the residual is the untagged `RT_CTRL_SITE_OTHER`
sites. The increment is guarded by the same `rt_exec_trace_enabled()`
early-return as every other trace counter, so it is a single relaxed
`fetch_add` only under `SURGE_TRACE_EXEC=1` and a predictable-branch no-op
otherwise. This keeps `rt_lane.c` and the generic lock path free of new work.

Census sites (one `rt_trace_control_lock_site(...)` line each):

| Site | Function (`file:line` at `daeac51e`) | Peeled by |
| --- | --- | --- |
| `RT_CTRL_SITE_CREATE` | `__task_create` (`rt_async_task.c:15`) | Task 6 |
| `RT_CTRL_SITE_JOIN_POLL` | `rt_task_poll` (`rt_async_task.c:88`) | Task 7 |
| `RT_CTRL_SITE_COMPLETION` | `mark_done` need-control (`rt_async_state.c:1518`) | Task 8 |
| `RT_CTRL_SITE_SCOPE` | all 5 `rt_async_scope.c` acquisitions + `apply_poll_outcome` cancelled scope teardown (`rt_async_state.c:1591`) | Task 9 |
| `RT_CTRL_SITE_AWAIT_COMPAT` | `rt_task_await` workers>1 branch (`rt_async_task.c:193`) | Task 10 |
| `RT_CTRL_SITE_HANDLE` | `rt_task_wake` (`:62`), `poll_ready_child_inline` re-lock (`:173`), `rt_task_cancel` (`:229`), `rt_task_clone` (`:243`), `task_release_lane_aware` control free (`rt_async_state.c`) | Task 7 |

`checkpoint` (`rt_async_task.c:289`) and `rt_sleep` (`:300`) stay in
`RT_CTRL_SITE_OTHER`: they are spawn-shaped, CREATE-adjacent, and negligible on
the net bench (the residual measured below is dominated by internal
`wake_task`-path acquisitions, not these). The single-worker `rt_task_await`
`run_until_done` release (`rt_async_task.c:215`) also stays in OTHER; it is the
non-net single-worker compat path.

`RT_CTRL_SITE_HANDLE` is the Task 7 handle slice: Task 7's surface is join-poll
+ wake + inline-child-poll + cancel + clone + release. Attributing only
`JOIN_POLL` would leave Task 7's before/after flip half-invisible, so the
handle sites carry their own bucket.

### Dump + bench wiring

`trace_exec_dump` appends six fields after `owner_replacements=`:
`ctrl_create ctrl_join_poll ctrl_completion ctrl_scope ctrl_await_compat
ctrl_handle` (dump buffer bumped 1152->1280). `scripts/bench_native_net.sh`
reports them as six new columns after `control lock acq`.

### Size discipline

Counter plumbing lives in the trace/lane files per the epic constraint;
`rt_async_state.c` grows by exactly three tag lines (mark_done, apply_poll
teardown, task_release_lane_aware) and stays at 1455 effective LOC, under its
1580 legacy cap. `rt_async_trace.c` is 648 effective LOC (ACCEPTABLE, <=675).
Lane order is preserved (tags sit inside the existing lock brackets); there is
no behavior change.

## B. Static Gate Machinery

`internal/vm/runtime_v2_lifecycle_static_test.go` (`//go:build
runtime_v2_pending`, package `vm_test`) mirrors Epic 7's
`runtime_v2_lock_split_static_test.go`: `clang -fsyntax-only` shape checks plus
source-scan gates, reusing `repoRoot`, `cFunctionBody`, and the
`lockSplit*` scanners.

### Active gates (green at `daeac51e`, wired into `make runtime-v2-check`)

Run by the new `runtime-v2-lifecycle-check` stage, whose `-run` regex
enumerates each green test by name (Epic 7 precedent; the six active gates + the
trace gate + Task 4's behavior contracts). Pending gates are added to the regex
by their owning task when the `Skip` is removed:

- **G1 `...StaticControlSiteEnumShape`** — the `rt_ctrl_site` enum carries the
  six census sites + `OTHER` at index 0 (`RT_CTRL_SITE_COUNT == 7`) and
  `rt_trace_control_lock_site` has the expected signature. Pins the counter API
  Tasks 6-10 measure against.
- **G2 `...StaticJoinWaiterRoutesByTargetOwner`** — `rt_waiter_route.c` maps
  `WAKER_JOIN` to `rt_task_owner_shard(ex, get_task(ex, key.id))` (S5-Q3, rule
  2). Stable across the whole epic (join never returns to control).
- **G3 `...StaticTaskTableAtomicSnapshot`** — `get_task` acquire-loads both the
  table pointer and the slot, and `rt_task_slot_store` / `rt_task_table_snapshot`
  exist (rule 1). This is the protocol S5-Q7 has the scope table adopt in Task 9.
- **G4 `...StaticJoinScopeWaitersUnqualified`** — the unqualified removal
  predicate `seq == 0 || w.seq == seq` is present (rule 6 / S9-Q7); join/scope
  keys must not adopt the channel `park_seq` qualification.
- **G5 `...StaticCreateSiteCounterWired`** — `__task_create` tags its
  acquisition `RT_CTRL_SITE_CREATE` (the escalation-critical counter).
- **G6 `...StaticCensusSitesTagged`** — every census function tags its
  acquisition with the matching `rt_ctrl_site`, so Tasks 6-10 cannot silently
  drop a tag (which would make the per-request attribution lie).

### Pending gates (Tasks 6-10; additive-then-peel)

Following the Epic 7 additive-then-peel rule (README rule 3), the per-path
"no control lane" / owner-lane assertions land now as machinery + a written
assertion but `t.Skip()` with their activating task and exact activation
criteria. The owning task **deletes the `Skip` line in the same commit that
peels its path**, turning the gate green with the migration. This is a
deliberate, documented deviation from Epic 7's red-until-wired static tests:
`t.Skip` keeps `go test -tags runtime_v2_pending ./...` green while Task 4
shares the tree.

| Gate | Task | Activation criterion (delete the `Skip`) |
| --- | --- | --- |
| `...StaticCreateReadyPushOwnerShard` | 6 | `__task_create` ready-push moves under `rt_shard_lock`; publish + `ensure_task_cap` stay control-lane (realization; see escalation below). |
| `...StaticJoinPollOwnerLane` | 7 | `rt_task_poll` holds no control lock across the join register/DONE read; `JOIN_POLL` counter drops toward zero on 8x1024. |
| `...StaticCompletionResultVisibilityOrder` | 8 | `mark_done` writes `result_kind`/`result_bits` before the `TASK_DONE` release store; `mark_done_needs_control` keeps only net-key + `done_waiters>0`. |
| `...StaticScopeOwnerLane` | 9 | `get_scope` acquire-loads a `scopes` snapshot; `WAKER_SCOPE` routes to the scope owner store instead of `ex->control_waiters`. |
| `...StaticAwaitCompatCountedSeparately` | 10 | worker-lane join (`rt_task_poll`) never references `done_cv`; only the workers>1 `rt_task_await` branch touches it. |

## C. Trace-Contract Gate

`internal/vm/runtime_v2_lifecycle_trace_test.go`
(`TestRuntimeV2LifecycleTraceControlSiteContract`, green, `SURGE_BACKEND=llvm`)
runs a spawn/await/cancel program under `SURGE_TRACE_EXEC=1`, parses the
`TRACE_EXEC reason=exit` dump, and asserts: all six per-site fields are present;
the two always-on census sites `ctrl_create` and `ctrl_join_poll` are non-zero;
and `sum(sites) <= control_lock_acquired` (the attribution invariant). It is run
by the same `runtime-v2-lifecycle-check` stage.

## D. 8x1024 Per-Site Baseline (the escalation decision)

Row: net `direct/seq`, 8 shards / 8 threads / 1024 connections / 8 req/conn =
**8192 requests**, `SURGE_TRACE_EXEC=1`, surge built at `daeac51e` + this task's
additive counters. `control_lock_acquired=215842` = **26.35/request**, which
re-verifies the Task 1 baseline of 26.4/request (the counters are non-perturbing).

| Site | Total | Per request |
| --- | ---: | ---: |
| `control_lock_acquired` (all) | 215842 | 26.348 |
| `ctrl_create` | 28673 | **3.500** |
| `ctrl_join_poll` | 31822 | 3.885 |
| `ctrl_scope` | 106499 | 13.000 |
| `ctrl_completion` | 4169 | 0.509 |
| `ctrl_await_compat` | 1 | 0.000 |
| `ctrl_handle` | 1 | 0.000 |
| sum(sites) | 171165 | 20.894 |
| residual (`OTHER`) | 44677 | 5.454 |

### Escalation verdict (S5-Q1)

The escalation criterion: if the create site accounts for **>= 2.0 control
acquisitions per request** on the 8x1024 row, Task 6 escalates realization from
(A) safe-minimal to (B) the segmented table.

**Create = 3.500/request >= 2.0 → ESCALATE. Task 6 adopts realization (B), the
segmented never-moved-slot task table**, so id-alloc + slot publish + ready-push
run under the owner shard lock with no control acquisition on create. The
per-connection amortization hypothesis for (A) is **disproven by measurement**:
create is a material per-request control consumer (request trees spawn multiple
tasks per request — 3.5 creates/request here), exactly the case S5-Q1 flagged.

### Secondary findings (for Tasks 7-9 sequencing)

- `ctrl_scope = 13.000/request` is the single largest attributable consumer —
  Task 9 (scope owner lane) has the biggest per-request control payoff of the
  lifecycle epic. The `SCOPE` bucket aggregates enter/register/join_all/exit/
  cancel + cancelled-poll teardown, so the whole scope lane is measurable as one
  flip.
- `ctrl_join_poll = 3.885/request` is Task 7's target; `ctrl_completion =
  0.509/request` is Task 8's.
- `ctrl_await_compat` and `ctrl_handle` are ~0 on the net bench: this workload
  does not call the public `rt_task_wake`/`clone`/`cancel` builtins or hit a
  worker-context control free, and uses worker-side joins (not external await).
  The residual 5.454/request is dominated by internal `wake_task`-path and
  `checkpoint`/sleep spawn acquisitions, which are out of the lifecycle census.

## E. Checks Run

- `git diff --check`: clean.
- `make c-check` (cfmt + strict warnings): OK.
- `make cppcheck`: OK.
- Active static gates (G1-G6) verified against the real sources; G1 `clang
  -fsyntax-only` snippet compiles.
- `./check_file_sizes.sh -a`: `rt_async_state.c` 1455 (<=1580 legacy),
  `rt_async_trace.c` 648 (ACCEPTABLE), `rt_async_task.c` 289, `rt_async_scope.c`
  167, `rt_async_internal.h` 529 — all within limits.
- `make runtime-v2-check` (incl. `runtime-v2-lifecycle-check`) and `make check`:
  run under the commit barrier after Task 4 lands (this stage's regex also runs
  Task 4's `TestRuntimeV2LifecycleBehavior*` contracts).
- 8x1024 baseline row: recorded above.

## Commit Boundary

One commit: `test(runtime): add epic 8 lifecycle static gates and per-site
control-lock counters`. Sequenced after Task 4's behavior-test commit so the
`TestRuntimeV2LifecycleBehavior*` symbols the `runtime-v2-lifecycle-check` regex
picks up already exist.
