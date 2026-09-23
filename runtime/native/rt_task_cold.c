#include "rt_task_cold.h"

#include "rt_frame.h"
#include "rt_remote_task.h"
#include "rt_sync_point.h"
#include "rt_task_refs.h"

// Cold task creation (RV2-DEBT-370).
//
// A call of an `async fn` and an `async { }` block create a task that does not
// run until `spawn` starts it or something awaits it
// (docs/RUNTIME_MODEL_EXPLAINED.ru.md 6.3). Creation still decides everything
// the rulings say creation decides -- the owning shard, the write-once
// creation_scope_key, the scope's membership and count, the parent's children,
// and the carrier pin of a task that borrows its creator's frame
// (docs/RUNTIME_V2.md section 9; rulings 2026-08-29, 2026-08-31, 2026-09-02) --
// and stops short of the one step that makes a task runnable: the ready push.
// The counted set is therefore a superset of the runnable set, which is the
// invariant "publication and count are one critical section" protects
// (RUNTIME_MODEL_EXPLAINED 6.2: no runnable task is ever uncounted).
//
// Every move out of RT_TASK_COLD is made under the task's owner shard lock, the
// lock every push of the task takes anyway, so a publication and a discard
// cannot both win:
//
//   - the first wake publishes it (wake_task_on_shard_locked): `spawn`, an
//     await off the task's carrier, a cancel, a scope join, a select or timeout
//     arm -- every path that makes a task runnable reaches that leaf;
//   - an await on the task's own shard and carrier claims it and polls it
//     inline, never queued (rt_task_claim_cold_inline);
//   - the drop of its last handle discards it (rt_task_release_cold_handle),
//     unless a cancel reached its gate first, in which case the drop publishes
//     it so the cancel is observed exactly as it is today.
//
// While a task is cold its owner shard cannot change: a repin needs a WAITING
// task and the placement adoption rewrites only a RUNNING task's own words, so
// the lock taken here is the lock its publication takes.
//
// The scope's cold_children hint is scope state the join reads. Its +1 is
// written where and by whom the count's +1 is written -- creation, under the
// scope's pinned lock -- so it has exactly the count's standing, including the
// count's inherited exception: a creator re-placed off its scope's shard writes
// both from its own lane (RUNTIME_V2 section 9 says "elsewhere it is a
// message"; this packet does not change that). Every -1 is written on the
// scope's owner lane (ruling 2026-09-02, Р6): by a publication made on that
// lane in the critical section it already holds, and otherwise by
// rt_scope_take_child_done_locked -- a foreign publication and a discard reach
// it through the existing scope event (rt_scope_event.c), as a foreign
// completion does.
//
// The inline claim keeps the base's scope: the base polled a child inline only
// when it sat on top of this worker's local queue, which in practice meant the
// awaiter's most recent creation. The cold equivalent is the creator's own
// last_cold_child word: at most one task, belonging to one running task, as a
// run-next slot holds at most one task for one carrier (RUNTIME_V2 section 1).
// A chain `let a = walk(n - 1); let b = leaf(); a.await()` therefore goes
// through the queue as it did, instead of nesting n polls on the C stack.

// A lane runs a shard when the task it is running is owned by that shard: the
// premise mark_done's scope_on_child_done already uses for a completion.
static int lane_runs_shard(uint32_t shard_id) {
    const rt_task* current = rt_current_task();
    return current != NULL && current->owner_shard_id == shard_id;
}

// What a publication owes the scope's owner lane when it could not settle the
// hint in its own critical section. Written by the one thread that moved the
// task out of COLD, under the owner lock, and settled by that same thread once
// it holds no lock; it names the task by id so nothing reads the task after it
// may have started running elsewhere.
typedef struct {
    waker_key key;
    uint64_t child_id;
    uint32_t source_shard_id;
    uint8_t owed;
} cold_publication_notice;

static _Thread_local cold_publication_notice cold_notice;

void rt_scope_note_cold_member_locked(rt_executor* ex, const rt_task* task) {
    if (ex == NULL || task == NULL || rt_task_publication_load(task) != RT_TASK_COLD ||
        !waker_valid(task->creation_scope_key)) {
        return;
    }
    // The same critical section, the same writer and the same standing as the
    // count this member was just given (rt_scope_publish_creation_locked).
    rt_scope* scope = rt_scope_resolve_key_locked(ex, task->creation_scope_key);
    if (scope != NULL) {
        scope->cold_children++;
    }
}

// Caller holds the task's owner shard lock and has just moved it out of COLD.
static void cold_publication_leaves_locked(rt_executor* ex, const rt_task* task) {
    waker_key key = task->creation_scope_key;
    if (!waker_valid(key) || task->scope_registered == 0) {
        return;
    }
    // The fast path Р6 asks to cost nothing: scope, member and publishing lane
    // share one shard, so the lock held is the scope's serializer and this lane
    // is its owner lane. No lock, no message, no read-modify-write.
    if (key.owner_shard_id == task->owner_shard_id && lane_runs_shard(key.owner_shard_id)) {
        rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
        if (scope != NULL && scope->cold_children > 0) {
            scope->cold_children--;
        }
        return;
    }
    cold_notice.key = key;
    cold_notice.child_id = task->id;
    cold_notice.source_shard_id = task->owner_shard_id;
    cold_notice.owed = 1;
}

