# Epic 8 Lifecycle Lane Dependency Map

Task 2 output. This map records what the **control lane** (`rt_control_lock`,
`ex->lock`) still protects on the task-lifecycle path today, who reads and
writes each piece of state, who wakes whom, and the target lane after the
Epic 8 migration. It mirrors `07-executor-lock-dependency-map.md` for the
lifecycle surfaces Epic 7 left on control.

Lane names carry over from Epic 7: **control** (the reduced global lock),
**shard** (the owning shard's `rt_shard.lock`), **atomic** (a designated
atomic with documented ordering), **immutable** (written once before threads
exist), **tls** (thread-local), **blocking** (the blocking pool's own lock),
**compat** (a named compatibility lane that keeps control but is counted
separately from request steady state).

Target lanes marked **(spike)** are Task 3 decisions; the direction the epic
document fixes is stated, and each open question is phrased so Task 3 answers
it with a yes/no or a concrete protocol choice, never "investigate". Task 3's
spike output rewrites this map's lane table on conflict (index rule); Tasks 4
and 5 must not start until both are reconciled.

Baseline commit for all anchors: `daeac51e` (Task 1 kickoff-baseline record;
tree after the Task 1 generation-qualified-removal fix). Line numbers were
re-verified against this tree and match the Task 1 census
(`08-evidence.md`).

## 1. Lifecycle Surface Inventory

The 16 steady-path control sites Epic 8 targets (`08-evidence.md` census),
with current `file:line`:

| # | Surface | Site | Class |
| --- | --- | --- | --- |
| 1 | task create/publish | `rt_async_task.c:15` (`__task_create`) | every user spawn |
| 2 | handle wake | `rt_async_task.c:62` (`rt_task_wake`) | external/parent wake |
| 3 | worker join poll | `rt_async_task.c:88` (`rt_task_poll`) | every await poll |
| 4 | inline child poll | `rt_async_task.c:167,173` (`poll_ready_child_inline`) | join fast path |
| 5 | cancel | `rt_async_task.c:229` (`rt_task_cancel`) | cancellation |
| 6 | handle ref clone | `rt_async_task.c:243` (`rt_task_clone`) | handle refcount |
| 7 | checkpoint spawn | `rt_async_task.c:289` (`checkpoint`) | checkpoint create |
| 8 | sleep spawn | `rt_async_task.c:300` (`rt_sleep`) | sleep-task create |
| 9 | scope enter | `rt_async_scope.c:10` (`rt_scope_enter`) | scope bookkeeping |
| 10 | scope register child | `rt_async_scope.c:45` (`rt_scope_register_child`) | scope bookkeeping |
| 11 | scope cancel-all | `rt_async_scope.c:84` (`rt_scope_cancel_all`) | scope bookkeeping |
| 12 | scope join-all | `rt_async_scope.c:100` (`rt_scope_join_all`) | scope bookkeeping |
| 13 | scope exit | `rt_async_scope.c:134` (`rt_scope_exit`) | scope bookkeeping |
| 14 | handle release/final free | `rt_async_state.c:1429` (`task_release_lane_aware`) | lifetime/free |
| 15 | completion epilogue | `rt_async_state.c:1508` (`mark_done`), gate `1486` (`mark_done_needs_control`) | done path |
| 16 | cancelled-poll scope teardown | `rt_async_state.c:1586` (`apply_poll_outcome` cancelled branch) | done path |

Named compatibility sites (stay on control, counted separately, **not**
steady-state targets):

| Surface | Site | Note |
| --- | --- | --- |
| external await (`workers>1`) | `rt_async_task.c:193-209` | `done_cv` wait; `done_waiters` gate |
| single-worker await (N=1) | `rt_async_task.c:213-217` | `run_until_done` runner bracket |
| N=1 runner | `rt_async_poll.c:155` (`run_ready_one`), `237` (`run_until_done`); `rt_worker_turn.c:16` (`rt_run_ready_one_nowait_locked`) | legacy runner |
| poll bracket | `rt_async_poll.c:102` (`poll_task`) | checkpoint inline poll under control |
| sync-channel compat | `rt_async_compat.c:132,159` (`rt_wait_current_worker_wakeup`) | `compat_cv`; `RV2-DEBT-017` |
| blocking submit/completion | `rt_async_blocking.c:119` (completion wake), `237` (submit) | pool boundary |
| select slow lane (x3) | `rt_async_select.c:43,149,250` | **named non-goal**; keep `seq==0` wake-only entries working |
| shutdown | `rt_shutdown.c:8` (request), `22` (net waiter drain) | infrastructure |

## 2. State Ownership By Struct

### `rt_executor` (`rt_async_internal.h:251-290`)

| Field | Written by (today) | Read by | Target lane |
| --- | --- | --- | --- |
| `next_id` (`:252`) | `__task_create:16`, `spawn_internal_task_locked:253`, blocking submit | same | control (id allocation stays control) |
| `next_scope_id` (`:253`) | `rt_scope_enter:16` | same | control **(spike)**: same-owner scopes may allocate off control |
| `now_ms` (`:256`) | clock tick/advance | sleep/timer readers | atomic (Epic 7, unchanged) |
| `tasks_table` (`:258`) | `ensure_task_cap:489` (grow, release), `rt_task_slot_store:379` (publish/clear) | `get_task:360-364` (acquire) | control for grow/publish/clear; lock-free acquire reads **(spike)** |
| `scopes`,`scopes_cap` (`:259-260`) | `ensure_scope_cap`, `scope_exit_locked:168` | `get_scope:386` | control **(spike)**: scope table vs owner store |
| `lock` (`:261`) | — | — | remains the control mutex |
| `compat_cv` (`:264`) | `wake_task_with_policy:1044` broadcast | sync-compat waiters | compat (unchanged) |
| `done_cv` (`:265`) | `mark_done:1567` broadcast (gated) | external await `:199` | compat; broadcast gated on `done_waiters` |
| `shutdown` (`:270`) | `rt_shutdown.c` | worker/io/runner loops | atomic read + control write (unchanged) |
| `control_waiters` (`:286`) | scope-key add/remove/pop | scope-key wakes | control **(spike)**: scope keys stay here or move to owner store |
| `done_waiters` (`:289`) | `rt_task_await:197,201` | `mark_done_needs_control:1502`, `mark_done:1566` | atomic (Epic 7, unchanged) |
| blocking pool fields | blocking pool | trace | blocking (unchanged) |

### `rt_shard` (`rt_async_internal.h:149-174`)

Already shard-owned since Epic 7; lifecycle work targets these lanes rather
than adding fields: `lock`, `worker_cv`, `poller_cv`, `scheduler` (inject +
local queues + `running_count` + `wake_pending`), `waiter_store`,
`sleep_store`, `fd_registry`, `channel_blocking_compat`, `poller_nudges`.

### `rt_task` (`rt_async_internal.h:181-227`)

| Field group | Current protection | Target lane |
| --- | --- | --- |
| `status`,`enqueued`,`cancelled`,`wake_token`,`polling`,`handle_refs` (`:187-202`) | atomics; transitions under control today | atomic; transitions under owner shard lock |
| `id`,`poll_fn_id` (`:182-183`) | set at spawn | immutable |
| `owner_shard_id`,`owner_shard_valid`,`placement_class` (`:190-192`) | control at spawn; accept re-place | owner shard lock; accept transition keeps its protocol |
| `park_key`,`park_prepared`,`park_seq` (`:199,212-213`) | control | owner shard lock; `park_seq` is single-writer on the running poller (S4) |
| `wait_keys[]`,`select_timers[]` (`:214-223`) | control | owner shard lock (list belongs to the task; each registration to its key owner) |
| `resume_kind`,`resume_bits` (`:189,203`) | control (channel/blocking wakers + self) | owner shard lock; wakers write in the collect-then-wake step |
| `result_kind`,`result_bits` (`:185-186`) | written once in `mark_done` | owner shard lock at write; readers after `TASK_DONE` acquire |
| `scope_id`,`parent_scope_id`,`scope_registered`,`cancel_pending`,`children[]` (`:200-201,206-207,224-226`) | control | control **(spike)**: scope tree lane (S5.9-13) |
| `sleep_armed`,`sleep_delay`,`sleep_deadline` (`:198,204-205`) | Epic 7 sleep store | owner shard lock + clock lane (Epic 7, unchanged) |
| `net_ready_accept_*`,`timeout_task_id` (`:217-220`) | control / owner poll | owner shard lock (net + accept protocol, not this epic) |

### `rt_scope` (`rt_async_internal.h:229-239`)

Every field (`id`, `owner`, `failfast`, `failfast_triggered`,
`failfast_child`, `active_children`, `children[]`) stays **control** in Epic 7.
Target lane is the central Task 3 scope decision (S5.9-13, S9): the epic fixes
the *direction* (same-owner request trees move to the scope owner lane;
cross-owner keeps a named control fallback), not the field-by-field protocol.

### `rt_task_table` (`rt_async_internal.h:246-249`)

Atomic-snapshot structure already: `cap` + `_Atomic(rt_task*) slots[]`.
Growth publishes a fresh copy with release; retired generations are never
freed (doubling bounds them), so a lock-free reader never dereferences
reclaimed memory. This is the lifetime foundation S3 builds on.

## 3. Task-Table And Slot Protocol

| Operation | Site | Today | Target |
| --- | --- | --- | --- |
| id allocation | `__task_create:16`, `spawn_internal_task_locked:253` | `ex->next_id++` under control | control (monotonic, no reuse — epic fixed) |
| table growth | `ensure_task_cap:443-490` | control; alloc copy, release-publish; retired never freed | control (epic fixed: growth stays control-lane) |
| slot publication | `rt_task_slot_store:371-380` | control-held, release store | **(spike)**: owner-lane publish when capacity already exists |
| slot lookup | `get_task:356-365` | acquire load of table + slot (already lock-free-capable) | **(spike)**: lock-free read vs owner-locked deref per surface |
| slot clear | `free_task:1410` (`rt_task_slot_store(...,NULL)`) | control | control (tied to final free) |
| final free | `free_task:1388-1412` via `task_release*` | control (D3) | control (epic fixed: free requires control lane) |
| handle refcount | `task_add_ref:1381`, `task_release:1419-1426`, `task_release_lane_aware:1435-1449` | atomic fetch/sub; free under control | atomic refcount; free under control when last-ref DONE |

Preferred direction (epic doc "Task identity remains stable"): table growth
stays control-lane while steady-state slot publication and lookup avoid the
control lane when capacity already exists and the owner shard lock or the
atomic slot store provides the lifetime proof.

## 4. Waiter-Store Ownership And Generation Contract

The Task 1 blocker fix made the waiter store's generation protocol part of the
lifecycle ownership contract. Three moving parts, all of which Task 3 must
preserve when it moves join/scope registration off control:

1. **`park_seq`** (`rt_task` field `:212`) — per-task park generation. Bumped
   when a channel park registers (`channel_park_prepare_locked`,
   `rt_channel_lane.h:200-204,215-218`) and when the task consumes a delivered
   channel resume (`rt_async_channel.c:79,187`). Single-writer: only the task's
   own running poller writes it, so it needs no lock while the task is RUNNING.
2. **Entry `seq`** (`waiter.seq`) — copied from `park_seq` at registration
   (`rt_channel_lane.h:204,219`). Non-channel registrations store `seq==0`
   (`add_waiter:551,573`), which keeps unqualified behavior.
3. **Generation-qualified removal** — `remove_waiter_generation`
   (`rt_async_waiter.c:445-463`) removes only entries whose `seq` matches the
   captured generation (`remove_waiter_from_store_seq:142-174`, predicate
   `seq == 0 || w.seq == seq` at `:159`). The two deferred removers capture the
   generation under the owner lock and run the removal after releasing it:
   `wake_task_with_policy` (capture `:1007-1009`, deferred remove `:1013-1015`)
   and `park_current`'s abort path (capture `:1169-1172`, deferred remove
   `:1184`).

