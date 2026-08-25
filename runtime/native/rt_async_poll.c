#include "rt_async_internal.h"
#include "rt_remote_spawn.h"
#include "rt_sync_point.h"
#include "rt_value_ops.h"

// Async runtime polling and scheduler logic.

static poll_outcome poll_checkpoint_task(const rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task_cancelled_load(task) != 0) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->checkpoint_polled) {
        out.kind = POLL_DONE_SUCCESS;
        return out;
    }
    task->checkpoint_polled = 1;
    out.kind = POLL_YIELDED;
    return out;
}

static poll_outcome poll_sleep_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task_cancelled_load(task) != 0) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (!task->sleep_armed) {
        task->sleep_deadline = rt_clock_deadline_base(ex) + task->sleep_delay;
        task->sleep_armed = 1;
        // Arm once: the owner shard's sleep store is the deadline index the
        // tick/advance paths pop; the timer-key waiter entry is the park.
        rt_shard* owner_shard = rt_task_owner_shard(ex, task);
        rt_shard_lock(owner_shard);
        rt_runtime_status status =
            rt_sleep_store_add(&owner_shard->sleep_store, task->sleep_deadline, task->id);
        rt_shard_unlock(owner_shard);
        if (status != RT_RUNTIME_STATUS_OK) {
            panic_msg("async: sleep store allocation failed");
        }
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id);
        return out;
    }
    if (rt_clock_now(ex) < task->sleep_deadline) {
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id);
        return out;
    }
    out.kind = POLL_DONE_SUCCESS;
    return out;
}

static void restore_poll_context(jmp_buf* saved_env,
                                 int saved_active,
                                 poll_outcome saved_result,
                                 waker_key saved_pending) {
    poll_env = saved_env;
    poll_active = saved_active;
    poll_result = saved_result;
    pending_key = saved_pending;
}

static poll_outcome poll_user_task(const rt_executor* ex, const rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    // Single-thread awaits can poll another task while an async poll is already active.
    // Keep the outer poll terminator target intact for when the outer task resumes.
    jmp_buf env;
    jmp_buf* saved_env = poll_env;
    int saved_active = poll_active;
    poll_outcome saved_result = poll_result;
    waker_key saved_pending = pending_key;
    pending_key = waker_none();
    poll_result.kind = POLL_NONE;
    poll_result.park_key = waker_none();
    poll_result.state = NULL;
    poll_env = &env;
    poll_active = 1;
    if (setjmp(env) == 0) {
        __surge_poll_call((uint64_t)task->poll_fn_id);
        restore_poll_context(saved_env, saved_active, saved_result, saved_pending);
        panic_msg("async poll returned without terminator");
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    out = poll_result;
    restore_poll_context(saved_env, saved_active, saved_result, saved_pending);
    return out;
}

poll_outcome poll_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL};
    if (task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task_status_load(task) == TASK_DONE) {
        out.kind =
            task->result_kind == TASK_RESULT_CANCELLED ? POLL_DONE_CANCELLED : POLL_DONE_SUCCESS;
        // The value stays in the task's own slot; a reader takes it from there.
        return out;
    }
    if (task->cancel_pending) {
        if (task->scope_id != 0 && ex != NULL) {
            rt_control_lock(ex);
            rt_scope* scope = get_scope(ex, task->scope_id);
            if (scope == NULL) {
                task->cancel_pending = 0;
                rt_control_unlock(ex);
                out.kind = POLL_DONE_CANCELLED;
                return out;
            }
            if (scope->active_children == 0) {
                task->cancel_pending = 0;
                scope_exit_locked(ex, scope);
                rt_control_unlock(ex);
                out.kind = POLL_DONE_CANCELLED;
                return out;
            }
            waker_key key = scope_key(scope->id);
            prepare_park(ex, task, key, 0);
            out.kind = POLL_PARKED;
            out.park_key = key;
            out.state = task->state;
            rt_control_unlock(ex);
            return out;
        }
        task->cancel_pending = 0;
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    switch (task->kind) {
        case TASK_KIND_CHECKPOINT:
            return poll_checkpoint_task(ex, task);
        case TASK_KIND_SLEEP:
            return poll_sleep_task(ex, task);
        case TASK_KIND_BLOCKING:
            return poll_blocking_task(ex, task);
        default:
            return poll_user_task(ex, task);
    }
}

