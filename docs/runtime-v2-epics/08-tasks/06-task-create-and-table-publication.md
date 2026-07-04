# Epic 8 Task 6: Task Create And Table Publication

Task 6 output. Self-contained: it restates the runtime state it depends on
(`file:line` evidence at baseline `27eeabd7`, the tree after Task 5) and does
not assume the reader has the whole epic memorized. The authoritative lane
decisions live in `../08-lifecycle-lane-proving-spike.md` (S5-Q1, rule 1);
this document quotes what it implements and points back for the rest.

## Escalation Decision (binding constraint, not a choice this task makes)

Task 5's per-site baseline (8x1024 row, `SURGE_TRACE_EXEC=1`,
`08-evidence.md` Task 5 section) measured `ctrl_create=28673=3.500
control-acq/request`, which is `>= 2.0` — the numeric escalation criterion
`08-lifecycle-lane-proving-spike.md` fixed in advance. Task 6 therefore
implements realization **(B)**, the segmented never-moved-slot task table,
not realization (A) safe-minimal. This is not re-litigated here.

## Scope

- `runtime/native/rt_async_internal.h`: `rt_task_table`/new `rt_task_segment`
  shape, `next_id` becomes atomic, new decls.
- New `runtime/native/rt_task_table.c`: segment allocation (the one new
  primitive this task adds).
- `runtime/native/rt_async_state.c`: `get_task`, `rt_task_slot_store`,
  `rt_task_table_snapshot` reimplemented for segments; old `ensure_task_cap`
  moved out; `cancel_task` fixed (see Hazard below).
- `runtime/native/rt_async_task.c`: `__task_create` restructured.
- `runtime/native/rt_async_waiter.c`, `rt_async_trace.c`: the two full-table
  scanners updated to the new `rt_task_table_snapshot`/`get_task` iteration
  shape.
- Tests: `internal/vm/runtime_v2_lifecycle_static_test.go` (P6 skip deleted),
  new `internal/vm/runtime_v2_lifecycle_task6_cancel_spawn_race_test.go`
  (the cancel-vs-spawn race probe), `internal/vm/runtime_v2_owner_local_waiter_static_test.go`
  (a pre-existing stub's signature updated to compile against the new
  `rt_task_table_snapshot` return type — this file predates Epic 8 and is not
  a lifecycle behavior-test file).
- `Makefile`: `runtime-v2-lifecycle-check` regex gains
  `StaticCreateReadyPushOwnerShard` and `CancelSpawnChildrenRace`.

## Non-Goals

- Join polling, handle release/free protocol (Task 7).
- `mark_done`/completion epilogue (Task 8).
- Scope bookkeeping (Task 9).
- `rt_task_await`/runner/blocking compat (Task 10).
- `poll_ready_child_inline`'s own control bracket (S5-Q4, Task 7 territory —
  see Evidence below for why this task's measurement interacts with it).
- checkpoint/`rt_sleep` spawn (`spawn_internal_task_locked`,
  `rt_async_task.c:256-281`, unchanged: still control-held, still untagged
  `OTHER` per the Task 5 census).

## Design: Segmented Task Table

### Baseline state (what existed at `27eeabd7`)

`rt_task_table` was a copy-on-grow flat array (`rt_async_internal.h:246-249`
pre-Task-6): `{ size_t cap; _Atomic(rt_task*) slots[]; }`, swapped as a whole
via `_Atomic(rt_task_table*) tasks_table` on the executor. `get_task`
(`rt_async_state.c:356-365` pre-Task-6) did two acquire loads (table pointer,
then slot) — already lock-free-capable for lookup. `ensure_task_cap`
(`:443-490` pre-Task-6) grew by allocating a full replacement copy and
publishing it with release; retired generations were never freed (bounded by
doubling). The spike's `-DUNSAFE_PUBLISH` negative control proved the flaw
this leaves for realization (B): a shard-lane publisher that stores into a
table a concurrent control-lane growth is about to retire loses its slot (a
reader loading the *new* table sees `NULL`). That is why realization (A) kept
id-alloc + growth + slot-publish serialized under control, with only the
ready-push moved to the owner shard.

