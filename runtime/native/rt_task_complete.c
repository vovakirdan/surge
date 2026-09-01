// Task completion and cancellation (RV2-DEBT-003 split):
// this module owns the terminal task transitions — cancel propagation
// (cancel_task tree walk), completion (mark_done) and poll-outcome
// application (apply_poll_outcome). Lane contract: cancel_task runs
// control-held (every caller holds the control lane; proof in
// docs/runtime-v2-epics/09-tasks/02-debt-023-cancel-wake-token.md);
// mark_done is lane-aware (a worker takes control only when the exit owns
// control-lane work); apply_poll_outcome runs on the polling thread and
// nests at most one shard lock at a time. Extracted verbatim from
// rt_async_state.c; no behavior change.

#include "rt_async_internal.h"
#include "rt_frame.h"
#include "rt_remote_task.h"
#include "rt_sync_point.h"
#include "rt_task_refs.h"
#include "rt_value_cell.h"

void clear_select_timers(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || task->select_timers_len == 0) {
        return;
    }
    for (size_t i = 0; i < task->select_timers_len; i++) {
        uint64_t timer_id = task->select_timers[i];
        if (timer_id == 0) {
            continue;
        }
        rt_task* timer = get_task(ex, timer_id);
        if (timer != NULL) {
            cancel_task(ex, timer_id);
            task_release(ex, timer);
        }
        task->select_timers[i] = 0;
    }
    task->select_timers_len = 0;
}

int current_task_cancelled(rt_executor* ex) {
    (void)ex;
    const rt_task* task = rt_current_task();
    return task != NULL && task_cancelled_load(task) != 0;
}

// The cancel gate (RV2-DEBT-263): ONE word, two claimants, one RMW each.
//
// A cancel and a completion decide one thing between them -- which kind a task
// commits -- and exactly one of them must win. This module owns both sides,
// which is why both transitions live here. Each side moves task->cancelled with
// a compare-and-swap OUT OF the open state:
//
//   cancel_task  CAS OPEN -> REQUESTED   the cancel got here first
//   mark_done    CAS OPEN -> SEALED      the result got here first
//
// Why that is enough, and why fences are not. Read-modify-writes on ONE atomic
// object are totally ordered by that object's modification order, and a CAS
// reads the value written by the modification immediately before it in that
// order (C11 5.1.2.4). Whichever of the two appears first therefore reads OPEN
// and moves the word; the other reads REQUESTED or SEALED and fails. So "both
// sides believed they won" has no execution at all -- there is no window to
// make narrow, because there is no window.
//
// A pair of ordered plain accesses does NOT give that, and the first shape of
// this fix wrongly claimed it did: it paired a store-then-load on the cancel
// side with a LOAD-then-store on the completion side, and
// L(cancelled)=OPEN, S(cancelled=REQUESTED), L(status)=RUNNING, S(status=DONE)
// is an ordinary interleaving rather than a cycle -- it is simply the real-time
// order M.load, C.store, C.load, M.store. Sequential consistency forbids only
// the store-then-load pair on BOTH sides, which is the shape section 8 of
// docs/RUNTIME_V2.md describes for the shard park race. The residual window it
// left ran from the flag read to the DONE store, and everything mark_done does
// in between is inside it -- including a mutex acquisition in
// release_matching_leases (rt_remote_task_lease.c). Hundreds of nanoseconds,
// not a few instructions.
//
// The other reason to prefer the CAS over the alternative (the task's owner
// shard lock around both sides) is cost: that would put a lock acquire and
// release on EVERY task completion, which is the steady request path Epic 7
// spent its scope taking off the control lane, to serialize two accesses that
// need an order and not mutual exclusion.
static int task_cancel_gate_request(rt_task* task) {
    uint8_t open = RT_TASK_CANCEL_OPEN;
    return atomic_compare_exchange_strong_explicit(&task->cancelled,
                                                   &open,
                                                   (uint8_t)RT_TASK_CANCEL_REQUESTED,
                                                   memory_order_acq_rel,
                                                   memory_order_acquire);
}

static int task_cancel_gate_seal(rt_task* task) {
    uint8_t open = RT_TASK_CANCEL_OPEN;
    return atomic_compare_exchange_strong_explicit(&task->cancelled,
                                                   &open,
                                                   (uint8_t)RT_TASK_CANCEL_SEALED,
                                                   memory_order_acq_rel,
                                                   memory_order_acquire);
}

