# Epic 4 FD Registry Dependency Map

Status: complete for Epic 4 Task 2 read-only mapping.

This map records the current net readiness lifecycle before any registry code
is added. All `file:line` references are pinned to baseline commit
`05ceb7c20b19e72125e320f07445959cb2b349bf`
(`chore(runtime): enforce Runtime V2 quality gates`). The working tree at
mapping time is `d7098fab` (docs-only commit on top of the baseline);
`git diff --stat 05ceb7c2 HEAD -- runtime/` is empty, so runtime code is
identical and every reference was re-verified against the working tree.

Scope guards:

- `N=1` only. `RT_RUNTIME_SHARD_COUNT` is `1U`
  (`runtime/native/rt_async_internal.h:127`) and all executor accessors route
  to shard 0.
- This is a docs-only task. `04-evidence.md` and `NOTES.md` updates belong to
  the main agent.
- Probes named here are pending evidence commands, not claimed passes
  (`LIVENESS_PROBES.md` closeout rule).

## Current Net Ownership Contract

- `ex->lock` owns tasks, scopes, the single shard waiter store, net
  waiter/poll scratch state, `net_polling`, timer state, and shutdown flags
  (invariant comment `runtime/native/rt_async_internal.h:248-268`).
- Net waiters live in the shard-owned `rt_waiter_store`
  (`runtime/native/rt_async_internal.h:93-98`), reached through the shard0
  compatibility accessors `rt_executor_waiter_store(_const)`
  (`runtime/native/rt_runtime.c:111-122`).
- Poll scratch is shard-owned state: `rt_net_poll_scratch` type
  (`runtime/native/rt_async_internal.h:139-144`), `rt_shard.net_poll_scratch`
  field (`runtime/native/rt_async_internal.h:156`), owner-first accessors
  `rt_shard_net_poll_scratch` / `rt_executor_net_poll_scratch`
  (`runtime/native/rt_runtime.c:72-80`, declared at
  `runtime/native/rt_async_internal.h:404-405`).
- Single-poller guard: `begin_net_poll` sets `ex->net_polling` only when net
  waiters exist (`runtime/native/rt_async_state.c:1074-1080`);
  `poll_net_waiters_owned` clears it and signals `io_cv` after the poll
  (`runtime/native/rt_async_state.c:1082-1087`). `has_net_waiters` reads
  `store->net_len` (`runtime/native/rt_async_state.c:1069-1072`).

## Lifecycle Paths

Each path answers Global Rule 2: who owns the state, who mutates it, who wakes
the task, who cleans up.

### Creation

- `rt_net_listen` (`runtime/native/rt_net.c:546`), `rt_net_connect`
  (`runtime/native/rt_net.c:605`), and `rt_net_accept`
  (`runtime/native/rt_net.c:684`) allocate heap `NetListener` / `NetConn`
  structs holding only `{int fd; bool closed;}`
  (`runtime/native/rt_net.c:44-52`).
- Ownership: the returned Surge handle owns the struct; the runtime keeps no
  durable readiness entry, no generation, and no shard-side record of the fd.
- Nothing is registered with the poller at creation time. Readiness state
  exists only while a waiter is parked.

### Wait Registration

- Entry points `rt_net_wait_accept` / `rt_net_wait_readable` /
  `rt_net_wait_writable` (`runtime/native/rt_net.c:888/897/906`) read the fd
  from the borrowed handle and call `net_wait_current_task`
  (`runtime/native/rt_net.c:841`).
- Under `ex->lock`, `net_wait_current_task`:
  - returns early (does not park) if the current task is cancelled
    (`current_task_cancelled` check, `runtime/native/rt_net.c:853-856`);
  - performs an immediate-readiness zero-timeout poll via `net_fd_ready_now`
    (`runtime/native/rt_net.c:817`, call at `runtime/native/rt_net.c:858`);
  - builds the waiter key with `net_accept_key` / `net_read_key` /
    `net_write_key` (switch at `runtime/native/rt_net.c:862-876`; key
    constructors `runtime/native/rt_async_waiter.c:42-55`);
  - pre-registers the waiter with `prepare_park` and hands the key to the
    yield path through thread-local `pending_key`
    (`runtime/native/rt_net.c:882-883`; `prepare_park` impl
    `runtime/native/rt_async_waiter.c:261-273`).
