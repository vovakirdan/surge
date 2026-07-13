#include "rt_remote_spawn_internal.h"

#include "rt_remote_task.h"

static pthread_mutex_t remote_spawn_lock = PTHREAD_MUTEX_INITIALIZER;
static _Atomic uint64_t remote_spawn_next_request_id = 1;
static rt_remote_spawn_pending* remote_spawn_pending_head;

void remote_spawn_pending_add_ref(rt_remote_spawn_pending* pending) {
    if (pending != NULL) {
        (void)atomic_fetch_add_explicit(&pending->refs, 1, memory_order_relaxed);
    }
}

void remote_spawn_pending_release(rt_remote_spawn_pending* pending) {
    if (pending == NULL) {
        return;
    }
    uint32_t refs = atomic_fetch_sub_explicit(&pending->refs, 1, memory_order_acq_rel);
    if (refs == 1) {
        if (pending->state_owned != 0 && pending->state_drop_fn_id != 0 && pending->state != NULL) {
            __surge_drop_call(pending->state_drop_fn_id, pending->state);
            pending->state = NULL;
        }
        rt_free((uint8_t*)pending, sizeof(*pending), _Alignof(rt_remote_spawn_pending));
    }
}

static void pending_link_locked(rt_remote_spawn_pending* pending) {
    pending->next = remote_spawn_pending_head;
    pending->listed = 1;
    remote_spawn_pending_head = pending;
}

static void pending_unlink_locked(rt_remote_spawn_pending* pending) {
    rt_remote_spawn_pending** cursor = &remote_spawn_pending_head;
    while (*cursor != NULL) {
        if (*cursor == pending) {
            *cursor = pending->next;
            pending->next = NULL;
            pending->listed = 0;
            return;
        }
        cursor = &(*cursor)->next;
    }
}

void remote_spawn_pending_link(rt_remote_spawn_pending* pending) {
    if (pending == NULL) {
        return;
    }
    pending->request_id =
        atomic_fetch_add_explicit(&remote_spawn_next_request_id, 1, memory_order_relaxed);
    pthread_mutex_lock(&remote_spawn_lock);
    pending_link_locked(pending);
    pthread_mutex_unlock(&remote_spawn_lock);
}

void remote_spawn_pending_finish(rt_executor* ex,
                                 rt_remote_spawn_pending* pending,
                                 rt_remote_spawn_status status,
                                 const rt_far_task_handle* handle) {
    int should_wake = 0;
    int drop_caller_ref = 0;
    rt_far_task_handle release_handle = {0};
    pthread_mutex_lock(&remote_spawn_lock);
    if (pending != NULL && pending->status == RT_REMOTE_SPAWN_STATUS_PENDING) {
        pending->status = status;
        if (handle != NULL) {
            pending->handle = *handle;
        }
        if (pending->abandoned != 0) {
            if (status == RT_REMOTE_SPAWN_STATUS_OK && handle != NULL) {
                release_handle = *handle;
            }
            if (pending->listed != 0) {
                pending_unlink_locked(pending);
                drop_caller_ref = 1;
            }
        } else {
            should_wake = 1;
        }
    }
    pthread_mutex_unlock(&remote_spawn_lock);
    if (should_wake) {
        wake_key_all_with_policy(
            ex, remote_spawn_reply_key(pending->request_id, pending->source_shard_id), 0);
    }
    if (release_handle.task_id != 0) {
        (void)rt_far_task_release(&release_handle);
    }
    if (drop_caller_ref) {
        remote_spawn_pending_release(pending);
    }
}

rt_remote_spawn_status remote_spawn_pending_snapshot(const rt_remote_spawn_pending* pending,
                                                     rt_far_task_handle* out) {
    rt_remote_spawn_status status = RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    pthread_mutex_lock(&remote_spawn_lock);
    if (pending != NULL) {
        status = pending->status;
        if (out != NULL) {
            *out = pending->handle;
        }
    }
    pthread_mutex_unlock(&remote_spawn_lock);
    return status;
}

void remote_spawn_pending_consume(rt_remote_spawn_pending* pending) {
    pthread_mutex_lock(&remote_spawn_lock);
    if (pending != NULL && pending->listed != 0) {
        pending_unlink_locked(pending);
    }
    pthread_mutex_unlock(&remote_spawn_lock);
    remote_spawn_pending_release(pending);
}

void remote_spawn_pending_fail_all(rt_executor* ex, rt_remote_spawn_status status) {
    for (;;) {
        pthread_mutex_lock(&remote_spawn_lock);
        rt_remote_spawn_pending* pending = remote_spawn_pending_head;
        while (pending != NULL && pending->status != RT_REMOTE_SPAWN_STATUS_PENDING) {
            pending = pending->next;
        }
        remote_spawn_pending_add_ref(pending);
        pthread_mutex_unlock(&remote_spawn_lock);
        if (pending == NULL) {
            return;
        }
        remote_spawn_pending_finish(ex, pending, status, NULL);
        remote_spawn_pending_release(pending);
    }
}

int rt_remote_spawn_abandon_handle(const rt_far_task_handle* out_handle) {
    if (out_handle == NULL) {
        return 0;
    }
    rt_remote_spawn_pending* found = NULL;
    rt_far_task_handle release_handle = {0};
    int drop_caller_ref = 0;
    pthread_mutex_lock(&remote_spawn_lock);
    for (rt_remote_spawn_pending* it = remote_spawn_pending_head; it != NULL; it = it->next) {
        if (it->out_handle != out_handle) {
            continue;
        }
        found = it;
        it->out_handle = NULL;
        it->abandoned = 1;
        if (it->status != RT_REMOTE_SPAWN_STATUS_PENDING) {
            if (it->status == RT_REMOTE_SPAWN_STATUS_OK) {
                release_handle = it->handle;
            }
            if (it->listed != 0) {
                pending_unlink_locked(it);
                drop_caller_ref = 1;
            }
        }
        break;
    }
    pthread_mutex_unlock(&remote_spawn_lock);
    if (release_handle.task_id != 0) {
        (void)rt_far_task_release(&release_handle);
    }
    if (drop_caller_ref) {
        remote_spawn_pending_release(found);
    }
    return found != NULL;
}

uint64_t rt_remote_spawn_pending_request_id(const rt_remote_spawn_pending* pending) {
    return pending != NULL ? pending->request_id : 0;
}