void cancel_task(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0) {
        return;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL) {
        return;
    }
    if (task_status_load(task) == TASK_DONE) {
        // The task's answer is already committed, and a result it published is
        // already available to every entitlement that has not asked yet. A
        // cancel arriving now changes nothing about it: it is task-global, so
        // there is no per-handle answer for it to write, and revoking a value
        // some sibling may already have been served would leave one handle
        // holding what its siblings are told does not exist.
        RT_SYNC_POINT_IF(rt_value_cell_is_ready(&task->result), SP_CANCEL_AT_COMMITTED_RESULT);
        RT_CANCEL_AFTER_COMMITTED_RESULT(ex, task);
        return;
    }
    // RV2-DEBT-263: this ONE compare-and-swap is both the idempotence check
    // (a second cancel finds REQUESTED) and the race against the completion (a
    // late cancel finds SEALED). Losing to a seal means the task's answer was
    // already decided, which the storage model says a cancel arriving then does
    // not revoke (23-storage-model-and-typed-carrier-abi.md: "after success is
    // committed it does not revoke already available independent results"). So
    // this cancel stops here -- no flag, no wake, and no walk into children a
    // completing task has already finished with -- exactly as the DONE check
    // above stops one that arrived later still. The gate closes a few
    // instructions before TASK_DONE becomes visible; a cancel landing in that
    // sliver is refused rather than silently swallowed, which is what
    // SP_MARKDONE_AFTER_SEAL_BEFORE_DONE holds open to prove.
    if (!RT_DEBT263_CANCEL_LANDED(task_cancel_gate_request(task), task)) {
        return;
    }
    if (task->kind == TASK_KIND_BLOCKING) {
        rt_blocking_request_cancel(ex, task);
    }
    // Wake-token ordering rule (RV2-DEBT-023, ). This cancel owns the gate
    // by the CAS above; the wake is UNCONDITIONAL - it does not gate on
    // observing TASK_WAITING. It is issued at the BOTTOM of this function,
    // after the walk below has reached every descendant, and that placement is
    // the rule stated at the walk. cancel_task runs control-held
    // (every caller holds the control lane; proof in
    // docs/runtime-v2-epics/09-tasks/02-debt-023-cancel-wake-token.md), so
    // free_task (control-lane only) cannot free this task between the get_task
    // above and this wake, and wake_task -> wake_task_on_shard_locked sets the
    // wake token unconditionally under the owner shard lock (rt_task_park.c) and
    // only ENQUEUES when the target is WAITING and not already enqueued. This
    // closes the lost-cancellation window: a RUNNING target that already passed
    // its poll's cancelled-check (rt_async_poll.c -> POLL_PARKED) and is
    // committing to TASK_WAITING in park_current re-checks the token
    // (rt_task_park.c); the token this wake sets aborts that park and re-runs
    // the target, so its next poll observes cancelled=1 and unwinds even on a
    // never-firing park_key. READY / RUNNING(YIELDED|DONE) / already-WAITING
    // targets are unaffected (already queued, re-run or complete, or enqueued as
    // before). A token set on a target that later parks on a LEGITIMATE key
    // costs one bounded spurious abort-and-requeue, already counted by
    // rt_trace_spurious_wake_absorbed (rt_task_park.c). SP_CANCEL_BEFORE_WAKE
    // reproduces the race deterministically against SP_PARK_BEFORE_WAITING; its
    // reached-count is the proof that this wake path engaged.
    // A cancel reaches the leaves before it makes anyone runnable again.
    //
    // The wake used to be issued HERE, above the walk, so a cancelled parent
    // entered the ready queue ahead of the children this same call was about to
    // cancel. It was then polled first, with those children still WAITING, and
    // rt_task_poll answered "not done" for the child it was awaiting; the
    // cancelled-awaiter check below that (rt_async_task.c) returned 0,
    // rt_async_yield saw the cancel and ended the parent at its suspension
    // (rt_async_poll.c). The parent's whole turn could only re-park it on its
    // scope key, and every result its cancelled children were about to publish
    // was unobservable to it -- not because the model says a cancelled awaiter
    // may not see them, but because nothing had run yet to produce them.
    //
    // Waking last inverts that: the walk cancels depth-first, each descendant is
    // made runnable as its own frame returns, and the task this cancel was aimed
    // at is enqueued behind all of them. Its next poll therefore finds the
    // children it awaits already DONE, and rt_task_poll's TASK_DONE fast path
    // (which answers from the TARGET, before any check of the awaiter's own
    // gate) hands their results over. The suspension still carries the cancel
    // for every await that has nothing to deliver, which is what
    // docs/CONCURRENCY.md means by observing cancellation at suspension points;
    // what changes is only that an await with an answer waiting is no longer
    // denied it by queue order.
    //
    // Nothing about the RV2-DEBT-023 argument above depends on the wake being
    // early. The gate CAS still precedes it, the token is still set
    // unconditionally under the owner shard lock, and a target that parks inside
    // the widened window re-checks that token in park_current exactly as before.
    // The task pointer is still live: this walk never releases the control lane,
    // and free_task is control-lane only.
    //
    // Since , task_add_child appends into this task's
    // children[] under the task's own owner shard lock, not control (the
    // steady-state __task_create path takes no control lock at all). This
    // control-held walk can no longer read task->children[]/children_len
    // directly - a concurrent append on a RUNNING task (this walk's caller
    // already holds control, but the appending thread does not) could
    // realloc the array mid-read. Collect-then-recurse (mirrors
    // wake_key_all's collect-then-wake and
    // rt_executor_wake_net_waiters_for_key_on_owner's inline-batch pattern):
    // snapshot the ids under the task's owner shard lock, release it, then
    // recurse. Legal lane order (control held, then at most one shard lock,
    // released before any further lock); never two shard locks, since each
    // recursion level locks/copies/unlocks before descending.
    //
    // Locking THIS task's CURRENT owner shard (not whatever shard protected
    // some earlier append) is sufficient even if this task's own owner
    // changed since an earlier append: every append and every self-replace
    // of this task's own owner_shard_id happen on this task's own executing
    // thread (see the invariant at __task_create's matching lock site,
    // rt_async_task.c), so program order plus the same-thread "lock
    // handoff" (a release sequenced-before a later acquire of a different
    // lock, both by the same thread, transitively carries prior writes
    // forward) publishes every earlier append to whoever locks the current
    // owner shard. This depends on that invariant staying true - re-check
    // it before adding any new rt_task_replace_owner call site.
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    rt_shard_lock(owner_shard);
    size_t child_count = task->children_len;
    uint64_t inline_children[8];
    uint64_t* children = inline_children;
    if (child_count > 8) {
        children = (uint64_t*)rt_alloc(child_count * sizeof(uint64_t), _Alignof(uint64_t));
        if (children == NULL) {
            rt_shard_unlock(owner_shard);
            fatal_oom_msg("async: cancel snapshot allocation failed");
            return;
        }
    }
    if (child_count > 0) {
        memcpy(children, task->children, child_count * sizeof(uint64_t));
    }
    rt_shard_unlock(owner_shard);
    for (size_t i = 0; i < child_count; i++) {
        cancel_task(ex, children[i]);
    }
    if (children != inline_children) {
        rt_free((uint8_t*)children, child_count * sizeof(uint64_t), _Alignof(uint64_t));
    }
    RT_SYNC_POINT(SP_CANCEL_BEFORE_WAKE);
    RT_DEBT023_CANCEL_WAKE(ex, task);
}

