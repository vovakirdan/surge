# Epic 8 Task 7: Join Poll And Handle Lifetime

Task 7 output. Self-contained: restates the runtime state it depends on
(`file:line` evidence at baseline `05d95b60`, the tree after Task 6) and does
not assume the reader has the whole epic memorized. The authoritative lane
decisions live in `../08-lifecycle-lane-proving-spike.md` (rule 1, rule 2,
S5-Q2/Q3/Q4/Q6/Q12); this document quotes what it implements and points back
for the rest. The F2 net-fairness fix (Epic 8 Task 11 / `RV2-DEBT-015`) is
folded into this task by main-session decision (2026-07-04); its spec lives in
the Task 11 investigator's worktree
(`08-tasks/11-net-fairness-starvation-investigation.md`, "F2 Design Spec"
section) and is quoted here where this task implements it.

## Sequencing Hazard: The Result-Visibility Reorder (Enabling Change)

The spike's rule 1 requires `result_kind`/`result_bits` to be written **before**
the `TASK_DONE` release store, so a joiner's acquire-load of `TASK_DONE`
publishes the result fields without needing the control lock. At baseline
`05d95b60`, `mark_done` (`rt_async_state.c:1526-1594`) does the opposite:

```c
task_status_store(task, TASK_DONE);      // :1559, release
task_enqueued_store(task, 0);            // :1560
task->result_kind = result_kind;         // :1561 -- AFTER the release store
task->result_bits = result_bits;         // :1562
```

