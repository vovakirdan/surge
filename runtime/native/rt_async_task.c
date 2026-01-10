#include "rt_async_internal.h"

// Async runtime task API and task builtins.

static rt_task* spawn_checkpoint_task_locked(rt_executor* ex);
static rt_task* spawn_sleep_task_locked(rt_executor* ex, uint64_t delay);
static void ensure_select_timers_cap(rt_task* task, size_t want);

enum {
    SELECT_TASK = 0,
    SELECT_CHAN_RECV = 1,
    SELECT_CHAN_SEND = 2,
    SELECT_TIMEOUT = 3,
    SELECT_DEFAULT = 4,
};

void* __task_create(
    uint64_t poll_fn_id,
    void* state) { // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_lock(ex);
    uint64_t id = ex->next_id++;
    ensure_task_cap(ex, id);
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        rt_unlock(ex);
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
    ex->tasks[id] = task;
    rt_task* parent = rt_current_task();
    if (parent != NULL) {
        task_add_child(parent, id);
    }
    ready_push(ex, id);
    rt_unlock(ex);
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
    rt_lock(ex);
    const rt_task* target = task_from_handle(task);
    if (target == NULL || task_status_load(target) == TASK_DONE) {
        rt_unlock(ex);
        return;
    }
    wake_task(ex, target->id, 1);
    rt_unlock(ex);
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
    rt_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("async poll outside task");
        return 2;
    }
    if (rt_current_task_id() == target->id) {
        rt_unlock(ex);
        panic_msg("task cannot await itself");
        return 2;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_unlock(ex);
        panic_msg("async: missing current task");
        return 2;
    }
    if (current_task_cancelled(ex)) {
        rt_unlock(ex);
        return 0;
    }
    if (task_status_load(target) != TASK_WAITING) {
        wake_task(ex, target->id, 1);
    }
    if (task_status_load(target) == TASK_DONE) {
        uint8_t kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        task_release(ex, target);
        rt_unlock(ex);
        return kind;
    }
    if (target->kind != TASK_KIND_CHECKPOINT) {
        waker_key key = join_key(target->id);
        prepare_park(ex, current, key, 0);
        pending_key = key;
    }
    rt_unlock(ex);
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
    if (ex->worker_count > 1) {
        rt_lock(ex);
        if (task_status_load(target) != TASK_WAITING && task_status_load(target) != TASK_DONE) {
            wake_task(ex, target->id, 1);
        }
        while (task_status_load(target) != TASK_DONE) {
            pthread_cond_wait(&ex->done_cv, &ex->lock);
        }
        if (out_kind != NULL) {
            *out_kind = target->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
        }
        if (out_bits != NULL) {
            *out_bits = target->result_bits;
        }
        task_release(ex, target);
        rt_unlock(ex);
        return;
    }
    run_until_done(ex, target, out_kind, out_bits);
    rt_lock(ex);
    task_release(ex, target);
    rt_unlock(ex);
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
    rt_lock(ex);
    cancel_task(ex, target->id);
    rt_unlock(ex);
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
    rt_lock(ex);
    task_add_ref(target);
    rt_unlock(ex);
    return target;
}

uint8_t rt_timeout_poll(void* task, uint64_t ms, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    rt_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("async timeout outside task");
        return 2;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_unlock(ex);
        panic_msg("async: missing current task");
        return 2;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        rt_unlock(ex);
        return 0;
    }

    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        rt_unlock(ex);
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
        timeout_task = spawn_sleep_task_locked(ex, ms);
        if (timeout_task == NULL) {
            rt_unlock(ex);
            return 2;
        }
        current->timeout_task_id = timeout_task->id;
    }

    if (task_status_load(target) == TASK_DONE) {
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
        rt_unlock(ex);
        return kind;
    }
    if (timeout_task != NULL && task_status_load(timeout_task) == TASK_DONE) {
        cancel_task(ex, target->id);
        if (out_bits != NULL) {
            *out_bits = 0;
        }
        current->timeout_task_id = 0;
        task_release(ex, timeout_task);
        task_release(ex, target);
        pending_key = waker_none();
        rt_unlock(ex);
        return 2;
    }

    if (task_status_load(target) != TASK_WAITING) {
        wake_task(ex, target->id, 1);
    }
    if (timeout_task != NULL && task_status_load(timeout_task) != TASK_WAITING &&
        task_status_load(timeout_task) != TASK_DONE) {
        wake_task(ex, timeout_task->id, 1);
    }

    waker_key first_key = join_key(target->id);
    size_t prev_len = current->wait_keys_len;
    add_wait_key(ex, current, first_key);
    int first_added = current->wait_keys_len > prev_len;
    if (timeout_task != NULL) {
        add_wait_key(ex, current, join_key(timeout_task->id));
    }
    prepare_park(ex, current, first_key, first_added);
    pending_key = first_key;
    rt_unlock(ex);
    return 0;
}

