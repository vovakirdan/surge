# Epic 7 Task 3: Locking-Model Proving Spike

Task 3 output. This document fixes the locking model for the executor lock
split and records the proof run. Every *(spike)* mark in
`07-executor-lock-dependency-map.md` is answered here. Implementation tasks
(6-11) implement these decisions; deviating from them requires updating this
document first.

## Proving Spike Record (RULES.md Global Rule 1)

- **Hypothesis:** the two-lane model — per-shard locks plus a reduced control
  lane, with lock order `control -> at most one shard lock`, register-then-
  commit parking with the existing `wake_token` double-check, owner-hinted
  waiter entries, and collect-then-wake through the control lane for foreign
  hints — has no lost wakeups, no deadlocks, and only bounded spurious wakes.
- **Files/surfaces allowed to change:** none in the repository; the proof
  prototype lives outside the tree (source inlined below).
- **Explicitly non-final behavior:** the prototype models tasks, queues,
  stores, and wakes structurally; it does not model memory freeing, accept
  re-placement, condvar mis-delivery classes, or the virtual clock. Those are
  settled by the structural rules in this document, not by the prototype.
- **Proof:** standalone C model (4 shards, 32 tasks, 16 keys, 20000
  park/wake cycles per task, 3 concurrent cross-shard wake threads) built
  with `clang -O1 -g -fsanitize=thread` and with `clang -O2 -DNDEBUG`.
- **Success criteria:** zero TSan reports; all tasks complete all cycles
  within the watchdog (no hang = no lost wakeup, since every registered
  waiter must be woken to progress); spurious parks well under 1% of wakes.
- **Failure criteria:** any TSan report, any hang, or spurious-park counts
  that grow with run length disproportionally to wakes.
- **Rollback note:** nothing to roll back; the prototype is not repository
  code. If the model had failed, the epic would re-scope toward keeping more
  paths on the control lane.

**Proof results (5/5 pass):**

| Build | Runs | Result | total_wakes | slow_wakes | spurious_parks |
| --- | --- | --- | --- | --- | --- |
| TSan `-O1` | 4 | all PASS, ~13-15s each, zero reports | 639968 | 480281 | 462-538 |
| `-O2 -DNDEBUG` | 2 | all PASS | 639968 | 480281 | 273-294 |

`total_wakes = 639968` is exactly the number of parks the model performs
(32 tasks x 20000 cycles minus the 32 final cycles that complete instead of
parking), so every registered waiter was woken exactly once: nothing was
lost and nothing was double-consumed. Spurious parks (a wake arriving in the
register-to-commit window, absorbed by `wake_token`) stayed at 0.04-0.08% of
wakes. The full prototype source is inlined at the end of this document.

## D1. Lanes, Locks, And Condition Variables

Each `rt_shard` gains:

- `lock` (`pthread_mutex_t`): owns the shard's scheduler queues,
  `running_count`, waiter store, sleep store, fd registry, net poll
  scratch/state (`net_polling`), and the schedulable state of tasks the shard
  owns (status transitions, park fields, resume fields).
- `worker_cv`: workers sleep here for ready work; also used by the
  sync-channel compat wait (D14).
- `poller_cv`: threads wait here for net-poll arbitration (`net_polling`
  in flight) and idle/poll-finished transitions; the N=1 io thread waits
  here (D15).

Two condvars per shard, not one: workers and poll-arbitration waiters have
different predicates, and a single cv with `pthread_cond_signal` can deliver
a ready-work signal to a poll-arbitration waiter that just re-sleeps,
stranding the ready task. Separate cvs keep `signal` (not `broadcast`) legal
for the hot ready-push path. **Rejected alternative:** one cv per shard with
broadcast-everything — correct but reintroduces the thundering herd inside a
shard for `SURGE_SHARDS=1` with many workers.

The control lane keeps `ex->lock` and `done_cv`. `ready_cv` and `io_cv` are
deleted once Tasks 7-11 migrate their last waiters. The blocking pool's
`blocking_lock`/`blocking_cv` are untouched.

## D2. Lock Order And Its Debug Assertion

Order, absolute: **control, then at most one shard lock.** A thread holding
any shard lock may not acquire the control lock or another shard lock. All
cross-structure work is therefore sequenced as: (optionally) control, then
one shard at a time, releasing between shards.