### New shape

```c
#define RT_TASK_TABLE_SEGMENT_SHIFT 12                       // 4096 slots/segment
#define RT_TASK_TABLE_SEGMENT_SIZE (1u << RT_TASK_TABLE_SEGMENT_SHIFT)
#define RT_TASK_TABLE_MAX_SEGMENTS (1u << 16)                // 65536 segments

typedef struct rt_task_segment { _Atomic(rt_task*) slots[RT_TASK_TABLE_SEGMENT_SIZE]; } rt_task_segment;
typedef struct rt_task_table { _Atomic(rt_task_segment*) segments[RT_TASK_TABLE_MAX_SEGMENTS]; } rt_task_table;
```

`rt_task_table` is now **embedded** in `rt_executor` (`tasks_table` is a
value, not a swapped pointer) — there is nothing to atomically swap at that
level any more. A segment, once allocated, is **never freed or moved**: this
is what removes the publish-vs-growth race the spike's negative control
caught, because an owner-lane publisher's `rt_task_slot_store` and a
concurrent segment allocation for a *different* id can never invalidate each
other's target memory. The directory itself (65536 pointers, 512KB) is fixed
size and never reallocated, so there is no second growth axis to protect.

This is the **same monotonic, never-freed growth shape the old table already
had** (retired generations were never freed either) — not new debt, just
redistributed from "N doublings of a shrinking-relative-size flat array" to
"one fixed 512KB directory of lazily-allocated 32KB segments." Per Global
Rule 5 (reuse before new machinery): the alternative of inventing a directory
that *also* grows (copy-on-grow of segment pointers) was rejected because it
reintroduces exactly the growth-vs-publish race this task exists to remove,
for no benefit — a fixed directory sized to ~268M task ids is effectively
unbounded for any real workload, and unused pages cost nothing until touched
(zero-fill-on-demand).

`get_task`/`rt_task_slot_store`/`rt_task_table_snapshot` **stay defined in
`rt_async_state.c`** rather than moving to the new file. This is required,
not stylistic: `TestRuntimeV2LifecycleStaticTaskTableAtomicSnapshot` (Task
5, active/green, not touched by this task) greps `rt_async_state.c`'s text
for the literal substrings `rt_task_slot_store` and `rt_task_table_snapshot`,
and greps `get_task`'s own function body for >= 2 occurrences of
`memory_order_acquire` plus the identifier `tasks_table`. `get_task` cannot
delegate to a helper without losing that proof. The bodies:

```c
rt_task* get_task(rt_executor* ex, uint64_t id) {           // 2 acquire loads, "tasks_table" — G3-compatible
    ...
    rt_task_segment* segment = atomic_load_explicit(&ex->tasks_table.segments[seg_idx], memory_order_acquire);
    ...
    return atomic_load_explicit(&segment->slots[slot_idx], memory_order_acquire);
}
```

`rt_task_table_snapshot`'s signature changed from `rt_task_table*` to
`uint64_t` (approved by main): it now returns an acquire snapshot of
`ex->next_id`, an exclusive upper bound on ids ever allocated, used by the
two full-table scanners (`clear_accept_winner_wait_keys`,
`rt_async_waiter.c`; the debug dump, `rt_async_trace.c`) as their
`get_task(ex, i)` loop bound instead of reaching into `rt_task_table`
internals directly — those internals are now private to
`rt_async_state.c`/`rt_task_table.c`. This is a tighter, more accurate bound
than the old `table->cap` (which over-counted to the next power-of-two), not
just a refactor for its own sake.

