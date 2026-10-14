#include "rt_async_internal.h"
#include "rt_sync_point.h"
#include "rt_task_cold.h"
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
            fatal_oom_msg("async: sleep store allocation failed");
        }
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id, rt_task_owner_shard_id(ex, task));
        return out;
    }
    if (rt_clock_now(ex) < task->sleep_deadline) {
        out.kind = POLL_PARKED;
        out.park_key = timer_key(task->id, rt_task_owner_shard_id(ex, task));
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
        if (waker_valid(task->active_scope_key) && ex != NULL) {
            waker_key key = waker_none();
            if (rt_scope_cancel_teardown(ex, task, 0, &key) == RT_SCOPE_TEARDOWN_PARKED) {
                out.kind = POLL_PARKED;
                out.park_key = key;
                out.state = task->state;
                return out;
            }
            out.kind = POLL_DONE_CANCELLED;
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
            // Only a user task is created cold. A cancel that reached it while
            // it was cold linearized before it could start
            // (RUNTIME_MODEL_EXPLAINED 6.3; RUNTIME_V2.md, owner ruling
            // 2026-08-29): it answers Cancelled() and its body is never
            // entered. mark_done gives the start frame back through the
            // reclaim pair.
            if (rt_task_take_cancelled_start(task)) {
                out.kind = POLL_DONE_CANCELLED;
                return out;
            }
            return poll_user_task(ex, task);
    }
}

// Stashes a suspension frame onto the current task before a cancellation
// completes it. This runs at most once per task lifetime (poll_task's TASK_DONE
// fast path and cancel_pending short-circuit both prevent compiled code from
// ever running again for a task that has already taken this branch once), so
// there is no overwrite/re-entry hazard to guard against; mark_done is the sole
// consumer, exactly once, on every path that can reach it.
//
// The frame's DESCRIPTOR is resolved here, at the boundary where the number
// generated code carries is still next to the frame it names. What the task
// keeps is the descriptor itself, so the release is a question about a type
// rather than about a table lookup nobody downstream can check.
static void stash_reclaim_frame(void* frame, uint64_t frame_type_id) {
    if (frame_type_id == 0) {
        return;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        return;
    }
    current->reclaim_frame = frame;
    current->reclaim_frame_ops = rt_channel_element_ops_for(frame_type_id);
}

void rt_async_yield(void* state, uint64_t state_type_id) {
    if (!poll_active || poll_env == NULL) {
        panic_msg("async_yield outside poll");
        return;
    }
    poll_result.state = state;
    if (current_task_cancelled(&exec_state)) {
        rt_channel_cancel_resume(rt_current_task(), &exec_state);
        stash_reclaim_frame(state, state_type_id);
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
        // RV2-DEBT-263 proof window. The body has produced its value and the
        // slot holds it; the scheduler has not been told the outcome yet. A
        // cancel landing here is past every suspension point this task will
        // ever have (rt_task_poll's TASK_DONE fast path answers from the target
        // and never consults the awaiter's flag), so the only thing left that
        // can still make this task answer Cancelled is mark_done's commit
        // boundary.
        RT_SYNC_POINT(SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT);
    }
    poll_result.state = state;
    poll_result.kind = POLL_DONE_SUCCESS;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    longjmp(*poll_env, 1);
}

void rt_async_return_cancelled(void* state, uint64_t state_type_id) {
    if (!poll_active || poll_env == NULL) {
        panic_msg("async_cancel outside poll");
        return;
    }
    poll_result.state = state;
    poll_result.kind = POLL_DONE_CANCELLED;
    poll_result.park_key = waker_none();
    pending_key = waker_none();
    stash_reclaim_frame(state, state_type_id);
    longjmp(*poll_env, 1);
}
