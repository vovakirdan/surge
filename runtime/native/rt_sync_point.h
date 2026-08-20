// Runtime V2 proving spike: test-only deterministic interleaving hooks.
//
// RT_SYNC_POINT(name) and RT_SYNC_POINT_IF(cond, name) mark an instruction-scale
// rendezvous inside REAL runtime code so a test can pause one thread at a named
// window and release it in lockstep with another, reproducing a few-instruction
// ordering window deterministically (see docs/runtime-v2-epics/09-tasks/
// 01-proving-spike-sync-points.md).
//
// Ownership and lifecycle: the hook is TEST-ONLY. Unless the build defines
// RT_TEST_SYNC_POINTS the macro emits no call, no branch, and no symbol, so a
// hook can never sit on the worker steady path of a shipping build. That
// property is enforced mechanically by check_sync_points.sh (nm of the
// default/tag-off build must show zero rt_sync_point_* symbols).
//
// Allowlist: `name` must be one of the RT_SYNC_POINT_* enumerators below or the
// translation unit fails to compile, in BOTH the armed and the release build
// (the release macro still references the enumerator). Adding a site therefore
// requires (1) adding an enumerator here and (2) listing the window in
// check_sync_points.sh. The first lifecycle windows were introduced here;
// adds transport park/wake contract windows whose production sites land
// in .
#ifndef RT_SYNC_POINT_H
#define RT_SYNC_POINT_H

