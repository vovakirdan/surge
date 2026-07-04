# Epic 8 Task 3: Lifecycle Lane Proving Spike

Task 3 output. This document fixes the shard-owned task-lifecycle model and
records the proof run. Every *(spike)* mark in
`08-lifecycle-dependency-map.md` is answered here, and this document's
decisions rewrite that map's lane table on conflict (index rule). Implementation
tasks (6-10) implement these decisions; deviating from them requires updating
this document first.

Baseline commit for all anchors: `daeac51e` (Task 1 kickoff-baseline; the tree
after the Task 1 generation-qualified-removal fix). Line numbers were
re-verified against this tree.

## Proving Spike Record (RULES.md Global Rule 1)

- **Hypothesis:** the Epic 8 lifecycle surfaces can move off the control lane
  onto the owner-shard lanes Epic 7 already built, without new lost wakeups,
  use-after-free, or lost publications, by (a) keeping id/scope-id allocation
  and task/scope table growth on the control lane, (b) publishing slots and
  waking ready work under the owner shard lock, (c) reading join results
  through release/acquire on `TASK_DONE` with a completion pin covering the
  body, (d) moving same-owner scope bookkeeping and `scope_key` waiters to the
  scope owner shard lane with a named control fallback for the one cross-owner
  edge (accept), and (e) leaving join/scope waiter entries unqualified
  (`seq == 0`) because their keys are monotonic never-reused ids, not reusable
  addresses.
- **Files/surfaces allowed to change:** none in the repository. The proof
  prototype lives outside the tree (scratchpad `lifecycle_publish_refcount_spike.c`,
  not committed). The tree's C state stayed pristine (docs-only commit).
- **Explicitly non-final behavior:** the prototype models the task table,
  shard ready queues, atomic refcount, and the completion-pin interleaving
  structurally. It does NOT model the full waiter store, channel generations,
  the virtual clock, `done_cv`, or accept re-placement; those are settled by
  the structural arguments in this document (pinned to shipping `file:line`)
  and by the existing Epic 7 waiter-contract tests, not by the prototype.
- **Proof:** (1) a standalone C model built `clang -O1 -g -fsanitize=thread`
  and `clang -O2 -DNDEBUG` (Ubuntu clang 18.1.3), 4 shards / 4 creator threads /
  160000 publications / 20000 completion-pin interleavings, with two
  deterministic negative controls; (2) the existing focused waiter-contract
  tests run read-only at baseline `daeac51e`; (3) an exhaustive grep audit of
  every writer of `rt_task.owner_shard_id`; (4) memory-ordering/lifetime
  arguments pinned to shipping code for every remaining question.
- **Success criteria:** zero TSan reports and zero lost publishes / zero
  use-after-free in the safe model, both builds; the negative controls fail
  (proving the assertions are not vacuous); the corroboration tests pass; the
  owner-shard-id audit shows a single post-spawn writer.
- **Failure criteria:** any TSan report or lost/UAF in the safe model; a
  negative control that passes; a second post-spawn owner writer; or any
  question whose only safe answer needs a Phase 4 surface (inbound queues,
  eventfd credits, remote select, seq-cst `PARKED`) — that would stop the epic
  for a re-scope.
- **Rollback note:** nothing to roll back; the prototype is scratchpad-only and
  the commit is docs-only. If a decision here is later disproven, the owning
  implementation task updates this document first (index rule) before code.

### Proof Results

Safe model (the decided design), `published=160000 lost_publishes=0
uaf_detected=0 pin_cases=20000`:

| Build | Runs | Result | exit |
| --- | --- | --- | --- |
| TSan `-O1 -g` | 2 | PASS, zero TSan reports | 0 |
| `-O2 -DNDEBUG` | 2 | PASS | 0 |

Deterministic negative controls (must fail, and do):

