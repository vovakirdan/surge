#include "rt_remote_task_internal.h"

// Finding a pending BY THE TASK it belongs to, which is a different question
// from the one the rest of the pending module answers.
//
// rt_remote_task_pending.c owns a pending's own lifecycle: create it, count its
// references, publish its reply, retire it. Every function there already has the
// pending in hand. The three below start from a TASK and walk the state's list
// to find which pending, if any, speaks for it -- so they share the list and the
// lock with that module but not its subject, and they are the part that grew it
// past the module-size limit.

rt_remote_task_pending* rt_remote_task_pending_take_owner(const rt_task* task) {
    rt_executor* ex = task != NULL ? ensure_exec() : NULL;
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return NULL;
    }
    rt_remote_task_pending* target = NULL;
    pthread_mutex_lock(&state->lock);
    for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
        if (it->status == RT_REMOTE_TASK_STATUS_PENDING && it->owner_registered != 0 &&
            it->handle.task_id == task->id && it->handle.generation == task->generation &&
            it->handle.owner_shard_id == task->owner_shard_id) {
            it->owner_registered = 0;
            target = it;
            break;
        }
    }
    pthread_mutex_unlock(&state->lock);
    return target;
}

// The anchored block's body reaches its channel and its poll state through
// the pending that created it: dispatch cached the channel atomically with
// the pin, so the body sees a valid channel for the block's whole lifetime
// even when a release races the block (the pin defers reclamation to the
// reply edge). Returns 0 when the calling task is not a bound anchored body.
int rt_remote_task_anchored_binding_current(void** out_channel, void** out_state) {
    rt_executor* ex = ensure_exec();
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    const rt_task* current = rt_current_task();
    if (state == NULL || current == NULL) {
        return 0;
    }
    int bound = 0;
    pthread_mutex_lock(&state->lock);
    for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
        if (it->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED &&
            it->status == RT_REMOTE_TASK_STATUS_PENDING && it->handle.task_id == current->id &&
            it->handle.generation == current->generation &&
            it->handle.owner_shard_id == current->owner_shard_id) {
            if (out_channel != NULL) {
                *out_channel = it->anchored_channel;
            }
            if (out_state != NULL) {
                *out_state = it->body_state;
            }
            bound = 1;
            break;
        }
    }
    pthread_mutex_unlock(&state->lock);
    return bound;
}

void* rt_remote_task_anchored_channel_current(void) {
    void* channel = NULL;
    if (!rt_remote_task_anchored_binding_current(&channel, NULL)) {
        return NULL;
    }
    return channel;
}