// Lane-aware completion (peel B1b): a control-lane caller runs the whole
// body as before; a worker (control-free) takes the control lock only when
// the exit actually owns control-lane work. The pure shard-local exit — no
// residual multi-key registrations, no scope, no main awaiter, refs held —
// never touches the control lane.
//
// park_needs_control is derived by mark_done from the park_key it snapshotted
// under the task's own owner shard lock (RV2-DEBT-019 race 2): reading park_key
// here unlocked raced wake_task_on_shard_locked's write. Only a net park_key
// still forces control here (cross-shard registry removal). S6-Q1 is now
// complete: the WAKER_JOIN reason went in , and the scope reason
// (parent_scope_id/scope_registered) and the WAKER_SCOPE park_key reason are
// gone in - scope completion bookkeeping runs on the scope owner shard
// lane (scope_on_child_done) and the scope_key waiter store moved to the scope
// owner shard, so both are owner-local. mark_done_needs_control's final form is
// net-key + done_waiters (plus the select/multi-key compat residual).
// completion_reason_out reports whether a genuine completion reason (net key /
// wait_keys / select timers) forced control, as the exact complement of the
// done_waiters-only case: the AWAIT_COMPAT tag in mark_done keys off it, so the
// tag split cannot drift from the control decision (they share this one
// evaluation; review finding).
static int mark_done_needs_control(const rt_executor* ex,
                                   const rt_task* task,
                                   int park_needs_control,
                                   int* completion_reason_out) {
    int completion_reason =
        task->wait_keys_len > 0 || task->select_timers_len > 0 || park_needs_control;
    if (completion_reason_out != NULL) {
        *completion_reason_out = completion_reason;
    }
    if (completion_reason) {
        return 1;
    }
    if (rt_done_waiters_load_before_done(ex) > 0) {
        return 1;
    }
    return 0;
}

