# Task 2: Accept Ownership Dependency Map

**Status:** Draft
**Kind:** design map
**Depends on:** Task 1

## Context

Task 1 freezes the exact starting line counts, struct shapes, and Sentrux
baseline. This task uses that frozen state to map every path that must change
or must explicitly stay global for `N>1` accept ownership to work, before any
code or spike changes it.

The epic document's Starting State and Epic 6 Boundary Decisions sections
already establish four load-bearing facts this map must work within, not
rediscover:

- `rt_executor.lock` stays global in this epic; only ownership of specific
  state (fd registry rows, waiter rows for net keys, net poll scratch, accept
  distribution) moves to per-shard structures under that same lock.
- Only net accept/readiness ownership moves. Channels, task join, scope wake,
  cancellation, blocking completions, timers, `now_ms`, sleep scans, and
  generic ready work keep routing through the existing shard-0 compatibility
  accessors in `rt_runtime.c` (`rt_executor_waiter_store`,
  `rt_executor_channel_blocking_compat`, etc.) and must not be touched by this
  epic.
- `struct rt_shard` (`rt_async_internal.h:150-160`) already has a `waiter_store`
  field distinct from `rt_executor`'s single wait mechanism, but today it is
  only ever `shards[0].waiter_store` in practice because `rt_runtime_shard0`
  is the only resolver. The map must say, for every current net waiter key
  (accept/read/write), which shard's `waiter_store` and `fd_registry` it will
  live in once shards[1..N-1] exist, and confirm no non-net waiter kind is
  accidentally routed through a non-zero shard.
- `rt_task` has no owner-shard field. Any place that currently assumes "the
  task" implicitly means "the one shard" must be named explicitly, because
  Task 7 has to add placement metadata somewhere (task, connection object, or
  both) and this map is what tells Task 7 where.

## Goal

Map the current accept, connection-fd, scheduler, close, cancellation,
wake-fd, and shutdown paths and classify every dependency by exactly who will
own it after Epic 6: per-shard net path, remains-global compatibility path, or
later-epic (Phase 4+) work.

## Why This Task Exists

`RUNTIME_V2.md`'s Refactor Policy says a broad structural refactor is only
useful if it follows the ownership boundaries, and a cosmetic split before
those boundaries are clear adds diff noise without reducing risk. Epic 4's
own Task 2 (`04-tasks/02-fd-registry-dependency-map.md`) proved this pattern
works: it fed directly into Task 3 (contract tests) and Task 5 (skeleton).
Epic 6 needs the same discipline, but the map is materially harder here
because ownership crosses two axes at once (net-vs-global primitive, and
shard-0-vs-shard-k placement) instead of one.

## Scope

- Map listener creation → accept → connection-fd creation → wait
  registration → poll readiness → completion → close → cancellation →
  shutdown, end to end, citing exact current `file:line` for each step.
- For every symbol identified, classify it as:
  - **net-shard-owned**: must become per-shard-resolved instead of always
    routing through `rt_runtime_shard0` (e.g. `rt_executor_fd_registry`,
    `rt_executor_net_poll_scratch`, net-key entries in `waiter_store`).
  - **stays-global-compat**: continues routing through the existing shard-0
    accessor unchanged in this epic (e.g. channel/timer/join waiter kinds in
    the same `waiter_store` type, `now_ms`, `tasks[]`/`scopes[]`).
  - **later-epic**: Phase 4 cross-shard messaging, migration control plane, or
    Tier 2 pool work explicitly out of scope (cite the exact Not Included
    bullet in the epic document).
- Identify every current caller of `rt_runtime_shard0`,
  `rt_executor_fd_registry`, `rt_executor_net_poll_scratch`,
  `rt_fd_registry_complete_ready_net_waiters`,
  `rt_fd_registry_drain_shutdown_net_waiters_locked`,
  `rt_fd_registry_wake_closed_net_waiters`, and the wake-pipe functions in
  `rt_net.c` (`net_poll_wake_init`, the read/write helpers around
  `rt_net.c:93-129`). For each caller, record whether it must become
  shard-index-aware.