Validation side: `channel_candidate_valid` (`rt_channel_lane.h:87-91`) requires
`peer->park_seq == w->seq`, so a popped entry from a superseded park validates
false instead of misdelivering into a reused mailbox. The dedupe re-arm in
`channel_park_prepare_locked:197-219` re-stamps a leftover entry with a fresh
generation so a stale-`seq` entry cannot pop-and-drop the live park.

Join and scope keys today carry `seq==0` and use the unqualified path. The
open question for Task 3 is S9-Q7 (whether join/scope registration needs the
same generation qualification once it runs off the control lane).

## 5. Per-Surface Dependency Map

Each surface: current lock(s) with `file:line`; state read/written; who wakes
whom; target lane; lifetime/generation hazard; Task 3 open question(s).

### 5.1 Task create/publish — `__task_create` (`rt_async_task.c:8-44`)

- **Current locks:** control `:15-42`.
- **State:** writes `next_id:16`, `ensure_task_cap:17`, task alloc/init `:18-33`,
  `rt_task_slot_store:34`, `task_add_child` + `rt_task_inherit_placement:36-39`,
  `rt_task_assign_spawn_owner:40`; reads `rt_current_task():35`.
- **Wakes:** `ready_push:41` → owner shard `worker_cv` (via
  `ready_push_task_locked:701-706`).
