#include "rt_async_internal.h"

// Async runtime task API and task builtins.

void* __task_create(uint64_t poll_fn_id, void* state) {
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
    task->poll_fn_id = (int64_t)poll_fn_id;
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

uint8_t rt_timeout_poll(void* task, uint64_t ms, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    if (ex->current == 0) {
        panic_msg("async timeout outside task");
        return 2;
    }
    rt_task* current = get_task(ex, ex->current);
    if (current == NULL) {
        panic_msg("async: missing current task");
        return 2;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        return 0;
    }

    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        return 2;
    }

    uint64_t timeout_id = current->timeout_task_id;
    rt_task* timeout_task = NULL;
    if (timeout_id != 0) {
        timeout_task = get_task(ex, timeout_id);
        if (timeout_task == NULL) {
            timeout_id = 0;
            current->timeout_task_id = 0;
        }
    }
    if (timeout_id == 0) {
        void* sleep_handle = rt_sleep(ms);
        if (sleep_handle == NULL) {
            return 2;
        }
        timeout_task = task_from_handle(sleep_handle);
        if (timeout_task == NULL) {
            return 2;
        }
        current->timeout_task_id = timeout_task->id;
    }

    if (target->status == TASK_DONE) {
        uint8_t kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        current->timeout_task_id = 0;
        if (timeout_task != NULL) {
            task_release(ex, timeout_task);
        }
        task_release(ex, target);
        pending_key = waker_none();
        return kind;
    }
    if (timeout_task != NULL && timeout_task->status == TASK_DONE) {
        cancel_task(ex, target->id);
        if (out_bits != NULL) {
            *out_bits = 0;
        }
        current->timeout_task_id = 0;
        task_release(ex, timeout_task);
        task_release(ex, target);
        pending_key = waker_none();
        return 2;
    }

    if (target->status != TASK_WAITING && target->status != TASK_DONE) {
        wake_task(ex, target->id, 1);
    }
    if (timeout_task != NULL && timeout_task->status != TASK_WAITING &&
        timeout_task->status != TASK_DONE) {
        wake_task(ex, timeout_task->id, 1);
    }

    waker_key first_key = join_key(target->id);
    add_wait_key(ex, current, first_key);
    if (timeout_task != NULL) {
        add_wait_key(ex, current, join_key(timeout_task->id));
    }
    pending_key = first_key;
    return 0;
}

int64_t rt_select_poll_tasks(uint64_t count, void** tasks, int64_t default_index) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return default_index >= 0 ? default_index : -1;
    }
    if (ex->current == 0) {
        panic_msg("async select outside task");
        return -1;
    }
    rt_task* current = get_task(ex, ex->current);
    if (current == NULL) {
        panic_msg("async: missing current task");
        return -1;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        return -1;
    }

    for (uint64_t i = 0; i < count; i++) {
        if (tasks == NULL) {
            break;
        }
        void* handle = tasks[i];
        if (handle == NULL) {
            continue;
        }
        rt_task* target = task_from_handle(handle);
        if (target == NULL) {
            continue;
        }
        if (target->status != TASK_WAITING && target->status != TASK_DONE) {
            wake_task(ex, target->id, 1);
        }
        if (target->status == TASK_DONE) {
            pending_key = waker_none();
            return (int64_t)i;
        }
    }

    if (default_index >= 0) {
        pending_key = waker_none();
        return default_index;
    }

    waker_key first_key = waker_none();
    for (uint64_t i = 0; i < count; i++) {
        if (tasks == NULL) {
            break;
        }
        void* handle = tasks[i];
        if (handle == NULL) {
            continue;
        }
        rt_task* target = task_from_handle(handle);
        if (target == NULL) {
            continue;
        }
        waker_key key = join_key(target->id);
        add_wait_key(ex, current, key);
        if (!waker_valid(first_key)) {
            first_key = key;
        }
    }

    pending_key = first_key;
    return -1;
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

void* rt_sleep(uint64_t ms) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    uint64_t delay = ms;
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