- Map how `SURGE_THREADS` (`rt_async_state.c:109-110`,
  used at `:201`) and the (not yet existing) `SURGE_SHARDS` variable will
  interact with `exec_init_once`, and name the exact function(s) that must
  change to implement the Epic 6 Boundary Decisions rule ("one Tier 1 worker
  per shard when `SURGE_SHARDS>1`; conflicting `SURGE_THREADS` is an explicit
  error").
- Map how a `NetListener`/`NetConn` (`rt_net.c:45-53`) instance currently
  flows from creation to the VM-visible `TcpListener`/`TcpConn` handle, so
  Task 8 knows exactly where to attach owner-shard metadata without breaking
  the public ABI (`RUNTIME_V2.md` Migration Plan Phase 0 rule: "keep the
  current public ABI stable").
- Map `SCHED_TRACE steal` (`rt_async_state.c:454` per `LIVENESS_PROBES.md`)
  and the worker-steal path in the scheduler so Task 7 knows exactly which
  branch must gain a same-shard-only check for connection tasks.
- Name the first safe implementation boundary — the same closing move Epic 4
  Task 2 made — i.e., which piece can land first (shard array/config) without
  requiring the others to exist yet.
- Explicitly record any dependency this map finds that neither Task 6-11 nor
  the epic's Not Included list currently covers, and flag it as a gap for the
  main agent to resolve before Task 6 starts (do not silently assume it is
  someone else's job).

## Out Of Scope

- No runtime code changes.
- No decision about the listener model itself (single-fd vs.
  `SO_REUSEPORT` group vs. fallback handoff) — that is Task 3's proving spike.
  This map only has to make Task 3's options legible, not choose between them.
- No test writing.

## Approach / Steps

1. Re-run and extend the `rg`/`grep` sweep from Task 1's Context section:
   every caller of the shard-0 accessor family in `rt_runtime.c`, every net
   waiter key touch point in `rt_async_state.c`/`rt_net.c`, every use of
   `rt_env_worker_count`.
2. Build the end-to-end path table described in Scope, one row per step, with
   exact `file:line` citations (do not paraphrase without citing).
3. Classify every row net-shard-owned / stays-global-compat / later-epic.
4. Cross-check every "stays-global-compat" row against the epic's explicit
   list (Epic 6 Boundary Decisions paragraph: "Channels, task join, scope
   wake, cancellation, blocking completions, timers, `now_ms`, sleep scans,
   and generic ready work"). If a row does not fit that list but you still
   want to mark it stays-global-compat, stop and flag it instead of silently
   classifying it.
5. Write the dependency-map document.
6. Update `06-evidence.md` and `NOTES.md`.

## Files

Read only:

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md`
- `docs/runtime-v2-epics/04-fd-registry-dependency-map.md` (prior-epic
  pattern to follow)
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `runtime/native/rt_async_internal.h`, `rt_runtime.c`, `rt_net.c`,
  `rt_fd_registry.h`, `rt_fd_registry.c`, `rt_async_state.c`,
  `rt_async_task.c`
- `runtime/native/rt_async_waiter.c` if present (waiter-store bridge)

Create:

- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md`
- Update `docs/runtime-v2-epics/06-evidence.md`, `NOTES.md`

## Skills & Working Practice

- Design-map tasks do not touch runtime code, so the plan-gate requirement is
  lighter than for implementation tasks, but a subagent doing this work should
  still state up front which symbols/paths it intends to grep and map before
  writing the document, per Global Rule 9's spirit.
- This task and Task 3 (listener-model spike) may be planned in parallel once
  Task 1 lands (per the epic's Parallelization Model paragraph), but Task 3's
  spike result can change which rows in this map are net-shard-owned vs.
  later-epic (specifically, how internal accept tasks are represented). Do
  not treat this map as final until Task 3 closes; reconcile any
  contradiction before Task 4/5 start.
- Use `rg -n` for every symbol lookup so citations stay exact; do not hand-copy
  line numbers from memory or from this task document without re-verifying,
  since the codebase may have drifted since this document was written.

## Checks

- `git diff --check`
- Targeted `rg -n` evidence for every mapped symbol (paste the exact commands
  used into the map document or `06-evidence.md`)

## Definition Of Done

- [ ] Every step of the accept → readiness → close → cancellation → shutdown
      path has an owner classification with exact `file:line` citations.
- [ ] Every current caller of the shard-0 accessor family is enumerated and
      classified.
- [ ] The `SURGE_THREADS`/`SURGE_SHARDS` interaction path is named precisely
      enough that Task 6 does not need to re-derive it.
- [ ] The `NetListener`/`NetConn` → public handle flow is mapped precisely
      enough that Task 8 can attach owner metadata without ABI breakage.
- [ ] Any dependency not covered by Tasks 6-11 or the epic's Not Included list
      is flagged explicitly, not silently absorbed into a classification.
- [ ] Deferred (later-epic) work is explicit and cites the matching Not
      Included bullet, not hidden inside an implementation task.
- [ ] Task 3, Task 6, and Task 7 can use this map as their contract source
      without re-deriving the symbol list from scratch.

## Evidence To Record

- The dependency-map document itself, committed under
  `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md`.
- In `06-evidence.md`: Task Identity And Scope, Files Touched (docs only),
  Commands/Checks (the `rg` sweep commands), Follow-Ups And Blockers (any
  flagged gap from step 4 above).
- In `NOTES.md`: what was mapped, what remains uncertain pending Task 3, and
  the first safe implementation boundary chosen.