This is sound today only because every join-result reader holds the control
lock (mutex happens-before covers it regardless of the result-write's
position). Task 7 drops the control lock from the join-result read, which
makes this ordering unsound on its own: a joiner that acquire-loads
`TASK_DONE` and immediately reads `result_kind`/`result_bits` with no lock at
all could observe the DONE status before the result writes are visible
(RV2-DEBT-019, Task 4's TSan finding, "RACE 1").

**Enabling change (this task, not deferred to Task 8):** swap the two
statement pairs so the result writes execute first:

```c
task->result_kind = result_kind;
task->result_bits = result_bits;
task_status_store(task, TASK_DONE);
task_enqueued_store(task, 0);
```

**Why this is sufficient.** Both writes are plain (non-atomic) stores made by
the same thread, in program order, strictly before the `TASK_DONE` store
(`memory_order_release`, `rt_async_internal.h`'s `task_status_store`). Any
reader that performs an acquire-load of `task->status` and observes `DONE`
(`task_status_load`, `memory_order_acquire`) establishes a synchronizes-with
edge with that release store; combined with the writer's own sequenced-before
relationship (result writes precede the release store on the same thread),
this composes into a happens-before edge from the result writes to the
reader's subsequent (unsynchronized) reads of `result_kind`/`result_bits`.
That is the exact mechanism rule 1 names and the exact mechanism S5-Q12
("free rule lets join read results off control") depends on. Nothing else in
`mark_done`'s body reads `result_kind`/`result_bits` between the old and new
write positions, so the reorder is behavior-preserving for every other
caller.

**Scope discipline.** This closes RACE 1 of `RV2-DEBT-019` only. RACE 2 (the
unlocked `park_key` read in `mark_done_needs_control` racing
`wake_task_on_shard_locked`'s owner-shard-locked write) and un-skipping
`TestRuntimeV2LifecycleCompletionPinInterleavingTSan` remain Task 8's, per
the Task 6 handoff and the spike's Implementation Shape Preview. Task 7 does
not touch `mark_done_needs_control` or the pending P8 static gate
(`TestRuntimeV2LifecycleStaticCompletionResultVisibilityOrder`) beyond what
this reorder happens to already satisfy textually; P8's Skip stays in place
for Task 8 to delete alongside its own `mark_done_needs_control` reduction,
since P8's stated activation bar bundles both changes together.

## Scope

- `runtime/native/rt_async_state.c`: the 2-line `mark_done` result-write
  reorder above. Nothing else in this file changes.
- `runtime/native/rt_async_task.c`: `rt_task_poll`, `poll_ready_child_inline`,
  `rt_task_clone`, `rt_task_wake`; new static helper
  `rt_task_poll_adopt_placement` (F2).
- `runtime/native/rt_async_trace.c`, `rt_async_internal.h`: new
  `placement_adoptions` trace counter (F2 proof obligation).
- `internal/vm/runtime_v2_lifecycle_static_test.go`: delete P7's `t.Skip`;
  update G6 (`TestRuntimeV2LifecycleStaticCensusSitesTagged`)'s table (see
  "Review-Visible Changes To Task 5's Shipped Gates" below).
- `internal/vm/runtime_v2_lifecycle_trace_test.go`: update
  `TestRuntimeV2LifecycleTraceControlSiteContract` (see below).
- new `internal/vm/runtime_v2_lifecycle_behavior_placement_adoption_test.go`,
  plus small additions to `runtime_v2_lifecycle_behavior_harness_test.go`
  (mode-string concatenation, two new enum values) and
  `runtime_v2_lifecycle_behavior_await_shutdown_test.go` (two new `main()`
  dispatch lines) to wire the new modes into the existing harness.
- `Makefile`: `runtime-v2-lifecycle-check` regex gains
  `StaticJoinPollOwnerLane` and `JoinConsumePlacementAdoption`.

## Non-Goals

- `mark_done`/`mark_done_needs_control` reduction, the `park_key` race, and
  the TSan pin-interleaving test's un-skip (Task 8).
- Scope bookkeeping, `ex->scopes` atomic-snapshot, `scope_key` waiter store
  migration (Task 9).
- `rt_task_await`/N=1 runner/`compat_cv` (Task 10).
- `rt_task_cancel`/`cancel_task` (S5-Q5: stays control tree-walk + owner-shard
  per-task wake, already correct since Task 6; not touched here).
- F1 (net-lane read/write registration re-placement): contingency only, not
  implemented unless Task 11's post-Task-7 re-verification shows a residual
  band.

## Design Per Surface

### Join register + result read (rule 2, S5-Q3) — `rt_task_poll`

Baseline `rt_task_poll` (`rt_async_task.c:155-226`) takes `rt_control_lock`
at entry and holds it for the whole function, including both DONE-consume
branches (the immediate-DONE fast path, `:193-201`, and the
register-then-verify DONE path, `:210-222`). The join waiter store already
routes `WAKER_JOIN` to the **target's own current owner shard**
(`rt_waiter_route.c:21-24,51-54`: `rt_task_owner_shard(ex, get_task(ex,
key.id))`), and `mark_done`'s completion drain
(`wake_key_all_with_policy(ex, join_key(task->id), 0)`, `rt_async_state.c`)
pops that same store under that same shard lock. So `add_waiter`
(`rt_async_waiter.c:522-582`, locks `rt_waiter_key_shard(ex,key)`) and the
drain already serialize on **one lock** today, nested *inside* the outer
control lock — dropping the outer control lock does not remove that inner
serialization; it was never the thing providing it. This is why the
register-then-verify shape (`prepare_park` then re-check `TASK_DONE`) can be
lifted out of the control lock verbatim: no reordering of the internal steps
is needed, only removal of the now-redundant outer lock.

**Change:** delete `rt_control_lock`/`rt_control_unlock` from `rt_task_poll`
entirely. The panics (`rt_current_task_id() == 0`, `== target->id`, missing
current task) no longer need a pre-panic unlock. Both DONE-return branches
switch `task_release(ex, target)` to `task_release_lane_aware(ex, target)`
(rule 1: final free requires the control lane; the lane-aware variant
acquires it only when this drop is the last reference to a DONE task — see
"Handle Release / Final Free" below).

**What does NOT need a lock change:** `current_task_cancelled`, `rt_current_task`,
`rt_current_task_id` read only the calling thread's own task/TLS (thread-local,
single-writer-while-running, same reasoning the codebase already applies to
`park_key`/`owner_shard_id` self-writes). `ready_take_current_local_tail` and
`wake_task`/`wake_task_with_policy` already manage their own shard locking
internally and are already called control-free elsewhere in the tree today
(`rt_executor_wake_net_waiters_for_key_on_owner`'s non-accept-key branch calls
`wake_net_task` -> `wake_task_with_policy` with no control held) — this is not
a new calling convention, just a new caller of an already-control-free-safe
function.

### F2: placement adoption at join consume

**Semantics (quoting the Task 11 spec).** When a task consumes the result of
a DONE child carrying `TASK_PLACEMENT_CONNECTION`, the consumer adopts the
child's placement via `rt_task_replace_owner` before `task_release`. This
makes `serve_many` (the stdlib request-loop task) adopt the accepting shard
per accept, so every `serve_conn` it spawns inherits that shard, and the
whole connection pipeline runs shard-local instead of funneling onto shard 0
(the mechanism behind `RV2-DEBT-015`'s starvation, `epic8-task11-placement-funnel`
in shared memory).

**Insertion point:** a new static helper,
`rt_task_poll_adopt_placement(rt_executor* ex, rt_task* current, rt_task*
target)`, called immediately before `task_release_lane_aware` in **both**
DONE-consume branches of `rt_task_poll` — matching the spec's insertion
points exactly (both branches, before release, since release may free the
child and placement fields must be read first).

```c
static void rt_task_poll_adopt_placement(rt_executor* ex, rt_task* current, rt_task* target) {
    if (current == NULL || target == NULL ||
        target->placement_class != TASK_PLACEMENT_CONNECTION ||
        target->owner_shard_valid == 0) {
        return;
    }
    if (current->placement_class == TASK_PLACEMENT_CONNECTION &&
        current->owner_shard_id == target->owner_shard_id) {
        return;
    }
    // Invariant (R1, main's review requirement): the caller must hold NO
    // shard lock when calling this -- rt_task_replace_owner takes control,
    // and control-after-shard is a lane-order violation rt_lane.c asserts
    // against. Both rt_task_poll call sites satisfy this: by the time either
    // DONE branch reaches this call, any shard lock taken earlier in that
    // branch (add_waiter/remove_waiter's internal locking) has already been
    // released by the callee before returning.
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_JOIN_POLL);
    }
    rt_task_replace_owner(ex, current, target->owner_shard_id, TASK_PLACEMENT_CONNECTION);
    rt_trace_placement_adoption();
    if (need_control) {
        rt_control_unlock(ex);
    }
}
```

**Hard-constraint arm chosen: (1), explicit control fallback.** The Task 11
spec's hard constraint requires either an explicit control fallback counted
under a named site (permitted since adoption is O(connections), never
per-request steady state once parent and child already share placement), or
an explicit re-derivation of the children[]-append happens-before chain
without control. This task takes arm (1): `rt_task_replace_owner` already
requires control (`rt_waiter_migrate_join_waiters`'s cross-shard waiter move
depends on the caller holding it, `rt_waiter_route.c:82-84`) and reuses the
exact accept-transition primitive and safety argument the Task 11 spec
already wrote (self-replace on a RUNNING task, on that task's own thread —
`current` here, exactly the shape `rt_net_place_current_task_on_owner`
already establishes as safe). No new happens-before argument is invented;
this task does not re-derive Task 6's children[]-append invariant because
arm (1) makes that unnecessary.

**Why `current`'s own fields are safe to read/write here without a lock:**
`current` is the task RUNNING on this thread, executing `rt_task_poll`
itself — reading `current->placement_class`/`owner_shard_id` and later
calling `rt_task_replace_owner(ex, current, ...)` (which writes those same
fields) are both self-writes/self-reads from the task's own executing
thread, the same "self-replace on RUNNING task" shape `rt_async_task.c`'s
`__task_create` invariant comment (lines 54-92) already anticipated: *"a
future self-adoption at join-consume would be the same shape."*

**Why `target`'s placement fields are safe to read here without a lock:**
we only reach this call after observing `target`'s status as `TASK_DONE`
(acquire-loaded). No code path calls `rt_task_replace_owner` on a `DONE`
task (the two self-replace sites operate on the calling thread's own RUNNING
task; the one cross-thread site,
`rt_executor_wake_net_waiters_for_key_on_owner`, targets a task popped from a
waiter store, i.e. `TASK_WAITING`) — so `target`'s placement fields are
frozen from the moment it went DONE, and reading them post-DONE needs no
lock, matching rule 1's lookup rule (c): the caller holds a handle reference
to `target` for the duration.

**Trace/proof obligation:** a new dedicated counter, `placement_adoptions`
(`rt_async_trace.c`, parallel to `owner_replaced`), incremented only at this
call site (not by the pre-existing accept-transition call sites), so bench
rows can directly attribute adoption events to join-consume rather than
inferring them from `owner_replacements`' aggregate delta.

### Inline child poll (S5-Q4) — `poll_ready_child_inline`

Baseline (`rt_async_task.c:228-259`) is entered with control already held by
the caller (`rt_task_poll`); it releases control around the nested
`poll_task` call (`:244`) and re-acquires it after (`:250-251`, tagged
`RT_CTRL_SITE_HANDLE`), purely because the caller used to hold control across
the whole helper. Task 6's evidence (`epic8-task6-create`,
`08-evidence.md` Task 6 section) identified this exact bracket as the
residual `ctrl_handle≈3.500/req` cost on the 8x1024 bench row — Task 6's
faster create path makes this helper's precondition (`ready_take_current_local_tail`:
the target is still at the current worker's own local-queue tail) hold far
more often, so the bracket fires on almost every inline child poll.