// The allowlist. Always defined (a pure compile-time enum; it emits no runtime
// symbol), so the release macro can reference it to enforce the allowlist
// without linking any rendezvous code.
typedef enum rt_sync_point_id {
    RT_SYNC_POINT_NONE = 0,
    // cancel_task: reached with the target's status still observed RUNNING,
    // immediately before the (now unconditional) wake. Pairs with
    // SP_PARK_BEFORE_WAITING to reproduce the RV2-DEBT-023 cancel-vs-park race.
    RT_SYNC_POINT_SP_CANCEL_BEFORE_WAKE,
    // user-task scheduler path: reached after poll has returned PARKED but
    // before committing TASK_WAITING, while status is RUNNING.
    RT_SYNC_POINT_SP_PARK_BEFORE_WAITING,
    // mark_done tail: reached after the TASK_DONE store, immediately before the
    // post-DONE seq-cst done_waiters load. Pairs with
    // SP_AWAIT_AFTER_INCREMENT to reproduce the RV2-DEBT-022 StoreLoad window.
    RT_SYNC_POINT_SP_MARKDONE_BEFORE_DONEWAITERS_LOAD,
    // rt_task_await: reached after the done_waiters increment, before the
    // seq-cst status predicate load.
    RT_SYNC_POINT_SP_AWAIT_AFTER_INCREMENT,
    // rt_task_await: reached after the external awaiter observed not-DONE and
    // immediately before pthread_cond_wait atomically releases ex->lock. Pairs
    // with RV2_DEBT_022_NEGATIVE_CONTROL to prove unlocked done_cv broadcasts
    // can be lost when they race this final wait transition.
    RT_SYNC_POINT_SP_AWAIT_BEFORE_DONECV_WAIT,
    // rt_task_poll: reached after prepare_park has published the current
    // task's JOIN waiter and pending_key records that park, before the target
    // DONE re-check. Tests can now cancel a proven registered joiner without
    // relying on scheduler timing.
    RT_SYNC_POINT_SP_TASK_POLL_AFTER_JOIN_REGISTER,
    // blocking task poll: reached after observing a pending blocking job but
    // before publishing its blocking-key waiter. The blocking worker can
    // complete and drain the empty key while this point is held, proving the
    // post-registration terminal re-check closes the lost-completion window.
    RT_SYNC_POINT_SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER,
    // wake_key_all_with_policy: reached inside the batch drain loop, so a cancel
    // can race a mid-drain key wake (token vs batch-compaction ordering).
    RT_SYNC_POINT_SP_WAKEKEY_MID_DRAIN,
    // rt_task_replace_owner / join-waiter migration: reached after the owner
    // replacement route publication boundary. The positive build publishes the
    // join route before this point; the RV2_DEBT_020_NEGATIVE_CONTROL build
    // reaches it before publishing, reproducing the old migrate gap.
    RT_SYNC_POINT_SP_MIGRATE_GAP,
    // transport consumer: reached after the inbound drain, before publishing
    // the shard PARKED state.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK,
    // transport consumer: reached after publishing PARKED, before the required
    // inbound re-check that prevents parked-with-work strands.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK,
    // transport producer: reached after publishing the complete message,
    // before reading the target shard's park state.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD,
    // transport producer: reached after reading park state, before deciding
    // whether to write the transport wake fd or elide the wake.
    RT_SYNC_POINT_SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE,
    // transport reply waiter: reached before a task suspends on a reply waiter;
    // this must not be a shard park.
    RT_SYNC_POINT_SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND,
    // transport shutdown: reached after shutdown is visible, before waking all
    // transport-parked shards and reply waiters.
    RT_SYNC_POINT_SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE,
    // remote task owner: reached after observing not-DONE but before
    // publishing the owner reply edge. A test completes in this window to
    // prove register-then-verify closes the lost completion.
    RT_SYNC_POINT_SP_REMOTE_TASK_BEFORE_OWNER_REGISTER,
    // remote task owner: reached after publishing the owner reply edge and
    // pinning the task, before the DONE re-check. A test completes the task in
    // this window to prove both the re-check and the task-lifetime pin.
    RT_SYNC_POINT_SP_REMOTE_TASK_AFTER_OWNER_REGISTER,
    // remote spawn destination: reached at dispatch entry, before the pending
    // snapshot that gates body creation. A test abandons the caller-owned
    // handle in this window to prove a pre-dispatch abandon still hands the
    // shipped state to exactly one owner (the published body) and turns the
    // eventual ack into an owner-routed release.
    RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH,
    // remote spawn destination: reached after the body task exists and the
    // pending carries its handle, before the body is published (the
    // spawn-on twin of SP_IMMEDIATE_ON_BEFORE_PUBLISH). An abandon in this
    // window must neither leak nor double-drop the shipped state.
    RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH,
    // remote spawn destination: reached after the child is published, before
    // its ack is enqueued. Cancellation here must abandon the caller-owned
    // lease and turn the eventual ack into an owner-routed release.
    RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK,
    // immediate on destination: reached at execute-dispatch entry, before
    // the token match and pending snapshot that gate body creation. A test
    // cancels the caller in this window to prove the teardown sweep
    // resolves the UNBOUND request and the late dispatch refuses to create
    // a body (the pending stays the state's sole owner and drops once).
    RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_DISPATCH,
    // immediate on destination: reached after the execute pending is bound to
    // the created body task and owner-registered, before the body task is
    // published. A test cancels the caller in this window to prove the
    // caller-cancel route and the reply edge resolve exactly once.
    RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH,
    // immediate on destination: reached AFTER the body task is published and
    // before the publication-accepted handoff store clears state_owned.
    // Publishing makes the body runnable, so in this window another thread
    // can complete it, consume the owner registration, drop the last pending
    // reference and free the pending -- while this thread is still about to
    // write into it. A test holds the dispatch here, drains the body and
    // resolves the caller, then releases: without the dispatch's own pending
    // reference the store lands in freed memory (RV2-DEBT-061 shape (b)), and
    // the free path can read a stale state_owned and drop body_state a second
    // time (shape (a)).
    RT_SYNC_POINT_SP_IMMEDIATE_ON_AFTER_PUBLISH,
    // ready_push_with_policy (force-inject requeue: yielded tasks, net
    // wakes): reached after the unlocked status/enqueued pre-checks pass,
    // before the owner shard lock. A wake in this window can enqueue the
    // task and hand it to another worker that stores RUNNING; the locked
    // re-validation must then refuse the duplicate push, or its READY store
    // overwrites RUNNING under a live poll and a second worker double-polls.
    RT_SYNC_POINT_SP_READY_REQUEUE_BEFORE_LOCK,
    // wake_task_with_policy: reached after the owner lock is released with a
    // captured stale park key, before the deferred waiter removal. The woken
    // task can re-poll and re-park on the SAME key in this window; an
    // unqualified removal here sweeps the fresh registration too and strands
    // a store-driven (join) park forever (RV2-DEBT-046).
    RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL,
    // remote select owner body (rt_anchored_channel_select): reached after
    // rt_select_poll has committed a winner (its control-lock critical
    // section already released) and before the winner value returns to
    // the caller's async-return/reply. A test cancels the caller in this
    // window to prove the one-lock commit ships as success even though a
    // cancel races in after the commit (Epic 20 Task 7 row 2).
    RT_SYNC_POINT_SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY,
    // remote select destination dispatch entry (rt_far_channel_dispatch_select):
    // reached before the pending snapshot that gates the arm pin loop, the
    // select twin of SP_IMMEDIATE_ON_BEFORE_DISPATCH. A test cancels the
    // caller in this window to prove the teardown sweep resolves the
    // UNBOUND request and the late dispatch refuses to pin any arm (Epic
    // 20 Task 7 row 3).
    RT_SYNC_POINT_SP_FAR_SELECT_BEFORE_DISPATCH,
    // Epic 23b final carrier liveness: hold the first exclusive jumbo
    // reservation, then prove the competing sender reached PARKED_CREDIT.
    RT_SYNC_POINT_SP_CARRIER_JUMBO_ADMITTED,
    RT_SYNC_POINT_SP_CARRIER_CREDIT_PARKED,
    // rt_sleep_fire_due_on_shard: reached after the due batch has been popped
    // out of the sleep store and the shard lock released, before the first
    // wake puts any of it back. Holding a worker here is the whole of the
    // RV2-DEBT-190 window: the fired sleeper is in no store, no queue and no
    // running count, so an idle sample taken now sees an empty executor and
    // the virtual clock advances past a deadline that has already come due.
    RT_SYNC_POINT_SP_SLEEP_FIRED_BEFORE_WAKE,
    RT_SYNC_POINT_COUNT
} rt_sync_point_id;