int run_ready_one(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    rt_trace_drain_signal_dump();
    rt_control_lock(ex);
    rt_shard* shard0 = rt_runtime_shard0(rt_executor_runtime(ex));
    if (shard0 != NULL) {
        rt_shard_lock(shard0);
        (void)rt_remote_spawn_drain_inbound_locked(ex, shard0, RT_TRANSPORT_DRAIN_TURN_LIMIT);
        rt_shard_unlock(shard0);
    }
    uint64_t id = 0;
    if (!next_ready(ex, &id)) {
        rt_control_unlock(ex);
        return 0;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL) {
        rt_control_unlock(ex);
        panic_msg("invalid task id");
        return 1;
    }
    task_status_store(task, TASK_RUNNING);
    (void)task_wake_token_exchange(task, 0);
    rt_set_current_task(task);

    if (task->kind != TASK_KIND_USER) {
        task_polling_enter(task, POLL_SITE_CONTROL_RUNNER_SYSTEM);
        poll_outcome outcome = poll_task(ex, task);
        task_polling_exit(task);
        switch (outcome.kind) {
            case POLL_DONE_SUCCESS:
                mark_done(ex, task, TASK_RESULT_SUCCESS);
                break;
            case POLL_DONE_CANCELLED:
                mark_done(ex, task, TASK_RESULT_CANCELLED);
                break;
            case POLL_YIELDED:
                task->state = outcome.state;
                task_status_store(task, TASK_READY);
                ready_push(ex, task->id);
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
        rt_set_current_task(NULL);
        rt_control_unlock(ex);
        return 1;
    }

    rt_control_unlock(ex);
    task_polling_enter(task, POLL_SITE_CONTROL_RUNNER_USER);
    poll_outcome outcome = poll_task(ex, task);
    task_polling_exit(task);
    // RV2-DEBT-023 proof window: user poll has already passed its
    // cancellation check and returned PARKED, but this worker has not
    // re-entered the control lane to commit TASK_WAITING in park_current.
    // A real external rt_task_cancel can run here and must set the wake
    // token even though the target still reads as TASK_RUNNING.
    RT_SYNC_POINT_IF(outcome.kind == POLL_PARKED, SP_PARK_BEFORE_WAITING);
    rt_control_lock(ex);
    switch (outcome.kind) {
        case POLL_DONE_SUCCESS:
            mark_done(ex, task, TASK_RESULT_SUCCESS);
            break;
        case POLL_DONE_CANCELLED:
            mark_done(ex, task, TASK_RESULT_CANCELLED);
            break;
        case POLL_YIELDED:
            task->state = outcome.state;
            task_status_store(task, TASK_READY);
            ready_push(ex, task->id);
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
    rt_set_current_task(NULL);
    rt_control_unlock(ex);
    return 1;
}

void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind) {
    if (ex == NULL || task == NULL) {
        panic_msg("invalid task handle");
        return;
    }
    rt_heap_accounting_cell* saved_cell = rt_heap_accounting_current_cell();
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    rt_heap_accounting_set_current_cell(rt_heap_accounting_main_cell(accounting));
    uint64_t id = task->id;
    rt_control_lock(ex);
    if (task_status_load(task) != TASK_WAITING && task_status_load(task) != TASK_DONE) {
        wake_task(ex, id, 1);
    }
    rt_control_unlock(ex);
    for (;;) {
        rt_trace_drain_signal_dump();
        rt_control_lock(ex);
        const rt_task* current = get_task(ex, id);
        if (current == NULL) {
            rt_control_unlock(ex);
            rt_heap_accounting_set_current_cell(saved_cell);
            panic_msg("invalid task id");
            return;
        }
        if (task_status_load(current) == TASK_DONE) {
            if (out_kind != NULL) {
                *out_kind = current->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
            }
            rt_control_unlock(ex);
            rt_heap_accounting_set_current_cell(saved_cell);
            return;
        }
        rt_control_unlock(ex);
        if (!run_ready_one(ex)) {
            rt_heap_accounting_set_current_cell(saved_cell);
            panic_msg("async deadlock");
            return;
        }
    }
}

// Stashes a suspend-point/scope-join state box onto the current task before
// a cancellation completes it. This runs at most once per task lifetime
// (poll_task's TASK_DONE fast path and cancel_pending short-circuit both
// prevent compiled code from ever running again for a task that has already
// taken this branch once), so there is no overwrite/re-entry hazard to guard
// against; mark_done is the sole consumer, exactly once, on every path that
// can reach it.
static void stash_abandoned_state(void* state, uint64_t state_drop_fn_id) {
    if (state_drop_fn_id == 0) {
        return;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        return;
    }
    current->abandoned_state = state;
    current->abandoned_state_drop_fn_id = state_drop_fn_id;
}

void rt_async_yield(void* state, uint64_t state_drop_fn_id) {
    if (!poll_active || poll_env == NULL) {
        panic_msg("async_yield outside poll");
        return;
    }
    poll_result.state = state;
    if (current_task_cancelled(&exec_state)) {
        stash_abandoned_state(state, state_drop_fn_id);
        poll_result.kind = POLL_DONE_CANCELLED;
        poll_result.park_key = waker_none();
        pending_key = waker_none();
        longjmp(*poll_env, 1);
    }
    if (waker_valid(pending_key)) {
        poll_result.kind = POLL_PARKED;
        poll_result.park_key = pending_key;
    } else {
        poll_result.kind = POLL_YIELDED;
        poll_result.park_key = waker_none();
    }
    pending_key = waker_none();
    longjmp(*poll_env, 1);
}

// Completes the current task with a value, or with none when `src` is NULL.
//
// The move into the task's own result slot happens HERE, inside the task's own
// poll, because this is the one place it can: the element's move is generated
// code and no runtime lock may be held across it, while by the time mark_done
// publishes DONE the completing lane holds one. From this point the value is
// the task's and the caller's storage is a husk.
void rt_async_return(void* state, void* src) {
    if (!poll_active || poll_env == NULL) {
        panic_msg("async_return outside poll");
        return;
    }
    if (src != NULL) {
        rt_task* current = rt_current_task();
        void* destination =
            current == NULL ? NULL : rt_value_cell_publish_storage(&current->result);
        if (destination == NULL) {
            // Either this task was created without a result to hold, or it has
            // already published one. Both are defects in the caller: a task
            // completes once, and the shape of its result is decided when it is
            // created.
            panic_msg("async: this task published a result it was not created to hold");
            return;
        }
        rt_value_move_init_detached(current->result.operations, destination, src);
        if (rt_value_cell_commit(&current->result) != RT_SLOT_CONTROL_OK) {
            panic_msg("async: a task's result could not be published");
            return;
        }
    }
    poll_result.state = state;
    poll_result.kind = POLL_DONE_SUCCESS;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    longjmp(*poll_env, 1);
}

void rt_async_return_cancelled(void* state, uint64_t state_drop_fn_id) {
    if (!poll_active || poll_env == NULL) {
        panic_msg("async_cancel outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.kind = POLL_DONE_CANCELLED;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    stash_abandoned_state(state, state_drop_fn_id);
    longjmp(*poll_env, 1);
}
