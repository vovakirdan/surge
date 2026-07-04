#include "rt_async_internal.h"

// Async runtime task API and task builtins.

static rt_task* spawn_checkpoint_task_locked(rt_executor* ex);
static void poll_ready_child_inline(rt_executor* ex, rt_task* current, rt_task* target);

void* __task_create(
    uint64_t poll_fn_id,
    void* state) { // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_control_lock(ex);
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        rt_control_unlock(ex);
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->poll_fn_id = (int64_t)poll_fn_id;
    task->state = state;
    task_status_store(task, TASK_READY);
    task->kind = TASK_KIND_USER;
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_slot_store(ex, id, task);
    rt_task* parent = rt_current_task();
    if (parent != NULL) {
        task_add_child(parent, id);
        rt_task_inherit_placement(task, parent);
    }
    rt_task_assign_spawn_owner(task);
    ready_push(ex, id);
    rt_control_unlock(ex);
    return task;
}

void* __task_state(void) { // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
    rt_task* task = rt_current_task();
    if (task == NULL) {
        panic_msg("async: __task_state without current task");
        return NULL;
    }
    void* state = task->state;
    task->state = NULL;
    return state;
}

void rt_task_wake(void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_control_lock(ex);
    rt_task* target = task_from_handle(task);
    if (target == NULL || task_status_load(target) == TASK_DONE) {
        rt_control_unlock(ex);
        return;
    }
    const rt_task* current = rt_current_task();
    if (current != NULL && current->scope_id != 0 && target->parent_scope_id == 0) {
        const rt_scope* scope = get_scope(ex, current->scope_id);
        if (scope != NULL) {
            target->parent_scope_id = scope->id;
        }
    }
    wake_task(ex, target->id, 1);
    rt_control_unlock(ex);
}

uint8_t rt_task_poll(void* task, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return 2;
    }
    rt_control_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_control_unlock(ex);
        panic_msg("async poll outside task");
        return 2;
    }
    if (rt_current_task_id() == target->id) {
        rt_control_unlock(ex);
        panic_msg("task cannot await itself");
        return 2;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_control_unlock(ex);
        panic_msg("async: missing current task");
        return 2;
    }
    if (current_task_cancelled(ex)) {
        rt_control_unlock(ex);
        return 0;
    }
    if (target->kind == TASK_KIND_USER && task_status_load(target) == TASK_READY &&
        task_enqueued_load(target) != 0 && ready_take_current_local_tail(ex, target->id)) {
        poll_ready_child_inline(ex, current, target);
    }
    if (task_status_load(target) != TASK_WAITING && task_status_load(target) != TASK_DONE) {
        wake_task(ex, target->id, 1);
    }
    if (task_status_load(target) == TASK_DONE) {
        uint8_t kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        task_release(ex, target);
        rt_control_unlock(ex);
        return kind;
    }
    if (target->kind != TASK_KIND_CHECKPOINT) {
        waker_key key = join_key(target->id);
        prepare_park(ex, current, key, 0);
        pending_key = key;
        // Register-then-verify: the target may complete on its own shard
        // between the DONE check above and the registration; its completion
        // drain and this insert serialize on the target owner's store lock,
        // so re-checking after registering closes the stranded-entry race.
        if (task_status_load(target) == TASK_DONE) {
            remove_waiter(ex, key, current->id);
            current->park_prepared = 0;
            current->park_key = waker_none();
            pending_key = waker_none();
            uint8_t kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
            if (out_bits != NULL) {
                *out_bits = target->result_bits;
            }
            task_release(ex, target);
            rt_control_unlock(ex);
            return kind;
        }
    }
    rt_control_unlock(ex);
    return 0;
}

static void poll_ready_child_inline(rt_executor* ex, rt_task* current, rt_task* target) {
    if (ex == NULL || current == NULL || target == NULL) {
        return;
    }
    rt_shard* owner_shard = rt_task_owner_shard(ex, target);
    rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (owner_shard == NULL || scheduler == NULL) {
        return;
    }
    task_enqueued_store(target, 0);
    task_status_store(target, TASK_RUNNING);
    (void)task_wake_token_exchange(target, 0);
    rt_shard_lock(owner_shard);
    scheduler->running_count++;
    rt_shard_unlock(owner_shard);
    rt_set_current_task(target);
    rt_control_unlock(ex);

    task_polling_enter(target);
    poll_outcome outcome = poll_task(ex, target);
    task_polling_exit(target);

    rt_control_lock(ex);
    rt_shard_lock(owner_shard);
    if (scheduler->running_count > 0) {
        scheduler->running_count--;
    }
    rt_shard_unlock(owner_shard);
    apply_poll_outcome(ex, target, outcome);
    rt_set_current_task(current);
}

void rt_task_await(void* task, uint8_t* out_kind, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return;
    }
    if (rt_worker_count() > 1) {
        rt_control_lock(ex);
        if (task_status_load(target) != TASK_WAITING && task_status_load(target) != TASK_DONE) {
            wake_task(ex, target->id, 1);
        }
        atomic_fetch_add_explicit(&ex->done_waiters, 1, memory_order_acq_rel);
        while (task_status_load(target) != TASK_DONE) {
            pthread_cond_wait(&ex->done_cv, &ex->lock);
        }
        atomic_fetch_sub_explicit(&ex->done_waiters, 1, memory_order_acq_rel);
        if (out_kind != NULL) {
            *out_kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        }
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        task_release(ex, target);
        rt_control_unlock(ex);
        return;
    }
    rt_task* current = rt_current_task();
    run_until_done(ex, target, out_kind, out_bits);
    rt_set_current_task(current);
    rt_control_lock(ex);
    task_release(ex, target);
    rt_control_unlock(ex);
}

void rt_task_cancel(void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    const rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return;
    }
    rt_control_lock(ex);
    cancel_task(ex, target->id);
    rt_control_unlock(ex);
}

void* rt_task_clone(void* task) {
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return NULL;
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_control_lock(ex);
    task_add_ref(target);
    rt_control_unlock(ex);
    return target;
}

static rt_task* spawn_internal_task_locked(rt_executor* ex, uint8_t kind, uint64_t sleep_delay) {
    if (ex == NULL) {
        return NULL;
    }
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task_status_store(task, TASK_READY);
    task->kind = kind;
    task->sleep_delay = sleep_delay;
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_inherit_placement(task, rt_current_task());
    rt_task_assign_spawn_owner(task);
    rt_task_slot_store(ex, id, task);
    ready_push(ex, id);
    return task;
}

static rt_task* spawn_checkpoint_task_locked(rt_executor* ex) {
    return spawn_internal_task_locked(ex, TASK_KIND_CHECKPOINT, 0);
}

rt_task* rt_spawn_sleep_task_locked(rt_executor* ex, uint64_t delay) {
    return spawn_internal_task_locked(ex, TASK_KIND_SLEEP, delay);
}

void* checkpoint(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_control_lock(ex);
    rt_task* task = spawn_checkpoint_task_locked(ex);
    rt_control_unlock(ex);
    return task;
}

void* rt_sleep(uint64_t ms) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_control_lock(ex);
    rt_task* task = rt_spawn_sleep_task_locked(ex, ms);
    rt_control_unlock(ex);
    return task;
}
