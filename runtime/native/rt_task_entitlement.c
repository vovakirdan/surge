#include "rt_task_entitlement.h"

#include "rt_async_internal.h"

// Every transition runs under the task's owner shard lock: the take sites hold
// nothing when they get here (the take runs generated code, which no runtime
// lock may enclose), so the lock is taken for the bookkeeping alone and let go
// before any value is touched. Lane order is kept -- a lane that already holds
// control may take one shard lock, and a lane that holds this shard is not one
// that asks a task for its result.
static rt_shard* entitlement_lock(rt_executor* ex, const rt_task* task) {
    rt_shard* owner = rt_task_owner_shard(ex, task);
    rt_shard_lock(owner);
    return owner;
}

void rt_task_entitlements_init(rt_task_entitlements* entitlements) {
    if (entitlements == NULL) {
        return;
    }
    entitlements->live = 1;
    entitlements->clone_readers = 0;
    entitlements->move_waiting = 0;
    entitlements->moved = 0;
    entitlements->duplicate = NULL;
}

void rt_task_entitlement_clone(rt_executor* ex, rt_task* task, rt_value_clone_init_fn duplicate) {
    if (ex == NULL || task == NULL) {
        return;
    }
    rt_shard* owner = entitlement_lock(ex, task);
    rt_task_entitlements* e = &task->entitlements;
    if (e->live == 0 || e->moved) {
        // The handle being cloned was already served or dropped. Compiled code
        // cannot reach this (an await consumes its handle and a moved binding
        // is refused), so a clone here is a protocol violation, not a case.
        rt_shard_unlock(owner);
        panic_msg("async: a task handle was cloned after it was consumed");
        return;
    }
    e->live++;
    // The recipe is per TYPE, so every clone of one task installs the same
    // body; the first one to arrive is as good as any.
    if (e->duplicate == NULL) {
        e->duplicate = duplicate;
    }
    rt_shard_unlock(owner);
}

rt_task_take_mode rt_task_entitlement_begin_take(rt_executor* ex,
                                                 rt_task* task,
                                                 int has_value,
                                                 int droppable) {
    if (ex == NULL || task == NULL) {
        return RT_TASK_TAKE_NONE;
    }
    rt_shard* owner = entitlement_lock(ex, task);
    rt_task_entitlements* e = &task->entitlements;
    rt_task_take_mode mode;
    if (!has_value || e->moved) {
        mode = RT_TASK_TAKE_NONE;
    } else if (!droppable) {
        mode = RT_TASK_TAKE_COPY;
    } else if (e->live > 1) {
        // Somebody else can still ask, so this asker is served a value of its
        // own and the slot keeps the original for them. The reader count is
        // what keeps the final move from running under this duplication.
        if (e->duplicate == NULL) {
            mode = RT_TASK_TAKE_REFUSED;
        } else {
            e->clone_readers++;
            mode = RT_TASK_TAKE_CLONE;
        }
    } else if (e->clone_readers > 0) {
        // The last asker, but an earlier one is still copying out of the slot.
        // The value cannot leave while it is being read; the reader that
        // retires last wakes this asker to try again.
        e->move_waiting = 1;
        mode = RT_TASK_TAKE_WAIT;
    } else {
        // The last asker, with the slot to itself: the value moves out, which
        // is what makes a cohort of E cost E-1 duplications and one move.
        e->moved = 1;
        e->move_waiting = 0;
        mode = RT_TASK_TAKE_MOVE;
    }
    rt_shard_unlock(owner);
    return mode;
}

void rt_task_entitlement_finish_take(rt_executor* ex, rt_task* task, rt_task_take_mode mode) {
    if (ex == NULL || task == NULL || mode == RT_TASK_TAKE_WAIT) {
        return;
    }
    rt_shard* owner = entitlement_lock(ex, task);
    rt_task_entitlements* e = &task->entitlements;
    int wake_mover = 0;
    if (mode == RT_TASK_TAKE_CLONE && e->clone_readers > 0) {
        e->clone_readers--;
        wake_mover = e->clone_readers == 0 && e->move_waiting;
    }
    if (e->live > 0) {
        e->live--;
    }
    rt_shard_unlock(owner);
    if (wake_mover) {
        // The mover parked on the task's join key, the same key completion
        // uses, and an external awaiter waits on done_cv: wake both.
        wake_key_all_with_policy(ex, join_key(task->id), 0);
        rt_done_cv_broadcast_after_done(ex);
    }
}

void rt_task_entitlement_drop(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    rt_shard* owner = entitlement_lock(ex, task);
    if (task->entitlements.live > 0) {
        task->entitlements.live--;
    }
    rt_shard_unlock(owner);
}
