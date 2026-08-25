#include "rt_remote_task_internal.h"

#include "rt_value_ops.h"

// Claims the producer's returned-result lease under the state lock: flips
// far_task_result_state 1 -> 2 and detaches far_task_result_lease. Returns
// the detached lease, or NULL when another path already claimed it.
static rt_far_task_lease* result_lease_take_locked(rt_task* producer) {
    uint8_t expected = 1;
    if (!atomic_compare_exchange_strong_explicit(&producer->far_task_result_state,
                                                 &expected,
                                                 2,
                                                 memory_order_acq_rel,
                                                 memory_order_acquire)) {
        return NULL;
    }
    return atomic_exchange_explicit(&producer->far_task_result_lease, NULL, memory_order_acq_rel);
}

int rt_far_task_adopt_result(rt_task* producer, rt_task* holder) {
    if (producer == NULL) {
        return 1;
    }
    rt_far_task_lease* lease =
        atomic_load_explicit(&producer->far_task_result_lease, memory_order_acquire);
    if (lease == NULL) {
        return atomic_load_explicit(&producer->far_task_result_state, memory_order_acquire) == 0;
    }
    rt_remote_task_state* state = rt_remote_task_state_get(lease->executor);
    if (state == NULL) {
        return 0;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* taken = result_lease_take_locked(producer);
    if (taken == lease) {
        lease->result_owner = NULL;
        lease->holder = holder;
        atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_OPEN, memory_order_release);
    }
    pthread_mutex_unlock(&state->lock);
    return taken == lease;
}

