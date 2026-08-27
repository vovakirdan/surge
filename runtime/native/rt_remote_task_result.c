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
    // Serving a result does not always consume it. WHICH of the four it is is
    // decided by the task's entitlement counts and by what the value can do,
    // under the owner lock and never from the handle refcount:
    //
    //   owns nothing         -> the bytes ARE the value. Copying them
    //                           reclaims nothing and leaves nothing behind,
    //                           which is why two joiners polling one task
    //                           both read the same word;
    //   a later asker can    -> the duplication the handle clone installed
    //   still come              builds this asker an independent value and
    //                           the slot keeps its own for that asker;
    //   the last asker       -> the value MOVES out, which is what makes a
    //                           cohort of E handles cost E-1 duplications and
    //                           one move (§10); a task nobody cloned is asked
    //                           once, and its first asker is already the last.
    //
    // The far path is deliberately excluded: a result that came back from
    // another shard is carried in a lease exactly one holder may adopt, and
    // that lease is its own answer to "who gets this".
    if (!result_is_far_carried(producer)) {
        rt_executor* ex = ensure_exec();
        const rt_value_ops* operations = producer->result.operations;
        int has_value = rt_value_cell_is_ready(&producer->result);
        // An asker is named by its task; an external awaiter (rt_task_await
        // from a thread that is no task) by its thread, which is enough for a
        // WAIT to be recognised on its way back.
        static _Thread_local uint8_t external_asker;
        const void* asker = holder != NULL ? (const void*)holder : (const void*)&external_asker;
        rt_task_take_mode mode =
            rt_task_entitlement_begin_take(ex, producer, asker, has_value, operations);
        switch (mode) {
            case RT_TASK_TAKE_COPY:
                if (out_dst != NULL) {
                    rt_value_cell_copy_value(&producer->result, out_dst);
                }
                break;
            case RT_TASK_TAKE_CLONE:
                if (out_dst != NULL) {
                    rt_value_duplicate_detached(rt_task_entitlement_duplicate(producer, operations),
                                                out_dst,
                                                rt_value_cell_value(&producer->result));
                }
                break;
            case RT_TASK_TAKE_MOVE: {
                // The obligation is the caller's from here, and the slot is
                // left with nothing to destroy.
                void* value = rt_value_cell_value(&producer->result);
                if (out_dst != NULL) {
                    rt_value_move_init_detached(operations, out_dst, value);
                } else {
                    rt_value_drop_in_place_detached(operations, value);
                }
                (void)rt_value_cell_commit_move(&producer->result);
                break;
            }
            case RT_TASK_TAKE_REFUSED:
                // An owning result that more than one handle can ask for,
                // whose type carries no duplication at all. Only a dynamic
                // array reaches here: duplicating a buffer is not an
                // operation the runtime offers, and answering a later asker
                // with the same buffer would give two of them one thing to
                // free. Refused at the moment the extra asker arrives, as the
                // old representation refused it; the compile-time refusal is
                // Wave F's.
                rt_task_entitlement_finish_take(ex, producer, mode);
                panic_msg("async: a cloned task handle cannot be served a result "
                          "that cannot be duplicated");
                return 2;
            case RT_TASK_TAKE_WAIT:
                // The last asker, with a reader still copying out of the slot.
                // Nothing is retired: the caller parks on the task's join key
                // (or on done_cv) and asks again when the reader that retires
                // last wakes it. A DONE task never otherwise answers 0.
                return 0;
            case RT_TASK_TAKE_NONE:
                break;
        }
        rt_task_entitlement_finish_take(ex, producer, mode);
        if (mode == RT_TASK_TAKE_NONE && has_value == 0) {
            // Nothing to hand over: either this task published no value, or an
            // earlier asker moved the only one out. Both answer "gone" rather
            // than Success, because Success here would promise a payload that
            // the caller's storage does not hold.
            return rt_value_cell_was_taken(&producer->result) ? 2 : kind;
        }
        return kind;
    }
    if (!rt_far_task_adopt_result(producer, holder)) {
        return 2;
    }
    void* value = rt_value_cell_value(&producer->result);
    if (value == NULL) {
        // Nothing left to hand over: either this task published no value, or an
        // earlier asker moved the only one out. Both answer "gone" rather than
        // Success, because Success here would promise a payload that the
        // caller's storage does not hold.
        return rt_value_cell_was_taken(&producer->result) ? 2 : kind;
    }
    // The single asker MOVES it out, which is what leaves the slot with nothing
    // to destroy: the obligation is the caller's from here.
    if (out_dst != NULL) {
        rt_value_move_init_detached(producer->result.operations, out_dst, value);
    } else {
        rt_value_drop_in_place_detached(producer->result.operations, value);
    }
    (void)rt_value_cell_commit_move(&producer->result);
    return kind;
}

rt_result_source rt_remote_task_pin_result(rt_task* task) {
    rt_result_source source = {0, 0, 0, 0};
    if (task == NULL || rt_remote_task_result_kind(task) != 1 ||
        !rt_value_cell_is_ready(&task->result)) {
        // Nothing to name. A cancelled outcome and a task with no result value
        // both land here, and both are answers rather than failures.
        //
        // The KIND is asked first, and it fails closed (RV2-DEBT-263). A
        // completion that refuses its value empties the slot before it publishes
        // TASK_DONE (rt_task_result_refuse), so a Cancelled task reaching here
        // with a ready cell should not be reachable -- but the holder of this
        // capability moves the value out UNCONDITIONALLY (finish_retry,
        // rt_remote_task_api.c) while the generated Cancelled arm never reads
        // the storage it lands in (emit_crossing_far_task.go), so naming a slot
        // on a non-Success answer would drop an obligation on the floor. The
        // local path has always asked the kind first
        // (rt_far_task_take_result); this one does too now.
        return source;
    }
    // The pin is the whole reason a capability is safe to send: the task it
    // names stays allocated, and its slot keeps the generation the capability
    // recorded, until whoever holds this releases it.
    task_add_ref(task);
    source.task_id = task->id;
    source.task_generation = task->generation;
    source.result_generation = rt_value_cell_generation(&task->result);
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
        void* value = rt_value_cell_value(&task->result);
        if (out_dst != NULL) {
            rt_value_move_init_detached(task->result.operations, out_dst, value);
        } else {
            rt_value_drop_in_place_detached(task->result.operations, value);
        }
        (void)rt_value_cell_commit_move(&task->result);
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