**Change:** delete both `rt_control_lock`/`rt_control_unlock` calls (and the
`RT_CTRL_SITE_HANDLE` tag with them). The helper now takes/releases the
child's owner shard lock only around `running_count++`/`--` (unchanged),
runs `poll_task` with **no** lock held (unchanged behavior — it always ran
unlocked; only the surrounding bracket held control), and calls
`apply_poll_outcome` normally — that function is already lane-aware
(`need_control = !rt_lane_holds_control() && ...`) and acquires whatever it
needs on its own. No caller of `poll_ready_child_inline` holds control after
this task's `rt_task_poll` change, so there is nothing left to bracket.

**Eligibility invariant (why no new lock is needed, and the one thing to
re-check if this ever changes):** the only child `poll_ready_child_inline`
is ever called on is the fresh, just-created child popped off the *current
worker's own local queue tail* (`ready_take_current_local_tail`, guarded at
`rt_task_poll`'s call site). By construction (`rt_task_inherit_placement`
copies the parent's owner shard before the child is published, Task 6),
that child's owner shard equals the current worker's shard, and it was only
ever reachable through this one worker's own local queue — no other thread
can be concurrently running or inline-polling the same child, and no other
thread can pop it from a ready queue elsewhere (it isn't enqueued anywhere
else). If a future change lets a child be inline-polled from a queue other
than the popping worker's own local tail, or lets two workers race to claim
the same child, this invariant must be re-derived — flagged per the review's
watch-item; no evidence of such a path exists today.