- **Target lane:** control for id+growth; **(spike)** owner shard for
  slot-publish + ready-push when capacity exists (order control → shard).
- **Hazard:** child slot must be visible (release) before the owner worker pops
  the ready id; parent `children[]` append races cancellation.
- **Task 3 open question (S5-Q1):** Can steady-state publish+ready-push run
  under only the owner shard lock (no control) when `ensure_task_cap` did not
  grow the table — yes/no, and if yes, is the ordering "assign owner → shard-lock
  → slot store (release) → ready push"?

### 5.2 Handle wake — `rt_task_wake` (`rt_async_task.c:57-77`)

- **Current locks:** control `:62-76`.
- **State:** reads `task_from_handle:63`, status `:64`; scope adoption writes
  `target->parent_scope_id:72` from `get_scope:70`.
- **Wakes:** `wake_task:75` → owner shard.
- **Target lane:** owner shard for the wake; **(spike)** control only for the
  scope-adoption write.
- **Hazard:** `get_scope` read + `parent_scope_id` write is scope-tree state
  (S5.9-13 lane).
- **Task 3 open question (S5-Q2):** Is the scope-adoption write (`:69-74`) rare
  enough to keep on a control fallback while the wake itself uses the owner
  shard — yes/no?

### 5.3 Worker join poll — `rt_task_poll` (`rt_async_task.c:79-149`)

