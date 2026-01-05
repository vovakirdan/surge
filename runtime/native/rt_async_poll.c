#include "rt_async_internal.h"

// Async runtime polling and scheduler logic.

static poll_outcome poll_checkpoint_task(const rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (ex == NULL || task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->cancelled) {
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
    if (task->cancelled) {
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

poll_outcome poll_task(const rt_executor* ex, rt_task* task) {
    poll_outcome out = {POLL_NONE, waker_none(), NULL, 0};
    if (task == NULL) {
        out.kind = POLL_DONE_CANCELLED;
        return out;
    }
    if (task->status == TASK_DONE) {
        out.kind =
            task->result_kind == TASK_RESULT_CANCELLED ? POLL_DONE_CANCELLED : POLL_DONE_SUCCESS;
        out.value_bits = task->result_bits;
        return out;
    }
    switch (task->kind) {
        case TASK_KIND_CHECKPOINT:
            return poll_checkpoint_task(ex, task);
        case TASK_KIND_SLEEP:
            return poll_sleep_task(ex, task);
        default:
            return poll_user_task(ex, task);
    }
}

int run_ready_one(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t id = 0;
    if (!next_ready(ex, &id)) {
        return 0;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL) {
        panic_msg("invalid task id");
        return 1;
    }
    ex->current = id;
    task->status = TASK_RUNNING;
    poll_outcome outcome = poll_task(ex, task);
    switch (outcome.kind) {
        case POLL_DONE_SUCCESS:
            mark_done(ex, task, TASK_RESULT_SUCCESS, outcome.value_bits);
            break;
        case POLL_DONE_CANCELLED:
            mark_done(ex, task, TASK_RESULT_CANCELLED, 0);
            break;
        case POLL_YIELDED:
            task->state = outcome.state;
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
    ex->current = 0;
    return 1;
}

void run_until_done(rt_executor* ex, const rt_task* task, uint8_t* out_kind, uint64_t* out_bits) {
    if (ex == NULL || task == NULL) {
        panic_msg("invalid task handle");
        return;
    }
    uint64_t id = task->id;
    if (task->status != TASK_WAITING && task->status != TASK_DONE) {
        wake_task(ex, id, 1);
    }
    for (;;) {
        const rt_task* current = get_task(ex, id);
        if (current == NULL) {
            panic_msg("invalid task id");
            return;
        }
        if (current->status == TASK_DONE) {
            if (out_kind != NULL) {
                *out_kind = current->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
            }
            if (out_bits != NULL) {
                *out_bits = current->result_bits;
            }
            return;
        }
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