| Control | Models | Result | exit |
| --- | --- | --- | --- |
| `-DUNSAFE_PUBLISH` | slot published shard-lane into the table captured before a concurrent control-lane growth retires it | `lost_publishes=1` (reader loads the current table, sees NULL) | 1 |
| `-DUNSAFE_NOPIN` | completion drops the pin; the joiner's last-handle release frees the task mid-body | poisoned-payload assertion abort (`v == 0xB0D5` fails) | 134 (SIGABRT) |

The safe model publishes 160000 tasks across 4 shards with id-alloc + growth +
slot-store under a control mutex and ready-push (id enqueue) under the owner
shard lock, and every popped id resolves through an acquire-load `get_task` to
the right task: zero lost publishes. The refcount arm runs 20000 forced
interleavings where the joiner's last-handle `task_release` lands *inside* the
completer's `mark_done` body; with the completion pin the task is never freed
under the body, and `-DUNSAFE_NOPIN` proves the model detects the violation
when the pin is removed.

Corroboration (existing tests, `-tags runtime_v2_pending`,
`SURGE_BACKEND=llvm`, baseline `daeac51e`, read-only):

| Test | Result |
| --- | --- |
| `TestRuntimeV2CancelledJoinWaiterDoesNotConsumeTaskCompletionWake` | PASS (3.44s) |
| `TestRuntimeV2FailfastScopeCancellationWakesOwner` | PASS (2.48s) |
| `TestRuntimeV2BlockingCompletionWakesAwaiter` | PASS (2.48s) |
| `TestRuntimeV2CancelledBlockingWaiterDoesNotConsumeCompletionWake` | PASS (3.19s) |

These are the register-then-verify join race, the scope failfast wake, and the
completion-wake-vs-cancelled-waiter contracts my S5-Q3 / S5-Q8 arguments
extend; they hold at baseline before any migration.

Owner-shard-id audit (S7-Q1): `rt_task.owner_shard_id` is written only through
`rt_task_set_placement` (`rt_scheduler_placement.c:72`). Its callers are
`rt_task_assign_spawn_owner` / `rt_task_inherit_placement` (spawn-time, before
publish/enqueue) and `rt_task_replace_owner` (`:80-96`). The only post-spawn
caller of `rt_task_replace_owner` is the accept path
(`rt_async_waiter.c:381`, `rt_net_accept_group.c:101,111`). All other
`owner_shard_id` occurrences are on different structs (`rt_channel`, listener,
connection, net poll snapshots) — not `rt_task`.

## Decisions Per Open Question

Each question from `08-lifecycle-dependency-map.md` §9, with the verdict, how it
was proven, and the resulting lane.

### S5-Q1 — Owner-shard slot publish + ready-push (create/checkpoint/sleep)

**Verdict: YES for ready-push; publish stays serialized with growth.** The
ordering `assign owner -> shard-lock -> slot-store(release) -> ready-push ->
unlock` is correct for *visibility*: the safe TSan model pops the id under the
owner shard lock and acquire-loads the slot through `get_task`, with zero lost
publishes over 160000 publications. But the copy-on-grow task table
(`rt_async_internal.h:246-249`, `ensure_task_cap` at `rt_async_state.c:443-490`)
has a **publish-vs-growth race with no lane-order-legal happens-before**: a
shard-lane publisher that stores into a table which a concurrent control-lane
growth is retiring loses its slot (a reader loading the current table sees
`NULL`). Growth touches all shards, so it cannot be serialized with a per-shard
publisher by a shared lock without violating the `control -> at most one shard`
order. The `-DUNSAFE_PUBLISH` control demonstrates the loss deterministically.

Two safe realizations:

- **(A) safe-minimal (Task 6 default):** id-alloc, `ensure_task_cap` growth, and
  the `slot_store` publish stay under the control lane; the owner shard lock
  nests for the ready-push (id enqueue) only (lane order control -> shard).
  Every intermediate commit stays legal and `SURGE_SHARDS=1`-preserving. Create
  keeps one control acquisition.