New file `rt_task_table.c` (41 effective lines) owns segment allocation:
`ensure_segment_locked` (caller holds control; double-checked alloc + zero +
release-publish) and the public `ensure_task_cap`/reuse contract unchanged
for its existing control-held callers (checkpoint/sleep spawn via
`spawn_internal_task_locked`; `rt_blocking_submit`, `rt_async_blocking.c` —
neither touched by this task, both still take control before calling it,
exactly as before). This is the one new primitive the task adds; it exists
because the old `ensure_task_cap`'s copy-on-grow protocol cannot give an
owner-lane publisher the lifetime guarantee it needs (the spike's proof).

### `next_id`

`ex->next_id` becomes `_Atomic uint64_t`. `__task_create`'s id-alloc uses
`atomic_fetch_add_explicit(&ex->next_id, 1, memory_order_relaxed)` (no lock).
Every other writer (`spawn_internal_task_locked` for checkpoint/sleep,
`rt_blocking_submit`, `exec_init_once`'s `ex->next_id = 1`, and several Go
test harnesses' `ex->next_id++`) is **unchanged text** — C11 atomics support
`++`/`=` directly, and mixing memory orders across RMW operations on the same
atomic object is well-defined (atomicity, not just ordering, is what
matters for correctness here).

## `__task_create` Restructure (`rt_async_task.c:8-79`)

```
id = atomic_fetch_add(&ex->next_id, 1)                 // no lock
if segment for id is missing:                           // lock-free peek
    rt_control_lock(ex); tag(CREATE); ensure_task_cap(ex, id); rt_control_unlock(ex)
alloc/init task (thread-local, no lock)
parent = rt_current_task()
if parent: rt_task_inherit_placement(task, parent)       // copies parent's owner shard
rt_task_assign_spawn_owner(task)                         // no-op if already placed
owner_shard = rt_task_owner_shard(ex, task)
rt_shard_lock(owner_shard)
    rt_task_slot_store(ex, id, task)                     // release
    if parent: task_add_child(parent, id)                // see Hazard below
    ready_push_task_locked(ex, owner_shard, task, 0, 0, 1)
rt_shard_unlock(owner_shard)
lane-aware compensation-worker check (mirrors wake_task's identical pattern)
return task
```

This is the spike-proven order: *assign owner -> shard-lock ->
slot-store(release) -> ready-push -> unlock*
(`08-lifecycle-lane-proving-spike.md` S5-Q1). The segment-growth branch is
the **only** control acquisition left on the steady-state create path, and it
is amortized: once a segment exists (after the first ~4096 ids), every
further id in it costs zero locks. The `rt_control_lock` + tag call stays
**literally inline in `__task_create`'s own body** (not delegated to a
helper) because `TestRuntimeV2LifecycleStaticCensusSitesTagged` (Task 5,
active/green) greps `__task_create`'s extracted body for the exact substring
`rt_trace_control_lock_site(RT_CTRL_SITE_CREATE)`.

`ready_push_task_locked` (existing, `rt_async_state.c`) is reused directly
rather than the higher-level `ready_push()` wrapper, so slot-store and
ready-push share **one** owner-shard critical section (not two sequential
acquisitions of the same lock) — matching the spike's proven interleaving
exactly. The wrapper's post-unlock compensation-worker side effect (for
sync-channel-blocked-worker scenarios, `RV2-DEBT-017`) is preserved
explicitly afterward, using the identical lane-aware pattern `wake_task`
already uses (`rt_async_state.c`, unchanged): an atomic peek at
`channel_blocked_workers`, and only if nonzero, a lane-aware
`rt_control_lock`/`maybe_start_compensation_worker_locked`/unlock. This is
the same reused idiom, not new machinery.

## Hazard Found And Fixed: `cancel_task` vs `task_add_child`

