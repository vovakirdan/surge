# Epic 8 Task 9: Scope Owner Lane

**Status:** Complete. **Depends on:** Tasks 7, 8. **Kind:** runtime code.

Moves same-owner scope enter/register/join-all/exit/failfast bookkeeping and the
`scope_key` waiter store off the control lane onto the scope's owner shard lane,
with a named counted control fallback for the rare cross-owner and failfast
cancel walks. Completes S6-Q1 (`mark_done_needs_control` reaches its final form:
net-key + `done_waiters`). Peels the P9 static gate including the
scope-reason-gone assertion Task 8's P8 deferred here.

Spike decisions implemented: S5-Q7 (scope table atomic snapshot), S5-Q8 (child
register + child-done on the scope owner shard), S5-Q9 (cross-owner cancel), S5-
Q10 (scope_key store on the scope owner shard, revising Epic 7 D8), S5-Q11 (scope
free), S5-Q14 (cancelled-poll teardown), S6-Q1 (surviving `mark_done` reasons).

## Baseline

Task 8 anchor row (`ae44d945`), 8 shards / 8 threads / 1024 connections / 8
requests-per-connection = 8192 requests, direct/seq, `scripts/bench_native_net.sh`,
`SURGE_TRACE_EXEC=1`. Re-measured with a fresh matching-commit build (the anchor
in `08-evidence.md` was a stale-binary capture; the numbers reproduce):

| Site | Baseline (fresh HEAD build) |
| --- | ---: |
| `control_lock_acquired` | 192262 |
| `ctrl_scope` | 106499 (13.00/req) |
| `ctrl_completion` | 28673 (3.50/req) |
| `ctrl_handle` | 29696 |
| `ctrl_join_poll` | ~2047 |

## Design

### Scope table = segmented atomic snapshot (S5-Q7, realization B)

`ex->scopes`/`scopes_cap` (a copy-on-grow pointer array read under control)
become a segmented never-moved-slot `rt_scope_table` (`rt_scope_table.c`, the
exact shape as `rt_task_table.c`, Global Rule 5). `get_scope` is now a lock-free
acquire load of `segment` then `slot` (both `memory_order_acquire`, mirroring
`get_task`), so scope object operations can read the scope off the control lane.

`rt_scope_enter` escalates to realization B (control-free publish), the scope
analogue of Task 6's S5-Q1 A→B escalation, because `ctrl_scope` is the epic's
largest control consumer and the near-zero target requires enter off control:
`next_scope_id` is an atomic `fetch_add`; segment growth is the only rare control
event (via `rt_scope_table_segment_missing`); the slot publish is a release store
into a never-moved slot; `owner->scope_id` is a thread-own write on the RUNNING
owner. **This revises S5-Q7's realization-A wording** (enter keeps control for
alloc/growth/publish); the escalation is recorded in the spike doc.

### Scope pinned to its birth shard (owner_shard_id)

`rt_scope` gains `uint32_t owner_shard_id`, set once at enter from the entering
task's owner shard and never changed. Every scope-object mutation and the
`scope_key` waiter store both serialize on that one shard's lock, stable for the
scope's whole life. This decouples the scope's serialization lock from the owner
TASK's mobility: F2 placement adoption (`rt_task_poll_adopt_placement`) can
re-place the scope-owning task mid-life, and resolving the lock through the mobile
owner would split a single scope's bookkeeping across two shard locks and race.
The scope stays pinned; the owner task, when woken off `scope_key`, is still
ready-pushed to its own current shard by `wake_task` (store-shard vs push-shard
are already decoupled by the collect-then-wake pattern).

### scope_key store on the scope owner shard (S5-Q10, revises Epic 7 D8)

`rt_waiter_route.c` `WAKER_SCOPE` (both `rt_waiter_store_for_key` and
`rt_waiter_key_shard`) resolves via `rt_scope_owner_shard(ex, get_scope(ex,
key.id))` — the scope's pinned shard store — instead of `ex->control_waiters`.
The generic waiter primitives (`prepare_park`/`add_waiter`/`remove_waiter`/
`pop_waiter`/`wake_key_all`) then take the pinned shard lock internally, exactly
as they do for join keys. No new primitive.

**Epic 7 D8 revision (recorded):** D8 kept `WAKER_SCOPE` on the control-owned
store because scope bookkeeping was entirely control-lane then, so a control-lane
store was the matching lane. Task 9 moves scope bookkeeping to the scope owner
lane, which changes the matching lane; keeping `scope_key` on control would force
every same-owner `join_all` completion back onto control (defeating the point).
The revision is a consequence of the scope-owner-lane decision, not a reversal of
D8's reasoning. `ex->control_waiters` remains only as the `rt_waiter_store_for_key`
default (unknown waker kind) and the diagnostic waiter dump.

### Owner-lane bookkeeping patterns