- The park is committed by `park_current`
  (`runtime/native/rt_async_state.c:1013-1047`), with `wake_token` rollback on
  wake-before-park (`runtime/native/rt_async_state.c:1022-1028` and
  `1035-1041`).
- `add_waiter` increments `store->net_len` for net keys via `net_waiter_added`
  (`runtime/native/rt_async_waiter.c:215-231` and `62-66`).

### Poll Readiness

- `poll_net_waiters` (`runtime/native/rt_net.c:915`) runs with `ex->lock`
  held, releasing it only around the `poll()` syscall.
- Capacity hint comes from `rt_executor_net_waiter_len`
  (`runtime/native/rt_net.c:917`; impl reads `store->net_len`,
  `runtime/native/rt_async_waiter.c:113-116`).
- Scratch arrays grow through `ensure_net_poll_fds` / `ensure_net_poll_pfds`
  (`runtime/native/rt_net.c:240` and `262`) on the shard scratch fetched at
  `runtime/native/rt_net.c:921`.
- The fd set is rebuilt every cycle from a full waiter-store scan:
  `rt_executor_visit_net_waiters` (call `runtime/native/rt_net.c:928`; impl
  `runtime/native/rt_async_waiter.c:118-134` walks every store entry and
  filters with `waker_is_net`) feeds `collect_net_poll_fd`
  (`runtime/native/rt_net.c:289-316`), which dedups fds with an O(n^2) linear
  scan and merges read/write interest per fd. The full-store scan is counted
  at `runtime/native/rt_net.c:926` (`io_waiter_scan_entries`).
- The wake pipe read end occupies poll slot `pfds[0]`
  (`runtime/native/rt_net.c:933` init, `952-958` slot fill).
- Unlock / `poll()` / relock happens at `runtime/native/rt_net.c:972-978`.
- Poll-error path completes ALL rebuilt keys, waking every parked net waiter
  regardless of which fd failed (`runtime/native/rt_net.c:979-991`).
- Readiness wakes go by fd/kind key: read-ready completes both
  `net_read_key(fd)` and `net_accept_key(fd)`, write-ready completes
  `net_write_key(fd)` (`runtime/native/rt_net.c:1003-1021`) via
  `complete_net_waiters` (`runtime/native/rt_net.c:234-238`) ->
  `rt_executor_wake_net_waiters_for_key`
  (`runtime/native/rt_async_waiter.c:136-163`), which removes every matching
  entry and wakes each live task with `wake_task`.
- Three poll-cycle callers:
  - `next_ready` (`runtime/native/rt_async_state.c:1135-1177`): zero-timeout
    poll at `1140`, timer-bounded poll at `1151`, indefinite poll at `1166`,
    with `io_cv` waits at `1155` and `1170` when another poller owns
    `net_polling`.
  - `rt_worker_main` idle loop: 1 ms slice
    (`worker_net_poll_slice_ms`, `runtime/native/rt_async_state.c:1533`; poll
    attempt `1547-1556`).
  - `rt_io_main` (`runtime/native/rt_async_state.c:1660-1730`): 50 ms slice
    (`poll_slice_ms`, `1669`), poll at `1715`, then drains up to 16 ready
    tasks (`net_ready_drain_limit`, `1670`; drain loop `1716-1720`) through
    `run_ready_one_nowait_locked` (`runtime/native/rt_async_state.c:1610`).

### Close

- `rt_net_close_listener` (`runtime/native/rt_net.c:656`) and
  `rt_net_close_conn` (`runtime/native/rt_net.c:670`) set `closed = true`,
  `close()` the fd, and set `fd = -1`. Double close returns `NotConnected`.
