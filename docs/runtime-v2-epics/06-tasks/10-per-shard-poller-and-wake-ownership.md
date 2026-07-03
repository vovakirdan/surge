# Task 10: Per-Shard Poller And Wake Ownership

**Status:** Complete
**Kind:** runtime code
**Depends on:** Task 6, Task 7

## Context

Today there is exactly one I/O thread, one `poll()` call site, one
`net_polling` flag on the single `rt_executor`
(`rt_async_internal.h:216-247`, field `net_polling` at line 230), and one
process-global wake pipe:

```c
static int net_poll_wake_read_fd = -1;   // rt_net.c:67
static int net_poll_wake_write_fd = -1;  // rt_net.c:68
```

`net_poll_wake_init` (`rt_net.c:93-109`) lazily creates this one pipe;
`poll_net_waiters` uses it as the wake fd in the `poll()` set
(`rt_net.c:824`, `wake_fd = net_poll_wake_init() ? net_poll_wake_read_fd :
-1`). Each `rt_shard` already has its own `net_poll_scratch` buffer
(`rt_async_internal.h:139-144,155`) and its own `fd_registry`
(`:156`) — the per-shard *data* the poller reads already exists structurally
since Epic 4/5 — but there is exactly one poller loop and one wake mechanism
serving whichever shard's scratch it happens to be pointed at (today, always
shard 0's, since only shard 0 exists).

The Epic 6 Boundary Decisions paragraph draws the exact line this task must
respect:

> "Epic 6 owns per-shard net polling. Each shard that owns net fds must have
> a poller owner and a wake mechanism for registry changes, close,
> cancellation, and shutdown. The implementation must choose either a poller
> thread per shard or shard-worker-owned polling before net lifecycle
> migration starts."
>
> "Per-shard wake in Epic 6 is not the Phase 4 cross-shard transport. A pipe
> is an acceptable first wake mechanism. Eventfd, inbound message queues,
> target credits, and the seq-cst `PARKED` protocol remain Phase 4 work and
> must not be implemented or pre-decided here."

So this task's wake mechanism is deliberately simple (N independent pipes or
equivalent, one per shard, waking only that shard's own poller for its own
registry changes/close/cancellation/shutdown) — it is explicitly not the
`RUNTIME_V2.md` §8 cross-shard wake-elision protocol (seq-cst `PARKED` state,
producer/consumer races, wake-fd elision counters). Do not import that
protocol's complexity here; it belongs to a future cross-shard messaging
epic.

## Goal

Give every shard that owns net fds its own poller owner and its own local
wake mechanism for registry changes, close, cancellation, and shutdown —
without implementing or pre-deciding the Phase 4 cross-shard transport.

## Why This Task Exists

Without this task, Task 11 (net lifecycle migration) has nowhere to route
per-shard readiness: there would be `N` shards with their own fd registries
but only one poller physically capable of servicing one of them. This is
also one of the concrete gaps identified in the Epic 6 draft review before
task-cutting began — the epic previously required "per-shard net poller"
behavior in its contract without a task that explicitly built it; this task
closes that gap.

## Scope

- Choose one of the two poller ownership models the epic names, and record
  the choice with justification:
  - a dedicated poller thread per shard, each with its own `poll()` loop over
    that shard's `net_poll_scratch`/`fd_registry`; or
  - shard-worker-owned polling, where each shard's own Tier 1 worker (from
    Task 6/7's one-worker-per-shard model) drives its own net polling
    inline instead of a separate I/O thread.
  Base the choice on what Task 7 already built: if `SURGE_SHARDS>1` gives
  each shard exactly one worker, shard-worker-owned polling may avoid an
  extra thread per shard; a dedicated poller thread is simpler to reason
  about independently but doubles the thread count. Either is acceptable;
  pick one and justify it against Task 7's actual shape, not in the
  abstract.
- Give each shard its own wake mechanism (a pipe per shard is acceptable, per
  the epic's explicit allowance) so that registry changes (new fd, new
  interest), close, cancellation, and shutdown for that shard can wake only
  that shard's poller — not every shard's poller, and not only shard 0's.
- Replace the process-global `net_poll_wake_read_fd`/`net_poll_wake_write_fd`
  statics (`rt_net.c:67-68`) with per-shard equivalents (e.g. fields on
  `rt_net_poll_scratch` or a new small per-shard wake struct on `rt_shard`).
- Preserve the `SURGE_SHARDS=1` behavior exactly: with one shard, this
  should reduce to "the same one poller and one wake pipe as before," just
  now addressed through shard 0's struct instead of a bare process-global
  static.
- Runtime shutdown must wake every shard's poller and worker, per the Accept
  Ownership Contract: *"Runtime shutdown wakes every shard poller and worker
  without leaving live connection waiters or benchmark child processes
  behind."* Confirm this explicitly for `N>1`, not only for shard 0.
