#include "remote_task_behavior.h"
#include "rt_task_refs.h"
#include "rt_value_ops.h"

#include <string.h>

// The caller-teardown sweep (rt_remote_task_release_owned,
// runtime/native/rt_remote_task_api.c) for AWAIT/CANCEL pendings. Unlike the
// EXECUTE-family sweep (rt_immediate_on_release_owned), this does NOT route
// a cancel to the owner: a routed cancel against an already-consumed
// handle state would fail its one-shot CAS and produce a bogus reply while
// leaking the far task's own reference. These rows drive the pending's own
// free path directly (the same technique the sibling rows in
// remote_task_behavior_drop.c use for task->result_bits): a real far-task
// heap AWAIT result requires a
// live two-shard transport round trip to construct honestly, so the
// deterministic proof constructs the pending's terminal state directly and
// exercises the exact functions the runtime uses at that state, rather than
// racing a real reply against a real cancellation.

enum { RTB_CALLER_ABANDON_MARK_ID = 61 };

static void rtb_caller_abandon_reset(void) {
    atomic_store_explicit(&rtb_result_drop_calls, 0, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_id, 0, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_value, NULL, memory_order_release);
}

static rt_task* rtb_caller_abandon_new_task(rt_executor* ex) {
    rt_task* task = NULL;
    if (rt_remote_spawn_create_body_task(ex, POLL_RTB_DROP_BODY, NULL, 0, 0, &task) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        task == NULL) {
        return NULL;
    }
    return task;
}

static rt_remote_task_pending*
rtb_caller_abandon_new_pending(rt_executor* ex, rt_remote_task_op op, uint64_t caller_id) {
    rt_far_task_handle handle = {
        .task_id = 999,
        .generation = 1,
        .owner_shard_id = 0,
        .kind = RT_FAR_HANDLE_KIND_TASK,
    };
    rt_remote_task_pending* pending = rt_remote_task_pending_new(ex, &handle, 0, op, 1);
    if (pending != NULL) {
        pending->caller_task_id = caller_id;
    }
    return pending;
}