uint8_t rt_task_publication_take_locked(rt_executor* ex, rt_task* task) {
    uint8_t publication = rt_task_publication_load(task);
    if (publication != RT_TASK_COLD) {
        return publication;
    }
    atomic_store_explicit(&task->publication, RT_TASK_PUBLISHED, memory_order_release);
    cold_publication_leaves_locked(ex, task);
    return RT_TASK_COLD;
}

void rt_task_cold_settle_publication(rt_executor* ex) {
    if (ex == NULL || cold_notice.owed == 0) {
        return;
    }
    cold_publication_notice notice = cold_notice;
    cold_notice.owed = 0;
    // The owner lane takes its own lock; any other lane sends the scope event.
    if (lane_runs_shard(notice.key.owner_shard_id)) {
        rt_shard* pinned = rt_waiter_key_shard(ex, notice.key);
        rt_shard_lock(pinned);
        (void)rt_scope_take_child_done_locked(
            ex, notice.key, notice.child_id, RT_SCOPE_OUTCOME_COLD_PUBLISHED, 1);
        rt_shard_unlock(pinned);
        return;
    }
    rt_scope_publish_cold_published(ex, notice.key, notice.child_id, notice.source_shard_id);
}

int rt_task_claim_cold_inline(rt_executor* ex, rt_task* current, rt_task* task) {
    if (ex == NULL || current == NULL || task == NULL || current->last_cold_child != task->id ||
        rt_task_publication_load(task) != RT_TASK_COLD) {
        return 0;
    }
    // The awaiter's own most recent creation, and nothing else: the base's
    // "top of my local queue". It is spent whether or not the claim succeeds.
    current->last_cold_child = 0;
    // The inline poll's premise (poll_ready_child_inline): this worker belongs
    // to the task's owner shard, and a carrier-affine task is polled only by
    // its carrier. Anything else publishes through the wake instead.
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    const rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (owner_shard == NULL || scheduler == NULL || tls_worker_ctx == NULL ||
        current_worker_scheduler(ex) != scheduler) {
        return 0;
    }
    if (task->carrier_valid != 0 && tls_worker_ctx->worker_id != task->carrier_worker_id) {
        return 0;
    }
    rt_shard_lock(owner_shard);
    int claimed = rt_task_publication_take_locked(ex, task) == RT_TASK_COLD;
    if (claimed) {
        // The claim a queued child gets from the pop that takes it: nothing
        // between here and the poll can hand the task to a second worker,
        // because it is in no queue and every waker now finds it RUNNING.
        task_enqueued_store(task, 0);
        task_status_store(task, TASK_RUNNING);
        (void)task_wake_token_exchange(task, 0);
    }
    rt_shard_unlock(owner_shard);
    rt_task_cold_settle_publication(ex);
    return claimed;
}

// Retires a discarded member with the outcome its gate committed -- NONE, a
// commit that is neither a value nor a cancellation -- and counted, as it was
// counted at creation: the two facts a scope event carries (Р6), both decided
// on the child's side and never re-derived by the scope. NONE raises no
// fail-fast (RUNTIME_V2 section 9, "Only a member raises fail-fast": the kind
// that raises is a cancellation) and retires the member's cold_children entry
// (rt_scope_take_child_done_locked). The owner lane applies it directly; any
// other lane publishes the scope event the owner lane applies on drain.
static void cold_retire_membership(rt_executor* ex, rt_task* task) {
    waker_key key = task->creation_scope_key;
    if (!waker_valid(key) || task->scope_registered == 0) {
        return;
    }
    task->scope_registered = 0;
    if (!lane_runs_shard(key.owner_shard_id)) {
        rt_scope_publish_child_done(ex, key, task, TASK_RESULT_NONE, 1);
        return;
    }
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    rt_shard_lock(pinned);
    rt_scope_child_done_effects fx =
        rt_scope_take_child_done_locked(ex, key, task->id, TASK_RESULT_NONE, 1);
    rt_shard_unlock(pinned);
    rt_scope_child_done_effects_apply(ex, key, fx);
}