- **(B) segmented table (escalation):** replace the copy-on-grow table with a
  segmented, never-moved-slot structure so growth only appends a segment and
  never invalidates an existing slot's address. Then id-alloc (atomic
  `next_id` fetch_add) + slot publish + ready-push run under the owner shard
  lock with no control acquisition on create.

**Decision:** Task 6 adopts (A) as the default. (B) is the approved enabling
change **iff** measurement shows create is a material per-request control
consumer. **Escalation criterion (numeric, so Task 6 does not relitigate):**
Task 5 adds per-site attribution for `control_lock_acquired`, at minimum a
create-site counter. On the 8-shard/1024-connection row, if the create site
accounts for **>= 2.0 control acquisitions per request** (i.e. `>~ 8%` of the
Task 1 baseline of ~26.4/request), Task 6 escalates to (B). Below that, (A)
stands and the residual create control acquisition is documented as
per-connection-amortized — **but that amortization is a measured conclusion,
not an assumption**: in request trees a spawn can be per-request, so Task 5's
create-site counter must confirm the per-request create rate on the 8x1024 row
before (A) is accepted as final.

Lane: **control for id-alloc + growth + slot publish; owner shard for
ready-push** (S5-Q1 resolved to A; B on the measured trigger).

### S5-Q2 — Scope-adoption write on wake stays a control fallback

**Verdict: YES.** `rt_task_wake` (`rt_async_task.c:57-77`) does two things: the
wake itself (`wake_task:75`), which is owner-shard work, and a rare
scope-adoption write (`:69-74`, `target->parent_scope_id = scope->id` after
`get_scope`) that only fires when the current task has a scope and the target
has none. The adoption write is scope-tree state (rule 3 lane) and is off the
steady request path. Keep it behind the named control fallback while the wake
uses the owner shard. **Written argument** (no experiment needed: the wake path
is the already-shipping `wake_task` owner-shard path; only the conditional
scope write is fenced to control).

Lane: **owner shard for the wake; control fallback for the scope-adoption
write.**

### S5-Q3 — Join register-then-verify under the target-owner store lock alone

**Verdict: YES.** Today `rt_task_poll` (`rt_async_task.c:79-149`) registers on
`join_key(target)` via `prepare_park` (`:127`) then re-checks `TASK_DONE`
(`:133-145`), all under control. The join waiter store already routes to the
**target task owner shard** (`rt_waiter_route.c:20-24`, `WAKER_JOIN` ->
`rt_task_owner_shard(get_task(key.id))`); `mark_done`'s completion drain
(`wake_key_all_with_policy` popping `join_key` at `rt_async_state.c:1565`,
pop loop `:1092-1124`) pops that same store under the same shard lock. So the
registration (`add_waiter` under the target owner store lock,
`rt_async_waiter.c:565-578`) and the completion drain serialize on **one lock**
— the target owner store lock — and the joiner's re-check of `TASK_DONE` after
registering closes the "target completes mid-registration" window exactly as
Epic 7's register-then-commit park does. Corroborated by
`TestRuntimeV2CancelledJoinWaiterDoesNotConsumeTaskCompletionWake` (PASS).
Requires rule 2's result-visibility reorder before the read drops control.

Lane: **target task owner shard** for join register + result read.

### S5-Q4 — Inline child poll entirely on the owner shard

**Verdict: YES.** `poll_ready_child_inline` (`rt_async_task.c:151-181`) is
entered control-held today, unlocks control around the poll (`:167`), and
re-locks after (`:173`), taking the child owner shard lock only for
`running_count` accounting (`:163-165,174-178`). The only eligible child is the
fresh just-created child popped off the current worker's own local queue
(`ready_take_current_local_tail`, guarded at `rt_task_poll:109-110`), so its
owner equals the current worker's shard. Once create (S5-Q1) and completion
(S5-Q15) are owner-shard, the surrounding control bracket is the only remaining
control use here; the whole helper runs under the child owner shard lane (poll
still runs unlocked; `running_count` and `apply_poll_outcome` under the owner
shard). **Written argument** grounded in the same-owner eligibility guard.