- **Current locks:** control `:88-147`.
- **State:** reads current/target status `:89-116`; consumes result
  `result_kind/result_bits:117-119`; `task_release:121`; register-then-verify
  `prepare_park:127` then DONE re-check `:133-145` with `remove_waiter:134` and
  `task_release:142`.
- **Wakes:** `wake_task:114`; inline child poll `:111`.
- **Target lane:** target task owner shard (join store add + result read);
  control only if the last-handle free fires here.
- **Hazard:** the register-then-verify window (`:129-132` comment) — target may
  complete between the DONE check and the join registration; the re-check after
  registering closes the stranded-entry race, and both sides serialize on the
  target owner's store lock.
- **Task 3 open question (S5-Q3):** After the join store moves to the target
  owner shard, does the register-then-verify recheck still close the race under
  the target-owner store lock alone (no control) — yes/no?

### 5.4 Inline child poll — `poll_ready_child_inline` (`rt_async_task.c:151-181`)

- **Current locks:** enters control-held; `rt_control_unlock:167` around the
  poll; `rt_control_lock:173` after; shard lock around `running_count`
  `:163-165,174-178`.
- **State:** `task_enqueued_store/status_store:160-161`, `running_count++/--`,
  `apply_poll_outcome:179`; `ready_take_current_local_tail:110` (caller) popped
  the fresh child off the current worker's local queue.
- **Wakes:** whatever `apply_poll_outcome` decides for the child.
- **Target lane:** child owner shard for scheduler accounting; the poll runs
  off any lock.
- **Hazard:** which locks are held at each step (Epic 7 hazard 5); child's
  scheduler accounting must stay on the child's owner shard; only the fresh
  just-created child (same owner as current worker) is eligible.
- **Task 3 open question (S5-Q4):** With create (S5.1) and completion (S5.15) on
  the owner shard, can this whole helper run under the owner shard lane with no
  control acquire — yes/no?

### 5.5 Cancel — `rt_task_cancel` (`rt_async_task.c:220-232`) / `cancel_task` (`rt_async_state.c:1458-1479`)

- **Current locks:** control `:229-231`.
- **State:** `task_cancelled_store:1469`; `rt_blocking_request_cancel:1471`;
  recurses `children[]:1476-1478`.
- **Wakes:** `wake_task:1474` if `TASK_WAITING`.
- **Target lane:** **(spike)** — cancel walks the child tree (control-owned
  today, S5.9-13 lane); per-task wake goes to that task's owner shard.
- **Hazard:** cleanup must stay proportional to the cancelled task's own
  registrations (epic "Cancellation stays bounded"); no whole-table or
  all-shard scan.
- **Task 3 open question (S5-Q5):** Does the child-tree walk stay on control
  (with per-task wake on the owner shard), or does it move to the scope/parent
  owner lane — pick one?

### 5.6 Handle clone — `rt_task_clone` (`rt_async_task.c:234-247`)

