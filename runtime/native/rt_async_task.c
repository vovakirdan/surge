#include "rt_async_internal.h"

// Async runtime task API and task builtins.

void* __task_create(void* poll_fn, void* state) {
    rt_executor* ex = ensure_exec();
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
    task->poll_fn_id = (int64_t)(uintptr_t)poll_fn;
    task->state = state;
    task->status = TASK_READY;
    task->kind = TASK_KIND_USER;
    task->handle_refs = 1;
    ex->tasks[id] = task;
    if (ex->current != 0) {
        rt_task* parent = get_task(ex, ex->current);
        task_add_child(parent, id);
    }
    ready_push(ex, id);
    return task;
}

void* __task_state(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL || ex->current == 0) {
        panic_msg("async: __task_state without current task");
        return NULL;
    }
    rt_task* task = get_task(ex, ex->current);
    if (task == NULL) {
        panic_msg("async: missing current task");
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
    rt_task* target = task_from_handle(task);
    if (target == NULL || target->status == TASK_DONE) {
        return;
    }
    wake_task(ex, target->id, 1);
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
    if (ex->current == 0) {
        panic_msg("async poll outside task");
        return 2;
    }
    if (ex->current == target->id) {
        panic_msg("task cannot await itself");
        return 2;
    }
    if (current_task_cancelled(ex)) {
        return 0;
    }
    if (target->status != TASK_WAITING && target->status != TASK_DONE) {
        wake_task(ex, target->id, 1);
    }
    if (target->status == TASK_DONE) {
        uint8_t kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        task_release(ex, target);
        return kind;
    }
    if (target->kind != TASK_KIND_CHECKPOINT) {
        pending_key = join_key(target->id);
    }
    return 0;
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
    run_until_done(ex, target, out_kind, out_bits);
    task_release(ex, target);
}

void rt_task_cancel(void* task) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return;
    }
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return;
    }
    cancel_task(ex, target->id);
}

void* rt_task_clone(void* task) {
    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return NULL;
    }
    task_add_ref(target);
    return target;
}

void* checkpoint(void) {
    rt_executor* ex = ensure_exec();
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
    task->status = TASK_READY;
    task->kind = TASK_KIND_CHECKPOINT;
    task->handle_refs = 1;
    ex->tasks[id] = task;
    ready_push(ex, id);
    return task;
}

void* rt_sleep(void* ms) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    uint64_t delay = (uint64_t)(uintptr_t)ms;
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        panic_msg("async: task allocation failed");
        return NULL;
    }
    memset(task, 0, sizeof(rt_task));
    task->id = id;
    task->status = TASK_READY;
    task->kind = TASK_KIND_SLEEP;
    task->sleep_delay = delay;
    task->handle_refs = 1;
    ex->tasks[id] = task;
    ready_push(ex, id);
    return task;
}
