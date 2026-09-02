#include "rt_remote_task_internal.h"

int rt_remote_task_prepare_reply_wait(rt_executor* ex,
                                      rt_task* current,
                                      rt_remote_task_pending* pending) {
    if (ex == NULL || current == NULL || pending == NULL || pending->executor != ex) {
        return 1;
    }
    waker_key key = rt_remote_task_reply_key(pending->request_id, pending->source_shard_id);
    (void)rt_transport_reply_wait_before_task_suspend();
    prepare_park(ex, current, key, 0);
    pending_key = key;
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) == RT_REMOTE_TASK_STATUS_PENDING) {
        return 0;
    }
    rt_remote_task_clear_reply_wait(ex, current, pending);
    return 1;
}

void rt_remote_task_clear_reply_wait(rt_executor* ex,
                                     rt_task* current,
                                     const rt_remote_task_pending* pending) {
    if (ex == NULL || current == NULL || pending == NULL) {
        return;
    }
    waker_key key = rt_remote_task_reply_key(pending->request_id, pending->source_shard_id);
    remove_waiter(ex, key, current->id);
    if (current->park_key.kind == key.kind && current->park_key.id == key.id) {
        current->park_key = waker_none();
    }
    current->park_prepared = 0;
    if (pending_key.kind == key.kind && pending_key.id == key.id) {
        pending_key = waker_none();
    }
}

void rt_remote_task_fail_all_pending(rt_executor* ex, rt_remote_task_status status) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return;
    }
    for (;;) {
        rt_remote_task_pending* pending = NULL;
        int release_owner_ref = 0;
        int should_wake = 0;
        pthread_mutex_lock(&state->lock);
        for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
            if (it->status != RT_REMOTE_TASK_STATUS_PENDING) {
                continue;
            }
            it->status = (uint8_t)status;
            it->result_kind = 2;
            it->result_bits = 0;
            release_owner_ref = it->owner_registered != 0;
            it->owner_registered = 0;
            should_wake = it->reply_wait_retired == 0;
            it->reply_wait_retired = 1;
            rt_remote_task_pending_add_ref(it);
            pending = it;
            break;
        }
        pthread_mutex_unlock(&state->lock);
        if (pending == NULL) {
            return;
        }
        if (should_wake) {
            wake_key_all_with_policy(
                ex, rt_remote_task_reply_key(pending->request_id, pending->source_shard_id), 0);
        }
        if (release_owner_ref) {
            rt_remote_task_pending_release(pending);
        }
        rt_remote_task_pending_release(pending);
    }
}