int64_t rt_select_poll_tasks(uint64_t count, void** tasks, int64_t default_index) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return default_index >= 0 ? default_index : -1;
    }
    rt_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("async select outside task");
        return -1;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_unlock(ex);
        panic_msg("async: missing current task");
        return -1;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        rt_unlock(ex);
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
        const rt_task* target = task_from_handle(handle);
        if (target == NULL) {
            continue;
        }
        if (task_status_load(target) != TASK_WAITING && task_status_load(target) != TASK_DONE) {
            wake_task(ex, target->id, 1);
        }
        if (task_status_load(target) == TASK_DONE) {
            pending_key = waker_none();
            rt_unlock(ex);
            return (int64_t)i;
        }
    }

    if (default_index >= 0) {
        pending_key = waker_none();
        rt_unlock(ex);
        return default_index;
    }

    waker_key first_key = waker_none();
    int first_added = 0;
    for (uint64_t i = 0; i < count; i++) {
        if (tasks == NULL) {
            break;
        }
        void* handle = tasks[i];
        if (handle == NULL) {
            continue;
        }
        const rt_task* target = task_from_handle(handle);
        if (target == NULL) {
            continue;
        }
        waker_key key = join_key(target->id);
        size_t prev_len = current->wait_keys_len;
        add_wait_key(ex, current, key);
        if (!waker_valid(first_key)) {
            first_key = key;
            first_added = current->wait_keys_len > prev_len;
        }
    }

    if (waker_valid(first_key)) {
        prepare_park(ex, current, first_key, first_added);
    }
    pending_key = first_key;
    rt_unlock(ex);
    return -1;
}