- **Current locks:** control `:243-245`.
- **State:** `task_add_ref:244` (atomic `handle_refs`).
- **Wakes:** none.
- **Target lane:** atomic — `task_add_ref` is already a relaxed atomic
  increment; the control lock only guards against a concurrent free.
- **Hazard:** clone must not race a last-ref free to zero (S5.14 lifetime rule).
- **Task 3 open question (S5-Q6):** Can clone drop the control lock and rely on
  the atomic refcount plus the rule "a live handle holder cannot observe its own
  target freed" — yes/no?

### 5.7 / 5.8 Checkpoint + sleep spawn — `checkpoint` (`:284-293`), `rt_sleep` (`:295-304`)

- **Current locks:** control `:289-291`, `:300-302`; both call
  `spawn_internal_task_locked` (`:249-274`: `next_id++:253`,
  `ensure_task_cap:254`, `rt_task_slot_store:271`, `ready_push:272`).
- **Wakes:** `ready_push:272` → owner shard.
- **Target lane:** same as S5.1 (create) — control for id+growth, **(spike)**
  owner shard for publish+ready-push.
- **Hazard:** identical to S5.1; sleep tasks additionally arm the per-shard
  sleep store on their first poll (Epic 7 sleep lane, unchanged here).
- **Task 3 open question (S5-Q1 applies):** same as create.

### 5.9 Scope enter — `rt_scope_enter` (`rt_async_scope.c:5-38`)

- **Current locks:** control `:10-36`.
- **State:** `next_scope_id++:16`, `ensure_scope_cap:17`, scope alloc/init
  `:18-30`, `ex->scopes[id]=scope:31`, `owner->scope_id=id:34`.
- **Wakes:** none.
- **Target lane:** **(spike)** scope owner lane for same-owner trees; the epic
  fixes the direction, not the scope-table protocol.
- **Hazard:** scope id/table lifetime mirrors the task table but is **not** an
  atomic snapshot today (`get_scope:382-387` reads `ex->scopes` under control).
- **Task 3 open question (S5-Q7):** For the scope table to be readable off
  control, does it adopt the task table's atomic-snapshot protocol (S3), or do
  scope reads stay control — pick one?

### 5.10 Scope register child — `rt_scope_register_child` (`rt_async_scope.c:40-77`)

- **Current locks:** control `:45-76`.
- **State:** `scope_add_child:63`, `child->parent_scope_id/scope_registered:64-65`,
  `scope->active_children++:66`; failfast branch on an already-DONE cancelled
  child sets `failfast_triggered/failfast_child:69-70`,
  `scope_cancel_children_locked:71`.
- **Wakes:** `wake_task(scope->owner):73` on failfast.
- **Target lane:** **(spike)** scope owner lane (same owner as the registering
  task by construction).
- **Hazard:** register vs the child's own `mark_done` (S5.15) both touch
  `active_children` and `scope_registered` — they must serialize on one lane.
- **Task 3 open question (S5-Q8):** Do child registration and child completion
  (`scope_child_done_locked`) serialize on the scope owner shard lock alone —
  yes/no?

### 5.11 Scope cancel-all — `rt_scope_cancel_all` (`rt_async_scope.c:79-93`)

- **Current locks:** control `:84-92`.
- **State:** `scope_cancel_children_locked:91` → `cancel_task` per child.
- **Wakes:** per-child wakes via `cancel_task` (S5.5).
- **Target lane:** **(spike)** scope owner lane; cross-owner children keep the
  control fallback.
- **Hazard:** same bounded-cleanup rule as S5.5.
- **Task 3 open question (S5-Q9):** When a child's owner shard differs from the
  scope owner, does cancel take the named control fallback (control → child
  owner) rather than a second shard lock — yes/no?

### 5.12 Scope join-all — `rt_scope_join_all` (`rt_async_scope.c:95-127`)

- **Current locks:** control `:100-125`.
- **State:** reads `failfast_triggered:108`, `active_children:113`; parks on
  `scope_key(scope_id)` via `prepare_park:120`.
- **Wakes:** woken by `scope_child_done_locked:1377` (`wake_key_all` on
  `scope_key` when `active_children==0`).
- **Target lane:** **(spike)** scope owner lane; `scope_key` store is
  `ex->control_waiters` today (S2 `control_waiters`).
- **Hazard:** the `scope_key` waiter store lane (control vs scope owner shard)
  determines whether join-all parks and the child-done wake share a lane.
- **Task 3 open question (S5-Q10):** Do `scope_key` waiters move from
  `ex->control_waiters` to the scope owner shard's `waiter_store`, or stay on
  the control store — pick one?