Lane: **child owner shard** (no control).

### S5-Q5 — Cancel child-tree walk stays control; per-task wake on owner

**Verdict: control tree walk + owner-shard per-task wake.** `cancel_task`
(`rt_async_state.c:1458-1479`) sets `cancelled`, wakes the task on its owner
shard if `TASK_WAITING` (`:1473-1474`), and recurses `task->children[]`
(`:1476-1478`). The child tree is control-owned tree state whose nodes may (via
the accept transition) live on other shards; keeping the walk on control with
per-task owner-shard wakes (control -> owner, one at a time) preserves lane
order and bounded cleanup (rule 4). Moving the whole tree to a scope/parent
owner lane would risk a scope-owner-shard + child-owner-shard double hold.

Lane: **control tree walk; owner-shard per-task wake.**

### S5-Q6 — Clone drops control (atomic refcount + live-handle rule)

**Verdict: YES.** `rt_task_clone` (`rt_async_task.c:234-247`) only calls
`task_add_ref` (`rt_async_state.c:1381-1386`), a relaxed atomic increment. The
caller holds a live handle (the handle it is cloning), so `handle_refs >= 1`;
the free rule frees only at `refs 1->0 && TASK_DONE`
(`task_release*:1424,1440`), so a live-handle clone can never race its target
to free. Drop the control lock. The refcount arm of the TSan model exercises
concurrent add_ref / release / last-handle-free with zero UAF.

Lane: **atomic refcount** (no control).

### S5-Q7 — Scope table adopts the task-table atomic-snapshot protocol

**Verdict: adopt the atomic-snapshot protocol (pick-one).** For scope
bookkeeping to run on the scope owner lane, `get_scope`
(`rt_async_state.c:382-387`, today a control-guarded `ex->scopes[id]` read)
must be readable off control. Rather than invent new machinery (Global Rule 5),
reuse the task table's shipping pattern: make `ex->scopes` an atomic-snapshot
structure (acquire-load table pointer + slot), growth under control publishing a
release copy, retired generations never freed. Scope enter keeps control for
scope-id alloc + table growth + scope-slot publish (symmetric with create, and
subject to the same publish-vs-growth serialization as S5-Q1-A). Register /
join-all / exit / failfast only *read* the scope (no table growth), so they run
on the scope owner shard lane with acquire-snapshot lookups.

Lane: **scope table = atomic snapshot; scope enter control for alloc/growth/
publish; scope object ops on the scope owner shard.**

### S5-Q8 — Child register and child-done serialize on the scope owner shard lock

**Verdict: YES.** `rt_scope_register_child` (`rt_async_scope.c:40-77`) mutates
`active_children` / `scope_registered` / `failfast_*`; `scope_child_done_locked`
(`rt_async_state.c:1368-1379`, called from `mark_done:1561`) mutates the same.
Children spawn same-owner by default (`rt_task_inherit_placement`), so a child's
`mark_done` runs on the scope owner shard — the same lock serializes register
and child-done. Corroborated by `TestRuntimeV2FailfastScopeCancellationWakesOwner`
(PASS), which exercises the single-serialization-point failfast decision.

Lane: **scope owner shard lock** serializes register + child-done (same-owner).

### S5-Q9 — Cross-owner scope cancel uses the named control fallback

**Verdict: YES, never two shard locks.** `rt_scope_cancel_all` /
`scope_cancel_children_locked` (`rt_async_scope.c:79-93`,
`rt_async_state.c:1359-1366`) walk `scope->children[]` calling `cancel_task`.
When a child's owner shard differs from the scope owner (only reachable via the
accept transition), cancel takes the named control fallback (control -> child
owner shard, one at a time), never scope-owner-shard + child-owner-shard
together. Same bounded-cleanup rule as S5-Q5 (rule 4).

