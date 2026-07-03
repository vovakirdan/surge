# Task 1: Kickoff Baseline And Sentrux

**Status:** Complete
**Kind:** evidence
**Depends on:** none

## Context

Epic 5 closed at commit `bc0a76d7` (per `docs/runtime-v2-epics/README.md`).
Epic 6 has not started implementation; only the epic document
`docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md` exists,
drafted and reviewed. There is no `docs/runtime-v2-epics/06-evidence.md` yet —
this task creates it, following the shape of `04-evidence.md` and
`05-evidence.md`.

Current structural facts this task must record as the frozen "before" state
(do not re-derive these from scratch in later tasks — cite this task's
evidence instead):

- `RT_RUNTIME_SHARD_COUNT` is `1U` (`runtime/native/rt_async_internal.h:127`).
- `struct rt_shard` (`rt_async_internal.h:150-160`) already carries
  `runtime`, `executor`, `scheduler`, `heap_accounting`, `net_poll_scratch`,
  `fd_registry`, `channel_blocking_compat`, `waiter_store`, and `shard_id` as
  direct members — Epic 4/5 already made these fields shard-shaped. Only one
  shard (`shards[0]`) is ever populated today.
- `struct rt_runtime` (`rt_async_internal.h:162-165`) is
  `{ size_t shard_count; rt_shard shards[RT_RUNTIME_SHARD_COUNT]; }` — a fixed
  one-element array behind a runtime `shard_count` field that is always `1`.
- `struct rt_executor` (`rt_async_internal.h:216-247`) remains one global
  struct: one `pthread_mutex_t lock`, one `tasks[]`/`scopes[]` array, one
  `workers` pointer, one `net_polling` flag, one blocking-pool. `rt_task`
  (`rt_async_internal.h:167-202`) has no shard/owner field at all.
- `rt_runtime.c:50-55` (`rt_runtime_shard0`) is the one compatibility accessor
  every other accessor in that file routes through:
  `rt_executor_scheduler` (`rt_runtime.c:79-83`),
  `rt_executor_net_poll_scratch` (`:96-100`),
  `rt_executor_channel_blocking_compat` (`:110-114`),
  `rt_executor_waiter_store` (`:131-135`),
  `rt_executor_fd_registry` (`:152-156`) all resolve to `shards[0]`.
  `rt_shard_scheduler_init` (`:165-187`) already takes an explicit
  `worker_count` and is reusable for a second/third shard once one exists.
- `runtime/native/rt_net.c:45-53` defines `NetListener{int fd; bool closed;}`
  and `NetConn{int fd; bool closed;}` — no owner-shard tag on either.
  `rt_net_listen` (`:413`) sets `SO_REUSEADDR` (`:435`), not `SO_REUSEPORT`.
  The wake pipe (`:67-68`, `net_poll_wake_read_fd`/`net_poll_wake_write_fd`) is
  a process-global static, separate from the per-shard `net_poll_scratch`
  buffers.
- `runtime/native/rt_fd_registry.h/.c` is fully shard-scoped in storage (each
  `rt_shard` owns one `rt_fd_registry` by value) but every public entry point
  takes either a raw `rt_fd_registry*` the caller already resolved, or an
  `rt_executor*` that internally resolves through the shard-0 accessor above.
  There is no fd-to-owning-shard lookup anywhere.
- `runtime/native/rt_async_state.c:109-110` (`rt_env_worker_count`) reads
  `SURGE_THREADS` and is used once, in `exec_init_once` (`:201`), to size the
  single executor's worker pool.
- Two static gates pin `N=1` today and will need updating at Task 6:
  `internal/vm/runtime_v2_skeleton_static_test.go:22-26,34` and
  `internal/vm/runtime_v2_fd_registry_static_test.go:390-394`.
- `.loc-legacy-allowlist` current ceilings relevant to this epic:
  `rt_async_state.c 1727`, `rt_net.c 904`, `rt_async_task.c 768` (see
  `RV2-DEBT-003`, `RV2-DEBT-004` in `DEBT.md`).
- Sentrux MCP tools were not connected in the prior epics' sessions; the CLI
  fallback (`sentrux check <path>`, `sentrux gate --save`, `sentrux gate`) was
  used and documented in `NOTES.md`. Check whether MCP `mcp__sentrux.*` tools
  are available in this session before falling back to the CLI.

## Goal

Record the exact Epic 6 starting state — commit, line counts, Sentrux
baseline, accepted debt, current Runtime V2 gate status — before any
dependency mapping or spike work begins, and turn the epic's brief task list
into concrete, checkable gates for Tasks 2-5.

## Why This Task Exists

Every prior epic (1-5) started with a kickoff/baseline task for the same
reason: scheduler and ownership mistakes are invisible after the fact
(`RULES.md` preamble), so the "before" picture must be nailed down while it is
still trivially reproducible. Epic 6 additionally needs an explicit gate plan
because it is the first epic with a mandatory proving spike (Task 3) — the
spike's success/failure criteria must be pre-agreed, not improvised mid-task.

## Scope

- Capture branch, commit (`git rev-parse HEAD`), and `git status --short`.
- Capture line counts for every file listed in the epic's `Inputs` section
  under `runtime/native/` plus `internal/vm/runtime_v2_*_test.go` and
  `internal/vm/mt_*_test.go` files this epic is likely to touch.