- They never wake or remove parked net waiters for that fd, never kick the
  poller, and never invalidate the fd's keys. A waiter parked on a closed fd
  stays in the store until `poll()` reports the fd (or an fd-reuse readiness
  event) completes its key.
- Because the waiter key id is the raw fd number (`waker_key`,
  `runtime/native/rt_async_internal.h:76-79`; constructors
  `runtime/native/rt_async_waiter.c:42-55`), numeric fd reuse can wake
  old-lifetime waiters. There is no generation or close-state guard.

### Cancellation

- `cancel_task` (`runtime/native/rt_async_state.c:1308-1329`) marks the task
  cancelled and, if it is `TASK_WAITING`, calls `wake_task(ex, id, 1)`
  (`1323-1325`), which removes the `park_key` waiter entry and clears
  `park_key` inside `wake_task_with_policy`
  (`runtime/native/rt_async_state.c:941-946`).
- Stale, done, or cancelled net entries are also dropped lazily during
  completion scans: `rt_executor_wake_net_waiters_for_key` skips them
  (`runtime/native/rt_async_waiter.c:153-155`), and `pop_waiter` drops them
  while scanning (`runtime/native/rt_async_waiter.c:288-292`).
- `mark_done` clears `wait_keys`, select timers, and any remaining `park_key`
  (`runtime/native/rt_async_state.c:1331-1345`).
- Cancellation does not wake the poller: an already-running `poll()` keeps the
  cancelled task's fd in its rebuilt set until the next rebuild drops the
  removed waiter.

### Wake-fd

- The wake pipe is a pair of process-global statics
  `net_poll_wake_read_fd` / `net_poll_wake_write_fd`
  (`runtime/native/rt_net.c:72-73`). It is not shard-local state.
- Lazy init happens inside the poll rebuild: `net_poll_wake_init`
  (`runtime/native/rt_net.c:187-203`), called at
  `runtime/native/rt_net.c:933`. The pipe is never closed.
- Writer: `rt_net_wake_poll` (`runtime/native/rt_net.c:205-215`). Its only
  caller is `park_current`, which kicks the poller after committing a park on
  a net key (`runtime/native/rt_async_state.c:1043-1045`).
- Drain: `net_poll_wake_drain` (`runtime/native/rt_net.c:217-232`), called
  when `pfds[0]` reports readiness (`runtime/native/rt_net.c:998-1002`).

### Shutdown

- `ex->shutdown` (field `runtime/native/rt_async_internal.h:233`) is read at
  `runtime/native/rt_async_state.c:1154`, `1169`, `1515`, `1547`, `1553`,
  `1562`, `1678`, and `1716`, but no writer sets it to `1` anywhere in
  `runtime/native`. Grep evidence (exact command and the complete relevant
  output):

  ```
  $ rg -n 'shutdown' runtime/native
  runtime/native/rt_async_state.c:1154:                    if (ex->net_polling && !ex->shutdown) {
  runtime/native/rt_async_state.c:1169:            if (ex->net_polling && has_net_waiters(ex) && !ex->shutdown) {
  runtime/native/rt_async_state.c:1440:    // Compensation workers live until executor shutdown; their context is process-lifetime.
  runtime/native/rt_async_state.c:1515:    while (!ex->shutdown && task->resume_kind == RESUME_NONE &&
  runtime/native/rt_async_state.c:1547:        while (!ex->shutdown && !worker_next_ready(ex, worker_id, &id)) {
  runtime/native/rt_async_state.c:1553:                if (ex->shutdown || worker_next_ready(ex, worker_id, &id)) {
  runtime/native/rt_async_state.c:1562:        if (ex->shutdown) {
  runtime/native/rt_async_state.c:1678:        if (ex->shutdown) {
  runtime/native/rt_async_state.c:1716:            for (int i = 0; i < net_ready_drain_limit && !ex->shutdown; i++) {
  runtime/native/rt_async_internal.h:233:    uint8_t shutdown;
  runtime/native/rt_async_internal.h:239:    uint8_t blocking_shutdown;
  runtime/native/rt_async_internal.h:252://   shutdown flags.
  runtime/native/rt_async_internal.h:267://   registered, or when shutdown changes. Workers sleep on ready_cv only after they
  runtime/native/rt_async_blocking.c:62:        while (ex->blocking_head == NULL && !ex->blocking_shutdown) {
  runtime/native/rt_async_blocking.c:65:        if (ex->blocking_shutdown && ex->blocking_head == NULL) {
  runtime/native/rt_async_blocking.c:125:    ex->blocking_shutdown = 0;
  ```

  The only assignment in the output is `ex->blocking_shutdown = 0;`
  (`runtime/native/rt_async_blocking.c:125`, initialization). `ex->shutdown`
  itself is only ever zero (static zero-initialization of `exec_state`).