Lane: **scope owner lane; control fallback (control -> child owner) for
cross-owner children.**

### S5-Q10 — `scope_key` waiters move to the scope owner shard store

**Verdict: move to the scope owner shard store (pick-one), revising Epic 7 D8.**
`rt_scope_join_all` parks the scope owner on `scope_key(scope_id)`
(`rt_async_scope.c:117-120`); `scope_child_done_locked` wakes `scope_key` when
`active_children == 0` (`rt_async_state.c:1377`). Today `scope_key` routes to
`ex->control_waiters` (`rt_waiter_route.c:25-26,55`, Epic 7 D8). Since both the
park and the child-done wake now run on the **scope owner shard lane**, the
`scope_key` waiter store should live on the scope owner shard's `waiter_store`
so park and wake share one lane (mirroring join keys living on the target owner
store). `rt_waiter_key_shard`/`rt_waiter_store_for_key` gain a `WAKER_SCOPE`
case resolving the scope owner shard via the scope's owner task.

**Epic 7 D8 revision (recorded for history):** Epic 7 D8 deliberately kept
`WAKER_SCOPE` on a small control-owned store because scope bookkeeping was
entirely control-lane then, so a control-lane store was the matching lane. Epic
8 moves scope bookkeeping to the scope owner lane (rule 3), which changes the
matching lane: keeping `scope_key` on control would force every same-owner
`join_all` completion back onto the control lane (defeating S6-Q1). The revision
is therefore a direct consequence of the scope-owner-lane decision, not a
reversal of D8's reasoning.

Lane: **scope owner shard `waiter_store`.**

### S5-Q11 — Scope free gated like task free

**Verdict: YES.** `scope_exit_locked` (`rt_async_scope.c:150-170`) frees the
scope object and clears `ex->scopes[id]` only after the `active_children > 0`
panic guard (`:141`). Gate the free the same way task free is gated (rule 1):
only the scope owner lane frees, only after `active_children == 0`, with the
scope-table slot cleared under the same protocol as the task slot. A late
cross-owner registration/cancel touching a freed scope id is prevented by the
`active_children == 0` precondition plus the atomic-snapshot slot clear.

Lane: **scope owner lane free, after `active_children == 0`.**

### S5-Q12 — Free rule lets the join path read results off control

**Verdict: YES.** The free rule — free only under control (or the lane-aware
free that acquires control, `task_release_lane_aware:1441-1444`), only when
`refs == 1 && status == TASK_DONE`, with `mark_done`'s completion pin
(`task_add_ref:1515` at entry, `task_release_lane_aware:1574` at exit) covering
the body — is sufficient for the join fast path to read results off control. A
joiner holds its own handle ref while reading, so `refs >= 1` and the target
cannot be freed under the read; and the completion pin prevents a joiner woken
mid-body from freeing the task on another shard before `mark_done` finishes
touching it. The TSan refcount arm asserts exactly this interleaving (joiner's
last-handle release lands inside the completer body) with zero UAF; the
`-DUNSAFE_NOPIN` control aborts, proving the pin is load-bearing.

Lane: **atomic refcount + control free; join reads results off control under
the pin.**

### S5-Q14 — Cancelled-poll scope teardown uses the scope owner lane

**Verdict: YES.** The `apply_poll_outcome` cancelled branch
(`rt_async_state.c:1585-1613`) does scope teardown (cancel children, re-park on
`scope_key`, or `scope_exit_locked`) under a control fallback today
(`need_control` at `:1589`). Move it to the scope owner shard lane (same as
S5-Q9-11), so a worker-context cancellation never takes control for a same-owner
scope; cross-owner children keep the control fallback (rule 4).