- Confirm the structural facts in Context above still hold (they may have
  drifted since this document was written); if any line number or struct
  shape has changed, record the corrected evidence here, not silently.
- Record accepted backend-test debt from `DEBT.md` relevant to Epic 6:
  `RV2-DEBT-001`, `002`, `003`, `004`, `005`, `006`, `007`, `010`, `011`,
  `012`.
- Run root, `runtime/`, and `runtime/native/` Sentrux scans (MCP if available,
  otherwise the CLI fallback) and record `quality_signal` for each as the
  Epic 6 baseline.
- Run `make runtime-v2-check` once as a clean baseline and record pass/fail
  with exact output, so later tasks can tell a pre-existing flake (for
  example the recorded Epic 3-era MT seed flake) from a new Epic 6
  regression.
- Turn the epic's Brief Task List purposes into concrete "what does Task N's
  Definition of Done get checked against" gates for Tasks 2 through 5
  specifically (later tasks already have their own detailed documents once
  this task is done).
- Create `docs/runtime-v2-epics/06-evidence.md` using
  `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`, seeded with the Task Identity
  And Scope and Baseline Commit/Status sections for Task 1 itself.

## Out Of Scope

- No runtime C changes.
- No dependency mapping detail (that is Task 2's job) — this task only names
  which files/paths the map must cover.
- No test writing (Tasks 4/5) and no spike work (Task 3).

## Approach / Steps

1. `git rev-parse HEAD`, `git status --short`, `git log --oneline -10`.
2. `wc -l` on every file named in the Context section above plus any other
   file already over the Rule 4 500-line limit that this epic's Scope
   mentions touching.
3. Re-run the exact `grep`/`rg` checks used to produce the Context section
   (`RT_RUNTIME_SHARD_COUNT`, `rt_runtime_shard0`, wake pipe statics, static
   test pins) and diff the result against what is written above; record any
   drift.
4. Run Sentrux root/`runtime/`/`runtime/native/` scans; record
   `quality_signal`, health bottleneck, and `check_rules` result for each,
   following `SENTRUX_POLICY.md`'s required call order (`scan` → `health` →
   `check_rules` per path, `session_start` only after the final scoped scan
   that will anchor the epic's before/after delta).
5. Run `make runtime-v2-check` and `make check`; record exact pass/fail and
   any flake class already known from Epic 3/4/5 evidence.
6. Write `docs/runtime-v2-epics/06-evidence.md`.
7. Update `docs/runtime-v2-epics/NOTES.md` with the current context and the
   intended proof for Task 2.

## Files

Read only:

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/05-evidence.md` (for the immediately preceding
  baseline format)
- `runtime/native/rt_async_internal.h`, `rt_runtime.c`, `rt_net.c`,
  `rt_fd_registry.h`, `rt_fd_registry.c`, `rt_async_state.c`
- `internal/vm/runtime_v2_skeleton_static_test.go`,
  `internal/vm/runtime_v2_fd_registry_static_test.go`
- `.loc-legacy-allowlist`

Create or update:

- `docs/runtime-v2-epics/06-evidence.md` (new)
- `docs/runtime-v2-epics/NOTES.md`

## Skills & Working Practice

- This is an evidence/documentation task; a single agent (main or one
  subagent) can do it directly without the two-phase plan gate that
  implementation tasks require, but if a subagent is used it should still
  state its file list and commands before running them (Global Rule 9 spirit).
- If `mcp__sentrux.*` tools are not present in this session's tool list,
  fall back to the `sentrux` CLI exactly as recorded in prior epics' `NOTES.md`
  and say so explicitly in the evidence — do not silently skip Sentrux.
- Do not attempt the dependency map (Task 2) or spike (Task 3) here even if
  the investigation naturally surfaces details relevant to them; instead note
  them as pointers for Task 2/3 in `NOTES.md`.

## Checks

- `git diff --check`
- Sentrux repository scan (`scan` → `health` → `check_rules`)
- Sentrux `runtime/` scan
- Sentrux `runtime/native/` scan
- `make runtime-v2-check`
- `make check`

## Definition Of Done

- [ ] Commit, branch, and `git status --short` are recorded.
- [ ] Line counts are recorded for every Context-listed file plus any new
      file the epic Scope implies touching.
- [ ] Every structural fact in this document's Context section is confirmed
      current, or corrected with new evidence.
- [ ] Accepted Epic 6-relevant debt items are listed with their current
      `DEBT.md` status.
- [ ] Sentrux `quality_signal` and `check_rules` are recorded for root,
      `runtime/`, and `runtime/native/`, with the exact scan path named for
      each result.
- [ ] `make runtime-v2-check` and `make check` results are recorded with
      exact pass/fail and any known flake class named.
- [ ] `docs/runtime-v2-epics/06-evidence.md` exists and is seeded correctly.
- [ ] `NOTES.md` has enough context that Task 2 can start dependency mapping
      without rediscovery.

## Evidence To Record

Use `EVIDENCE_TEMPLATE.md`'s full shape in `06-evidence.md`: Task Identity And
Scope, Baseline Commit/Status, Files Touched (docs only), Sentrux
Root/Scoped Signals, Commands/Checks, Follow-Ups And Blockers, Notes
Consolidation Checklist.