### 5.13 Scope exit — `rt_scope_exit` (`rt_async_scope.c:129-148`) / `scope_exit_locked` (`:150-170`)

- **Current locks:** control `:134-147`.
- **State:** `active_children>0` panic guard `:141`; `scope_exit_locked` clears
  `owner->scope_id:158`, frees `children` and the scope, `ex->scopes[id]=NULL:168`.
- **Wakes:** none.
- **Target lane:** **(spike)** scope owner lane; free of the scope object mirrors
  the task free lifetime rule (S5.14).
- **Hazard:** scope free vs a late cross-owner registration/cancel touching the
  same scope id.
- **Task 3 open question (S5-Q11):** Is scope free gated the same way as task
  free (only the owner lane frees, after `active_children==0`) — yes/no?

### 5.14 Handle release / final free — `task_release_lane_aware` (`rt_async_state.c:1429-1450`)

- **Current locks:** lane-aware — acquires control `:1442-1444` only when the
  drop is the last reference to a DONE task; `task_release:1414-1427` is the
  always-control variant used by control-held callers.
- **State:** `handle_refs` fetch-sub `:1439`; `free_task:1445` (clears slot,
  frees wait_keys/select_timers/children/task).
- **Wakes:** none.
- **Target lane:** atomic refcount; control for the actual `free_task` (epic
  fixed: free requires control). The lane-aware helper already realizes this.
- **Hazard (lifetime rule):** a joiner that consumes the last handle must not
  free the target while completion is still touching it — `mark_done` pins with
  `task_add_ref:1515` and drops via `task_release_lane_aware:1574` after the body.
- **Task 3 open question (S5-Q12):** Is the written rule "free only under
  control, only when `refs==1 && status==TASK_DONE`, with `mark_done`'s
  completion pin covering the body" sufficient for the join fast path to read
  results off control — yes/no?

### 5.15 Completion epilogue — `mark_done` (`rt_async_state.c:1508-1575`)

- **Current locks:** lane-aware — `need_control = !rt_lane_holds_control() &&
  mark_done_needs_control:1516`; control taken `:1517-1519` only when the exit
  owns control work; released `:1569-1571`.
- **State:** completion pin `task_add_ref:1515`; `clear_wait_keys:1521`,
  `clear_select_timers:1524`, `remove_waiter(park_key):1527`; sleep-store remove
  under the owner shard lock `:1531-1539`; `TASK_DONE` store `:1540`; result
  write `:1542-1543`; scope failfast + `scope_child_done_locked:1546-1564`;
  `wake_key_all(join_key):1565`; gated `done_cv` broadcast `:1566-1568`;
  `task_release_lane_aware:1574`.
- **Wakes:** joiners via `wake_key_all(join_key)` (collect-then-wake,
  `:1077-1137`); scope owner on failfast `:1557`; external awaiters via `done_cv`.
- **Target lane:** owner shard for task state + join wakes; control only for the
  residual reasons in S6. The pure shard-local exit already avoids control.
- **Hazard:** the completion pin (S5.14); join wake uses collect-then-wake so it
  never holds two shard locks; scope failfast touches the scope tree lane.
- **Task 3 open question (S5-Q13):** covered by S6 (which `mark_done_needs_control`
  reasons can be driven to zero on the request hot path).

### 5.16 Cancelled-poll scope teardown — `apply_poll_outcome` cancelled branch (`rt_async_state.c:1585-1613`)

- **Current locks:** lane-aware — `need_control = !rt_lane_holds_control():1589`;
  control taken `:1590-1592` for the scope branch only.
- **State:** reads `get_scope(task->scope_id):1593`; if children pending, sets
  `cancel_pending:1595`, `scope_cancel_children_locked:1596`, re-parks on
  `scope_key:1598-1600`; else `scope_exit_locked:1607`; then `mark_done:1613`.
- **Wakes:** child cancels (S5.5); self re-park.
- **Target lane:** **(spike)** scope owner lane (same as S5.9-13).
- **Hazard:** a cancelling task teardown that owns a scope must cancel children
  and park until they drain before completing.
- **Task 3 open question (S5-Q14):** Does this branch use the same scope owner
  lane as `rt_scope_*`, so a worker cancellation never takes control here —
  yes/no?

## 6. `mark_done_needs_control` Reasons (`rt_async_state.c:1486-1506`)

Each reason that forces the control lane on completion, and its classification:

| Reason | Site | Class | Epic 8 disposition |
| --- | --- | --- | --- |
| residual `wait_keys_len` or `select_timers_len` | `:1487` | multi-key park (select/timeout) | select is a **non-goal**; a normal join/channel task has none — stays compat |
| `parent_scope_id != 0` or `scope_registered` | `:1490` | scope membership | **target to remove**: same-owner scope completion on the scope owner lane (S5.10) |
| `park_key` is `WAKER_JOIN` | `:1498` | join removal resolves a foreign target | **target to remove**: join store on target owner (S5.3) makes removal owner-local |
| `park_key` is `WAKER_SCOPE` | `:1498` | scope key on control store | tied to S5-Q10 (scope-key store lane) |
| `park_key` is net | `:1498` | net removal scans shards | net/accept contract, **not** this epic — stays |
| `done_waiters > 0` | `:1502` | external await parked on `done_cv` | **compatibility** (epic fixed): counted separately, not hot-path debt |

Epic target: a task with no scope, no external `done_cv` awaiter, no residual
multi-key registration, and no foreign cleanup completes fully owner-shard-local
(epic "Completion is shard-local unless it really owns control work"). Reasons
that remain (net park removal, `done_waiters`) are documented as compatibility,
not unresolved hot-path debt.

**Task 3 open question (S6-Q1):** After S5.3 (join store on target owner) and
S5.10 (scope on owner lane), do the only surviving `mark_done_needs_control`
reasons on the request hot path become net-key removal and `done_waiters>0`
(both compatibility) — yes/no?

## 7. Accept Transition (The One Named Cross-Owner Edge)

`rt_task_replace_owner` (`rt_scheduler_placement.c:80-96`) is the only
post-spawn owner change (epic "Keep spawn local by default"). Driven from the
net accept-group / completion path, it re-places a parked connection task onto
the accepting shard. Because join waiters live in the owned task's store, it
migrates them: `old_shard_id != shard_id` → `rt_waiter_migrate_join_waiters:93`,
then `rt_task_set_placement:95`.

- **Lane today:** runs under control (net completion path).
- **Target lane:** the transition keeps its protocol; when the join store moves
  to the target owner shard (S5.3), the join-waiter migration is the mechanism
  that keeps a joiner's registration on the task's *current* owner.
- **Task 3 open question (S7-Q1):** Does the accept transition remain the single
  cross-owner lifecycle edge after the migration (i.e. no lifecycle surface
  introduces a second cross-owner owner change) — yes/no? If the spike finds
  another compat path producing a cross-owner edge, it must name that path's
  fallback (epic requirement).

## 8. Hazards The Migration Must Not Recreate

Mirroring `07-executor-lock-dependency-map.md` §7, plus lifecycle-specific
items:

- **Wake-vs-park race:** closed by the `wake_token` exchange in `park_current`
  (`:1153,1175`) and the leaf wake (`:967`). A wake between store-pop and
  owner-lock acquisition may produce at most one bounded spurious poll
  (`park_requeue_locked:1065-1075`).
- **Stale generation removal (Task 1 fix):** the deferred removals
  (`wake_task_with_policy:1013`, `park_current:1184`) run after the owner lock
  releases and MUST stay generation-qualified (S4); an unqualified removal eats
  a fresh re-registration and strands the park.
- **Register-then-verify (join):** `rt_task_poll:129-145` — moving the join
  store off control must keep the "register then re-check DONE under the target
  owner store lock" shape, or a target that completes mid-registration strands
  the joiner.
- **Completion pin:** `mark_done` pins the task (`:1515`) so a joiner woken
  mid-body cannot free it on another shard before the body finishes; the join
  fast path (S5.3) and free rule (S5.14) depend on this.
- **Duplicate ready enqueue:** guarded by `enqueued` under the owner shard lock
  (`ready_push_task_locked:699`, `wake_task_on_shard_locked:973`).
- **Scope failfast ordering:** `mark_done:1550-1558` and
  `rt_scope_register_child:67-74` both decide "first cancellation wins" —
  moving scope bookkeeping must keep a single serialization point for
  `failfast_triggered`.
- **No whole-table / all-shard scans in cancellation:** `cancel_task` recurses
  only the task's own `children[]` (S5.5); the net removal all-shard loop
  (`remove_waiter:493-509`) is net-key only and stays out of the lifecycle path.