#ifdef RT_TEST_SYNC_POINTS

// Armed build only. Reaching an armed point performs its configured rendezvous
// action (barrier / block / open); reaching an unarmed point is a cheap no-op.
void rt_sync_point_reach(rt_sync_point_id id);

// Test-driver helpers (harness-callable, armed build only):
//   rt_sync_point_reached_count  how many times a point was reached this
//                                process, so a proof can assert the window was
//                                actually exercised (never a silent skip).
//   rt_sync_point_open           grant one permit to the shared semaphore,
//                                releasing one thread blocked at a `block`
//                                point. Lets a driver thread order an
//                                interleaving explicitly (block in runtime
//                                code, release from the driver after it has
//                                performed the racing action).
//   rt_sync_point_wait_until_after
//                                block the driver until a point's reached
//                                count exceeds a captured value; returns zero
//                                only on the bounded deadlock guard.
unsigned rt_sync_point_reached_count(rt_sync_point_id id);
int rt_sync_point_wait_until_after(rt_sync_point_id id, unsigned before);
void rt_sync_point_open(void);

#define RT_SYNC_POINT(name) rt_sync_point_reach(RT_SYNC_POINT_##name)
#define RT_SYNC_POINT_IF(cond, name)                                                               \
    do {                                                                                           \
        if (cond) {                                                                                \
            RT_SYNC_POINT(name);                                                                   \
        }                                                                                          \
    } while (0)

#else

// Release/default build. The cast keeps the allowlist load-bearing (an
// unknown `name` still fails to compile) while folding to nothing: no call, no
// symbol, nothing on the steady path.
#define RT_SYNC_POINT(name) ((void)RT_SYNC_POINT_##name)
#define RT_SYNC_POINT_IF(cond, name) ((void)RT_SYNC_POINT_##name)

#endif // RT_TEST_SYNC_POINTS

// RV2-DEBT-023 negative-control toggle (proof infrastructure, available
// in every build so cancel_task compiles either way). Default expands to the
// unconditional wake (the fix). A build that defines RV2_DEBT_023_NEGATIVE_
// CONTROL restores the pre-fix status-gated wake, which MUST strand the
// deterministic cancel-vs-park proof (the non-vacuity check). One statement
// either way, so cancel_task's effective LOC is unchanged by the toggle.
#ifdef RV2_DEBT_023_NEGATIVE_CONTROL
#define RT_DEBT023_CANCEL_WAKE(ex, task)                                                           \
    do {                                                                                           \
        if (task_status_load(task) == TASK_WAITING) {                                              \
            wake_task((ex), (task)->id, 1);                                                        \
        }                                                                                          \
    } while (0)
#else
#define RT_DEBT023_CANCEL_WAKE(ex, task) wake_task((ex), (task)->id, 1)
#endif