Debug assertion: a TLS record of currently-held lane (none / control /
shard id), checked by `rt_lock`-replacement wrappers, enabled together with
the existing debug env (`rt_async_debug_enabled` family) and in the focused
stress probes. Violations panic with the held/requested pair.

## D3. Task Identity, Lookup, And Lifetime Rule

- Task ids stay monotonic; `ex->tasks[id]` slots are never reused for a
  different task (already true: `next_id++`, slot keyed by id).
- **Deref rule:** dereferencing a pointer read from `ex->tasks[id]` is legal
  only (a) while holding the control lock, or (b) while holding shard lock S
  when a shard-owned structure implies the task's owner is S: the id came
  from S's ready queue, the task is running on S's worker, the id came from a
  waiter entry in S's store with `owner_hint == S`, or from S's sleep store.
- **Free rule:** freeing task memory and clearing the slot requires the
  control lock plus the task's current owner shard lock, control acquired
  first. `mark_done` runs its shard phase under the owner lock, then (only
  when needed) a control epilogue re-acquires control then the owner lock
  for the free (D11).
- **Owner stability rule:** `owner_shard_id` is written only under the
  control lock, and only when the task is not enqueued, not running, and has
  no registrations other than the accept registrations being consumed (D4).
  Consequences: a task parked on a non-accept key cannot be re-placed, so a
  waiter entry's `owner_hint` for non-accept keys is always current; an
  enqueued id implies the task's owner is the queue's shard.

Waiter entries grow an `owner_hint` field: `{key, task_id, owner_hint}`.

**Rejected alternatives, do not retry:**

- Free under the owner lock alone: a stale DONE id in a queue or store could
  be dereferenced after free by another lane.
- Owner-chasing under shard locks (lock hinted shard, re-read owner, hop):
  the re-read requires a deref that is only safe if the free rule already
  serialized with us — it has not; unsafe without control.
- Dereferencing foreign-hint waiter entries under the store lock (today's
  `pop_waiter` stale-skip): illegal under the deref rule; validation moves to
  the wake step under control (D5).

## D4. Accept Re-Placement Is Control-Lane

Accept completion (`rt_executor_wake_net_waiters_for_key_on_owner` for
`WAKER_NET_ACCEPT`, including `clear_accept_winner_wait_keys`) runs under the
control lock: pop/remove the winner's and siblings' registrations from each
member store one shard lock at a time, write the new owner and placement
(the only `owner_shard_id` write after spawn), then lock the new owner shard
to enqueue. Read/write readiness stays entirely shard-local (D5 fast path):
Epic 6 already guarantees a connection task's owner equals its fd's shard.
The accept-winner cleanup keeps its current whole-table scan under control
for this epic; replacing the scan with an owner-shard pending-accept set is
a recorded follow-up, not an Epic 7 requirement. Per-connection control cost
is accepted until Phase 4 messaging.

## D5. Park And Wake Under Split Locks

Park (register-then-commit, both steps as today, locks split):

1. lock the key owner's store, append `{key, task_id, owner_hint}`, unlock;
2. lock the task owner's shard; `wake_token` exchange; if taken, abort;
   store `TASK_WAITING`; `wake_token` exchange again; if taken, revert to
   `TASK_READY` and abort; unlock.
3. On abort: after releasing the owner lock, lock the store again and remove
   the entry by value (idempotent; a concurrent pop at worst re-sets the
   token, absorbed as one spurious poll).

Wake for a key: lock the key owner's store; pop all matching entries;
same-shard hints (`owner_hint ==` store shard) are validated and woken under
that same held lock — this is the whole hot path for net read/write
readiness, same-shard channels, join, timer, and blocking wakes. Foreign
hints go to a local batch; after unlocking the store, take control, then per
task: read the (control-stable) owner, lock that shard, validate status,
write resume fields if the protocol carries them, wake, unlock. Channel
value handoff validates the peer before consuming the value: an
undeliverable foreign peer means re-lock the store and take the next
candidate, so no value is lost (the `runtime-v2-waiter-check` cancelled-
waiter contracts stay the proof).

`wake_task(remove_waiter_flag=1)` callers (cancel, timeout): reading
`park_key` happens under the owner lock; removing the registration from a
foreign store happens after releasing the owner lock, value-based and
idempotent, same as park-abort. Never two shard locks.

