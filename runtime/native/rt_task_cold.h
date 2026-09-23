#ifndef SURGE_RUNTIME_NATIVE_RT_TASK_COLD_H
#define SURGE_RUNTIME_NATIVE_RT_TASK_COLD_H

#include "rt_async_internal.h"

// A task created cold (RV2-DEBT-370): recorded everywhere creation records a
// task, and pushed into no ready queue until something publishes it. The
// publication word and its three values live in rt_task_state.h; this is the
// protocol that moves a task out of RT_TASK_COLD, in rt_task_cold.c.

// Creation, beside membership: caller holds the creation scope's pinned shard
// lock, right after rt_scope_publish_creation_locked, and counts a cold member
// in the scope's cold_children hint. A task that is not cold, or not scoped, is
// ignored. Only the scope's owner lane writes the hint (ruling 2026-09-02, Р6).
void rt_scope_note_cold_member_locked(rt_executor* ex, const rt_task* task);

// The first wake of a cold task is its publication. Caller holds the task's
// owner shard lock (wake_task_on_shard_locked, rt_task_claim_cold_inline,
// rt_task_release_cold_handle). Answers the word as it was: RT_TASK_COLD means
// this call published it and the caller goes on to make it runnable;
// RT_TASK_DISCARDED means its last handle already ended it and the caller must
// not push it. A publication that cannot settle the scope's hint in the
// critical section it holds leaves a notice for rt_task_cold_settle_publication.
uint8_t rt_task_publication_take_locked(rt_executor* ex, rt_task* task);

// Settles the notice this thread's last publication left, once it holds no
// shard lock: the scope's owner lane applies it under its own lock, any other
// lane sends it as a scope event (rt_scope_publish_cold_published). Every
// caller that took a task out of COLD calls it after its unlock.
void rt_task_cold_settle_publication(rt_executor* ex);

// An await by `current` claims a cold task for an inline poll without queueing
// it -- only the awaiter's own most recent creation (its last_cold_child), on
// its own shard and, for an affine task, its carrier: the base's "top of my
// local queue". Nonzero means the caller owns it, RUNNING, unqueued, wake token
// consumed -- the claim ready_claim_current_local_tail makes for a queued
// child. Zero leaves the task as it was.
int rt_task_claim_cold_inline(rt_executor* ex, rt_task* current, rt_task* task);

// The drop of one handle of a task that read cold. Nonzero means the caller
// owes the free: this was the last handle, and the task was either discarded
// here -- its gate sealed, its frame released, its membership retired with
// outcome NONE, never polled -- or completed after something else published
// it. A last drop that finds a cancel already in the gate publishes the task.
int rt_task_release_cold_handle(rt_executor* ex, rt_task* task);

// The join's half: a scope whose hint says it may still have a cold member
// publishes every cold member before it waits, as an await of each would
// (rt_scope_join_all). Caller holds no lock; the walk takes the control lane,
// as the cancel walk does, so no member can be freed under it.
void rt_scope_publish_cold_members(rt_executor* ex, waker_key key);

#endif // SURGE_RUNTIME_NATIVE_RT_TASK_COLD_H
