#include "rt_channel_lane.h"

#include <stddef.h>

// Terminal cleanup runs inside the cancelled poll, before its packed frame is
// handed to mark_done. That frame (or a structurally held owning activation)
// keeps the channel alive: a wake may already have retired the waiter pin, so
// the token alone cannot authorize reaching through its pool owner pointer.
void rt_channel_cancel_resume(rt_task* task, rt_executor* ex) {
    rt_shard* task_owner = rt_task_owner_shard(ex, task);
    rt_shard_lock(task_owner);
    rt_park_token slot = task->resume_slot;
    task->resume_slot = (rt_park_token){0};
    task->resume_kind = RESUME_NONE;
    rt_channel* ch = NULL;
    if (slot.owner != NULL) {
        ch = (rt_channel*)((uint8_t*)slot.owner - offsetof(rt_channel, parks));
        rt_channel_pin(ch);
    }
    // Clear the mailbox before unlocking: detached drops may re-enter the
    // runtime, and no second cleanup may claim this token from the task.
    rt_shard_unlock(task_owner);
    if (ch == NULL) {
        return;
    }
    rt_shard* channel_owner = channel_owner_shard(ex, ch);
    rt_shard_lock(channel_owner);
    // BUSY belongs to the receiver already taking these bytes. Its operation
    // holds its own channel pin and finishes the slot after the callback.
    channel_end_park_locked(ex, channel_owner, ch, &slot);
    rt_shard_unlock(channel_owner);
    rt_channel_unpin(ch);
}