## D6. Ready Queues And Worker Loop

Ready push targets the task owner's queues under the owner's shard lock;
the `enqueued` guard stays. Worker loop turns hold only their shard lock;
user-task polls run unlocked as today; checkpoint/sleep/blocking polls stay
under the shard lock (D13). `signal_ready_workers` dies: a wake signals only
the owner shard's `worker_cv`. The parked-with-work assertion
(`rt_debug_assert_no_parked_with_work`) moves under the shard lock,
unchanged in meaning.

## D7. Virtual Clock And Sleep Stores

- `ex->now_ms` becomes an atomic u64. Yield-tick = atomic increment (same
  count of ticks as today for any shard count) plus an inline due-check of
  the ticking shard's own sleep store under its held shard lock.
- Each shard owns a sleep store ordered by `(deadline, task_id)`; a shard's
  due sleepers are woken by that shard (tick path, poll path, or the
  idle-advance sweep). The whole-table scans die.
- Idle advance (virtual jump to the next deadline) requires global idleness,
  as today. Each shard maintains an atomic busy indicator and an atomic
  min-deadline mirror, written under its shard lock. The last worker to go
  idle attempts the advance: take control, verify all shards idle via the
  atomics, CAS the clock forward to the global min deadline, then sweep
  (control -> one shard at a time) waking due sleepers. The N=1 runner and
  io-thread advance paths become this same control-lane advance.
- Wake order for equal deadlines: preserved per shard by `(deadline, id)`
  order; `SURGE_SHARDS=1` observable timing is bit-for-bit today's semantics
  (one store, ascending ids). Cross-shard interleaving for N>1 becomes an
  implementation artifact of the sweep order (shard 0..N-1), recorded here.
- The racy-idle edge (a shard waking concurrently with the idle check) is
  narrowed by re-checking busy indicators after taking control; the residual
  race is within the existing N>1 scheduling nondeterminism envelope.
  **Rejected:** per-shard clocks (changes observable sleep semantics).

## D8. Scope Keys Are Control-Lane

`WAKER_SCOPE` waiters move to a small control-owned waiter store. Scope
bookkeeping (`rt_scope`, children arrays, failfast) already stays control;
`scope_child_done_locked` wakes the owner through control -> owner shard.

## D9. Blocking Completion

The pool thread completes a job, then: control lock -> owner lookup -> owner
shard lock -> wake. Cold path by nature (a syscall just happened).

## D10. `done_cv` Gating

`done_cv` stays control-lane. `rt_task_await` (workers>1) increments a
control-guarded waiter count with an atomic mirror; `mark_done` signals
`done_cv` only when the atomic mirror is nonzero (taking control for the
signal). Plain join-awaited tasks complete without touching control (D11).

## D11. `mark_done` Splits Into Shard Phase + Control Epilogue

Shard phase (owner lock held): clear wait keys (per-store removals, one lock
at a time, owner lock released around foreign stores per D5), set
`TASK_DONE`, clear `enqueued`, write results, wake join waiters from the own
store (join waiters live with the target task per the map). Control epilogue
(taken only when needed): parent-scope bookkeeping and failfast, `done_cv`
signal when awaited (D10), free when `handle_refs == 0` (re-acquiring
control then owner per D3). A task with no scope, no done-waiter, and a held
handle completes entirely on its shard.

## D12. Multi-Key Paths Are Control-Serialized This Epic

`rt_timeout_poll` and `rt_select_poll` (join + channel + timer key mixes
across owners) register and clean up under the control lock, taking key
owners' store locks one at a time beneath it. They are the counted slow
lane; the epic doc already prices remote select as slow. Single-key paths —
`rt_task_poll` join, channel send/recv, net read/write, sleep, blocking —
never take control on their steady path.

## D13. Non-User Polls Stay Under The Shard Lock

Checkpoint, sleep, and blocking-task polls are short state transitions; they
keep today's under-lock execution, now under the shard lock.

## D14. Sync-Channel Compat And Compensation