This was **not optional** — it is a data race Task 6's own change makes
possible, so the same commit closes it (main's requirement 1).

**The race.** Before this task, `task_add_child` (the parent's `children[]`
append) and `cancel_task`'s tree walk (`rt_async_state.c`, reads
`task->children_len`/`task->children[i]` directly) were both control-held,
hence mutually exclusive. Moving `task_add_child` off control (required —
any control acquisition left in create, even a tiny one, still counts fully
against `ctrl_create`/request, so a partial fix would not hit the escalation
target) opens a window: `rt_task_cancel` on a **running** parent (a supported
case — cancelling a task via handle while it executes is normal) can now run
concurrently with that parent appending a fresh child, on a different
thread, racing a `realloc`-backed array (potential use-after-free on the
`children` pointer, not just a stale read).

**The fix.** `task_add_child` now runs nested inside the *same* owner-shard
critical section `__task_create` already takes for slot-store + ready-push —
free, because a fresh child's owner shard is always identical to its
parent's (`rt_task_inherit_placement` copies it before the lock is taken).
`cancel_task` now snapshots a task's children ids under that task's own
owner shard lock before recursing (collect-then-recurse, mirroring the
collect-then-wake shape `wake_key_all`/
`rt_executor_wake_net_waiters_for_key_on_owner` already use): lock, `memcpy`
into an inline (or heap-fallback, for >8 children) buffer, unlock, then
recurse per copied id. Lane-legal: `cancel_task`'s callers all already hold
control; nesting one shard lock per recursion level (never two at once,
each released before the next is taken) is exactly "control lane before at
most one shard lock." This does **not** change `cancel_task`'s decided lane
(S5-Q5: control tree-walk + owner-shard per-task wake stays) — only the
mechanics of reading one struct field safely.