void mark_done(rt_executor* ex, rt_task* task, uint8_t result_kind) {
    if (ex == NULL || task == NULL) {
        return;
    }
    // Pin the task across completion: a joiner woken mid-body may consume
    // the result and drop the last handle on another shard before the body
    // finishes touching the task.
    task_add_ref(task);
    // RV2-DEBT-263: the kind is decided FIRST, and by the gate rather than by
    // whoever carried it in. A task whose body ran all the way to a value can
    // still have been cancelled after its last suspension point -- rt_task_poll's
    // TASK_DONE fast path answers from the TARGET and never consults the
    // awaiter's own gate, so once every remaining await resolves from an
    // already-DONE child there is nothing left in the poll that could see the
    // cancel. `cancel` through a live handle is task-global and must be observed
    // by every awaited entitlement before committed success, so the seal is what
    // draws that line.
    //
    // It happens here, at the top, and not beside the result_kind store below,
    // for two reasons. The seal is a single RMW: once it has run the answer
    // cannot change, so there is nothing to keep tight. And a refused value has
    // to leave the slot BEFORE anything downstream can name it --
    // rt_remote_task_on_owner_done sends a reply that names this very slot in a
    // capability, and that runs later in this function.
    int sealed = task_cancel_gate_seal(task);
    if (result_kind == TASK_RESULT_SUCCESS && !RT_DEBT263_COMMIT_SEALED(sealed)) {
        result_kind = TASK_RESULT_CANCELLED;
        // Empties the slot now; the value's own drop is generated code and runs
        // once this lane holds no scheduler lock (rule 8 P2).
        rt_task_result_refuse(ex, task);
    }
    // The task is RUNNING here (just polled to completion, TASK_DONE is not
    // stored until below), and the wake path only reads or clears a task's
    // park_key while that task is parked (WAITING, not enqueued) under its
    // owner shard lock (wake_task_on_shard_locked's WAITING gate). So no other
    // thread writes this RUNNING task's park_key, and this plain read/clear is
    // race-free without a lock (RV2-DEBT-019: the wake-side gate is the
    // load-bearing half; the old unlocked read here raced only because the
    // wake path used to clear park_key on RUNNING tasks too).
    waker_key park = task->park_key;
    // A net park_key needs the cross-shard registry removal. Join keys
    // and scope keys (scope_key store moved to the scope owner shard)
    // are owner-local, so they are removed control-free below (S6-Q1 complete).
    int park_needs_control = waker_valid(park) && waker_is_net(park);
    // Attribute honestly (rule 5): a completion forced onto
    // the control lane SOLELY because a non-worker awaiter is parked on
    // done_cv (done_waiters>0, with no net park_key and no residual
    // multi-key work) is external-await compatibility, counted separately
    // from worker steady-state completion. Net-key / wait_keys / select-
    // timer removals are genuine completion control work and stay COMPLETION.
    // completion_reason comes out of the same evaluation that decides
    // need_control, so the tag split cannot drift from the control decision.
    int completion_reason = 0;
    int need_control = !rt_lane_holds_control() &&
                       mark_done_needs_control(ex, task, park_needs_control, &completion_reason);
    if (need_control) {
        rt_control_lock(ex);
        if (completion_reason) {
            rt_trace_control_lock_site(RT_CTRL_SITE_COMPLETION);
        } else {
            rt_trace_control_lock_site(RT_CTRL_SITE_AWAIT_COMPAT);
        }
    }
    if (task->wait_keys_len > 0) {
        clear_wait_keys(ex, task);
    }
    if (task->select_timers_len > 0) {
        clear_select_timers(ex, task);
    }
    if (waker_valid(park)) {
        remove_waiter(ex, park, task->id);
    }
    task->park_key = waker_none();
    task->park_prepared = 0;
    if (task->kind == TASK_KIND_SLEEP && task->sleep_armed) {
        // Cancelled sleepers leave the deadline index here; fired sleepers
        // were already popped, so the remove is a no-op for them.
        rt_shard* sleep_shard = rt_task_owner_shard(ex, task);
        rt_shard_lock(sleep_shard);
        (void)rt_sleep_store_remove(&sleep_shard->sleep_store, task->id);
        rt_shard_unlock(sleep_shard);
        task->sleep_armed = 0;
    }
    // enabling change (rule 1): the result fields must be
    // written before the TASK_DONE release store, so a joiner's acquire-load
    // of TASK_DONE (rt_task_poll, now control-free) publishes them without
    // needing the control lock. Nothing else in this function reads either
    // field, so the reorder is behavior-preserving. `result_kind` is what the
    // gate decided at the top of this function (RV2-DEBT-263), not what the
    // caller handed in.
    task->result_kind = result_kind;
    // RV2-DEBT-263 proof window: the gate is closed and the kind is chosen, the
    // slot is already empty if the value was refused, and TASK_DONE is not
    // published yet. A cancel landing here must be REFUSED -- its CAS finds
    // SEALED -- and must not leave a task answering Success while something
    // believes it cancelled it.
    RT_SYNC_POINT(SP_MARKDONE_AFTER_SEAL_BEFORE_DONE);
    // A suspension frame a cancellation left behind without ever resuming
    // compiled code (rt_async_yield stashes it here before completing the task
    // by this same route). Every completion funnels through mark_done exactly
    // once per task, regardless of how many scope-drain re-parks deferred
    // getting here, so this is the one place that can give it back exactly
    // once. What giving it back means -- walk the members first, or hand the
    // storage straight to the allocator -- is the frame's own answer, and this
    // site does not supply it.
    if (task->reclaim_frame != NULL) {
        rt_frame_release(task->reclaim_frame_ops, task->reclaim_frame);
        task->reclaim_frame = NULL;
        task->reclaim_frame_ops = NULL;
    }
    rt_far_task_release_owned(ex, task);
    rt_immediate_on_release_owned(ex, task);
    rt_remote_task_release_owned(ex, task);
    // The same fact the store below publishes, recorded where the free is
    // decided. A handle drop must be able to answer "was this the last
    // reference to a COMPLETED task?" out of its own decrement; reading the
    // status separately afterwards let a poller resurrect the task in between
    // and free it first (see rt_task.handle_refs). Raised before the status
    // store so the two can never be observed the other way round: a thread that
    // sees TASK_DONE and drops the last reference always finds the flag.
    task_mark_completed(task);
    rt_task_status_store_done_for_external_awaiters(task);
    rt_remote_task_on_owner_done(ex, task);
    RT_SYNC_POINT(SP_MARKDONE_BEFORE_DONEWAITERS_LOAD);
    task_enqueued_store(task, 0);
    task->state = NULL;
    // Scope completion bookkeeping (S5-Q8): runs on the scope
    // owner shard lane. The steady same-owner non-failfast child-done is
    // control-free under the pinned shard lock; only a cross-owner (re-placed
    // child) or a failfast-triggering completion takes the counted control
    // fallback inside scope_on_child_done, so it no longer forces
    // mark_done_needs_control on the request hot path.
    scope_on_child_done(ex, task, result_kind);
    wake_key_all_with_policy(ex, join_key(task->id), 0);
    rt_done_cv_broadcast_after_done(ex);
    if (need_control) {
        rt_control_unlock(ex);
    }
    // Drop the completion pin; the release frees under the control lane when
    // this was the last reference.
    task_release_lane_aware(ex, task);
}