`rt_wait_current_worker_wakeup` parks the OS worker on its own shard's
`worker_cv`; the task-wake path already broadcasts the owner shard's
`worker_cv` when compat waiters are registered (shard-local counter).
Compensation-worker accounting (`channel_blocking_compat`) moves to the
control lane with its current semantics; starting a compensation worker is
control-lane. This path stays a compatibility path and must not add control
acquisitions to unrelated shard hot paths.

## D15. The io Thread

- `SURGE_SHARDS=1`: the io thread waits on shard 0's `poller_cv` under shard
  0's lock; its predicates (net waiters, `net_polling`, idle, timers) are
  shard-0 plus atomics. Net registration, poll-pass completion, and
  idle transitions broadcast `poller_cv` (rare or already-syscall paths).
- `SURGE_SHARDS>1`: shard workers poll their own net (Epic 6); the io
  thread's only residual duty is a robustness backstop for idle timer
  advance, implemented as a coarse `pthread_cond_timedwait` (250ms) on a
  control-lane cv plus a trace counter (`io_backstop_advances`) that must
  stay ~0 in focused tests — the last-idle-worker advance of D7 is the real
  mechanism.

## D16. Spawn, Cancel, Await, Runner, Shutdown, Trace Lanes

- Spawn: control (id, publish, parent/children) then owner shard (ready
  push); order control -> shard.
- `cancel_task`: control-lane recursion over the children tree; per-task
  wake control -> owner shard, one at a time.
- `rt_task_await` workers>1: control + `done_cv` (D10). `run_until_done` /
  `next_ready` (threads==1): control + shard 0 in order, semantics
  unchanged.
- Shutdown: control write of the atomic `shutdown` flag, then per-shard
  sweep (lock, broadcast both cvs, unlock; write wake pipes), then blocking
  pool, then `done_cv`. No thread stays parked: every sleeper's predicate
  includes the atomic flag.
- Trace snapshot dumps: control plus sequential shard locks (debug only).

## Implementation Shape Preview (for Tasks 6-11)

New focused files, each <=500 effective lines: shard sync lifecycle and
lock-order debug (`rt_shard_sync.c`), sleep store and clock
(`rt_async_sleep.c`), and the split park/wake/collect-then-wake path
(extracted from `rt_async_state.c`, which must not grow). Exact file names
are the implementing tasks' choice; the concepts-per-file rule is not.

## Prototype Source (verbatim)

