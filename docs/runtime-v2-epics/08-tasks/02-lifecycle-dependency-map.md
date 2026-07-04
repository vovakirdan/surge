# Epic 8 Task 2: Lifecycle Dependency Map

**Kind:** design map. **Depends on:** Task 1.

**Goal:** produce the authoritative dependency map for the task-lifecycle
control-lane surfaces Epic 8 will migrate, so the Task 3 proving spike starts
from a complete, `file:line`-pinned picture of what the control lane still
protects, who owns each piece of state, who wakes whom, and the target lane per
surface. This is the lifecycle analogue of
`07-executor-lock-dependency-map.md`.

## Runtime State This Task Depends On

Re-stated here (index rule: each task stands on its own) and re-verified
against the current tree at baseline `daeac51e`:

- Epic 7 split the old executor lock into per-shard lanes plus a reduced
  control lane. The worker turn runs under the owner shard lane
  (`rt_worker_turn.c:94-196`); waiter stores route by owner key; the task
  table is an atomic-snapshot structure (`get_task` at
  `rt_async_state.c:356-365`; `rt_task_table` at
  `rt_async_internal.h:246-249`).
- The remaining steady-path control consumer is task lifecycle. The 16
  steady-path sites (Task 1 census, `08-evidence.md`) with current lines:
  - `rt_async_task.c`: `__task_create:15`, `rt_task_wake:62`,
    `rt_task_poll:88`, `poll_ready_child_inline:167,173`,
    `rt_task_cancel:229`, `rt_task_clone:243`, `checkpoint:289`,
    `rt_sleep:300`.
  - `rt_async_state.c`: `task_release_lane_aware:1429`, `mark_done:1508`
    (gate `mark_done_needs_control:1486`), `apply_poll_outcome` cancelled
    branch `:1586`.
  - `rt_async_scope.c`: `rt_scope_enter:10`, `rt_scope_register_child:45`,
    `rt_scope_cancel_all:84`, `rt_scope_join_all:100`, `rt_scope_exit:134`.
- Named compatibility sites stay on control and are counted separately:
  external/N=1 await (`rt_async_task.c:193-217`), the N=1 runner
  (`rt_async_poll.c:155,237`; `rt_worker_turn.c:16`), sync-channel compat
  (`rt_async_compat.c:132,159`), blocking submit/completion
  (`rt_async_blocking.c:119,237`), and the select slow lane
  (`rt_async_select.c:43,149,250` — a **named non-goal**).
- The Task 1 generation-qualified-removal fix made the waiter-store generation
  protocol part of the lifecycle ownership contract: `park_seq`
  (`rt_async_internal.h:212`), entry `seq`, and generation-qualified removal
  (`remove_waiter_generation` at `rt_async_waiter.c:445`;
  `remove_waiter_from_store_seq:142`; channel re-arm at
  `rt_channel_lane.h:197-219`; validation at `rt_channel_lane.h:87-91`).
- The one named cross-owner lifecycle edge is the accept transition
  (`rt_task_replace_owner` at `rt_scheduler_placement.c:80-96`), which migrates
  join waiters with the task.
- The `rt_executor` invariant comment (`rt_async_internal.h:292-304`) still
  describes the old executor-wide ownership model and must be reconciled at
  closeout.

## Scope

- Create `docs/runtime-v2-epics/08-lifecycle-dependency-map.md`: the map,
  mirroring `07-executor-lock-dependency-map.md`. For each lifecycle surface it
  records current lock(s) with `file:line`, state read/written, who wakes whom,
  target lane (owner shard / control / named compat fallback), lifetime and
  generation hazards, and the Task 3 open question(s). It covers: the surface
  inventory; state ownership by struct (`rt_executor`, `rt_shard`, `rt_task`,
  `rt_scope`, `rt_task_table`); the task-table/slot protocol; the waiter-store
  generation contract; a per-surface section for all 16 steady-path sites; the
  enumerated `mark_done_needs_control` reasons; the accept transition; the
  hazards the migration must not recreate; and the consolidated Task 3 open
  questions plus a target-lane summary.
- Update `08-tasks/README.md` (Task 2 status → Complete), `08-evidence.md`
  (Task 2 evidence section per `EVIDENCE_TEMPLATE.md`), and `NOTES.md`
  (working notes).

## Out Of Scope

- No C, test, benchmark-script, or CI changes (docs-only task).
- No lane-model **decisions**: the map records the current lane, the target
  lane, and the open question for each `(spike)` cell. Task 3 decides; its
  spike output rewrites the map's lane table on conflict (index rule).
- Select slow lane migration (named non-goal; the map lists it only so
  lifecycle tasks do not regress its `seq==0` wake-only entries).
- No Phase 4 surfaces (`far`, `submit_to`, inbound queues, remote select,
  eventfd credits, seq-cst `PARKED`).

## Checks

Docs-only. Required record: `git diff --check` clean; nothing is compiled, so
no build/test gate applies. Recorded in the Task 2 evidence section.

## Evidence Plan

- `08-evidence.md` gains a `## Task 2` section: files touched, the docs-only
  check results, contracts preserved (all — no behavior changes), and the
  target-lane summary reference.
- `NOTES.md` gains an Epic 8 Task 2 handoff: what the map concluded, the target
  lane per surface, and the open questions Task 3 must resolve.

## Commit Boundary

One commit, `docs(runtime): ...` style. Stage only the five docs files:
`08-lifecycle-dependency-map.md`, `08-tasks/02-lifecycle-dependency-map.md`,
`08-tasks/README.md`, `08-evidence.md`, `NOTES.md`. Do not stage tool
droppings (`.claude-flow/`, `.claude/`, `CLAUDE.md`, `.mcp.json`, `.swarm/`,
`ruvector.db`). No `Co-Authored-By` trailer.

## Success Criteria

- `08-lifecycle-dependency-map.md` exists, mirrors the Epic 7 map's shape, and
  covers all 16 steady-path sites plus the named compat sites, each pinned to
  a current `file:line`.
- Every `(spike)` target-lane cell has an open question phrased for a yes/no or
  concrete-protocol-choice answer by Task 3.
- The generation contract (`park_seq`, entry `seq`, deferred removals) is
  documented as part of the waiter-store ownership contract.
- `08-tasks/README.md`, `08-evidence.md`, and `NOTES.md` are updated and the
  task index status is Complete.