// Row: a landed AWAIT reply nobody consumed is reclaimed exactly once when the
// caller-teardown sweep releases the pending's last reference.
//
// What "nobody consumed" means changed with the reply itself. The reply used to
// CARRY the value as a word, and the pending destroyed it. It NAMES the
// producer's result now -- a capability plus a pin -- so the pending releases
// the pin instead, and the producer's own slot destroys what it still holds,
// exactly once, where it always did. This row drives that: it pins a real
// producer's result, abandons the pending, and asks the producer.
int rtb_mode_caller_abandon_drops_landed_result(void) {
    rt_executor* ex = ensure_exec();
    rtb_caller_abandon_reset();
    const rt_task* caller = rtb_caller_abandon_new_task(ex);
    if (caller == NULL) {
        return rtb_fail("caller-abandon-drop: caller task creation failed");
    }
    rt_task* producer = NULL;
    if (rt_remote_spawn_create_body_task(
            ex, POLL_RTB_DROP_BODY, NULL, 0, RTB_DROP_MARK_ID, &producer) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        producer == NULL) {
        return rtb_fail("caller-abandon-drop: producer creation failed");
    }
    // A published result the producer still owns: the value the pin protects.
    void* destination = rt_value_cell_publish_storage(&producer->result);
    if (destination == NULL) {
        return rtb_fail("caller-abandon-drop: producer result storage missing");
    }
    rtb_shipped_state published = {RTB_DROP_MARK_ID};
    rt_value_move_init_detached(producer->result.operations, destination, &published);
    if (rt_value_cell_commit(&producer->result) != RT_SLOT_CONTROL_OK) {
        return rtb_fail("caller-abandon-drop: producer result publish failed");
    }
    producer->result_kind = TASK_RESULT_SUCCESS;
    task_status_store(producer, TASK_DONE);
    // Completing is what sets the completion flag in the task's reference word,
    // and the release below decides the free from its own decrement plus that
    // flag -- asking the status afterwards was a double free. A stand that
    // stores the status by hand performs only half of a completion, so it seals
    // the other half here; without this the release finds an uncompleted task,
    // declines the free, and the result this row is about is never dropped.
    task_mark_completed(producer);

    rt_remote_task_pending* pending =
        rtb_caller_abandon_new_pending(ex, RT_REMOTE_TASK_OP_AWAIT, caller->id);
    if (pending == NULL) {
        return rtb_fail("caller-abandon-drop: pending creation failed");
    }
    pending->result_kind = 1;
    pending->result_source = rt_remote_task_pin_result(producer);
    if (pending->result_source.task_id != producer->id) {
        return rtb_fail("caller-abandon-drop: the reply named no producer result");
    }
    // pending_new leaves refs at 1 (the caller's own slot) -- this row models
    // "the reply already landed and dispatch_reply already released its own
    // ref", so no extra add_ref here: the sweep's release is the LAST one.
    rt_remote_task_release_owned(ex, caller);
    if (atomic_load_explicit(&rtb_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("caller-abandon-drop: an abandoned reply must not destroy the result");
    }
    // Releasing the pin is what lets the producer be freed, and its own slot
    // destroys what it still holds on the way out -- exactly once.
    task_release_lane_aware(ex, producer);
    if (atomic_load_explicit(&rtb_drop_calls, memory_order_acquire) != 1) {
        return rtb_fail("caller-abandon-drop: result not dropped exactly once");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Negative control: a Copy result (result_type_id 0) never reaches the
// drop dispatch, even when abandoned.
int rtb_mode_caller_abandon_copy_inert(void) {
    rt_executor* ex = ensure_exec();
    rtb_caller_abandon_reset();
    const rt_task* caller = rtb_caller_abandon_new_task(ex);
    if (caller == NULL) {
        return rtb_fail("caller-abandon-copy-inert: caller task creation failed");
    }
    rt_remote_task_pending* pending =
        rtb_caller_abandon_new_pending(ex, RT_REMOTE_TASK_OP_AWAIT, caller->id);
    if (pending == NULL) {
        return rtb_fail("caller-abandon-copy-inert: pending creation failed");
    }
    pending->result_kind = 1;
    // An inert Copy result: no capability, no block -- nothing rides beside
    // the kind any more, so there is nothing here for a release to touch.
    pending->result_type_id = 0;
    rt_remote_task_release_owned(ex, caller);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("caller-abandon-copy-inert: inert Copy result reached the drop dispatch");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Negative control: a result already consumed by the caller's own
// finish_retry cleared the obligation before the sweep could ever see it
// (this row simulates that ordering directly) -- the sweep's release must
// not double-drop.
int rtb_mode_caller_abandon_consumed_no_double_drop(void) {
    rt_executor* ex = ensure_exec();
    rtb_caller_abandon_reset();
    const rt_task* caller = rtb_caller_abandon_new_task(ex);
    if (caller == NULL) {
        return rtb_fail("caller-abandon-consumed: caller task creation failed");
    }
    rt_remote_task_pending* pending =
        rtb_caller_abandon_new_pending(ex, RT_REMOTE_TASK_OP_AWAIT, caller->id);
    if (pending == NULL) {
        return rtb_fail("caller-abandon-consumed: pending creation failed");
    }
    void* block = rt_alloc(RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    if (block == NULL) {
        return rtb_fail("caller-abandon-consumed: block alloc failed");
    }
    pending->result_kind = 1;
    pending->result_type_id = RTB_CALLER_ABANDON_MARK_ID;
    // Simulate finish_retry's own clear-before-consume ordering: ownership
    // already moved to the caller, which frees the value itself. The store
    // looks redundant to an analyser and is the POINT of this stand: it pins
    // that the clear happens, in this order, whatever the field held before.
    // cppcheck-suppress redundantAssignment
    pending->result_type_id = 0;
    rt_free((uint8_t*)block, RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    rt_remote_task_release_owned(ex, caller);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("caller-abandon-consumed: sweep double-dropped a consumed result");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row: the sweep's filter is exact -- it releases ONLY the AWAIT/CANCEL
// pending matching the given caller_task_id, leaving an EXECUTE-family
// pending (rt_immediate_on_release_owned's own territory) and an
// AWAIT pending belonging to a DIFFERENT caller both untouched. Proves the
// op+caller_task_id filter, not just the release mechanism.
int rtb_mode_caller_abandon_filters_by_op_and_caller(void) {
    rt_executor* ex = ensure_exec();
    rtb_caller_abandon_reset();
    const rt_task* caller_a = rtb_caller_abandon_new_task(ex);
    const rt_task* caller_b = rtb_caller_abandon_new_task(ex);
    if (caller_a == NULL || caller_b == NULL) {
        return rtb_fail("caller-abandon-filter: caller task creation failed");
    }
    const rt_remote_task_pending* target =
        rtb_caller_abandon_new_pending(ex, RT_REMOTE_TASK_OP_AWAIT, caller_a->id);
    rt_remote_task_pending* other_caller =
        rtb_caller_abandon_new_pending(ex, RT_REMOTE_TASK_OP_AWAIT, caller_b->id);
    rt_remote_task_pending* other_op =
        rtb_caller_abandon_new_pending(ex, RT_REMOTE_TASK_OP_EXECUTE, caller_a->id);
    if (target == NULL || other_caller == NULL || other_op == NULL) {
        return rtb_fail("caller-abandon-filter: pending creation failed");
    }
    // Give the two untouched pendings a second ref each so a wrongful
    // release would leave refs at 1 (observable) rather than freeing them
    // outright (unobservable through this handle alone).
    rt_remote_task_pending_add_ref(other_caller);
    rt_remote_task_pending_add_ref(other_op);

    rt_remote_task_release_owned(ex, caller_a);

    if (target->caller_task_id != 0) {
        return rtb_fail("caller-abandon-filter: target pending's caller_task_id was not cleared");
    }
    if (other_caller->caller_task_id != caller_b->id) {
        return rtb_fail("caller-abandon-filter: a different caller's pending was touched");
    }
    if (other_op->caller_task_id != caller_a->id) {
        return rtb_fail(
            "caller-abandon-filter: an EXECUTE-op pending was touched by the AWAIT/CANCEL sweep");
    }
    rt_remote_task_pending_release(other_caller);
    rt_remote_task_pending_release(other_op);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row: when the reply has NOT landed yet (the in-flight-request ref is
// still live), the sweep releases only the caller's own reference and
// leaves the pending listed -- it must survive to be resolved normally
// later, not be force-freed or force-cancelled.
int rtb_mode_caller_abandon_in_flight_survives(void) {
    rt_executor* ex = ensure_exec();
    rtb_caller_abandon_reset();
    const rt_task* caller = rtb_caller_abandon_new_task(ex);
    if (caller == NULL) {
        return rtb_fail("caller-abandon-in-flight: caller task creation failed");
    }
    rt_remote_task_pending* pending =
        rtb_caller_abandon_new_pending(ex, RT_REMOTE_TASK_OP_CANCEL, caller->id);
    if (pending == NULL) {
        return rtb_fail("caller-abandon-in-flight: pending creation failed");
    }
    // Mirrors start_remote_task's own ref model exactly: pending_new
    // leaves refs at 1 (the caller's slot), start_remote_task immediately
    // adds the in-flight-request ref.
    rt_remote_task_pending_add_ref(pending);

    rt_remote_task_release_owned(ex, caller);

    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("caller-abandon-in-flight: nothing should have dropped yet");
    }
    // The pending must still be alive and listed: releasing its last
    // reference (simulating the eventual reply's own dispatch_reply
    // consume) must be safe and must not double-free.
    rt_remote_task_pending_consume(pending);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
