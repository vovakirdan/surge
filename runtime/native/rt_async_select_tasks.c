#include "rt_async_internal.h"

// select over TASK arms only: the form a `select` takes when every arm is an
// await and no channel or timer is involved.
//
// It is separated from the general poll because the two answer different
// questions with different vocabularies. This one asks whether a task has
// finished, and a task's completion is a status the scheduler already
// published; the general poll asks whether a VALUE can move, which is what the
// typed-slot work is about. Keeping the task-only form here means that work
// changes one file rather than reaching around this function on its way past.
//
// No payload crosses this file, which is the other half of why it is the piece
// that moved: the carrier census has no row here, and moving code that carries
// one would rewrite a census of a commit that has already happened.

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