- Recorded as fact: no executor shutdown/drain contract exists today. Workers,
  the I/O thread, and blocking workers observe flags that are never raised.
  Tasks 10-11 create the shutdown contract; this map only fixes ownership of
  the observation points.

## Symbol User Inventory

### `rt_net_poll_scratch`

Grep evidence: `rg -n 'rt_net_poll_scratch' runtime/native` (9 hits, all
listed):

| Ref | Role |
| --- | --- |
| `runtime/native/rt_async_internal.h:139-144` | Type definition (`fds`, `fds_cap`, `pfds`, `pfds_cap`). |
| `runtime/native/rt_async_internal.h:156` | By-value field `rt_shard.net_poll_scratch` (shard-owned). |
| `runtime/native/rt_async_internal.h:404-405` | Accessor declarations. |
| `runtime/native/rt_runtime.c:72-80` | `rt_shard_net_poll_scratch` (owner path) and `rt_executor_net_poll_scratch` (shard0 compatibility adapter). |
| `runtime/native/rt_net.c:240` | `ensure_net_poll_fds` grows the `NetPollFd` array. |
| `runtime/native/rt_net.c:262` | `ensure_net_poll_pfds` grows the `struct pollfd` array. |
| `runtime/native/rt_net.c:921` | Sole runtime consumer fetch inside `poll_net_waiters` (ensure calls at `923` and `940`). |

No destroy/free path exists for the scratch arrays; they live for the process
lifetime once grown.

### `rt_executor_visit_net_waiters`

Grep evidence: `rg -n 'rt_executor_visit_net_waiters' runtime/native` (3 hits,
all listed):

| Ref | Role |
| --- | --- |
| `runtime/native/rt_async_waiter.c:118-134` | Implementation: full `waiter_store` walk filtered by `waker_is_net`. |
| `runtime/native/rt_async_internal.h:435-436` | Declaration. |
| `runtime/native/rt_net.c:928` | Sole caller, inside the per-cycle poll rebuild. |

This is the exact "rebuild the poll set from the full waiter store" path the
FD Registry Contract forbids after Task 7.

### Net waiter key wakeups

Grep evidence: `rg -n 'net_accept_key|net_read_key|net_write_key|waker_is_net|net_len' runtime/native`,
`rg -n 'rt_executor_wake_net_waiters_for_key|rt_executor_net_waiter_len|rt_executor_waiter_len' runtime/native`,
and `rg -n 'rt_net_wake_poll|net_poll_wake' runtime/native`.