Lane: **scope owner lane** (control fallback only cross-owner).

### S6-Q1 — Surviving `mark_done_needs_control` reasons on the hot path

**Verdict: YES — only net-key removal and `done_waiters > 0` survive.**
Enumerating `mark_done_needs_control` (`rt_async_state.c:1486-1506`):

| Reason | Site | Disposition after Epic 8 |
| --- | --- | --- |
| residual `wait_keys_len` / `select_timers_len` | `:1487` | select non-goal; a plain join/channel task has none — compat |
| `parent_scope_id != 0` / `scope_registered` | `:1490` | **removed** — same-owner scope completion on the scope owner lane (S5-Q8/Q10) |
| `park_key` is `WAKER_JOIN` | `:1498` | **removed** — join store on the target owner (S5-Q3) makes removal owner-local |
| `park_key` is `WAKER_SCOPE` | `:1498` | **removed** — `scope_key` store on the scope owner (S5-Q10) makes removal owner-local |
| `park_key` is net | `:1498` | **stays** — net removal scans shards (net/accept contract, not this epic) |
| `done_waiters > 0` | `:1502` | **stays** — external await compat (rule 5) |

After S5-Q3 and S5-Q10, the only reasons that force control on the request hot
path are net-key removal (net contract) and `done_waiters > 0` (external-await
compat) — both documented compatibility, not unresolved hot-path debt.
**Written argument** by enumeration.

### S7-Q1 — Accept stays the single cross-owner lifecycle edge

**Verdict: YES.** The owner-shard-id audit above shows `rt_task.owner_shard_id`
has exactly one post-spawn writer, `rt_task_replace_owner`, driven only by the
accept path. No lifecycle surface in this spike introduces a second cross-owner
owner change; the join-waiter migration
(`rt_waiter_migrate_join_waiters`, `rt_waiter_route.c:67-126`) is the mechanism
that keeps a joiner's registration on the task's current owner across the accept
transition. **Grep-backed argument** (exhaustive).

### S9-Q7 — Generation qualification for join/scope waiters

**Verdict: `seq == 0` (unqualified) is correct; do NOT adopt `park_seq` for
join/scope.** Channel keys embed a reusable channel **address**
(`(uint64_t)(uintptr_t)ch`, `rt_async_waiter.c:34-42`) that can be freed and
reallocated, so a stale entry could misdeliver into a different park on a reused
address — that is why channels carry `park_seq` generation qualification
(`channel_candidate_valid` at `rt_channel_lane.h:87-91`, re-arm at `:197-219`,
deferred removals capturing `park_seq` at `rt_async_state.c:1007-1009,1169-1172`).
Join and scope keys embed a **monotonic, never-reused id** (task id via
`next_id++`, scope id via `next_scope_id++`; task slots are never reused for a
different task). A stale join/scope entry for id `N` can only ever refer to the
one task/scope `N` for all time, so misdelivery into a *different* target is
impossible. Join/scope wakes are wake-only (no value mailbox): a stale entry
causes at most one bounded spurious wake, absorbed by the `wake_token`
double-check in `park_current` (`:1153,1175`). The unqualified removal predicate
`seq == 0 || w.seq == seq` (`rt_async_waiter.c:159`) is therefore correct for
join/scope. **Written argument** (the crux distinction: address reuse vs
monotonic id).

## Written Rules (the contract Tasks 4-10 implement against)

### Rule 1 — Task lifetime (lookup, result visibility, handle release, final free)

- **Ids and slots:** task ids are monotonic (`next_id++` under control,
  `rt_async_task.c:16`); `ex->tasks_table` slots are never reused for a
  different task.
