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
    if (rt_remote_task_pending_snapshot(pending, NULL) == RT_REMOTE_TASK_STATUS_PENDING) {
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
            // A body that holds channel pins -- an anchored block, a remote
            // select -- gives them back on its own completion, and that
            // completion finds this pending through the owner registration.
            // The registration and the reference it holds stay with such a
            // body: the sweep runs under the control lock while the body may
            // be mid-turn on another carrier, holding the channel the pin
            // keeps alive, so the pin cannot be given back here, and a
            // severed registration would leave nobody to give it back at
            // all (RV2-DEBT-322). Every other body's completion has nothing
            // to return, and its registration is severed so it answers no
            // reply to a caller the sweep has already answered.
            int body_holds_pins =
                it->owner_registered != 0 &&
                ((it->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED && it->anchor_pinned != 0) ||
                 (it->op == RT_REMOTE_TASK_OP_CHANNEL_SELECT && it->select_arms != NULL));
#ifdef RV2_DEBT_322_NEGATIVE_CONTROL
            // Rule 13: sever every registration, as the sweep did before, and
            // the teardown row reads the pin still in place after shutdown.
            body_holds_pins = 0;
#endif
            release_owner_ref = it->owner_registered != 0 && !body_holds_pins;
            if (!body_holds_pins) {
                it->owner_registered = 0;
                // The severed registration takes the body's pointer with
                // it: the reference it held is released below, and a body
                // completing later must not find a freed pending.
                rt_task* body = release_owner_ref ? get_task(ex, it->handle.task_id) : NULL;
                if (body != NULL && body->remote_owner_pending == it) {
                    body->remote_owner_pending = NULL;
                }
            }
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
        // No reply will be enqueued for a pending resolved here, so the slot
        // its request reserved goes back to the lane now; and a request still
        // parked on a slot key never left, so its registration goes too, with
        // the message reference nothing will ever enqueue.
        if (rt_remote_admission_abandon(ex, pending->caller_task_id, &pending->admission)) {
            rt_remote_task_pending_release(pending);
        }
        if (release_owner_ref) {
            rt_remote_task_pending_release(pending);
        }
        rt_remote_task_pending_release(pending);
    }
}