| Ref | Role |
| --- | --- |
| `runtime/native/rt_async_waiter.c:42-55` | Key constructors; key id is the raw fd number. |
| `runtime/native/rt_async_internal.h:389-392` | Constructor and `waker_is_net` declarations. |
| `runtime/native/rt_net.c:862-876` | Registration-side key build in `net_wait_current_task`. |
| `runtime/native/rt_net.c:983-987` | Poll-error completion of all read/accept/write keys. |
| `runtime/native/rt_net.c:1012-1018` | Readiness completion by fd/kind key. |
| `runtime/native/rt_async_waiter.c:57-60` | `waker_is_net` gate for net bookkeeping. |
| `runtime/native/rt_async_waiter.c:62-77` | `net_waiter_added` / `net_waiters_removed` maintain `store->net_len`. |
| `runtime/native/rt_async_state.c:1000-1006` | Duplicated `net_len` adjustment inside `wake_key_all_with_policy`. |
| `runtime/native/rt_async_state.c:1043-1045` | `park_current` kicks the wake pipe for net keys. |
| `runtime/native/rt_async_waiter.c:136-163` | `rt_executor_wake_net_waiters_for_key`: removes all entries for one key, wakes live tasks, drops stale ones. |
| `runtime/native/rt_net.c:234-238` | Sole caller `complete_net_waiters` (plus completion counters). |
| `runtime/native/rt_async_state.c:1069-1072` | `has_net_waiters` consumes `net_len` for poll gating. |
| `runtime/native/rt_async_waiter.c:113-116` | `rt_executor_net_waiter_len` consumes `net_len` for poll capacity (`runtime/native/rt_net.c:917`). |
| `runtime/native/rt_net.c:74-91`, `93-112` | `TRACE_NET` counters and dump format (`io_poll_*`, `io_direct_waits`, `io_waiter_*`). |
| `runtime/native/rt_net.c:139-144` | `rt_net_trace_dump`; runtime caller is `trace_dump_all` (`runtime/native/rt_async_trace.c:411-417`, net dump at `414`) used by the SIGUSR1/exit dump path. |

`wake_key_all` has no net-key producers today: its callers are blocking
completion (`runtime/native/rt_async_blocking.c:110`, `WAKER_BLOCKING`) and
scope child drain (`runtime/native/rt_async_state.c:1248`, `WAKER_SCOPE`);
`mark_done` wakes joiners through `wake_key_all_with_policy` with a
`WAKER_JOIN` key (`runtime/native/rt_async_state.c:1371`). The net-key branch
at `runtime/native/rt_async_state.c:1000-1006` is therefore a currently
dead defensive path that duplicates `net_waiters_removed`.

## Dependency Classification

Each row has exactly one owner class. "Migrates in" names the Epic 4 task that
moves or re-proves the row; "later-epic" rows do not move in Epic 4.