// RV2-DEBT-046 negative-control toggle. Default (the fix): a consumed park's
// stale JOIN key is exempt from the deferred waiter removal — join entries
// carry no park generation, so the removal is unqualified and sweeps a fresh
// re-registration made in the post-unlock window, stranding the joiner (join
// wakes are store-driven). The stale entry is self-cleaning instead: the join
// target completes exactly once and its completion drain pops every entry for
// the key; a stale pop is absorbed as one spurious wake by the wake token.
// A build defining RV2_DEBT_046_NEGATIVE_CONTROL restores the pre-fix
// remove-every-kind behavior, which MUST strand the deterministic
// wake-vs-repark proof (the non-vacuity check).
#ifdef RV2_DEBT_046_NEGATIVE_CONTROL
#define RT_DEBT046_STALE_KEY_REMOVABLE(key) 1
#else
#define RT_DEBT046_STALE_KEY_REMOVABLE(key) ((key).kind != WAKER_JOIN)
#endif

// RV2-DEBT-143 negative-control toggle. The fixed blocking poll publishes its
// waiter and then treats a terminal re-check as authoritative. Defining the
// negative control makes that post-registration observation inert, restoring
// the old lost-completion path for the deterministic proof.
#ifdef RV2_DEBT_143_NEGATIVE_CONTROL
#define RT_DEBT143_POST_REGISTER_TERMINAL(status) 0
#else
#define RT_DEBT143_POST_REGISTER_TERMINAL(status)                                                  \
    ((status) == BLOCKING_JOB_DONE || (status) == BLOCKING_JOB_CANCELLED)
#endif

// RV2-DEBT-190 negative-control toggle. The fix counts a collect-then-wake
// batch as in flight from the moment it leaves its structure until it is
// republished, so an idle sample taken in that gap sees work rather than an
// empty executor — which matters because idleness is what the virtual clock
// advances on. Defining the negative control makes the claim inert, restoring
// the state in which the batch is invisible, so the deterministic proof MUST
// observe the executor reporting itself idle with a fired sleeper in hand.
#ifdef RV2_DEBT_190_NEGATIVE_CONTROL
#define RT_DEBT190_PUBLISHING(count) ((size_t)0)
#else
#define RT_DEBT190_PUBLISHING(count) (count)
#endif

// RV2-DEBT-201 negative-control toggle. park_current's FIRST abort branch
// (the wake token was already set when the park began) fires BEFORE
// add_waiter, so the only registration in flight is the one
// channel_park_prepare_locked appended — and that one carries a NONZERO park
// generation. The fix reads that generation before park_requeue_locked
// requeues the task (which clears park_key and park_prepared but not
// park_seq) and retires the entry after the owner lock is released, exactly
// as the post-commit abort below it already does. Defining the negative
// control zeroes the generation, which makes the retirement inert on THIS
// branch only, so the deterministic proof MUST observe the registration
// outliving the task that owned it.
#ifdef RV2_DEBT_201_NEGATIVE_CONTROL
#define RT_DEBT201_ABORT_SEQ(task, key) ((uint32_t)0)
#else
#define RT_DEBT201_ABORT_SEQ(task, key)                                                            \
    (((key).kind == WAKER_CHAN_SEND || (key).kind == WAKER_CHAN_RECV) ? (task)->park_seq           \
                                                                      : (uint32_t)0)
#endif

// RV2-DEBT-199 negative-control toggle. A far channel IS freed
// (rt_far_channel.c release_entry -> rt_channel_free), so resolving a channel
// waiter key by casting key.id back to rt_channel* and reading the object is a
// use-after-free from the moment the reclaim lands — and the reader is the
// ROUTING path itself (rt_waiter_route.c), reached by remove_waiter /
// remove_waiter_generation / add_waiter / wake_key_all with a key those callers
// merely carry. The fix stamps the channel's owner shard INTO the key at
// construction time, where the channel is provably alive because the operation
// building the key holds it, so routing reads the key and never the object.
// Defining the negative control restores the dereference, which the
// deterministic proof MUST observe as an ASan heap-use-after-free.
#ifdef RV2_DEBT_199_NEGATIVE_CONTROL
#define RT_DEBT199_CHANNEL_OWNER_SHARD(key)                                                        \
    rt_channel_owner_shard_id((const rt_channel*)(uintptr_t)(key).id)
#else
#define RT_DEBT199_CHANNEL_OWNER_SHARD(key) ((key).owner_shard_id)
#endif

#endif // RT_SYNC_POINT_H