### Handle clone (S5-Q6) — `rt_task_clone`

Baseline (`rt_async_task.c:314-328`) takes control unconditionally purely to
guard `task_add_ref`, a relaxed atomic increment. The spike's verdict is
unconditional ("Drop the control lock"), not a rare fallback — unlike
`rt_task_wake`'s scope-adoption case, there is no scenario where clone still
needs control. New body:

```c
void* rt_task_clone(void* task) {
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return NULL;
    }
    task_add_ref(target);
    return target;
}
```

The caller already holds a live handle to `target` (the handle being
cloned), so `handle_refs >= 1` before this call; the free rule (free only at
`refs 1->0 && TASK_DONE`) can never race a live-handle clone to zero. No
executor/lane lookup is needed at all now that no lock is taken.

### Handle wake (S5-Q2) — `rt_task_wake`

Baseline (`rt_async_task.c:132-153`) takes control for the whole function:
the target-DONE check, the scope-adoption write
(`target->parent_scope_id = scope->id` when `current` has a scope and
`target` doesn't), and the wake itself.

**Change:** peek `current->scope_id != 0` (current's own field — thread-local
while current is RUNNING, safe to read without a lock, same reasoning as
`current->owner_shard_id` above) *before* ever taking a lock; the overwhelming
common case (no scope on the waking task) never touches control. Only when
that peek is true do we take control and, **under the lock**, re-check
`target->parent_scope_id == 0` before writing it — this re-check is new
compared to baseline: previously the whole function held control from entry,
so `target->parent_scope_id` was never read outside a lock at all; splitting
the fast path out means the *first* read of `target->parent_scope_id` (the
peek) would otherwise be an unsynchronized read racing `rt_scope_register_child`'s
control-held write (`rt_async_scope.c`, unchanged this task, Task 9's
territory) — so the peek reads only `current`'s own field, never `target`'s,
and the only read of `target->parent_scope_id` happens under the lock.
`wake_task(ex, target->id, 1)` then runs unconditionally, control-free
(already an established control-free-safe call, see above).

### Cancel (S5-Q5) — unchanged

`rt_task_cancel`/`cancel_task` keep the control tree-walk + owner-shard
per-task wake shape Task 6 already implemented correctly (children[] snapshot
under the task's own owner shard lock, then recurse). Not touched by this
task.

### Handle release / final free (rule 1)

`task_release_lane_aware` (`rt_async_state.c:1401-1423`) already implements
rule 1 exactly (atomic refcount fetch-sub; control acquired only when the
drop is the last reference to a `TASK_DONE` task; `mark_done`'s completion
pin, `task_add_ref` at entry / `task_release_lane_aware` at exit, keeps the
task alive across a joiner's concurrent last-handle release). This task's
only change here is which callers use it: `rt_task_poll`'s two DONE branches
switch from `task_release` (control-only) to `task_release_lane_aware`,
since `rt_task_poll` no longer unconditionally holds control by the time it
reaches them.

## Review-Visible Changes To Task 5's Shipped Gates

Two already-active (not pending) tests from Task 5 assert things this
migration legitimately changes. Both edits were reviewed and approved by
main before implementation (plan-gate response, this task).

1. **`TestRuntimeV2LifecycleStaticCensusSitesTagged` (G6).** Greps each
   function's *own* body for its control-site tag string.
   - `rt_task_clone` no longer tags anything (S5-Q6 is unconditional, not a
     rare fallback) — its case is deleted from the table.
   - `rt_task_poll`'s `RT_CTRL_SITE_JOIN_POLL` tag now lives in the separate
     `rt_task_poll_adopt_placement` helper — it *must* be a separate
     function, not inlined, because P7 (below) requires `rt_task_poll`'s own
     body to contain zero occurrences of `rt_control_lock(`. The table entry
     is repointed from `{"rt_task_poll", "RT_CTRL_SITE_JOIN_POLL"}` to
     `{"rt_task_poll_adopt_placement", "RT_CTRL_SITE_JOIN_POLL"}`. This is
     structural, not evasive: P7's own bar (below) forces the helper split,
     and G6's repoint just follows it.
2. **`TestRuntimeV2LifecycleTraceControlSiteContract`.** Asserted
   `ctrl_join_poll` non-zero in a synthetic checkpoint/spawn/await program
   with no `TASK_PLACEMENT_CONNECTION` tasks. After this task, that is
   genuinely zero in that program (F2's fallback only fires when adopting a
   connection-placed child) — `ctrl_join_poll` is removed from the
   must-be-nonzero list; `ctrl_create` stays (segment growth still fires at
   least once per process). A comment records why, dated to this task.

## P7 Activation

`TestRuntimeV2LifecycleStaticJoinPollOwnerLane`'s `t.Skip` is deleted. Its
assertion (`rt_task_poll`'s own body contains no `rt_control_lock(`
substring) passes as designed: every control acquisition this task still
needs (the F2 fallback) lives in `rt_task_poll_adopt_placement`, a distinct
function.

## New Test: F2 Positive/Negative

Task 4/5's static and behavior gates predate F2 (it did not exist when they
were written) and do not cover it. This task adds
`TestRuntimeV2LifecycleJoinConsumePlacementAdoption` (new file, harness modes
`placement-adopt-positive`/`placement-adopt-negative`): a joiner on shard 0
consumes a DONE child pinned to shard 1. In the positive case the child is
`TASK_PLACEMENT_CONNECTION`-placed and the joiner's own
`owner_shard_id`/`placement_class` must become shard-1/`CONNECTION`
afterward. In the negative case the child is `TASK_PLACEMENT_GENERIC`-placed
and the joiner's placement must be **unchanged** — the guard
(`target->placement_class != TASK_PLACEMENT_CONNECTION` short-circuit) is as
load-bearing as the adoption itself, per main's explicit review requirement.

## Measurement: Results (Net Bench 8x1024, `SURGE_TRACE_EXEC=1`, 3 Runs)

Direct mode, 8 shards / 8 threads / 1024 connections / 8 req/conn = 8192
requests, run against `benchmarks/native/net_request_reply` directly
(bit-exact reproducible across all 3 runs, every per-site counter). Before =
Task 6's committed baseline (`5523094e`/`a2d3f87c`/`05d95b60`).

| Site | Before /req (total) | After /req (total) |
| --- | ---: | ---: |
| `control_lock_acquired` | 22.780 (186593) | 23.90 (195430-195751) |
| `ctrl_join_poll` | 3.881 (31792) | **0.249 (2019-2037)** |
| `ctrl_completion` | 0.506 (4141) | 3.500 (28673, bit-exact) |
| `ctrl_scope` | 13.000 (106499) | 13.000 (106499, exact match) |
| `ctrl_handle` | 3.500 (28673) | 3.626 (29696) |
| `placement_adoptions` (new) | n/a | 0.247 (2019-2037) |

**F2 verified working**, the primary goal: `placement_adoptions` fires
2019-2037 times (O(connections), not O(requests), matching the frequency
bound). A mid-load `SIGUSR1` dump (60 req/conn, sampled ~1s in) shows the
owner histogram genuinely spread across all 8 shards for the first time
(338, 430, 380, 362, 387, 365, 362, 338 tasks on shards 0-7 respectively),
versus the pre-F2 baseline's "3073/3073 owner=0"
(`epic8-task11-placement-funnel`). `TRACE_STORE` waiter counts are
similarly distributed (338-444/shard vs all-in-shard-0 before).
`inject_len=0` at both the mid-load sample and exit (vs ~1023 steady
before). `ctrl_join_poll` drops from 3.881 to ~0.25/req exactly as designed
(S5-Q3) — the residual is entirely the rare F2 fallback, not steady-state
join traffic.

**Honest accounting of a real, reproducible increase** (mirroring Task 6's
"not the whole story" precedent): `control_lock_acquired`'s total went *up*
(186593 -> ~195600), driven by `ctrl_completion` jumping to a bit-exact
28673 every run. Root cause, fully traced: before this task,
`poll_ready_child_inline` held control across its entire body including the
nested `mark_done` call, so `mark_done`'s own `need_control` check
short-circuited false (`rt_lane_holds_control()` was already true) and its
control-lane work ran "for free" and untagged under the caller's ambient
lock. Dropping `poll_ready_child_inline`'s control (S5-Q4, this task's
explicit mandate) removes that ambient hold, so `mark_done` now correctly
evaluates and tags its own need for these same completions — and since
Task 6 already established this exact benchmark drives the
`write_owned(...).await()`/`net.read_some(...).await()` inline-child-poll
pattern on almost every request (`ctrl_handle`=28673 at Task 6 landing),
it now takes its own separate, honestly-tagged `RT_CTRL_SITE_COMPLETION`
lock for nearly that same population (28673, matching almost exactly).
This is not a bug and not something to revert — `poll_ready_child_inline`
correctly has zero control acquisitions of its own now, exactly the
verdict S5-Q4 called for — it is a previously-hidden cost becoming visible,
and Task 8/9 are positioned to remove it (`mark_done_needs_control`'s
scope/join-key reasons, S6-Q1; scope ownership off control, Task 9).
`ctrl_handle` also rose slightly (28673 -> 29696) despite
`poll_ready_child_inline`'s own bracket being fully removed; not fully
attributed within this task's scope (candidate: F2's redistribution
shifting the timing of some existing `rt_task_cancel`/timeout-race code
path in `stdlib/net`), reported honestly as an open item rather than
claimed as understood.

Per `epic8-task11-starvation`'s `RV2-DEBT-016` reinterpretation flag: this
row's *character* changed as predicted (execution genuinely distributing
across shards for the first time) — Task 8/9's own before/after
measurements should baseline against this task's row, not Task 6's, since
this is the first row where 8 workers are truly contending rather than one
worker running alone.

## Commit Boundary

One commit: the result-write reorder (enabling change), the join-poll
migration, F2 (both insertion points + the new counter), the
`poll_ready_child_inline`/`rt_task_clone`/`rt_task_wake` migrations, the two
review-visible test-file edits, the new F2 test, the Makefile regex update,
and doc/evidence/notes updates. These share the same two call sites (both
`rt_task_poll` DONE branches) and the same lane-order proof (control dropped
from the steady join path; F2's fallback is the only control acquisition
left on that path) — there is no meaningfully separable intermediate proof
state, unlike Task 6/Task 9's docs-only splits.