| Dependency | Owner class | Migrates in | Protected invariant |
| --- | --- | --- | --- |
| fd number + interest rows (`NetPollFd`, `collect_net_poll_fd` dedup/merge, `rt_net.c:54-58`, `289-316`) | registry-owned | T7 | One durable row per live fd; interest attached to the fd entry, not rediscovered by scanning waiters. |
| Poll scratch arrays (`rt_net_poll_scratch`, `rt_net.c:240/262/921`) | registry-owned | T5/T7 | Scratch may be rebuilt from the registry for `poll()`, never from the full waiter store. |
| Poll-set construction + wake-fd slot layout (`rt_net.c:928-970`) | registry-owned | T7 | Poll input derives from registry entries; slot 0 stays the wake pipe. |
| `net_len` capacity hint (`rt_async_waiter.c:62-77/113-116`, `rt_async_state.c:1069-1072`, `rt_net.c:917`) | registry-owned | T7 | Replaced by registry entry count; today it is a polling hint, not ownership. |
| Close state / generation guard (missing today; `NetListener.closed`, `rt_net.c:656-682`) | registry-owned | T5 shape, T9 behavior | Close removes future interest, wakes affected waiters exactly once, blocks stale completions. |
| fd-reuse stale-wake guard (missing today; raw-fd key id, `rt_async_internal.h:76-79`) | registry-owned | T9 | Reused numeric fd cannot wake waiters from the previous lifetime. |
| `NetListener` / `NetConn` fd linkage (`rt_net.c:44-52`, creation paths `546/605/684`) | registry-owned | T6 | Handle creation/close maps 1:1 onto registry entry lifetime. |
| Completion routing by fd/kind (`rt_executor_wake_net_waiters_for_key` lookup + removal, `rt_async_waiter.c:136-163`; caller `rt_net.c:234-238`) | registry-owned | T7/T9 | Readiness finds interested waiters through the fd entry, not a store scan. See split note below. |
| Task wake step inside net completion (`wake_task` call, `rt_async_waiter.c:158`) | waiter-owned | stays (re-proven T7) | Wake/park token protocol is the waiter store's contract, not the registry's. |
| Park/wake token protocol (`prepare_park` `rt_async_waiter.c:261-273`, `park_current` `rt_async_state.c:1013-1047`, `pending_key`, `wake_token`) | waiter-owned | stays | Wake-before-park races stay closed by `wake_token`; net registration keeps using `prepare_park`. |
| Cancellation semantics (`cancel_task` `rt_async_state.c:1308-1329`, lazy stale drops `rt_async_waiter.c:153-155/288-292`, `mark_done` `rt_async_state.c:1331-1345`) | waiter-owned | stays (T9 adds registry-side interest removal) | Cancel wakes only the cancelled task and leaves other waiters intact. |
| FIFO-by-key waiter store (`rt_waiter_store`, `add_waiter`/`remove_waiter`/`pop_waiter`) | waiter-owned | stays | Epic 3 contract; Epic 4 must not fork a parallel waiter list. |
| Wake pipe init/write/drain (`rt_net.c:72-73/187-232`) | wake-fd-owned | T11 | Interest changes while `poll()` may be blocked must wake the poller. |
| `pfds[0]` wake slot + drain-on-ready (`rt_net.c:933/952-958/998-1002`) | wake-fd-owned | T11 | Wake writes are observed by the running poll cycle. |
| `park_current` net kick (`rt_async_state.c:1043-1045`) | wake-fd-owned | T6/T11 | New net interest wakes a blocked poller; registry updates must preserve this edge. |
| `ex->shutdown` observation points (`rt_async_state.c:1154/1169/1515/1547/1553/1562/1678/1716`) | shutdown-owned | T10/T11 | Shutdown wakes the poller and drains registry state without stranding parked net waiters. |
| Missing shutdown writer/drain contract (grep evidence above) | shutdown-owned | T10/T11 | A raised shutdown flag must exist before drain behavior can be tested. |
| `wake_key_all_with_policy` net-`net_len` adjustment (`rt_async_state.c:1000-1006`) | registry-owned (dead-path cleanup) | T7 | Rule 5 duplication smell: same bookkeeping as `net_waiters_removed`; delete or unify once poll capacity comes from the registry. Checkable because all `wake_key_all(_with_policy)` producers are non-net (see inventory). |
| `N>1` shard scheduling, accept distribution / `SO_REUSEPORT`, cross-shard wake-fd protocol, `epoll`/`kqueue`/`io_uring`, crossing syntax, channel/timer/blocking waiter semantics | later-epic | out of Epic 4 | Recorded to keep implementation tasks from silently expanding scope. |

Split decision (Task 3 needs this unambiguous):
`rt_executor_wake_net_waiters_for_key` straddles two owners. The lookup and
removal of interested waiters by fd/kind key becomes registry-owned in
Tasks 7/9 (the registry entry knows its interests). The actual task wake —
`wake_task` with token/park-key cleanup — stays waiter-owned. Contract tests
must assert observable behavior (every readiness event completes the parked
task exactly once), not which module performed the store scan.

## Gaps And Hazards Feeding The FD Registry Contract

1. Close never wakes waiters: `rt_net_close_listener` / `rt_net_close_conn`
   close the fd and strand parked waiters until an unrelated poll event
   completes their key (`runtime/native/rt_net.c:656-682`).
2. fd reuse stale-wake hazard: keys are raw fd numbers with no generation
   (`runtime/native/rt_async_internal.h:76-79`), so a reused fd can wake
   previous-lifetime waiters.
3. Poll error completes every fd: one failing `poll()` wakes all parked net
   waiters (`runtime/native/rt_net.c:979-991`), hiding which fd was bad.
4. Cancelled fd stays polled: cancellation removes the waiter but does not
   kick the poller, so the in-flight poll set still contains the fd until the
   next rebuild.
