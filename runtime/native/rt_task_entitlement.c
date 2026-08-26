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
    entitlements->claimed = 0;
    entitlements->mover = NULL;
    atomic_store_explicit(&entitlements->clone_readers, 0, memory_order_relaxed);
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

rt_value_clone_init_fn rt_task_entitlement_duplicate(const rt_task* task,
                                                     const rt_value_ops* operations) {
    if (operations != NULL && (operations->layout.flags & RT_VALUE_FLAG_CLONABLE) != 0 &&
        operations->clone_init != NULL) {
        return operations->clone_init;
    }
    return task != NULL ? task->entitlements.duplicate : NULL;
}

// The mover's own decision: move if the slot is free of readers, otherwise
// park. Made under the lock, for the first arrival and for every retry alike.
static rt_task_take_mode mover_decision(rt_task_entitlements* e) {
    if (atomic_load_explicit(&e->clone_readers, memory_order_acquire) > 0) {
        e->move_waiting = 1;
        return RT_TASK_TAKE_WAIT;
    }
    e->move_waiting = 0;
    e->moved = 1;
    return RT_TASK_TAKE_MOVE;
}

rt_task_take_mode rt_task_entitlement_begin_take(rt_executor* ex,
                                                 rt_task* task,
                                                 const void* asker,
                                                 int has_value,
                                                 const rt_value_ops* operations) {
    if (ex == NULL || task == NULL) {
        return RT_TASK_TAKE_NONE;
    }
    int droppable = operations != NULL && (operations->layout.flags & RT_VALUE_FLAG_DROPPABLE) != 0;
    rt_shard* owner = entitlement_lock(ex, task);
    rt_task_entitlements* e = &task->entitlements;
    rt_task_take_mode mode;
    if (e->mover != NULL && e->mover == asker) {
        // The reserved mover coming back after a WAIT: still claimed, still
        // the mover, only the readers can have changed.
        mode = mover_decision(e);
    } else if (!has_value || e->moved) {
        e->claimed++;
        mode = RT_TASK_TAKE_NONE;
    } else if (!droppable) {
        e->claimed++;
        mode = RT_TASK_TAKE_COPY;
    } else {
        e->claimed++;
        // Whether a handle that is NOT asking right now still exists. While
        // one does, a later asker can come and the slot must keep the
        // original for it; once every remaining handle is inside a take, one
        // of them -- the first to see that -- is reserved for the final move
        // and the others clone. That is what makes a cohort of E cost E-1
        // duplications and one move whether the askers come one after another
        // or all at once.
        int unclaimed_left = e->live > e->claimed;
        if (unclaimed_left || e->mover != NULL) {
            if (rt_task_entitlement_duplicate(task, operations) == NULL) {
                mode = RT_TASK_TAKE_REFUSED;
            } else {
                atomic_fetch_add_explicit(&e->clone_readers, 1, memory_order_acq_rel);
                mode = RT_TASK_TAKE_CLONE;
            }
        } else {
            e->mover = asker;
            mode = mover_decision(e);
        }
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
    if (mode == RT_TASK_TAKE_CLONE &&
        atomic_load_explicit(&e->clone_readers, memory_order_acquire) > 0) {
        uint32_t left = atomic_fetch_sub_explicit(&e->clone_readers, 1, memory_order_acq_rel) - 1;
        wake_mover = left == 0 && e->move_waiting;
    }
    if (mode == RT_TASK_TAKE_MOVE) {
        e->mover = NULL;
    }
    if (e->claimed > 0) {
        e->claimed--;
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

int rt_task_entitlement_move_ready(const rt_task* task) {
    return task == NULL ||
           atomic_load_explicit(&task->entitlements.clone_readers, memory_order_acquire) == 0;
}
