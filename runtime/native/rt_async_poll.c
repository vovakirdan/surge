#include "rt_async_internal.h"

// Async runtime polling and scheduler logic.

static poll_outcome poll_checkpoint_task(const rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
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

static poll_outcome poll_sleep_task(const rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task_cancelled_load(task) != 0) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (!task->sleep_armed) {
        task->sleep_deadline = ex->now_ms + task->sleep_delay;
        task->sleep_armed = 1;
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id);
        return out;
    }
    if (ex->now_ms < task->sleep_deadline) {
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id);
        return out;
    }
    out.kind = POLL_DONE_SUCCESS;
    return out;
}

static poll_outcome poll_user_task(const rt_executor* ex, const rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    pending_key = waker_none();
    poll_result.kind = POLL_NONE;
    poll_result.park_key = waker_none();
    poll_result.state = NULL;
    poll_result.value_bits = 0;
    poll_active = 1;
    if (setjmp(poll_env) == 0) {
        __surge_poll_call((uint64_t)task->poll_fn_id);
        poll_active = 0;
        panic_msg("async poll returned without terminator");
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    poll_active = 0;
    return poll_result;
}

poll_outcome poll_task(rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task_status_load(task) == TASK_DONE) {
        out.kind =
            task->result_kind == TASK_RESULT_CANCELLED ? POLL_DONE_CANCELLED : POLL_DONE_SUCCESS;
        out.value_bits = task->result_bits;
        return out;
    }
    if (task->cancel_pending) {
        if (task->scope_id != 0 && ex != NULL) {
            rt_lock(ex);
            rt_scope* scope = get_scope(ex, task->scope_id);
            if (scope == NULL) {
                task->cancel_pending = 0;
                rt_unlock(ex);
                out.kind = POLL_DONE_CANCELLED;
                return out;
            }
            if (scope->active_children == 0) {
                task->cancel_pending = 0;
                scope_exit_locked(ex, scope);
                rt_unlock(ex);
                out.kind = POLL_DONE_CANCELLED;
                return out;
            }
            waker_key key = scope_key(scope->id);
            prepare_park(ex, task, key, 0);
            out.kind = POLL_PARKED;
            out.park_key = key;
            out.state = task->state;
            rt_unlock(ex);
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
        case TASK_KIND_NET_ACCEPT:
        case TASK_KIND_NET_READ:
        case TASK_KIND_NET_WRITE:
            return poll_net_task(ex, task);
        default:
            return poll_user_task(ex, task);
    }
}

int run_ready_one(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    rt_lock(ex);
    uint64_t id = 0;
    if (!next_ready(ex, &id)) {
        rt_unlock(ex);
        return 0;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL) {
        rt_unlock(ex);
        panic_msg("invalid task id");
        return 1;
    }
    task_status_store(task, TASK_RUNNING);
    (void)task_wake_token_exchange(task, 0);
    rt_set_current_task(task);

    if (task->kind != TASK_KIND_USER) {
        task_polling_enter(task);
        poll_outcome outcome = poll_task(ex, task);
        task_polling_exit(task);
        switch (outcome.kind) {
            case POLL_DONE_SUCCESS:
                mark_done(ex, task, TASK_RESULT_SUCCESS, outcome.value_bits);
                break;
            case POLL_DONE_CANCELLED:
                mark_done(ex, task, TASK_RESULT_CANCELLED, 0);
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
        rt_unlock(ex);
        return 1;
    }

    rt_unlock(ex);
    task_polling_enter(task);
    poll_outcome outcome = poll_task(ex, task);
    task_polling_exit(task);
    rt_lock(ex);
    switch (outcome.kind) {
        case POLL_DONE_SUCCESS:
            mark_done(ex, task, TASK_RESULT_SUCCESS, outcome.value_bits);
            break;
        case POLL_DONE_CANCELLED:
            mark_done(ex, task, TASK_RESULT_CANCELLED, 0);
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
    rt_unlock(ex);
    return 1;
}

void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind, uint64_t* out_bits) {
    if (ex == NULL || task == NULL) {
        panic_msg("invalid task handle");
        return;
    }
    uint64_t id = task->id;
    rt_lock(ex);
    if (task_status_load(task) != TASK_WAITING && task_status_load(task) != TASK_DONE) {
        wake_task(ex, id, 1);
    }
    rt_unlock(ex);
    for (;;) {
        rt_lock(ex);
        const rt_task* current = get_task(ex, id);
        if (current == NULL) {
            rt_unlock(ex);
            panic_msg("invalid task id");
            return;
        }
        if (task_status_load(current) == TASK_DONE) {
            if (out_kind != NULL) {
                *out_kind = current->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
            }
            if (out_bits != NULL) {
                *out_bits = current->result_bits;
            }
            rt_unlock(ex);
            return;
        }
        rt_unlock(ex);
        if (!run_ready_one(ex)) {
            panic_msg("async deadlock");
            return;
        }
    }
}

void rt_async_yield(void* state) {
    if (!poll_active) {
        panic_msg("async_yield outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.value_bits = 0;
    if (current_task_cancelled(&exec_state)) {
        poll_result.kind = POLL_DONE_CANCELLED;
        poll_result.park_key = waker_none();
        pending_key = waker_none();
        longjmp(poll_env, 1);
    }
    if (waker_valid(pending_key)) {
        poll_result.kind = POLL_PARKED;
        poll_result.park_key = pending_key;
    } else {
        poll_result.kind = POLL_YIELDED;
        poll_result.park_key = waker_none();
    }
    pending_key = waker_none();
    longjmp(poll_env, 1);
}

void rt_async_return(void* state, uint64_t bits) {
    if (!poll_active) {
        panic_msg("async_return outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.value_bits = bits;
    poll_result.kind = POLL_DONE_SUCCESS;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    longjmp(poll_env, 1);
}

void rt_async_return_cancelled(void* state) {
    if (!poll_active) {
        panic_msg("async_cancel outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.value_bits = 0;
    poll_result.kind = POLL_DONE_CANCELLED;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    longjmp(poll_env, 1);
}
