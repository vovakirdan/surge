// Runtime V2 Epic 9 proving spike: test-only deterministic interleaving hooks.
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
// check_sync_points.sh. Epic 9 introduced the first lifecycle windows; Epic 13
// Task 3 adds transport park/wake contract windows whose production sites land
// in Task 4.
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
    // remote spawn destination: reached after the child is published, before
    // its ack is enqueued. Cancellation here must abandon the caller-owned
    // lease and turn the eventual ack into an owner-routed release.
    RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK,
    // immediate on destination: reached after the execute pending is bound to
    // the created body task and owner-registered, before the body task is
    // published. A test cancels the caller in this window to prove the
    // caller-cancel route and the reply edge resolve exactly once.
    RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH,
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
unsigned rt_sync_point_reached_count(rt_sync_point_id id);
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

// RV2-DEBT-023 negative-control toggle (Epic 9 proof infrastructure, available
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

#endif // RT_SYNC_POINT_H