- **Lookup / deref:** `get_task` (`rt_async_state.c:356-365`) acquire-loads the
  table pointer then the slot. The copy-on-grow table
  (`rt_async_internal.h:246-249`, `ensure_task_cap:443-490`) never frees retired
  generations (doubling bounds them), so a lock-free reader never dereferences
  reclaimed memory. Dereferencing a pointer read from a slot is legal only:
  (a) under the control lock; or (b) under owner shard lock `S` when a
  shard-owned structure implies the task's owner is `S` (id from `S`'s ready
  queue, task running on `S`'s worker, waiter entry with `owner_hint == S`,
  `S`'s sleep store); or (c) while the caller holds a handle reference to the
  task (clone/join), which keeps the task alive by the free rule.
- **Result visibility:** `result_kind`/`result_bits` MUST be written **before**
  the `TASK_DONE` release store, and read **after** an acquire load of
  `TASK_DONE`. **Required change:** `mark_done` currently writes the result
  (`rt_async_state.c:1542-1543`) *after* the `TASK_DONE` release store
  (`:1540`, `task_status_store` is `memory_order_release`,
  `rt_async_internal.h:360-365`). This is sound today only because readers hold
  the control lock; Task 8 MUST reorder the result writes before the
  `TASK_DONE` store when Task 7 drops control from the join read.
- **Handle release:** `task_add_ref` (`:1381-1386`) is a relaxed atomic
  increment; `task_release` / `task_release_lane_aware` (`:1414-1450`) use a
  `memory_order_acq_rel` fetch-sub. Free happens only when the drop takes
  `refs 1->0` **and** `status == TASK_DONE`.
- **Final free:** `free_task` (`:1388-1412`) clears wait keys / select timers /
  children, clears the slot (`rt_task_slot_store(...,NULL)`), and frees the
  struct — **only under the control lane** (`task_release_lane_aware` acquires
  control if not held, `:1441-1444`).
- **Completion pin (the interleaving the epic calls out):** while a completing
  worker is still inside `mark_done`, the task is pinned by `mark_done`'s own
  `task_add_ref` at entry (`:1515`) and released via `task_release_lane_aware`
  at exit (`:1574`). So a joiner woken mid-body that consumes the last handle
  drops `refs` from 2 to 1, not to 0, and cannot free the task on another shard
  while completion is still touching it. The TSan model asserts this exact
  interleaving every iteration; `-DUNSAFE_NOPIN` (pin removed) aborts on a
  poisoned-payload read, proving the pin is what holds the struct.

### Rule 2 — Join result visibility

A joiner observes a completed target through release/acquire, not the control
lock: `mark_done` writes the result before the `TASK_DONE` release store (rule
1), and the joiner acquire-loads `TASK_DONE` (`task_status_load`,
`rt_async_task.c:116`) then reads `result_kind`/`result_bits` (`:117-119`).
Registration uses register-then-verify (`rt_async_task.c:127-145`): the joiner
adds `join_key(target)` to the **target owner store** and re-checks `TASK_DONE`;
the registration and `mark_done`'s completion drain
(`wake_key_all_with_policy` at `rt_async_state.c:1565`) serialize on the target
owner store lock, so a target completing mid-registration strands no joiner. The
accept transition migrates join waiters with the task
(`rt_waiter_migrate_join_waiters`) so the registration always sits on the
target's current owner.

### Rule 3 — Scope owner-lane model + named cross-owner control fallback

A scope's owner is the owner shard of the task that entered it (`scope->owner`,
`rt_async_scope.c:26`). Same-owner request trees (children spawn local by
default) serialize all scope object bookkeeping — register
(`rt_async_scope.c:63-66`), child-done (`scope_child_done_locked`,
`rt_async_state.c:1368-1379`), `join_all` park/wake
(`rt_async_scope.c:117-120` / `rt_async_state.c:1377`), failfast, and teardown
(`apply_poll_outcome` cancelled branch) — on the **scope owner shard lock**. The
`scope_key` waiter store moves from `ex->control_waiters` to the scope owner
shard `waiter_store` (S5-Q10, revising Epic 7 D8). `ex->scopes` becomes an
atomic-snapshot table so `get_scope` is a lock-free acquire load; scope enter
keeps control for scope-id alloc + table growth + scope-slot publish. The one
cross-owner case (a child re-placed by the accept transition) uses the **named
control fallback**: control -> child owner shard, one at a time, **never two
shard locks**. Scope free is gated like task free (rule 1): scope owner lane,
after `active_children == 0`.