Because the waiter primitives take the shard lock internally, callers hold NO
shard lock across them. Scope object mutation runs under the pinned shard lock and
releases it before any park/wake/cancel, using two patterns already blessed here:

- **join_all** — register-then-verify (rt_task_poll's shape): read
  `active_children` under the pinned lock; if `>0`, `prepare_park(scope_key)`
  (locks internally), then RE-CHECK `active_children` under the pinned lock; if it
  hit 0, self-consume (`remove_waiter`) and return done. A child-done that drives
  active to 0 between the read and the registration is caught by the re-check; one
  that lands after wakes the registration; a RUNNING owner double-woken is
  absorbed by `wake_task_on_shard_locked`'s status gate.
- **child-done** (`scope_on_child_done`, from `mark_done`) and **register success**
  — mutate under the pinned lock, then wake `scope_key` outside it if active hit 0.

### Completion path and S6-Q1

`mark_done_needs_control` drops the `parent_scope_id`/`scope_registered` reason and
`mark_done`'s `park_needs_control` drops the `WAKER_SCOPE` term. Final form:
net-key + `done_waiters` (plus the `wait_keys_len`/`select_timers_len` compat
residual). The completion-side scope bookkeeping (`scope_on_child_done`) does the
common same-owner non-failfast child-done control-free under the pinned shard
lock; only cross-owner or failfast-triggering completions take the counted control
fallback (tagged `RT_CTRL_SITE_SCOPE`), so scope completion no longer forces the
control lane on the request hot path.

## Cancel-interplay re-derivation (rider on the control-free walk)

The plan proposed a fully control-free failfast cancel walk (snapshot-release-
walk). The re-derivation **does not close cleanly**, so the walk (and cross-owner
child-done) take a counted control fallback instead; same-owner child-done stays
control-free.

**Why the walk needs control.** The failfast/cancel walk runs
`cancel_task(child)` for each child. `cancel_task` reads the child's
`owner_shard_id` (via `rt_task_owner_shard`) to pick the shard lock for snapshotting
that child's own `children[]`. F2 placement adoption
(`rt_task_poll_adopt_placement` → `rt_task_replace_owner`) writes `owner_shard_id`
**under the control lane** (it acquires control precisely for this). `owner_shard_id`
is a plain non-atomic `uint32_t`; a control-free walk reading it races the
control-held F2 write — a data race — and breaks Task 6's owner-lock invariant
(`rt_async_task.c:71-93`), whose case (c) states the reader "holds control plus the
parent's current owner shard lock." Control is the mutual exclusion, not the shard
lock. Task 7's review record made the same point: F2 adoption's safety relied on
cancel walks and the adopt fallback both being on control. So the walk keeps
control (S5-Q9's literal counted fallback).

**Why cross-owner child-done needs control.** A re-placed (cross-owner) child's
`parent_scope_id`/`scope_registered` were published by `rt_scope_register_child`
under the OLD pinned shard lock, before the F2 control barrier. There is no
happens-before from that shard-lock release to a later read on the child's new
shard, so reading them at completion needs control. `scope_on_child_done` detects
cross-owner (`scope->owner_shard_id != task->owner_shard_id`) and takes control.

**Why same-owner child-done is control-free (the clawback path).** For a
same-owner child, the child's owner shard == the scope's pinned shard, so
`rt_scope_register_child` (success) and `mark_done`'s child-done both lock that one
shard. The register's `task_status_load(child)` under the lock plus the atomic
`TASK_DONE` store close the register-vs-early-completion leak in every
interleaving: register-first → decremented later; done-first → register sees DONE
and skips (no `active_children++`); concurrent → the shared pinned lock serializes
them. Proven by the multi-shard behavior tests under genuine multi-worker
contention.

## Lane invariant audit (never two shard locks; control before ≤1 shard)

- scope_enter: no lock (rare growth: control only). ✓
- register_child success: one pinned shard lock. ✓
- join_all: pinned shard lock and prepare_park's internal lock are sequential,
  never nested. ✓
- child-done (mark_done): same-owner pinned lock released before wake_key_all;
  cross-owner/failfast is control → pinned shard (release) → walk under control →
  child shard, one at a time. ✓
- failfast/cancel walk: control held, never the pinned shard lock, so cancel_task
  takes one child shard lock at a time. ✓
- scope shard vs task shard are different locks but never nested. ✓

`rt_lane.c` assertions stay always-on; the full blast-radius suite and the
no-keepalive completion-pin TSan (shards 1/2/8) are green.

## Scope lifetime and stale-key story (Q1 rider)

Scope ids are monotonic and never reused within a run (`next_scope_id` fetch_add);
a slot cleared on `scope_exit` is release-stored NULL and the id is never
reallocated. A late/racing `scope_key` remove/wake for id N can therefore only
name the one scope N; `get_scope(N)` returns the live scope or NULL (freed) →
`rt_scope_owner_shard(NULL)` routes to shard 0, where the drain finds no matching
entry. No generation qualification is needed — the same S9-Q7/rule-6
monotonic-never-reused-id argument as join/scope waiters (`seq == 0`).

**Scope pointer lifetime rule** (mirrors rule 1): a scope pointer / `owner_shard_id`
is dereferenced only while a registration or active child exists — join_all park,
register, child-done/failfast of a still-registered child, and the routing lookups
those drive. All are causally before `scope_exit`'s free, which runs only after
`active_children == 0` and the owner has stopped joining, and after every
`scope_key` waiter has drained. So routing never dereferences a freed scope, and
`scope_exit_locked` clears the slot (release NULL) before freeing the struct.

## Measurement (8x1024 direct/seq, 8192 req, fresh matching-commit builds)

| Site | Before | After | Δ |
| --- | ---: | ---: | ---: |
| `control_lock_acquired` | 192262 | 105285 | -86977 (-45%) |
| `ctrl_scope` | 106499 | 19464 | -87035 (-82%) |
| `ctrl_completion` | 28673 | 28673 | 0 |
| `ctrl_handle` | 29696 | 29696 | 0 |
| `ctrl_join_poll` | 2047 | 2039 | ~0 |
| `ctrl_create` | 10 | 8 | ~0 |

`ctrl_scope` drops 82% (enter/register/join-all/exit now off control on the
same-owner steady path). The residual `ctrl_scope=19464` is the cross-owner
`scope_on_child_done` control fallback: net-wrapper request children are F2-adopted
to the accepting shard, which frequently differs from their scope owner's shard,
so their child-done takes the counted cross-owner control path (legitimately, per
the re-derivation). Total control-lane acquisitions drop 45%.

### `ctrl_completion` finding (corrects Task 8's clawback attribution)

`ctrl_completion` is bit-identical 28673 before and after, i.e. **scope-independent**.
Task 8's DEBT.md/RV2-DEBT-016 note attributed the 28673 to the scope reason
(`parent_scope_id`/`scope_registered` in `mark_done_needs_control`) and reassigned
its clawback to Task 9. That attribution was a misdiagnosis. Two proofs:

1. Removing the scope reason entirely (P9 asserts it is gone) leaves
   `ctrl_completion` unchanged.
2. A throwaway split-tag probe (tag `mark_done`'s control `COMPLETION` only when
   `park_needs_control` is a net key, else `AWAIT_COMPAT`; reverted) moved the
   entire 28674 to `await_compat` and drove `ctrl_completion` to 0.

So those completions take control via `mark_done_needs_control`'s `wait_keys_len >
0` branch: net-wrapper children carry NET keys in `wait_keys[]`, and
`clear_wait_keys` at completion removes them, which scans shards for the net
registry — the net/accept-contract removal S6-Q1 explicitly keeps out of this epic
(net-key removal stays), reached via the `wait_keys` array rather than `park_key`.
`ctrl_completion=28673` is therefore a net-handle/accept residual, not scope, and
RV2-DEBT-016's clawback note is corrected accordingly.

## Files

- New: `runtime/native/rt_scope_table.c` (41 LOC).
- `rt_async_internal.h`: `rt_scope.owner_shard_id`, `rt_scope_table`/segment types,
  `scopes_table`, atomic `next_scope_id`, scope helper decls; +12 effective LOC
  (543→555, green ≤575).
- `rt_async_state.c`: segmented `get_scope` + `rt_scope_slot_store`; removed the
  array `ensure_scope_cap`/`ensure_ptr_array_cap`; `mark_done_needs_control` and
  `mark_done` scope-reason removal; `mark_done` scope block → `scope_on_child_done`;
  apply_poll_outcome cancelled teardown restructured (register-then-verify);
  removed `scope_cancel_children_locked`/`scope_child_done_locked`. Net **1447 →
  1377 effective LOC** (shrank 70, ≤1580 ceiling).
- `rt_async_scope.c`: enter/register/join_all/cancel_all/exit on the pinned shard
  lane; `rt_scope_owner_shard`, `scope_on_child_done`,
  `scope_cancel_children_controlled`, `scope_exit_locked`. 167→296 LOC.
- `rt_waiter_route.c`: `WAKER_SCOPE` → scope owner shard store (both functions).
- Tests: `runtime_v2_lifecycle_static_test.go` P9 activated (+ scope-reason-gone);
  `runtime_v2_lifecycle_behavior_scope_test.go` three `...AcrossShards` tests;
  `Makefile` lifecycle-check regex additions.

## Gates

`git diff --check` clean; `make c-check`, `make cppcheck`, `make check`,
`make runtime-v2-check` (incl. lifecycle-check with P9 + across-shards) pass; the
no-keepalive `CompletionPinInterleavingTSan` is green at shards 1/2/8, TSan-clean;
`./check_file_sizes.sh -a` passes (deltas above); bench before/after captured with
matching-commit builds. Sentrux scoped rescan recorded in `08-evidence.md`.