**Why this is sufficient (the invariant, written down per main's
requirement).** The fix's soundness rests on: *a parent's owner_shard_id is
never being concurrently rewritten by another thread while this thread reads
it to lock `task_add_child`'s critical section.* This is true because
`rt_task_replace_owner` (`rt_scheduler_placement.c:80-96`, the accept
transition, the epic's one named cross-owner edge) has exactly three call
sites, and only one of them can target a task other than the calling
thread's own:

- `rt_executor_wake_net_waiters_for_key_on_owner` (`rt_async_waiter.c:381`):
  targets a task popped from a waiter store batch — by construction that
  task was `TASK_WAITING` (parked), which cannot itself be executing
  `__task_create` at that moment. This is the **only** cross-thread,
  cross-task call site.
- `net_task_set_ready_accept` (`rt_net_accept_group.c:94-101`, called from
  `rt_net_wait_accept`, `rt_async_waiter.c`): operates on
  `rt_current_task()` — the calling thread's own task, synchronously.
- `rt_net_place_current_task_on_owner` (`rt_net_accept_group.c:104-111`):
  same — `rt_current_task()`, synchronously.

The two self-replace sites pose no race: a single thread's own sequential
operations (replace, then later spawn) are always consistent with
themselves, and the classic same-thread "lock handoff" argument (a release
of one lock sequenced-before a later acquire of a *different* lock, both by
the same thread, transitively carries prior writes to whoever later
acquires that second lock — happens-before is the transitive closure of
sequenced-before and synchronizes-with) means even a parent that
self-replaces its own owner *between* two of its own children's spawns
leaves both appends visible to a canceller that locks the parent's
*current* owner shard. **If a future task ever adds a path where a parent
can spawn while parked, or where a different thread replaces a task's owner
while that task is unambiguously running (not just popped-from-a-store),
this invariant breaks and must be re-derived before relying on it** — no
such path exists today (grep-verified, three call sites, enumerated above).

**Proof.** `internal/vm/runtime_v2_lifecycle_task6_cancel_spawn_race_test.go`
(new, self-contained, does not edit the Task 4 behavior-test files): 64
top-level parent tasks each spawn 24 children via `__task_create` in small
yielding batches while a separate non-worker pthread concurrently calls
`rt_task_cancel` on all of them across 30 passes.
`TestRuntimeV2LifecycleCancelSpawnChildrenRace` (deterministic build, added
to the enumerated `runtime-v2-lifecycle-check` regex) asserts no
panic/abort/hang and that children were actually spawned (the race was
exercised). `TestRuntimeV2LifecycleCancelSpawnChildrenRaceTSan` (same
scenario, `-fsanitize=thread -O1`, not enumerated — best-effort, skips if
clang lacks TSan) is the actual data-race oracle; it passed clean (zero TSan
reports) in this environment.

## Checks Run

- `git diff --check`: clean.
- `make c-check` (cfmt + strict warnings): OK.
- `make cppcheck`: OK (54 files, no findings).
- `timeout 1200s make runtime-v2-check`: exit 0, includes
  `runtime-v2-lifecycle-check` (all 9 Task-4 behavior contracts, all 7 Task-5
  static/trace gates, the newly-activated P6, and the new
  `CancelSpawnChildrenRace` gate — all green).
- `make check`: exit 0 (all Go packages, `golangci-lint`: 0 issues, `c-check`,
  file sizes).
- `./check_file_sizes.sh -a`: `rt_async_state.c` **1444** effective lines
  (down from 1455 at Task 5; ceiling 1580, not grown — removing the old
  `ensure_task_cap` (~48 lines) outweighed the `cancel_task` fix's ~20 added
  lines). `rt_task_table.c` 41 lines (new, well under the 500 cap).
  `rt_async_task.c` 307, `rt_async_internal.h` 535 — both OK.
- Sentrux (CLI `sentrux check <path>`): root quality **6175** (baseline
  6174), `runtime` **5294** (baseline 5296), `runtime/native` **5385**
  (baseline 5387) — all "All rules pass", no quality drop (differences are
  within normal noise for a scan taken at a different commit).

## Evidence: Before/After Measurement

Net bench `direct/seq`, 8 shards / 8 threads / 1024 connections / 8
req/conn = 8192 requests, `SURGE_TRACE_EXEC=1`
(`scripts/bench_native_net.sh`, `SURGE_NET_BENCH_SHARDS=8
SURGE_NET_BENCH_CONNECTIONS=1024 SURGE_NET_BENCH_MODES=direct
SURGE_NET_BENCH_PATTERNS=seq SURGE_NET_BENCH_REQUESTS=8`). "Before" is
Task 5's committed baseline (`27eeabd7`); "after" is this task's commit,
run 3 times, fully stable (`ctrl_create`, `ctrl_scope`, `ctrl_handle`
identical to the digit every run; `ctrl_join_poll`/`ctrl_completion` vary by
<1%, ordinary scheduling noise).

| Site | Before (total / per-req) | After (total / per-req) |
| --- | ---: | ---: |
| `control_lock_acquired` (all) | 215842 / 26.348 | 186593 / 22.780 |
| `ctrl_create` | 28673 / **3.500** | **8 / 0.001** |
| `ctrl_join_poll` | 31822 / 3.885 | 31792 / 3.881 |
| `ctrl_completion` | 4169 / 0.509 | 4141 / 0.506 |
| `ctrl_scope` | 106499 / 13.000 | 106499 / 13.000 |
| `ctrl_await_compat` | 1 / ~0 | 1 / ~0 |
| `ctrl_handle` | 1 / ~0 | 28673 / 3.500 |
| sum(sites) | 171165 / 20.894 | 171114 / 20.892 |
| residual `OTHER` | 44677 / 5.454 | 15479 / 1.890 |

`ctrl_create` drops from 3.500 to 0.001/request — the escalation's numeric
target, essentially eliminated. The residual 8 total acquisitions are
segment-growth events (`~28681` ids allocated / 4096 per segment `~= 7`,
matching), exactly the "rare, amortized" cost the design predicts.

**This is not the whole story, reported honestly (main's requirement 3).**
`sum(sites)` barely moved (171165 -> 171114, noise), because `ctrl_create`'s
drop (-28665) is almost exactly offset by `ctrl_handle`'s rise (+28672).
`ctrl_handle` tags five call sites (`rt_task_wake`/`poll_ready_child_inline`/
`rt_task_cancel`/`rt_task_clone`/`task_release_lane_aware`); this benchmark
does not call the public wake/clone/cancel builtins (Task 5's own evidence
already noted this — confirmed unchanged here). That leaves
`poll_ready_child_inline` (`rt_async_task.c:154-185`, unchanged by this
task) as the source: it takes a control bracket at its tail
(`rt_control_lock` + tag `HANDLE`, `:176-177`) whenever `rt_task_poll`'s
inline-child fast path fires — the exact "I just spawned this, and I'm
about to `.await()` it" pattern the benchmark's `write_owned(...).await()`
and `net.read_some(...).await()` calls produce. Before this task, that fast
path's precondition (`ready_take_current_local_tail`: the target must still
be the tail of the current worker's own local queue) apparently almost never
held (`ctrl_handle=1`); after, it holds essentially every time
(`ctrl_handle=28673`, matching `ctrl_create`'s old total near-exactly and
reproducing bit-for-bit across three runs). This is consistent with, and
predicted by, the dependency map's own S5-Q4 note: *"With create (S5.1) and
completion (S5.15) on the owner shard, can [poll_ready_child_inline] run
under the owner shard lane with no control acquire?"* — Task 7 owns that
surface and will remove this exact residual control bracket.

The **actual** net win — `control_lock_acquired` dropping by 3.570/request,
not merely 3.500 - 3.500 = 0 — comes almost entirely from the `OTHER`
residual (5.454 -> 1.890/request, a 3.564/request drop, matching the total
drop within noise). `OTHER` is dominated by checkpoint/sleep-spawn
acquisitions and other untagged control traffic whose *frequency* depends on
this benchmark's real-time scheduling progression (`tick_virtual`,
cooperative-checkpoint insertion at loop back-edges); this task did not
instrument that bucket further and does not claim a fully-attributed
mechanism for its size, only that it is real, reproducible, and a genuine
improvement (not a measurement artifact — the two `SURGE_TRACE_EXEC=1` runs
used the same fixture, connection count, and request count). Task 7, which
will directly touch `poll_ready_child_inline`/`rt_task_poll`, is
well-positioned to pin this down further with its own before/after rows.

## Task 7 Handoff Notes

- `get_task`'s contract is unchanged externally (same signature, same
  lock-free acquire-acquire semantics) — segmented internally, but every
  existing caller across the tree (`rt_async_channel.c`, `rt_async_scope.c`,
  `rt_async_select.c`, `rt_channel_lane.h`, `rt_waiter_route.c`,
  `rt_worker_turn.c`, `rt_async_poll.c`) needed no changes.
- `rt_task_table_snapshot` now returns `uint64_t` (a `next_id` bound), not a
  struct pointer. If Task 7/9 need a similar full-table scan, use the
  `get_task(ex, i)` + `rt_task_table_snapshot(ex)` bound pattern, not direct
  struct access (the segmented internals are private to
  `rt_async_state.c`/`rt_task_table.c`).
- `poll_ready_child_inline` now dominates the `HANDLE` bucket on this
  benchmark (see Evidence) — when Task 7 migrates its control bracket per
  S5-Q4, expect `ctrl_handle` to drop sharply and `control_lock_acquired`
  total to drop by a comparable amount.
- `cancel_task`'s children[] read is now owner-shard-lock-protected (see
  Hazard); any future change to `task_add_child`'s lane must keep this
  paired.

## Commit Boundary

One commit: segmented task table + `__task_create` restructure + the
`cancel_task` hazard fix + P6 activation + the new race-probe test + doc/
evidence/notes updates. No other lifecycle surface (join, completion,
scope, await) changes in this commit.