void apply_poll_outcome(rt_executor* ex, rt_task* task, poll_outcome outcome) {
    if (ex == NULL || task == NULL) {
        return;
    }
    // What keeps a polled task addressable during its own turn is its RUNNING
    // status, not a reference: a release frees only a task that has completed,
    // so a RUNNING one cannot be reclaimed under its poller. Every arm below
    // ends that protection -- it publishes READY, WAITING or DONE -- and then
    // goes on using the pointer.
    //
    // The yielded arm is the one AddressSanitizer caught. It publishes
    // TASK_READY and re-pushes, and ready_push_with_policy resolves the owner
    // shard, BLOCKS on that shard's lock, and re-reads the task's status on the
    // far side of the acquisition. A task that is READY and not yet enqueued
    // can be woken by an awaiting poll in that window, popped by another
    // worker, run to completion, and freed by the last handle drop, all while
    // this thread waits for the lock; the re-read then reads freed memory.
    //
    // A reference held across the whole function is what makes the pointer
    // outlive the turn -- the same pin mark_done takes, one level down. The
    // release is the last statement, so a turn that held the final reference
    // frees here, with no lock held.
    //
    // RT_POLL_OUTCOME_PIN_NEGATIVE_CONTROL removes the pin, which MUST make the
    // yielded re-push read a freed task under AddressSanitizer; that build is
    // how the row proves it is asking a real question.
#ifndef RT_POLL_OUTCOME_PIN_NEGATIVE_CONTROL
    task_add_ref(task);
#endif
    switch (outcome.kind) {
        case POLL_DONE_SUCCESS:
            mark_done(ex, task, TASK_RESULT_SUCCESS);
            break;
        case POLL_DONE_CANCELLED:
            if (task->scope_id != 0) {
                // Owner-cancelled scope teardown (S5-Q14, ): the
                // child cancel walk needs the control lane (re-derivation:
                // cancel_task reads sibling owner_shard_ids that F2 self-replace
                // writes under control), so this rare branch takes control. But
                // same-owner child-done now runs control-free on the pinned
                // shard lane, so control no longer excludes it - the re-park on
                // scope_key uses register-then-verify (scope_key routes to the
                // pinned store) to avoid losing a child-done wake.
                int need_control = !rt_lane_holds_control();
                if (need_control) {
                    rt_control_lock(ex);
                    rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
                }
                rt_scope* scope = get_scope(ex, task->scope_id);
                rt_shard* pinned = scope != NULL ? rt_scope_owner_shard(ex, scope) : NULL;
                size_t active = 0;
                if (scope != NULL) {
                    rt_shard_lock(pinned);
                    active = scope->active_children;
                    rt_shard_unlock(pinned);
                }
                if (scope != NULL && active > 0) {
                    task->cancel_pending = 1;
                    scope_cancel_children_controlled(ex, scope);
                    task->state = outcome.state;
                    waker_key key = scope_key(scope->id);
                    prepare_park(ex, task, key, 0);
                    rt_shard_lock(pinned);
                    size_t active_after = scope->active_children;
                    rt_shard_unlock(pinned);
                    if (active_after != 0) {
                        park_current(ex, key);
                        if (need_control) {
                            rt_control_unlock(ex);
                        }
                        break;
                    }
                    // All children drained during the walk/registration: undo
                    // the park and fall through to exit + mark_done.
                    remove_waiter(ex, key, task->id);
                    task->park_prepared = 0;
                    task->park_key = waker_none();
                    pending_key = waker_none();
                }
                if (scope != NULL) {
                    rt_shard_lock(pinned);
                    scope_exit_locked(ex, scope);
                    rt_shard_unlock(pinned);
                }
                if (need_control) {
                    rt_control_unlock(ex);
                }
            }
            mark_done(ex, task, TASK_RESULT_CANCELLED);
            break;
        case POLL_YIELDED:
            task->state = outcome.state;
            task_status_store(task, TASK_READY);
            // Yielded tasks go through the inject queue to avoid local LIFO starvation.
            (void)ready_push_yielded_task(ex, task->id);
            tick_virtual(ex);
            break;
        case POLL_PARKED:
            task->state = outcome.state;
            park_current(ex, outcome.park_key);
            break;
        default:
            panic_msg("async: unknown poll outcome");
            break;
    }
#ifndef RT_POLL_OUTCOME_PIN_NEGATIVE_CONTROL
    task_release_lane_aware(ex, task);
#endif
}