5. Wake pipe is process-global, not shard-owned
   (`runtime/native/rt_net.c:72-73`) and is never closed. Open decision for
   Task 11: shard-owned wake-fd state vs. staying global under `N=1`. This map
   names the decision but does not resolve it.
6. Poll scratch is never freed; growth is monotonic for process lifetime.
7. Shutdown is never triggered: all observation points read a flag with no
   writer (grep evidence above), so no drain behavior exists to migrate — it
   must be created (Tasks 10-11).
8. Duplicated `net_len` bookkeeping between `net_waiters_removed`
   (`runtime/native/rt_async_waiter.c:68-77`) and `wake_key_all_with_policy`
   (`runtime/native/rt_async_state.c:1000-1006`) is a Rule 5 duplication
   smell; the second copy currently has no net-key producer.
9. Every poll cycle scans the entire waiter store (all kinds, not just net) to
   rebuild the fd set (`runtime/native/rt_async_waiter.c:118-134`), with
   O(n^2) fd dedup on top (`runtime/native/rt_net.c:300-306`). This is the
   central cost the persistent registry removes.

## First Safe Implementation Boundary (Task 5)

The first change that cannot break behavior:

- Add a shard-local fd registry container as a new `rt_shard` field beside
  `net_poll_scratch` (`runtime/native/rt_async_internal.h:152-160`), with
  owner-first accessors mirroring the `rt_shard_net_poll_scratch` /
  `rt_executor_net_poll_scratch` pattern (`runtime/native/rt_runtime.c:72-80`):
  `rt_shard_fd_registry` plus an `rt_executor_fd_registry` shard0
  compatibility adapter.
- Lifecycle APIs return explicit `rt_runtime_status` codes
  (`runtime/native/rt_async_internal.h:116-120`), per Global Rule 8
  (owner-first arguments, `init`/`free`, no `panic_msg` for recoverable
  errors).
- Zero readers: `net_wait_current_task` and `poll_net_waiters` do not touch
  the registry in Task 5. The container is compiled, initialized/freed with
  the shard, and statically tested (Task 4 shape guard) before any behavior
  routing.
- First behavior migration is Task 6: wait registration writes interest into
  the registry alongside `prepare_park`, while wake behavior is still driven
  by the waiter store.

Header placement note: `runtime/native/rt_async_internal.h` is currently at
499 lines against the 500-line Runtime V2 limit (Global Rule 4). Task 5 must
therefore place the fd-registry declarations in a new dedicated header
(candidate: `runtime/native/rt_fd_registry.h`) included from
`rt_async_internal.h`, instead of growing `rt_async_internal.h` past the
limit. The include keeps the declarations reachable for existing translation
units and for the Task 4 static-shape snippet, which includes only
`rt_async_internal.h`. The implementation belongs in a new cohesive
`runtime/native/rt_fd_registry.c` (already anticipated by
`04-tasks/05-registry-container-skeleton.md`), not in the over-limit
`rt_net.c`.

## Later Tests And Probes

Pending evidence commands for the migration tasks. Listed as pending only;
none of these was run for this docs-only task, and this map claims no passes.

- Net wakeup and live SIGUSR1 trace (`LIVENESS_PROBES.md` existing probe):
  `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMTNetWaiterWakeupLatency' -v --timeout 90s`
- Single-thread net plus sync channel compatibility
  (`LIVENESS_PROBES.md` existing probe):
  `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestNativeNetSingleThreadBlockingChannelInAsyncServer' -v --timeout 90s`
- FD registry wake persistence (`LIVENESS_PROBES.md` missing mandatory probe):
  does not exist yet; Tasks 3 and 8 own the fixtures (repeated readiness on
  one fd, duplicate waiters, cancellation of one waiter, fd close,
  re-registration under `SURGE_TRACE_EXEC=1`).
- Native net benchmark with hard outer cap (`LIVENESS_PROBES.md` existing
  probe; required for the performance-sensitive Task 7/12 evidence):
  `timeout 120s env SURGE=/path/to/current/surge ./scripts/bench_native_net.sh`