### Rule 4 — Cancellation boundedness

Cancellation cleanup stays proportional to the cancelled task's own
registrations. `cancel_task` (`rt_async_state.c:1458-1479`) recurses only the
task's own `children[]`; per-task wake is control -> that task's owner shard,
one at a time. `rt_scope_cancel_all` walks only `scope->children[]`. No
lifecycle cancellation path scans the whole task table or all shards; the
all-shard net-removal loop (`rt_async_waiter.c:493-509`) is net-key only and
stays out of the lifecycle path. Scope failfast cancellation keeps its single
serialization point for `failfast_triggered`
(`rt_async_state.c:1550-1558` and `rt_async_scope.c:67-74`).

### Rule 5 — External-await compatibility boundary

`rt_task_await` from a non-worker thread with `workers > 1`
(`rt_async_task.c:192-210`) keeps `done_cv` under control; `done_waiters`
(`rt_async_internal.h:289`, atomic mirror incremented at `:197`, decremented at
`:201`) gates `mark_done`'s `done_cv` broadcast (`rt_async_state.c:1566-1568`)
so a plain worker join never touches `done_cv`. `done_cv` is used ONLY by
external / main-thread await. The single-worker runner (`run_until_done`) and
the sync-channel compat wait (`compat_cv`, `rt_async_compat.c`) stay
counted-separately compatibility lanes. Worker-side join uses the owner-lane
join path (rule 2), never `done_cv`.

### Rule 6 — Generation qualification for join/scope waiters

Join and scope waiter entries carry `seq == 0` (unqualified) and MUST NOT adopt
the channel `park_seq` generation qualification. Channel keys embed a reusable
address that can be freed and reallocated (needs `park_seq` to reject a stale
entry after address reuse, `rt_channel_lane.h:87-91`); join/scope keys embed a
monotonic never-reused id (task id / scope id), so a stale entry can only ever
name the one task/scope it was registered for — misdelivery into a different
target is impossible, and a wake-only stale entry costs at most one bounded
spurious wake absorbed by `wake_token`. The unqualified removal predicate
`seq == 0 || w.seq == seq` (`rt_async_waiter.c:159`) is correct for join/scope.

## Implementation Shape Preview (for Tasks 6-10)

- **Task 6 (create/publish):** realization (A) — id-alloc + growth + slot
  publish under control, ready-push under the owner shard; add the Task 5
  create-site `control_lock_acquired` counter; escalate to the segmented table
  (B) only on the >= 2.0/request trigger.
- **Task 7 (join poll + handle lifetime):** move the join register + result read
  to the target owner store lane (rule 2); clone/release/free per rule 1.
- **Task 8 (completion epilogue):** reorder `mark_done` result writes before the
  `TASK_DONE` release store (rule 1); drive `mark_done_needs_control` down to
  net-key + `done_waiters` (S6-Q1).
- **Task 9 (scope owner lane):** `ex->scopes` atomic snapshot; scope object
  bookkeeping + `scope_key` store on the scope owner shard; named control
  fallback cross-owner (rule 3).
- **Task 10 (await/runner/blocking compat):** keep `done_cv` / `compat_cv`
  external-only and counted separately (rule 5).

Each task drops the control lane from a path only in the same commit that proves
its owner-lane guardian (behavior test from Task 4 plus static gate from Task 5),
mirroring the Epic 7 additive-then-peel shape, and preserves `SURGE_SHARDS=1`
behavior and the `control -> at most one shard` lane order throughout.