- **`SURGE_SHARDS=1` behavior preserved** throughout (epic principle).
- **Stale invariant comment** (`rt_async_internal.h:292-304`) still describes
  the old executor-wide ownership ("`ex->lock` owns tasks/scopes, shard stores,
  scheduler queues/counters", "running_count increments under `ex->lock`").
  This is factually wrong post-Epic-7 and must be reconciled with the lane model
  before Epic 8 closeout (epic "Starting State" bullet).

## 9. Open Questions For The Task 3 Spike

Consolidated, each answerable yes/no or by a concrete protocol choice:

| ID | Question | Surface |
| --- | --- | --- |
| S5-Q1 | Owner-shard slot publish + ready-push when the table did not grow — yes/no; if yes, confirm order assign→shard-lock→slot-store(release)→ready-push. | create/checkpoint/sleep (5.1, 5.7-8) |
| S5-Q2 | Keep scope-adoption write on a control fallback while the wake uses the owner shard — yes/no. | wake (5.2) |
| S5-Q3 | Register-then-verify recheck closes the join race under the target-owner store lock alone — yes/no. | join poll (5.3) |
| S5-Q4 | Inline child poll runs entirely on the owner shard lane, no control — yes/no. | inline child poll (5.4) |
| S5-Q5 | Cancel child-tree walk stays control (per-task wake on owner) vs moves to scope/parent owner lane — pick one. | cancel (5.5) |
| S5-Q6 | Clone drops control, relies on atomic refcount + live-handle rule — yes/no. | clone (5.6) |
| S5-Q7 | Scope table adopts the task-table atomic-snapshot protocol vs scope reads stay control — pick one. | scope enter (5.9) |
| S5-Q8 | Child register and child-done serialize on the scope owner shard lock alone — yes/no. | scope register/done (5.10) |
| S5-Q9 | Cross-owner scope cancel uses named control fallback (control→child owner), never two shard locks — yes/no. | scope cancel-all (5.11) |
| S5-Q10 | `scope_key` waiters move to the scope owner shard store vs stay on `ex->control_waiters` — pick one. | scope join-all (5.12) |
| S5-Q11 | Scope free gated like task free (owner lane frees, after `active_children==0`) — yes/no. | scope exit (5.13) |
| S5-Q12 | Free rule ("control + `refs==1 && DONE`, `mark_done` pin covers the body") lets the join path read results off control — yes/no. | free (5.14) |
| S5-Q14 | Cancelled-poll scope teardown uses the same scope owner lane, never control in a worker — yes/no. | apply_poll_outcome (5.16) |
| S6-Q1 | After S5.3 + S5.10, do only net-key removal and `done_waiters>0` (both compat) remain as `mark_done_needs_control` reasons on the hot path — yes/no. | completion (6) |
| S7-Q1 | Accept transition stays the single cross-owner lifecycle edge after the join-store move — yes/no. | accept (7) |
| S9-Q7 | Do join/scope registrations need `park_seq`-style generation qualification once off control, or is `seq==0` (unqualified) still correct because their keys are single-target and not address-reused like channels — pick one. | waiter contract (4) |

### Target-Lane Summary

| Surface | Today | Target lane |
| --- | --- | --- |
| task create id + table growth | control | control (fixed) |
| task slot publish + ready push | control | owner shard **(spike S5-Q1)** |
| task lookup (`get_task`) | acquire load (control-era callers) | lock-free acquire / owner-locked deref **(spike)** |
| handle wake | control | owner shard (+ control scope-adoption fallback) **(spike S5-Q2)** |
| worker join poll + result read | control | target task owner shard **(spike S5-Q3)** |
| inline child poll | control (unlock around poll) | child owner shard **(spike S5-Q4)** |
| cancel (tree walk / per-task wake) | control | control tree + owner-shard wake **(spike S5-Q5)** |
| handle clone | control | atomic refcount **(spike S5-Q6)** |
| checkpoint/sleep spawn | control | control (id+growth) + owner shard (publish) **(spike S5-Q1)** |
| scope enter/register/cancel/join/exit | control | scope owner lane (named control fallback cross-owner) **(spike S5-Q7..Q11)** |
| handle release / final free | lane-aware (control for free) | atomic refcount + control free (realized) |
| completion epilogue | lane-aware (control on residual) | owner shard; control only for S6 residual reasons |
| cancelled-poll scope teardown | lane-aware (control scope branch) | scope owner lane **(spike S5-Q14)** |
| accept transition | control (net path) | owner-change + join-waiter migration (fixed, named edge) |
| external await / N=1 runner / sync-compat / select | control + `done_cv`/`compat_cv` | compat (counted separately; select a non-goal) |
