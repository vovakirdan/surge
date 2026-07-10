#include "rt_async_internal.h"
#include "rt_remote_task.h"

// Select and timeout arms. Registration is control-serialized (spike D12):
// these paths register one task under several key owners, so they stay on
// the slow lane while single-key awaits run shard-local.

enum {
    SELECT_TASK = 0,
    SELECT_CHAN_RECV = 1,
    SELECT_CHAN_SEND = 2,
    SELECT_TIMEOUT = 3,
    SELECT_DEFAULT = 4,
};

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

uint8_t rt_timeout_poll(void* task, uint64_t ms, uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return 2;
    }
    rt_control_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_control_unlock(ex);
        panic_msg("async timeout outside task");
        return 2;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_control_unlock(ex);
        panic_msg("async: missing current task");
        return 2;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        rt_control_unlock(ex);
        return 0;
    }

    rt_task* target = task_from_handle(task);
    if (target == NULL) {
        rt_control_unlock(ex);
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
        timeout_task = rt_spawn_sleep_task_locked(ex, ms);
        if (timeout_task == NULL) {
            rt_control_unlock(ex);
            return 2;
        }
        current->timeout_task_id = timeout_task->id;
    }

    if (task_status_load(target) == TASK_DONE) {
        uint8_t kind = rt_far_task_take_result(target, current, out_bits);
        current->timeout_task_id = 0;
        if (timeout_task != NULL) {
            task_release(ex, timeout_task);
        }
        task_release(ex, target);
        pending_key = waker_none();
        rt_control_unlock(ex);
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
        rt_control_unlock(ex);
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
    int first_added = 0;
    {
        size_t prev_len = current->wait_keys_len;
        add_wait_key(ex, current, first_key);
        first_added = current->wait_keys_len > prev_len;
    }
    if (timeout_task != NULL) {
        add_wait_key(ex, current, join_key(timeout_task->id));
    }
    prepare_park(ex, current, first_key, first_added);
    pending_key = first_key;
    // Register-then-verify: an arm may complete on its own shard between the
    // scans above and the registrations; self-token so the park aborts and
    // the next poll observes the completion (clear_wait_keys cleans up).
    if (task_status_load(target) == TASK_DONE ||
        (timeout_task != NULL && task_status_load(timeout_task) == TASK_DONE)) {
        (void)task_wake_token_exchange(current, 1);
    }
    rt_control_unlock(ex);
    return 0;
}

int64_t rt_select_poll_tasks(uint64_t count, void** tasks, int64_t default_index) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return default_index >= 0 ? default_index : -1;
    }
    rt_control_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_control_unlock(ex);
        panic_msg("async select outside task");
        return -1;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_control_unlock(ex);
        panic_msg("async: missing current task");
        return -1;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        pending_key = waker_none();
        rt_control_unlock(ex);
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
            rt_control_unlock(ex);
            return (int64_t)i;
        }
    }

    if (default_index >= 0) {
        pending_key = waker_none();
        rt_control_unlock(ex);
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
        if (!waker_valid(first_key)) {
            size_t prev_len = current->wait_keys_len;
            add_wait_key(ex, current, key);
            first_key = key;
            first_added = current->wait_keys_len > prev_len;
        } else {
            add_wait_key(ex, current, key);
        }
    }

    if (waker_valid(first_key)) {
        prepare_park(ex, current, first_key, first_added);
    }
    pending_key = first_key;
    // Register-then-verify (see rt_timeout_poll).
    for (uint64_t i = 0; i < count; i++) {
        if (tasks == NULL) {
            break;
        }
        const rt_task* target = tasks[i] != NULL ? task_from_handle(tasks[i]) : NULL;
        if (target != NULL && task_status_load(target) == TASK_DONE) {
            (void)task_wake_token_exchange(current, 1);
            break;
        }
    }
    rt_control_unlock(ex);
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
    rt_control_lock(ex);
    if (rt_current_task_id() == 0) {
        rt_control_unlock(ex);
        panic_msg("async select outside task");
        return -1;
    }
    rt_task* current = rt_current_task();
    if (current == NULL) {
        rt_control_unlock(ex);
        panic_msg("async: missing current task");
        return -1;
    }
    clear_wait_keys(ex, current);
    if (current_task_cancelled(ex)) {
        clear_select_timers(ex, current);
        pending_key = waker_none();
        rt_control_unlock(ex);
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
            rt_control_unlock(ex);
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
                    rt_control_unlock(ex);
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
                    rt_control_unlock(ex);
                    panic_msg("send on closed channel");
                    return -1;
                }
                break;
            }
            case SELECT_TIMEOUT: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_control_unlock(ex);
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
        rt_control_unlock(ex);
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
                    rt_control_unlock(ex);
                    return -1;
                }
                waker_key key = join_key(target->id);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }
                break;
            }
            case SELECT_CHAN_RECV: {
                waker_key key = channel_recv_key((rt_channel*)handle);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }
                break;
            }
            case SELECT_CHAN_SEND: {
                waker_key key = channel_send_key((rt_channel*)handle);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }
                break;
            }
            case SELECT_TIMEOUT: {
                const rt_task* target = task_from_handle(handle);
                if (target == NULL) {
                    rt_control_unlock(ex);
                    return -1;
                }
                waker_key key = join_key(target->id);
                if (!waker_valid(first_key)) {
                    size_t prev_len = current->wait_keys_len;
                    add_wait_key(ex, current, key);
                    first_key = key;
                    first_added = current->wait_keys_len > prev_len;
                } else {
                    add_wait_key(ex, current, key);
                }

                uint64_t timer_id = 0;
                if (current->select_timers_len == count && current->select_timers != NULL) {
                    timer_id = current->select_timers[i];
                }
                if (timer_id == 0) {
                    uint64_t delay = ms != NULL ? ms[i] : 0;
                    const rt_task* timer_task = rt_spawn_sleep_task_locked(ex, delay);
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
                        if (!waker_valid(first_key)) {
                            size_t prev_timer_len = current->wait_keys_len;
                            add_wait_key(ex, current, timer_key_join);
                            first_key = timer_key_join;
                            first_added = current->wait_keys_len > prev_timer_len;
                        } else {
                            add_wait_key(ex, current, timer_key_join);
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
    // Register-then-verify (see rt_timeout_poll): covers task and timeout
    // arms; channel arms stay control-serialized with channel wakes until
    // the channel lane migration revisits this scan.
    for (uint64_t i = 0; i < count; i++) {
        uint8_t kind = kinds != NULL ? kinds[i] : SELECT_TASK;
        if (kind != SELECT_TASK && kind != SELECT_TIMEOUT) {
            continue;
        }
        void* handle = handles != NULL ? handles[i] : NULL;
        const rt_task* target = handle != NULL ? task_from_handle(handle) : NULL;
        int timer_done = 0;
        if (kind == SELECT_TIMEOUT && current->select_timers != NULL &&
            current->select_timers_len == count && current->select_timers[i] != 0) {
            const rt_task* timer_task = get_task(ex, current->select_timers[i]);
            timer_done = timer_task != NULL && task_status_load(timer_task) == TASK_DONE;
        }
        if ((target != NULL && task_status_load(target) == TASK_DONE) || timer_done) {
            (void)task_wake_token_exchange(current, 1);
            break;
        }
    }
    rt_control_unlock(ex);
    return -1;
}
