# Epic 8 Task 4: Lifecycle Behavior Contract Tests

Task 4 output. Adds focused behavior/liveness tests for the task-lifecycle
surfaces Tasks 6-10 migrate, selected against the epic's Proof And Quality
Contract focused-probe list
(`08-task-lifecycle-lane-and-net-fairness.md`) and against the six written
rules and sixteen verdicts in
`08-lifecycle-lane-proving-spike.md`. This document is self-contained: every
claim is pinned to `file:line` re-verified against this tree.

Baseline commit for all anchors: `daeac51e` (Task 1 kickoff-baseline; the
tree after the Task 1 generation-qualified-removal fix, same tree the spike
was proven against). Re-verified line-by-line during Task 4 development;
current source matches the spike's citations exactly (no drift since Task 3).

## Scope

- New Go test files (build tag `runtime_v2_pending`, matching the existing
  `runtime_v2_lock_split_*_test.go` / `runtime_v2_scheduler_placement_test.go`
  / `runtime_v2_task_scope_blocking_waiter_contract_test.go` precedent):
  - `internal/vm/runtime_v2_lifecycle_behavior_harness_test.go`
  - `internal/vm/runtime_v2_lifecycle_behavior_create_join_test.go`
  - `internal/vm/runtime_v2_lifecycle_behavior_handle_lifetime_test.go`
  - `internal/vm/runtime_v2_lifecycle_behavior_scope_test.go`
  - `internal/vm/runtime_v2_lifecycle_behavior_await_shutdown_test.go`
- No C or H file in the repository is touched. The harness is a native C
  program compiled at test time from Go string constants (the established
  pattern), linked against `runtime/native/*.c` (excluding `rt_entry.c`), so
  it exercises the exact shipping runtime code, not a model of it.
- Out of scope: static-shape/trace-gate tests and C trace counter code (Task
  5, already landed per `08-evidence.md`/`NOTES.md`); any lifecycle
  migration (Tasks 6-10); net-fairness investigation (Task 11).

## Harness Pattern

Mirrors `buildRuntimeV2LockSplitHarness`
(`internal/vm/runtime_v2_lock_split_harness_test.go:16-63`) and
`buildRuntimeV2SchedulerPlacementHarness`
(`internal/vm/runtime_v2_scheduler_placement_test.go:116-163`): a Go string
constant is written to a temp `.c` file and compiled with
`clang -std=c11 -Wall -Wextra -Werror -pthread -I runtime/native` against
every `runtime/native/*.c` file except `rt_entry.c`. `buildRuntimeV2LifecycleHarnessWithFlags`
(`runtime_v2_lifecycle_behavior_harness_test.go`) adds a second build variant
with `-fsanitize=thread -g -O1` for the one TSan-gated test, mirroring the
Task 3 spike's own proof methodology (`clang -O1 -g -fsanitize=thread`) but
against the real tree instead of the scratchpad model.

The harness source is split across five Go string constants
(`lifecycleHarnessCommon`, `lifecycleHarnessCreateJoinModes`,
`lifecycleHarnessHandleLifetimeModes`, `lifecycleHarnessScopeAndShutdown`,
`lifecycleHarnessMain`), one per owning Go file, concatenated into one
translation unit at build time — purely to keep every Go source file at or
under the project's 500-line cap (Global Rule 4); this has no effect on the
compiled program. Each mode is selected by `argv[1]`; each mode function
spawns pinned tasks via `spawn_pinned`/`spawn_pinned_with_state` (mirroring
the existing `spawn_pinned`/`pin_shard` helpers), drives the real public C
API (`rt_task_poll`, `rt_task_clone`, `rt_task_cancel`, `rt_task_await`,
`rt_scope_enter`/`register_child`/`join_all`/`exit`, `rt_channel_*`,
`rt_blocking_submit`, `rt_sleep`), and asserts via bounded wait loops
(`wait_task_status`, `wait_ptr`, `wait_u32_at_least`, `await_expect`) so a
lost wakeup fails fast (explicit attempt caps, never an unbounded loop).

## Scenario -> Probe-List Mapping

The epic's Proof And Quality Contract (`08-task-lifecycle-lane-and-net-fairness.md`)
lists nine focused-probe requirements for lifecycle tasks. Coverage:

| Probe-list item | Test(s) | Notes |
| --- | --- | --- |
| Owner-local task creation and ready publication | `TestRuntimeV2LifecycleOwnerLocalCreateAndReadyPublication` | Verifies via `rt_debug_current_worker_shard_id()` (`rt_async_state.c:352`, the same proof mechanism `TestRuntimeV2SchedulerPlacementNoStealWorkerPath` uses) that a task pinned to shard N by `__task_create`'s `rt_task_assign_spawn_owner`+`ready_push` (`rt_async_task.c:40-41`) actually runs there, at `SURGE_SHARDS=1,2,8`. |
| Join polling/result observation across `SURGE_SHARDS=1,2,8` | `TestRuntimeV2LifecycleJoinPollResultObservation` | Same-shard and cross-shard joins via `rt_task_poll` (`rt_async_task.c:79-149`), matrix run. |
| Join waiter cleanup: target completes before/during/after registration | `TestRuntimeV2LifecycleJoinWaiterCleanupRegisterThenVerify` | Three subtests: `pre-done` (immediate-DONE short-circuit, `:116-124`), `race-stress` (500 iterations stressing the register-then-verify window, `:127-145`), `post-park` (deterministic gated target, proving the normal park-then-wake path). |
| Handle clone/release and last-reference free | `TestRuntimeV2LifecycleCloneReleaseLastReferenceFree`, `TestRuntimeV2LifecycleCompletionPinInterleavingTSan` (`t.Skip`'d) | See "Finding" below — the TSan test surfaces two real pre-existing races, recorded as `RV2-DEBT-019`, owned by Task 8. |
| Scope enter/register/join/exit, incl. failfast cancellation | `TestRuntimeV2LifecycleScopeEnterRegisterJoinExit`, `TestRuntimeV2LifecycleScopeFailfastCancellation`, `TestRuntimeV2LifecycleScopeCancelledPollTeardown` | Failfast has both a dedicated raw-C probe and a selected existing test — see "Failfast Scoping Decision" below. |
| Worker-side await vs external `rt_task_await` | `TestRuntimeV2LifecycleWorkerAwaitVsExternalAwait` | External await (main thread, `done_cv` path) and worker-side join (`rt_task_poll`) run concurrently and both observe correct results. |
| Shutdown with tasks parked in join/scope/timer/channel/blocking/net waits | `TestRuntimeV2LifecycleShutdownWithParkedTasks` | Covers join, scope join-all, timer/sleep, channel recv, blocking completion. Net is deliberately **not** duplicated — see "Net And No-Parked-With-Work Scoping" below. |
| No parked-with-work after lifecycle ops | (see below; descoped from the shutdown test) | |
| No shard-0 fallback for owner-known task lifecycle paths | Covered incidentally by every cross-shard test above (each explicitly pins non-zero shards and asserts residence there) | No dedicated test; the create/join tests already assert non-shard-0 placement holds. |

## Failfast Scoping Decision

Scope failfast cancellation (one child's cancellation propagating to
siblings via `rt_scope_register_child`'s failfast branch,
`rt_async_scope.c:67-75`, and `mark_done`'s failfast branch,
`rt_async_state.c:1550-1558`) has two independent probes:

- `TestRuntimeV2FailfastScopeCancellationWakesOwner`
  (`internal/vm/runtime_v2_task_scope_blocking_waiter_contract_test.go:79-136`)
  already behaviorally proves this contract through compiled Surge/LLVM
  codegen, and the Task 3 spike's own corroboration table
  (`08-lifecycle-lane-proving-spike.md`, "Corroboration" section) selects it
  as a passing focused probe at this baseline. The epic rules explicitly
  allow a task to "add or select" focused probes
  (`08-task-lifecycle-lane-and-net-fairness.md` Proof And Quality Contract;
  `08-tasks/README.md` "Rules").
- `TestRuntimeV2LifecycleScopeFailfastCancellation` (added after the main
  session asked for a dedicated raw-C-level probe as well, since failfast is
  an explicitly required item in the epic's contract) drives the same
  `rt_scope_register_child` failfast branch directly at the C level, with an
  explicit registration order the harness controls: a slow sibling is
  registered first (landing in `scope->children[]`), a second child is
  cancelled and drained, then registered while already
  `TASK_DONE`+`TASK_RESULT_CANCELLED` — triggering `failfast_triggered`,
  cancelling the sibling, and observable via `rt_scope_join_all`'s
  `failfast` out param (`rt_async_scope.c:107-108`). This complements rather
  than duplicates the Surge-source test: that one proves the contract
  through codegen's natural registration order, this one proves the same C
  entry points with an order the test controls precisely.

## Net And No-Parked-With-Work Scoping

- Net-parked shutdown is not duplicated in the new harness: `LIVENESS_PROBES.md`
  already names `TestRuntimeV2NetPollerShutdownWakesEveryShard` (wired into
  `make runtime-v2-accept-check`, `Makefile:124`) as the focused probe for
  shutdown waking every shard's net waiters. Building a real listening socket
  into this synthetic harness would add net-fd plumbing outside this task's
  scope for no additional lifecycle coverage.
- The "no parked-with-work after lifecycle ops" probe is **not** proven by an
  external call to `rt_debug_assert_no_parked_with_work`
  (`rt_scheduler_placement.c:126-135`) from harness driver code against
  actively-running workers. That helper only checks whether a shard's ready
  queue is non-empty — it does not itself check whether any worker is
  asleep — and its only real callers are (a) the worker's own sleep decision
  (`rt_worker_turn.c:154`, right before `pthread_cond_wait`) and (b) the
  existing `TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant`
  (`runtime_v2_scheduler_placement_test.go:97-114`), which triggers it
  synchronously in a single-threaded harness invocation with no concurrently
  running workers. Calling it externally while genuinely busy tasks are
  cycling ready<->running (as an early draft of this task's shutdown test
  did, using a busy-yielding "spin forever" task to keep the joiner parked)
  panics essentially unconditionally, since a continuously re-enqueuing task
  keeps the ready queue non-empty by construction — this is not a race, it is
  the helper behaving exactly as designed for a state that is not actually a
  parked-with-work violation. The shutdown test instead uses a genuinely
  parking "never completes" primitive (`poll_park_forever`, recv on a
  never-sent-to channel) for anything that must stay alive-but-idle, and
  relies on the existing `TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant`
  / `TestRuntimeV2SchedulerPlacementParkedWithWorkSourceGate`
  (`runtime_v2_scheduler_placement_source_test.go:63-77`) pair as the
  selected focused probe for this invariant, per the same "select, don't
  re-derive" principle as the failfast decision above.

## Discovered Runtime Behavior: Virtual-Clock Idle Fast-Forward

While developing the shutdown test, a long `rt_sleep` (e.g. one hour) was
found to fire within roughly 200ms of real time once no other task was ready.
`tick_virtual`/`advance_time_to_next_timer` (`rt_async_state.c:1199-1257`)
fast-forwards the virtual clock straight to the next timer deadline whenever
workers go idle, rather than literally blocking wall-clock time. This means a
long sleep is not a stable "stays parked indefinitely" primitive in this
runtime whenever the system can otherwise go idle. The timer-parked leg of
`TestRuntimeV2LifecycleShutdownWithParkedTasks` accepts either
`TASK_WAITING` or `TASK_DONE` as evidence of correct scheduling (only getting
stuck at `TASK_READY` forever would indicate a real lost-wakeup bug); the
"genuinely never completes" primitive used elsewhere in the same test
(`poll_park_forever`) uses a channel recv specifically because channels have
no such auto-fire mechanism. This is recorded here because it is directly
relevant to any future timer/timeout/shutdown liveness work
(`LIVENESS_PROBES.md`'s "Missing" "Timer, timeout, and shutdown liveness"
row) and was not previously written down anywhere this task's research
found.

## Finding: Two Pre-Existing Data Races Under TSan (RV2-DEBT-019)

`TestRuntimeV2LifecycleCompletionPinInterleavingTSan` builds the harness with
ThreadSanitizer and runs 300 concurrent (target, joiner) pairs where the
target completes with no yield and the joiner busy-registers via
`rt_task_poll` — the exact interleaving rule 1 names (a joiner consuming the
last handle while `mark_done` is mid-body). Developing this test surfaced
**two** distinct real races in the current baseline, confirmed reproducible
via both a standalone harness run and the actual `go test` invocation:

1. **Result-visibility ordering (already documented).** `mark_done` writes
   `result_kind`/`result_bits` (`rt_async_state.c:1544-1545` in the current
   tree, matching the spike's `:1542-1543` citation almost exactly) *after*
   the `TASK_DONE` release store (`:1540`), and only takes the control lock
   when `mark_done_needs_control` (`:1486-1506`) says so. For a plain task
   with no scope and no residual waiters — exactly a freshly spawned target
   with nothing else registered — `mark_done_needs_control` returns false,
   so the result write and the `TASK_DONE` store both happen with no lock
   held. A concurrent `rt_task_poll` read of `result_bits` after observing
   `TASK_DONE`, even though `rt_task_poll` itself holds the control lock
   (`rt_async_task.c:88`), is **not** protected by that lock, because the
   writer never touched it for this completion. This is exactly Rule 1's
   documented "Required change" for Task 8 ("mark_done currently writes the
   result... after the TASK_DONE release store... sound today only because
   readers hold the control lock... Task 8 MUST reorder"). This task's test
   is the first to confirm it dynamically (TSan, not just written argument)
   against the real tree. **Mitigation applied:** the test keeps one external
   awaiter alive for the whole stress window (`keepalive_awaiter_thread` in
   `runtime_v2_lifecycle_behavior_handle_lifetime_test.go`), which holds
   `ex->done_waiters > 0` and therefore forces `mark_done_needs_control` true
   for every completion in-flight, matching the precondition Rule 1's
   "sound today" claim actually depends on. This mitigation eliminated this
   specific TSan report across repeated runs.

2. **`park_key` race (not found documented anywhere in this epic's records).**
   With the above mitigation applied, TSan still reports a second, different
   race: `wake_task_on_shard_locked` (`rt_async_state.c:965`,
   `task->park_key = waker_none();`) writes the target's `park_key` under
   the owner shard lock, concurrently with `mark_done_needs_control`
   (`:1494`, `waker_valid(task->park_key)`) reading that same field with
   **no lock at all** — structurally unavoidable, since that read is what
   decides whether `mark_done` acquires any lock in the first place; there
   is no way to lock the decision of whether to lock. The write comes from
   `rt_task_poll`'s own "helper wake" (`rt_async_task.c:113-114`: `if
   (task_status_load(target) != TASK_WAITING && != TASK_DONE) { wake_task(ex,
   target->id, 1); }`), reachable when a joiner polls a target that is still
   transitioning (neither fully `TASK_WAITING` nor `TASK_DONE` yet) at the
   exact moment that target is completing on another shard/thread. This
   reproduced consistently (2 TSan warnings, same root cause, across every
   run) once the first race was eliminated. **This is unresolved.** It does
   not have an existing debt entry as far as this task's research found, and
   it sits squarely in the surfaces Task 7 (join poll and handle lifetime)
   and Task 8 (completion epilogue) are about to migrate, so it is directly
   relevant to their correctness, not an unrelated tangent.

**Resolution (main session decision):** `TestRuntimeV2LifecycleCompletionPinInterleavingTSan`
is committed with its full body (both races remain exercisable, not
silently weakened), gated by `t.Skip("pending Task 8: baseline races
RV2-DEBT-019 -- (1) mark_done result writes must move before the TASK_DONE
release store; (2) mark_done_needs_control's unlocked park_key read vs
wake_task_on_shard_locked write. Task 8's peel commit deletes this skip and
adds the test to runtime-v2-lifecycle-check.")`, matching the epic's
established pending-gate convention (Task 5's P6-P10 static gates use the
same `t.Skip`-with-activation-criteria pattern). The result-visibility
mitigation for race (1) is now toggleable via
`LIFECYCLE_PIN_STRESS_NO_KEEPALIVE=1` (unset by default, which isolates
race (2) alone; set it to reproduce both races together, i.e. this test's
state before the mitigation). The finding itself is recorded as
`RV2-DEBT-019` in `DEBT.md`, owned by Task 8 (completion epilogue), noting
the interaction with Task 7's helper-wake call site, with an explicit close
condition: reorder `mark_done`'s result writes, make the `park_key`
read/write pair race-free, delete the `t.Skip`, and add the test's name to
`runtime-v2-lifecycle-check`. Confirmed the full `^TestRuntimeV2Lifecycle`
regex (this task's 10 tests plus Task 5's static/trace tests, exactly what
`make runtime-v2-lifecycle-check` runs) is green with one `SKIP` and zero
`FAIL`.

## Files/Line Counts

All five new files are at or under the project's 500-line cap (Global Rule
4): `runtime_v2_lifecycle_behavior_harness_test.go` (478),
`runtime_v2_lifecycle_behavior_create_join_test.go` (213),
`runtime_v2_lifecycle_behavior_handle_lifetime_test.go` (185),
`runtime_v2_lifecycle_behavior_scope_test.go` (353),
`runtime_v2_lifecycle_behavior_await_shutdown_test.go` (374).

## Commands Run (recorded here; full matrix in `08-evidence.md`)

- Standalone harness compile: `clang -std=c11 -Wall -Wextra -Werror -pthread
  -I runtime/native -o lifecycle_harness lifecycle_harness.c
  runtime/native/*.c` (excl. `rt_entry.c`) — clean, zero warnings.
- TSan harness compile: same plus `-fsanitize=thread -g -O1` — clean, zero
  warnings.
- `go vet -tags runtime_v2_pending ./internal/vm/...` — clean.
- `gofmt -l` on all five new files — clean (no output).
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Lifecycle' -count=1
  -parallel=1 -p=1 -v --timeout 360s` — this task's 10 tests **all PASS
  except `CompletionPinInterleavingTSan`, which SKIPs as designed**; Task
  5's static/trace tests in the same regex PASS/SKIP as expected; zero
  FAIL, ~28s total (this is exactly the command
  `make runtime-v2-lifecycle-check` runs).
- Same command for `CompletionPinInterleavingTSan` alone with the `t.Skip`
  temporarily removed, `--timeout 180s` — reproduces **FAIL**, 92.9s, two
  TSan reports (see "Finding" above); confirms the recorded debt is real and
  the `t.Skip` is currently load-bearing.
- Focused SURGE_SHARDS matrix (1, 2, 8) for the create/join tests: PASS at
  all three shard counts, both via the standalone harness and via `go test`.