void rt_far_task_release_result(rt_executor* ex, rt_task* producer) {
    (void)ex;
    if (producer == NULL) {
        return;
    }
    rt_far_task_lease* lease =
        atomic_load_explicit(&producer->far_task_result_lease, memory_order_acquire);
    rt_remote_task_state* state = lease != NULL ? rt_remote_task_state_get(lease->executor) : NULL;
    if (state == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    rt_far_task_lease* taken = result_lease_take_locked(producer);
    if (taken == lease) {
        lease->result_owner = NULL;
        atomic_store_explicit(&lease->state, RT_FAR_TASK_LEASE_RELEASING, memory_order_release);
    }
    pthread_mutex_unlock(&state->lock);
    if (taken == lease) {
        rt_far_task_lease_release_route(lease);
        rt_far_task_lease_drop_ref(lease);
    }
}

// Reports whether the result came back from another shard, which carries it in
// a lease exactly one holder may adopt. That lease is its own answer to "who
// gets this", and it answers a second asker with "gone" rather than by
// reclaiming under the first, so the copy-per-asker path leaves it alone.
static int result_is_far_carried(const rt_task* producer) {
    return atomic_load_explicit(&producer->far_task_result_lease, memory_order_acquire) != NULL ||
           atomic_load_explicit(&producer->far_task_result_state, memory_order_acquire) != 0;
}

uint8_t rt_far_task_take_result(rt_task* producer, rt_task* holder, void* out_dst) {
    if (producer == NULL) {
        return 2;
    }
    uint8_t kind = rt_remote_task_result_kind(producer);
    if (kind != 1) {
        return kind;
    }
    // Serving a result does not always consume it. WHICH of the three it is is
    // decided by what this value can do, not by who is asking:
    //
    //   a second asker exists  -> the duplication the handle clone installed
    //                             builds this asker an independent value and
    //                             the slot keeps its own, so a later asker
    //                             still has one to read;
    //   owns nothing           -> the bytes ARE the value. Copying them
    //                             reclaims nothing and leaves nothing behind,
    //                             which is why two joiners polling one task
    //                             both read the same word;
    //   owns something         -> exactly one asker may have it, so the take is
    //                             a move and the slot is left with nothing to
    //                             destroy.
    //
    // The far path is deliberately excluded from the first two: a result that
    // came back from another shard is carried in a lease exactly one holder may
    // adopt, and that lease is its own answer to "who gets this".
    if (!result_is_far_carried(producer) && rt_task_result_is_ready(&producer->result)) {
        const rt_value_ops* operations = producer->result.operations;
        if (producer->result_shared && producer->result_duplicate != NULL) {
            if (out_dst != NULL) {
                rt_value_duplicate_detached(
                    producer->result_duplicate, out_dst, rt_task_result_value(&producer->result));
            }
            return kind;
        }
        if ((operations->layout.flags & RT_VALUE_FLAG_DROPPABLE) == 0) {
            if (out_dst != NULL) {
                rt_task_result_copy_value(&producer->result, out_dst);
            }
            return kind;
        }
        if (producer->result_shared) {
            // An owning result that more than one handle can ask for, whose
            // type carries no duplication at all. Only a dynamic array reaches
            // here: duplicating a buffer is not an operation the runtime
            // offers, and answering a second asker with the same buffer would
            // give two of them one thing to free.
            //
            // Saying so is where the old representation said it, at the moment
            // the second asker arrives, because the operation that took on this
            // obligation is the handle clone and this is the first point that
            // can see it could not be kept.
            panic_msg("async: a cloned task handle cannot be served a result "
                      "that cannot be duplicated");
            return 2;
        }
    }
    if (!rt_far_task_adopt_result(producer, holder)) {
        return 2;
    }
    void* value = rt_task_result_value(&producer->result);
    if (value == NULL) {
        // Nothing left to hand over: either this task published no value, or an
        // earlier asker moved the only one out. Both answer "gone" rather than
        // Success, because Success here would promise a payload that the
        // caller's storage does not hold.
        return rt_task_result_was_taken(&producer->result) ? 2 : kind;
    }
    // The single asker MOVES it out, which is what leaves the slot with nothing
    // to destroy: the obligation is the caller's from here.
    if (out_dst != NULL) {
        rt_value_move_init_detached(producer->result.operations, out_dst, value);
    } else {
        rt_value_drop_in_place_detached(producer->result.operations, value);
    }
    (void)rt_task_result_commit_move(&producer->result);
    return kind;
}

rt_result_source rt_remote_task_pin_result(rt_task* task) {
    rt_result_source source = {0, 0, 0, 0};
    if (task == NULL || !rt_task_result_is_ready(&task->result)) {
        // Nothing to name. A cancelled outcome and a task with no result value
        // both land here, and both are answers rather than failures.
        return source;
    }
    // The pin is the whole reason a capability is safe to send: the task it
    // names stays allocated, and its slot keeps the generation the capability
    // recorded, until whoever holds this releases it.
    task_add_ref(task);
    source.task_id = task->id;
    source.task_generation = task->generation;
    source.result_generation = rt_task_result_generation(&task->result);
    source.owner_shard_id = task->owner_shard_valid != 0 ? task->owner_shard_id : 0;
    return source;
}

// Resolves a capability to the task it names, or NULL when it names nothing
// live. Every field is checked: an id can be reused, a task can complete and be
// replaced, and a slot can be rebound, so the id alone proves nothing.
static rt_task* result_source_task(rt_executor* ex, const rt_result_source* source) {
    if (ex == NULL || source == NULL || source->task_id == 0 || source->result_generation == 0) {
        return NULL;
    }
    rt_task* task = get_task(ex, source->task_id);
    if (task == NULL || task->generation != source->task_generation) {
        return NULL;
    }
    return task;
}

int rt_remote_task_take_result_source(rt_executor* ex,
                                      const rt_result_source* source,
                                      void* out_dst) {
    rt_task* task = result_source_task(ex, source);
    if (task == NULL) {
        return 0;
    }
    int taken = 0;
    if (rt_task_result_matches(&task->result, source)) {
        void* value = rt_task_result_value(&task->result);
        if (out_dst != NULL) {
            rt_value_move_init_detached(task->result.operations, out_dst, value);
        } else {
            rt_value_drop_in_place_detached(task->result.operations, value);
        }
        (void)rt_task_result_commit_move(&task->result);
        taken = 1;
    }
    task_release_lane_aware(ex, task);
    return taken;
}

void rt_remote_task_release_result_source(rt_executor* ex, const rt_result_source* source) {
    rt_task* task = result_source_task(ex, source);
    if (task != NULL) {
        task_release_lane_aware(ex, task);
    }
}