// Ends a task nothing will ever run. The start frame holds the captured
// arguments, PACKED since the constructor built it, so its release destroys
// them through the frame's own descriptor (rt_frame.h) -- which is also where a
// borrowed argument stops pointing into the creator's frame -- and no poll is
// ever made.
static void cold_discard(rt_executor* ex, rt_task* task) {
    void* frame = task->state;
    const rt_value_ops* frame_ops = task->reclaim_frame_ops;
    task->state = NULL;
    task->reclaim_frame_ops = NULL;
    if (frame != NULL && frame_ops == NULL) {
        panic_msg("async: a cold task was created without its frame's descriptor");
        return;
    }
    rt_frame_release(frame_ops, frame);
    // The owned releases every completion runs (mark_done), so a far Task the
    // constructor handed this task leaves no holder naming it.
    rt_far_task_release_owned(ex, task);
    rt_immediate_on_release_owned(ex, task);
    rt_remote_task_release_owned(ex, task);
    cold_retire_membership(ex, task);
    task->result_kind = TASK_RESULT_NONE;
    task_status_store(task, TASK_DONE);
}

int rt_task_release_cold_handle(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return 0;
    }
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    if (owner_shard == NULL) {
        return 0;
    }
    // The decrement and the discard are one observation under the owner lock.
    // Two handles dropped at once must not both read "not last" -- each
    // decides from its own decrement -- and a spawn, a cancel's wake or a join
    // that publishes the task takes this same lock, so it either finds the task
    // discarded or leaves it published for this drop to find.
    rt_shard_lock(owner_shard);
    uint32_t refs = atomic_load_explicit(&task->handle_refs, memory_order_relaxed);
    if ((refs & RT_TASK_REFS_COUNT_MASK) == 0) {
        rt_shard_unlock(owner_shard);
        return 0;
    }
    refs = atomic_fetch_sub_explicit(&task->handle_refs, 1, memory_order_acq_rel);
    uint32_t count = refs & RT_TASK_REFS_COUNT_MASK;
    int cold = count == 1 && (refs & RT_TASK_REFS_COMPLETED) == 0 &&
               rt_task_publication_load(task) == RT_TASK_COLD;
    int discard = 0;
    int pushed = 0;
    if (cold) {
        // The commit of a task that never ran is the gate's single RMW, as any
        // commit is (rt_task_complete.c): sealing it here refuses a cancel that
        // arrives later. A cancel that took the gate first is owed the run it
        // has today, so the drop publishes the task instead of ending it.
        uint8_t open = RT_TASK_CANCEL_OPEN;
        discard = atomic_compare_exchange_strong_explicit(&task->cancelled,
                                                          &open,
                                                          (uint8_t)RT_TASK_CANCEL_SEALED,
                                                          memory_order_acq_rel,
                                                          memory_order_acquire);
        if (discard) {
            atomic_store_explicit(&task->publication, RT_TASK_DISCARDED, memory_order_release);
        } else if (rt_task_publication_take_locked(ex, task) == RT_TASK_COLD) {
            pushed = ready_push_task_locked(ex, owner_shard, task, 0, 0, 1);
        }
    }
    rt_shard_unlock(owner_shard);
    rt_task_cold_settle_publication(ex);
    if (pushed) {
        rt_compensation_check_after_push(ex);
    }
    if (!discard) {
        // Published after all, or by this very drop: the rule of
        // task_drop_ref_owes_free, which a running task's own completion
        // answers when this was not the last reference or it has not completed.
        return !cold && RT_CANONICAL_UNPINNED(ex, count) && (refs & RT_TASK_REFS_COMPLETED) != 0;
    }
    cold_discard(ex, task);
    return 1;
}

void rt_scope_publish_cold_members(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    rt_shard* pinned = rt_waiter_key_shard(ex, key);
    uint64_t inline_children[8];
    uint64_t* children = inline_children;
    size_t count = 0;
    rt_shard_lock(pinned);
    const rt_scope* scope = rt_scope_resolve_key_locked(ex, key);
    if (scope == NULL) {
        rt_shard_unlock(pinned);
        return;
    }
    count = scope->children_len;
    if (count > 8) {
        children = (uint64_t*)rt_alloc(count * sizeof(uint64_t), _Alignof(uint64_t));
        if (children == NULL) {
            rt_shard_unlock(pinned);
            fatal_oom_msg("async: scope publication snapshot allocation failed");
            return;
        }
    }
    if (count > 0) {
        memcpy(children, scope->children, count * sizeof(uint64_t));
    }
    rt_shard_unlock(pinned);
    // A member listed here may complete, and its task be freed, on another
    // shard before its scope event is applied; the free runs on the control
    // lane, so holding it makes get_task's answer safe to use. Each wake
    // settles its own notice (wake_task_with_policy).
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
    }
    for (size_t i = 0; i < count; i++) {
        const rt_task* child = get_task(ex, children[i]);
        // The window a last drop on another thread races: the pointer came from
        // get_task under control, and the drop can decide and discard but not
        // free until this walk lets control go.
        RT_SYNC_POINT(SP_COLD_JOIN_WALK_AFTER_GET_TASK);
        if (child != NULL && rt_task_publication_load(child) == RT_TASK_COLD) {
            wake_task(ex, children[i], 1);
        }
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
    if (children != inline_children) {
        rt_free((uint8_t*)children, count * sizeof(uint64_t), _Alignof(uint64_t));
    }
}