- Do not implement: `eventfd`, inbound message queues, target credits, or
  the seq-cst `PARKED` wake-elision protocol. If implementation pressure
  makes one of these tempting (e.g. "a pipe write per wake feels wasteful"),
  stop and record the temptation as a Phase 4 forward note instead of acting
  on it.

## Out Of Scope

- Migrating the actual read/write/close/cancellation waiter logic to use the
  new per-shard poller (Task 11's job) — this task only builds the poller/
  wake plumbing; Task 11 is what routes real net lifecycle events through
  it.
- Cross-shard wake-fd elision, `PARKED` state, or any Phase 4 primitive —
  explicitly barred, see Context above.
- Trace counters for the new per-shard wake mechanism in full detail (Task
  12), though this task should expose whatever minimal counters it needs to
  prove its own correctness (e.g. "shard k's poller woke N times for its own
  registry changes").

## Approach / Steps

1. Confirm Task 6 (real shard array) and Task 7 (one worker per shard, task
   placement) have landed.
2. Choose and record the poller ownership model.
3. Design the per-shard wake struct/fields; replace the process-global pipe
   statics with per-shard storage.
4. Wire shard-local registry-change/close/cancellation/shutdown signals to
   write only their own shard's wake fd.
5. Prove `SURGE_SHARDS=1` is unaffected (existing net wakeup liveness probe,
   `LIVENESS_PROBES.md` "Net wakeup and live SIGUSR1 trace" row, must stay
   green).
6. Prove `SURGE_SHARDS>1`: each shard's poller wakes only from its own
   shard's signals, never from another shard's registry change — write a
   deliberate cross-shard-silence test (shard A registers a new fd interest;
   shard B's poller must not observe a spurious wake from it).
7. Prove shutdown wakes every shard's poller and worker under `N>1`, with no
   leftover process/socket.
8. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch:

- `runtime/native/rt_net.c` (wake pipe statics → per-shard fields, poller
  loop)
- `runtime/native/rt_async_internal.h` (per-shard wake struct fields, if
  added here rather than inline in `rt_net_poll_scratch`)
- `runtime/native/rt_async_state.c` (shutdown path, if the per-shard wake
  wiring touches `exec_init_once`/shutdown signaling)

Read:

- `docs/RUNTIME_V2.md` §8 (Cross-Shard Wakeups — read to understand exactly
  what NOT to build yet)
- `docs/runtime-v2-epics/LIVENESS_PROBES.md` ("Net wakeup and live SIGUSR1
  trace" row; "Timer, timeout, and shutdown liveness" row)
- `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` (Task 2, wake-fd
  call sites)

## Skills & Working Practice

- Full Global Rule 9 plan gate: state the poller-ownership-model choice and
  the exact per-shard wake struct shape before writing code.
- This is a wakeup-placement change, so `LIVENESS_PROBES.md`'s Mandatory
  Gate By Change Type row "Wakeup placement or wake-before-park handling"
  applies: MT process timeout wrapper, direct async channel wakeups, net
  wakeup and live SIGUSR1 trace, parked-with-work invariant.
- Resist scope creep toward Phase 4. The epic is explicit that this
  restraint is a boundary decision, not an oversight — if the deliberate
  cross-shard-silence test in step 6 is hard to write without something
  that smells like a message queue, that is a sign the wake mechanism is
  drifting toward Phase 4, not a sign the test is wrong.
- May proceed in parallel with the early part of Task 9 if their write sets
  stay disjoint (accept-path fd creation vs. poller/wake plumbing); default
  to sequencing after Task 7 regardless.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- Existing net wakeup liveness probe (`TestMTNetWaiterWakeupLatency` per
  `LIVENESS_PROBES.md`)
- New per-shard cross-silence test
- New multi-shard shutdown test
- `git diff --check`
- Sentrux root and scoped scans

## Definition Of Done

- [ ] The poller-ownership model (dedicated thread vs. shard-worker-owned)
      is chosen and justified against Task 7's actual worker shape.
- [ ] Every shard that owns net fds has its own poller owner and its own
      wake mechanism.
- [ ] The process-global wake pipe statics are replaced by per-shard
      storage; `SURGE_SHARDS=1` behavior is unaffected.
- [ ] A shard's poller never wakes from another shard's registry change,
      proven by a deliberate test, under `SURGE_SHARDS>1`.
- [ ] Runtime shutdown wakes every shard's poller and worker with no
      leftover live waiters or child processes, under `SURGE_SHARDS>1`.
- [ ] No Phase 4 primitive (`eventfd`-as-protocol, inbound queues, credits,
      seq-cst `PARKED` state) was implemented or pre-decided.

## Evidence To Record

- `06-evidence.md`: poller-ownership-model decision and justification,
  Contracts Touched (per-shard wake, shutdown), Commands/Checks, Trace
  Counters/Liveness Proof (cross-silence and shutdown probes).
- `NOTES.md`: the chosen model, and an explicit forward note listing any
  Phase 4 temptation noticed during implementation (per step 6's caution),
  so the future cross-shard-messaging epic starts with that context instead
  of rediscovering it.