```c
// Epic 7 Task 3 proving spike: park/wake protocol under split locks.
//
// Models the two-lane locking design:
//   - one lock per shard owning that shard's ready queue and waiter store;
//   - a control lock for cross-shard wake arbitration (owner lookup);
//   - lock order: control -> at most one shard lock;
//   - park: register in key owner's store, then commit under task owner's
//     lock with a wake_token double-check;
//   - wake: pop under store lock; same-shard hints wake inline; foreign
//     hints via control -> owner (collect-then-wake);
//   - park-abort and stale entries absorbed by wake_token (bounded spurious).

#include <assert.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define SHARDS 4
#define TASKS_PER_SHARD 8
#define TASKS (SHARDS * TASKS_PER_SHARD)
#define KEYS_PER_SHARD 4
#define KEYS (SHARDS * KEYS_PER_SHARD)
#define CYCLES 20000
#define QUEUE_CAP (TASKS * 4)
#define STORE_CAP (TASKS * 4)

typedef enum { ST_READY = 0, ST_RUNNING = 1, ST_WAITING = 2, ST_DONE = 3 } status_t;

typedef struct {
    _Atomic int status;
    _Atomic int wake_token;
    _Atomic int enqueued;
    int owner; // immutable in this model (re-placement is control-serialized)
    _Atomic long cycles;
    _Atomic long spurious;
} task_t;

typedef struct {
    int key;
    int task_id;
    int owner_hint;
} entry_t;

typedef struct {
    pthread_mutex_t lock;
    pthread_cond_t cv;
    int queue[QUEUE_CAP];
    int qhead, qlen;
    entry_t store[STORE_CAP];
    int slen;
} shard_t;

static shard_t shards[SHARDS];
static pthread_mutex_t control = PTHREAD_MUTEX_INITIALIZER;
static task_t tasks[TASKS];
static _Atomic int done_tasks;
static _Atomic long total_wakes;
static _Atomic long slow_wakes;
static _Atomic int stop;

static int key_owner(int key) { return key % SHARDS; }

static unsigned long xorshift(unsigned long* s) {
    unsigned long x = *s;
    x ^= x << 13;
    x ^= x >> 7;
    x ^= x << 17;
    *s = x;
    return x;
}

// Caller holds shards[t->owner].lock.
static void ready_push_locked(int task_id) {
    task_t* t = &tasks[task_id];
    shard_t* s = &shards[t->owner];
    int expected = 0;
    if (!atomic_compare_exchange_strong(&t->enqueued, &expected, 1)) {
        return; // duplicate enqueue guard
    }
    assert(s->qlen < QUEUE_CAP);
    s->queue[(s->qhead + s->qlen) % QUEUE_CAP] = task_id;
    s->qlen++;
    pthread_cond_signal(&s->cv);
}

// Caller holds shards[t->owner].lock. Mirrors wake_task_with_policy.
static void wake_task_locked(int task_id) {
    task_t* t = &tasks[task_id];
    atomic_store(&t->wake_token, 1);
    int st = atomic_load(&t->status);
    if (st == ST_WAITING) {
        atomic_store(&t->status, ST_READY);
        ready_push_locked(task_id);
    }
    // RUNNING/READY: token alone; the task observes it (spurious path).
}

static void wake_key(int key) {
    int owner = key_owner(key);
    shard_t* s = &shards[owner];
    int foreign[STORE_CAP];
    int nforeign = 0;

    pthread_mutex_lock(&s->lock);
    int out = 0;
    for (int i = 0; i < s->slen; i++) {
        entry_t e = s->store[i];
        if (e.key != key) {
            s->store[out++] = e;
            continue;
        }
        atomic_fetch_add(&total_wakes, 1);
        if (e.owner_hint == owner) {
            wake_task_locked(e.task_id); // fast path under the held lock
        } else {
            foreign[nforeign++] = e.task_id;
        }
    }
    s->slen = out;
    pthread_mutex_unlock(&s->lock);

    if (nforeign > 0) {
        pthread_mutex_lock(&control);
        for (int i = 0; i < nforeign; i++) {
            atomic_fetch_add(&slow_wakes, 1);
            task_t* t = &tasks[foreign[i]];
            pthread_mutex_lock(&shards[t->owner].lock);
            wake_task_locked(foreign[i]);
            pthread_mutex_unlock(&shards[t->owner].lock);
        }
        pthread_mutex_unlock(&control);
    }
}

// Returns 1 if parked, 0 if absorbed by wake_token (stays READY).
static int park(int task_id, int key) {
    task_t* t = &tasks[task_id];
    int kowner = key_owner(key);
    shard_t* ks = &shards[kowner];
    shard_t* os = &shards[t->owner];

    pthread_mutex_lock(&ks->lock);
    assert(ks->slen < STORE_CAP);
    ks->store[ks->slen++] = (entry_t){key, task_id, t->owner};
    pthread_mutex_unlock(&ks->lock);

    pthread_mutex_lock(&os->lock);
    if (atomic_exchange(&t->wake_token, 0) == 1) {
        atomic_store(&t->status, ST_READY);
        pthread_mutex_unlock(&os->lock);
        goto abort_remove;
    }
    atomic_store(&t->status, ST_WAITING);
    if (atomic_exchange(&t->wake_token, 0) == 1) {
        atomic_store(&t->status, ST_READY);
        pthread_mutex_unlock(&os->lock);
        goto abort_remove;
    }
    pthread_mutex_unlock(&os->lock);
    return 1;

abort_remove:
    // remove after releasing the owner lock: never two shard locks at once
    pthread_mutex_lock(&ks->lock);
    for (int i = 0; i < ks->slen; i++) {
        if (ks->store[i].key == key && ks->store[i].task_id == task_id) {
            ks->store[i] = ks->store[ks->slen - 1];
            ks->slen--;
            break;
        }
    }
    pthread_mutex_unlock(&ks->lock);
    atomic_fetch_add(&t->spurious, 1);
    return 0;
}

typedef struct {
    int shard_id;
    unsigned long rng;
} worker_arg_t;

static void* worker_main(void* arg) {
    worker_arg_t* wa = (worker_arg_t*)arg;
    shard_t* s = &shards[wa->shard_id];
    for (;;) {
        pthread_mutex_lock(&s->lock);
        while (s->qlen == 0 && !atomic_load(&stop)) {
            pthread_cond_wait(&s->cv, &s->lock);
        }
        if (atomic_load(&stop) && s->qlen == 0) {
            pthread_mutex_unlock(&s->lock);
            return NULL;
        }
        int task_id = s->queue[s->qhead];
        s->qhead = (s->qhead + 1) % QUEUE_CAP;
        s->qlen--;
        task_t* t = &tasks[task_id];
        atomic_store(&t->enqueued, 0);
        int st = atomic_load(&t->status);
        if (st == ST_DONE) {
            pthread_mutex_unlock(&s->lock);
            continue;
        }
        assert(t->owner == wa->shard_id); // no-steal invariant
        atomic_store(&t->status, ST_RUNNING);
        atomic_exchange(&t->wake_token, 0);
        pthread_mutex_unlock(&s->lock);

        long c = atomic_fetch_add(&t->cycles, 1) + 1; // "poll" outside locks
        if (c >= CYCLES) {
            pthread_mutex_lock(&s->lock);
            atomic_store(&t->status, ST_DONE);
            pthread_mutex_unlock(&s->lock);
            atomic_fetch_add(&done_tasks, 1);
            continue;
        }
        int key = (int)(xorshift(&wa->rng) % KEYS);
        if (!park(task_id, key)) {
            pthread_mutex_lock(&s->lock);
            ready_push_locked(task_id); // absorbed park: requeue
            pthread_mutex_unlock(&s->lock);
        }
    }
}

static void* firer_main(void* arg) {
    unsigned long rng = (unsigned long)(uintptr_t)arg;
    while (!atomic_load(&stop)) {
        wake_key((int)(xorshift(&rng) % KEYS));
    }
    return NULL;
}

int main(void) {
    for (int i = 0; i < SHARDS; i++) {
        pthread_mutex_init(&shards[i].lock, NULL);
        pthread_cond_init(&shards[i].cv, NULL);
    }
    for (int i = 0; i < TASKS; i++) {
        tasks[i].owner = i % SHARDS;
        atomic_store(&tasks[i].status, ST_READY);
    }
    for (int i = 0; i < TASKS; i++) {
        pthread_mutex_lock(&shards[tasks[i].owner].lock);
        ready_push_locked(i);
        pthread_mutex_unlock(&shards[tasks[i].owner].lock);
    }

    pthread_t workers[SHARDS];
    worker_arg_t wargs[SHARDS];
    for (int i = 0; i < SHARDS; i++) {
        wargs[i].shard_id = i;
        wargs[i].rng = 0x9e3779b97f4a7c15UL * (unsigned long)(i + 1);
        pthread_create(&workers[i], NULL, worker_main, &wargs[i]);
    }
    pthread_t firers[3];
    for (int i = 0; i < 3; i++) {
        pthread_create(&firers[i], NULL, firer_main, (void*)(uintptr_t)(0xdeadbeef + i));
    }

    int waited = 0;
    while (atomic_load(&done_tasks) < TASKS && waited < 120) {
        sleep(1);
        waited++;
    }
    int ok = atomic_load(&done_tasks) == TASKS;
    atomic_store(&stop, 1);
    for (int i = 0; i < SHARDS; i++) {
        pthread_mutex_lock(&shards[i].lock);
        pthread_cond_broadcast(&shards[i].cv);
        pthread_mutex_unlock(&shards[i].lock);
    }
    for (int i = 0; i < 3; i++) {
        pthread_join(firers[i], NULL);
    }
    for (int i = 0; i < SHARDS; i++) {
        pthread_join(workers[i], NULL);
    }

    long spurious = 0;
    for (int i = 0; i < TASKS; i++) {
        spurious += atomic_load(&tasks[i].spurious);
    }
    printf("done_tasks=%d/%d elapsed<=%ds total_wakes=%ld slow_wakes=%ld spurious_parks=%ld\n",
           atomic_load(&done_tasks), TASKS, waited, atomic_load(&total_wakes),
           atomic_load(&slow_wakes), spurious);
    if (!ok) {
        printf("FAIL: hang detected (lost wakeup or deadlock)\n");
        return 1;
    }
    printf("PASS\n");
    return 0;
}
```
