# Epic 8 Task 3: Lifecycle Lane Proving Spike

**Kind:** proving spike (RULES.md Global Rule 1). **Depends on:** Tasks 1, 2.

**Goal:** decide the shard-owned task-lifecycle model by answering all 16 open
questions the Task 2 map left as `(spike)` cells (S5-Q1..Q14, S6-Q1, S7-Q1,
S9-Q7), each with evidence, and produce the written rules the implementation
tasks (6-10) build against: task lifetime (lookup, result visibility, handle
release, final free), join result visibility, the scope owner-lane model with a
named cross-owner control fallback, cancellation boundedness, the external-await
compatibility boundary, and the join/scope generation-qualification decision.

## Runtime State This Task Depends On

Re-stated here (index rule: each task stands on its own) and re-verified against
baseline `daeac51e`:

- Epic 7 split the executor lock into per-shard lanes plus a reduced control
  lane; lane order is `control -> at most one shard lock`, enforced always-on by
  `rt_lane.c:43-86`. The task table is an atomic-snapshot structure
  (`rt_async_internal.h:246-249`; `get_task` at `rt_async_state.c:356-365`;
  `ensure_task_cap` copy-on-grow at `:443-490`).
- The 16 steady-path control sites (Task 1 census): `rt_async_task.c`
  `__task_create:15`, `rt_task_wake:62`, `rt_task_poll:88`,
  `poll_ready_child_inline:167,173`, `rt_task_cancel:229`, `rt_task_clone:243`,
  `checkpoint:289`, `rt_sleep:300`; `rt_async_state.c`
  `task_release_lane_aware:1429`, `mark_done:1508` (gate
  `mark_done_needs_control:1486`), `apply_poll_outcome` cancelled branch
  `:1586`; `rt_async_scope.c` `rt_scope_enter:10`,
  `rt_scope_register_child:45`, `rt_scope_cancel_all:84`,
  `rt_scope_join_all:100`, `rt_scope_exit:134`.
- Join waiters already route to the target task owner shard
  (`rt_waiter_route.c:20-24`); `scope_key` waiters route to `ex->control_waiters`
  (`:25-26,55`, Epic 7 D8). The completion pin lives in `mark_done`
  (`task_add_ref:1515`, `task_release_lane_aware:1574`).
- The one named cross-owner lifecycle edge is the accept transition
  (`rt_task_replace_owner`, `rt_scheduler_placement.c:80-96`), which migrates
  join waiters (`rt_waiter_migrate_join_waiters`).
- The waiter generation contract from the Task 1 fix: `park_seq`
  (`rt_async_internal.h:212`), entry `seq`, and generation-qualified removal
  (`rt_async_waiter.c:445-463`, predicate `:159`); channels re-arm at
  `rt_channel_lane.h:197-219` and validate at `:87-91`.

## Scope

- Produce `docs/runtime-v2-epics/08-lifecycle-lane-proving-spike.md`: the spike
  record (mirroring `07-locking-model-proving-spike.md`) — the RULES.md Global
  Rule 1 preamble, the proof run, a per-question decision for all 16 questions,
  and the six written rules.
- Reconcile `docs/runtime-v2-epics/08-lifecycle-dependency-map.md`: rewrite the
  `(spike)` target-lane cells and the Target-Lane Summary to the decided lanes
  (index rule: spike output rewrites the lane table on conflict).
- Update `08-tasks/README.md` (Task 3 → Complete), `08-evidence.md` (Task 3
  section), and `NOTES.md`.

## Proof Mechanism

- **Throwaway C model** (scratchpad `lifecycle_publish_refcount_spike.c`, not
  committed): a TSan + `-O2` model of (1) the shard-owned publish + ready-push
  ordering (S5-Q1) and (2) the atomic-refcount clone/release lifetime with the
  completion-pin interleaving (S5-Q6, S5-Q12, rule 1), plus two deterministic
  negative controls proving the assertions are not vacuous.
- **Corroboration:** the existing waiter-contract tests
  (`runtime_v2_task_scope_blocking_waiter_contract_test.go`) run read-only at
  baseline for the S5-Q3 / S5-Q8 contracts.
- **Audit:** an exhaustive grep of every `rt_task.owner_shard_id` writer for
  S7-Q1.
- **Written arguments** pinned to shipping `file:line` for the remaining
  questions.

## Out Of Scope

- No committed C, test, benchmark-script, or CI changes (docs-only commit; the
  throwaway model stays in scratchpad and the tree's C state stays pristine).
- No implementation: the spike decides the model; Tasks 4-10 implement it.
- Select slow lane migration (named non-goal).
- Phase 4 surfaces (`far`, `submit_to`, inbound queues, remote select, eventfd
  credits, seq-cst `PARKED`). If any answer had required one, the spike stops
  for a re-scope; none did.

## Checks

Final commit is docs-only, so the required record is: `git diff --check` clean
and `git status` showing the tree's C state pristine (the model exists only in
scratchpad). The experiment commands are recorded in `08-evidence.md`:
`clang -O1 -g -fsanitize=thread` and `clang -O2 -DNDEBUG` builds/runs, the two
negative-control builds, and the `go test -tags runtime_v2_pending
SURGE_BACKEND=llvm` corroboration run.

## Evidence Plan

- `08-evidence.md` gains a `## Task 3` section: files touched, the docs-only
  check results, the experiment commands and outcomes (safe PASS both builds
  x2, negative controls fail, corroboration tests PASS, owner-shard-id audit),
  and the decision summary.
- `NOTES.md` gains an Epic 8 Task 3 handoff: the 16 verdicts, the six rules, the
  S5-Q1 escalation criterion, the Epic 7 D8 revision, and the `mark_done`
  result-order required change for Task 8.

## Commit Boundary

One commit, `docs(runtime): record epic 8 lifecycle lane proving spike`. Stage
only the six docs files: `08-lifecycle-lane-proving-spike.md`,
`08-tasks/03-lifecycle-lane-proving-spike.md`, `08-lifecycle-dependency-map.md`,
`08-tasks/README.md`, `08-evidence.md`, `NOTES.md`. Do not stage tool droppings
(`.claude-flow/`, `.claude/`, `CLAUDE.md`, `.mcp.json`, `.swarm/`, `ruvector.db`)
or the scratchpad probe. No `Co-Authored-By` trailer.

## Success Criteria

- All 16 open questions have a recorded verdict with evidence.
- The six written rules are recorded; rule 1 states what pins the task struct
  while a completing worker is inside `mark_done` and a joiner concurrently
  consumes the last handle.
- The safe model passes both builds with zero lost publishes and zero UAF; the
  negative controls fail; the corroboration tests pass.
- `08-lifecycle-dependency-map.md` is reconciled (no `(spike)` cell conflicts a
  decision).
- `08-tasks/README.md` (Task 3 Complete), `08-evidence.md`, and `NOTES.md` are
  updated.