int64_t rt_select_poll(uint64_t count,
                       const uint8_t* kinds,
                       void** handles,
                       const uint64_t* values,
                       const uint64_t* ms,
                       int64_t default_index) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return default_index >= 0 ? default_index : -1;
    }
    rt_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_unlock(ex);
        panic_msg("async select outside task");
        return -1;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_unlock(ex);
        panic_msg("async: missing current task");
        return -1;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        clear_select_timers(ex, current);
        pending_key = waker_none();
        rt_unlock(ex);
        return -1;
    }

    int has_timeout = 0;
    for (uint64_t i = 0; i < count; i++) {
        if (kinds != NULL && kinds[i] == SELECT_TIMEOUT) {
            has_timeout = 1;
            break;
        }
    }
    if (!has_timeout && current->select_timers_len > 0) {
        clear_select_timers(ex, current);
    }
    if (has_timeout && current->select_timers_len != count) {
        clear_select_timers(ex, current);
        if (count > 0) {
            ensure_select_timers_cap(current, (size_t)count);
        }
        if (current->select_timers_cap < count) {
            pending_key = waker_none();
            rt_unlock(ex);
            return -1;
        }
        current->select_timers_len = (size_t)count;
        if (current->select_timers != NULL) {
            for (uint64_t i = 0; i < count; i++) {
                current->select_timers[i] = 0;
            }
        }
    }

    int64_t selected = -1;
    int selected_timeout = 0;
    uint64_t selected_task_id = 0;

    for (uint64_t i = 0; i < count; i++) {
        uint8_t kind = kinds != NULL ? kinds[i] : SELECT_TASK;
        void* handle = handles != NULL ? handles[i] : NULL;
        switch (kind) {
            case SELECT_DEFAULT:
                break;
            case SELECT_TASK: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_unlock(ex);
                    return -1;
                }
                if (task_status_load(target) != TASK_WAITING &&
                    task_status_load(target) != TASK_DONE) {
                    wake_task(ex, target->id, 1);
                }
                if (task_status_load(target) == TASK_DONE) {
                    selected = (int64_t)i;
                }
                break;
            }
            case SELECT_CHAN_RECV: {
                uint8_t status = rt_channel_try_recv_status_locked(ex, handle, NULL);
                if (status == 1 || status == 2) {
                    selected = (int64_t)i;
                }
                break;
            }
            case SELECT_CHAN_SEND: {
                uint64_t value_bits = values != NULL ? values[i] : 0;
                uint8_t status = rt_channel_try_send_status_locked(ex, handle, value_bits);
                if (status == 1) {
                    selected = (int64_t)i;
                } else if (status == 2) {
                    rt_unlock(ex);
                    panic_msg("send on closed channel");
                    return -1;
                }
                break;
            }
            case SELECT_TIMEOUT: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_unlock(ex);
                    return -1;
                }
                if (task_status_load(target) != TASK_WAITING &&
                    task_status_load(target) != TASK_DONE) {
                    wake_task(ex, target->id, 1);
                }
                if (task_status_load(target) == TASK_DONE) {
                    selected = (int64_t)i;
                    break;
                }
                if (current->select_timers_len == count && current->select_timers != NULL) {
                    uint64_t timer_id = current->select_timers[i];
                    if (timer_id != 0) {
                        const rt_task* timer_task = get_task(ex, timer_id);
                        if (timer_task != NULL && task_status_load(timer_task) == TASK_DONE) {
                            selected = (int64_t)i;
                            selected_timeout = 1;
                            selected_task_id = target->id;
                        }
                    }
                }
                break;
            }
            default:
                break;
        }
        if (selected >= 0) {
            break;
        }
    }

    if (selected < 0 && default_index >= 0) {
        selected = default_index;
    }

    if (selected >= 0) {
        if (selected_timeout) {
            cancel_task(ex, selected_task_id);
            wake_task(ex, selected_task_id, 1);
        }
        clear_select_timers(ex, current);
        pending_key = waker_none();
        rt_unlock(ex);
        return selected;
    }

    waker_key first_key = waker_none();
    int first_added = 0;
    for (uint64_t i = 0; i < count; i++) {
        uint8_t kind = kinds != NULL ? kinds[i] : SELECT_TASK;
        void* handle = handles != NULL ? handles[i] : NULL;
        switch (kind) {
            case SELECT_TASK: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_unlock(ex);
                    return -1;
                }
                waker_key key = join_key(target->id);
                size_t prev_len = current->wait_keys_len;
                add_wait_key(ex, current, key);
                if (!waker_valid(first_key)) {
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                }
                break;
            }
            case SELECT_CHAN_RECV: {
                waker_key key = channel_recv_key((rt_channel*)handle);
                size_t prev_len = current->wait_keys_len;
                add_wait_key(ex, current, key);
                if (!waker_valid(first_key)) {
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                }
                break;
            }
            case SELECT_CHAN_SEND: {
                waker_key key = channel_send_key((rt_channel*)handle);
                size_t prev_len = current->wait_keys_len;
                add_wait_key(ex, current, key);
                if (!waker_valid(first_key)) {
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                }
                break;
            }
            case SELECT_TIMEOUT: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_unlock(ex);
                    return -1;
                }
                waker_key key = join_key(target->id);
                size_t prev_len = current->wait_keys_len;
                add_wait_key(ex, current, key);
                if (!waker_valid(first_key)) {
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                }

                uint64_t timer_id = 0;
                if (current->select_timers_len == count && current->select_timers != NULL) {
                    timer_id = current->select_timers[i];
                }
                if (timer_id == 0) {
                    uint64_t delay = ms != NULL ? ms[i] : 0;
                    const rt_task* timer_task = spawn_sleep_task_locked(ex, delay);
                    if (timer_task != NULL && current->select_timers != NULL &&
                        current->select_timers_len == count) {
                        current->select_timers[i] = timer_task->id;
                        timer_id = timer_task->id;
                    }
                }
                if (timer_id != 0) {
                    const rt_task* timer_task = get_task(ex, timer_id);
                    if (timer_task != NULL) {
                        waker_key timer_key_join = join_key(timer_task->id);
                        size_t prev_timer_len = current->wait_keys_len;
                        add_wait_key(ex, current, timer_key_join);
                        if (!waker_valid(first_key)) {
                            first_key = timer_key_join;
                            first_added = current->wait_keys_len > prev_timer_len;
                        }
                    }
                }
                break;
            }
            case SELECT_DEFAULT:
            default:
                break;
        }
    }

    if (waker_valid(first_key)) {
        prepare_park(ex, current, first_key, first_added);
    }
    pending_key = first_key;
    rt_unlock(ex);
    return -1;
}

static rt_task* spawn_checkpoint_task_locked(rt_executor* ex) {
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
    task->kind = TASK_KIND_CHECKPOINT;
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    ex->tasks[id] = task;
    ready_push(ex, id);
    return task;
}

static rt_task* spawn_sleep_task_locked(rt_executor* ex, uint64_t delay) {
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
    task->kind = TASK_KIND_SLEEP;
    task->sleep_delay = delay;
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    ex->tasks[id] = task;
    ready_push(ex, id);
    return task;
}

static void ensure_select_timers_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return;
    }
    if (task->select_timers_cap >= want) {
        return;
    }
    size_t next_cap = task->select_timers_cap == 0 ? 4 : task->select_timers_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = task->select_timers_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)task->select_timers, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: select timer allocation failed");
        return;
    }
    task->select_timers = next;
    task->select_timers_cap = next_cap;
}

void* checkpoint(void) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_lock(ex);
    rt_task* task = spawn_checkpoint_task_locked(ex);
    rt_unlock(ex);
    return task;
}

void* rt_sleep(uint64_t ms) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return NULL;
    }
    rt_lock(ex);
    rt_task* task = spawn_sleep_task_locked(ex, ms);
    rt_unlock(ex);
    return task;
}
