// Task handle lifetime (RV2-DEBT-003 split): this module
// owns the task handle refcount and the free of a DONE task's memory. Lane
// contract: free_task runs only on the control lane (D3); task_release
// assumes the caller already holds control, task_release_lane_aware acquires
// it only for the last-reference free of a DONE task. Extracted verbatim from
// rt_async_state.c; no behavior change.

#include "rt_async_internal.h"
#include "rt_remote_task.h"
#include "rt_sync_point.h"
#include "rt_task_refs.h"
#include "rt_value_ops.h"
#ifdef RT_TASK_RELEASE_SPLIT_NEGATIVE_CONTROL
#include <time.h>
#endif

void task_add_ref(rt_task* task) {
    if (task == NULL) {
        return;
    }
    (void)atomic_fetch_add_explicit(&task->handle_refs, 1, memory_order_relaxed);
}

void task_mark_completed(rt_task* task) {
    if (task == NULL) {
        return;
    }
    // Release, and paired with the acquire half of the decrement below: the
    // thread that frees must see everything mark_done wrote before it got here.
    // No free can be owed by this store itself -- mark_done holds a reference
    // across it, so the count is at least one -- which is why raising the flag
    // needs no answer.
    (void)atomic_fetch_or_explicit(
        &task->handle_refs, RT_TASK_REFS_COMPLETED, memory_order_release);
}

// Drops one handle reference and answers, from that one decrement, whether this
// drop is the one that must free the task: it emptied the count AND the task had
// already completed. Nothing is read from the task afterwards, which is the
// whole point -- see the handle_refs field comment for the double free that a
// decrement plus a separate status load produced.
// "Last reference" is also the canonical result's pin: an asker inside a take
// holds one of these references, and a claimed clone reader duplicates out of
// the result slot in place with no lock held, so the reclaim that destroys that
// value may not run while any of them is still out. Shutdown is not an
// exception -- it stops new entitlements and lets claimed work finish, and the
// canonical slot goes when the last asker has let go. RT_CANONICAL_UNPINNED is
// that rule, and its negative control reads shutdown as permission to drop.
static int task_drop_ref_owes_free(rt_executor* ex, rt_task* task) {
    // Only the canonical-pin negative control reads the executor; the shipping
    // rule answers from the word alone.
    (void)ex;
#ifdef RT_TASK_RELEASE_SPLIT_NEGATIVE_CONTROL
    // The pre-fix shape, restored so the stand can show what it costs: the
    // decrement decides "last reference", a SEPARATE load decides "completed".
    // The sleep is not the defect and not a fix for it -- the window between
    // the two is the defect, and the sleep only holds this thread inside that
    // window long enough for it to be taken often instead of about once in
    // forty thousand program runs.
    uint32_t split_refs = atomic_load_explicit(&task->handle_refs, memory_order_relaxed);
    if (split_refs == 0) {
        return 0;
    }
    split_refs = atomic_fetch_sub_explicit(&task->handle_refs, 1, memory_order_acq_rel);
    if (split_refs != 1) {
        return 0;
    }
    if (task_status_load(task) != TASK_DONE) {
        // The window itself: the count is at zero and the task has NOT
        // completed, so this thread is about to answer "do not free" -- and the
        // answer it will actually give comes from the load AFTER this pause. A
        // poller that resurrects, completes and frees the task in the meantime
        // is invisible to it. Waiting here is what makes the window get taken;
        // it does not create it.
        struct timespec widen = {0, 20000000};
        (void)nanosleep(&widen, NULL);
    }
    return task_status_load(task) == TASK_DONE;
#else
    uint32_t refs = atomic_load_explicit(&task->handle_refs, memory_order_relaxed);
    if ((refs & RT_TASK_REFS_COUNT_MASK) == 0) {
        return 0;
    }
    refs = atomic_fetch_sub_explicit(&task->handle_refs, 1, memory_order_acq_rel);
    return RT_CANONICAL_UNPINNED(ex, refs & RT_TASK_REFS_COUNT_MASK) &&
           (refs & RT_TASK_REFS_COMPLETED) != 0;
#endif
}

static void free_task(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    if (task->wait_keys_len > 0) {
        clear_wait_keys(ex, task);
    }
    if (task->wait_keys != NULL && task->wait_keys_cap > 0) {
        rt_free((uint8_t*)task->wait_keys,
                (uint64_t)task->wait_keys_cap * (uint64_t)sizeof(waker_key),
                _Alignof(waker_key));
    }
    if (task->select_timers != NULL && task->select_timers_cap > 0) {
        rt_free((uint8_t*)task->select_timers,
                (uint64_t)task->select_timers_cap * (uint64_t)sizeof(uint64_t),
                _Alignof(uint64_t));
    }
    if (task->children != NULL && task->children_cap > 0) {
        rt_free((uint8_t*)task->children,
                (uint64_t)task->children_cap * (uint64_t)sizeof(uint64_t),
                _Alignof(uint64_t));
    }
    rt_far_task_release_result(ex, task);
    // Owner-side result reclamation (RV2-DEBT-053a): a RESULT no consumer took
    // -- release-while-DONE, cancel-after-done, a far caller that died before
    // it fetched -- is destroyed here exactly once, by the slot that owns it.
    // A consumed result left the slot MOVED, so this and the consume path are
    // mutually exclusive by the slot's own state rather than by a cleared id.
    //
    // reclaim_task destroys the result BEFORE it takes control, so by the time
    // this runs the slot is empty and this call is a no-op. It stays as the
    // last-resort discharge: if a future caller ever reaches the structural
    // free with a live result under a lock, the detached-dispatch guard aborts
    // here loudly instead of running a drop under the lock quietly.
    rt_value_cell_dispose(&task->result);
    rt_task_slot_store(ex, task->id, NULL);
    rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
}

// Reclaiming a task is TWO steps with opposite requirements, and this is the one
// place that gets to say so.
//
// Destroying the result runs generated code, which may not run under a
// scheduler lock (§8 P2). Freeing the task's memory must be serialized against
// every other lane that can still read it -- a cancel arriving on another
// thread reads the task's placement -- and the control lock is what does that.
// So the result is destroyed first with nothing held, and only then is control
// taken for the structural free.
static void reclaim_task(rt_executor* ex, rt_task* task) {
    rt_value_cell_dispose(&task->result);
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_HANDLE);
        rt_trace_control_lock_handle_site(RT_CTRL_HANDLE_FREE);
    }
    free_task(ex, task);
    if (need_control) {
        rt_control_unlock(ex);
    }
}

// Tasks whose free was deferred out from under a scheduler lock, newest first.
//
// The list threads through the tasks themselves, so a deferral allocates
// nothing: a task waiting to be freed is memory that already exists, and a
// side table would put an allocation on the path whose whole purpose is to run
// a destructor safely.
static _Thread_local rt_task* task_reclaim_head;
static _Thread_local rt_executor* task_reclaim_executor;

static void free_task_when_unlocked(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    if (!rt_lane_holds_control() && !rt_lane_holds_any_shard()) {
        reclaim_task(ex, task);
        return;
    }
    task_reclaim_executor = ex;
    task->reclaim_next = task_reclaim_head;
    task_reclaim_head = task;
}

// Owned blocks whose destruction was deferred out from under a scheduler lock.
//
// A state box a cancellation abandoned is destroyed by mark_done, which runs
// holding control -- and destroying it runs the type's generated drop, which
// may not run under a lock. There is no task to thread it through by then
// either: mark_done clears the fields on the way past. So the pair travels in
// its own small ring until the lane is free.
//
// The ring is fixed and per-thread: one completion abandons at most one state,
// and the drain runs the moment the lane drops its last lock, so more than a
// handful in flight would mean a lock was held across many completions. A full
// ring falls back to destroying in place, which is the old behaviour rather
// than a leak, and the detached guard says so loudly if that ever happens
// under a lock.
#define RT_DEFERRED_BLOCK_SLOTS 8

typedef struct {
    const rt_value_ops* operations;
    void* storage;
    // The task whose own bytes `storage` points into, or NULL when the block is
    // an allocation the release must also free. A refused task result that fits
    // inline lives in the task (RV2-DEBT-263), so the deferral pins it: the
    // drop must not reach for bytes a reclaim has already given back.
    rt_task* inline_owner;
} rt_deferred_block;

static _Thread_local rt_deferred_block deferred_blocks[RT_DEFERRED_BLOCK_SLOTS];
static _Thread_local size_t deferred_block_count;

static void deferred_block_release(rt_deferred_block* block) {
    const rt_value_ops* operations = block->operations;
    void* storage = block->storage;
    rt_task* owner = block->inline_owner;
    block->operations = NULL;
    block->storage = NULL;
    block->inline_owner = NULL;
    if (owner == NULL) {
        rt_value_release_owned_block(operations, storage);
        return;
    }
    // Inline bytes: drop the value where it lies, never free it, then give back
    // the pin that kept those bytes addressable.
    rt_value_drop_in_place_detached(operations, storage);
    task_release_lane_aware(task_reclaim_executor, owner);
}

static void
release_when_unlocked(const rt_value_ops* operations, void* storage, rt_task* inline_owner) {
    if (operations == NULL || storage == NULL) {
        return;
    }
    rt_deferred_block block = {operations, storage, inline_owner};
    if ((!rt_lane_holds_control() && !rt_lane_holds_any_shard()) ||
        deferred_block_count == RT_DEFERRED_BLOCK_SLOTS) {
        deferred_block_release(&block);
        return;
    }
    deferred_blocks[deferred_block_count] = block;
    deferred_block_count++;
}

void rt_release_owned_block_when_unlocked(const rt_value_ops* operations, void* storage) {
    release_when_unlocked(operations, storage, NULL);
}

// A completion that refused the value its body produced (RV2-DEBT-263).
//
// The slot is emptied HERE, synchronously, because what comes after in
// mark_done can NAME it: rt_remote_task_on_owner_done answers a far await with
// a capability that points at this very cell, and the caller then moves the
// value out of it -- into storage whose generated Cancelled arm never reads it.
// A task that answers Cancelled must therefore hold no value by the time
// TASK_DONE is published, which restores the invariant every reader of a result
// slot already relies on. The DESTRUCTION is what waits: it runs the value's own
// drop, which may not run under a scheduler lock (rule 8 P2).
void rt_task_result_refuse(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    const rt_value_ops* operations = NULL;
    int owns_block = 0;
    void* storage = rt_value_cell_hand_off(&task->result, &operations, &owns_block);
    if (storage == NULL) {
        return;
    }
    if (owns_block) {
        release_when_unlocked(operations, storage, NULL);
        return;
    }
    // The bytes are the task's own. Pin it so the deferral cannot outlive them;
    // deferred_block_release gives the pin back after the drop.
    task_add_ref(task);
    task_reclaim_executor = ex;
    release_when_unlocked(operations, storage, task);
}

// Called by the lane the moment it releases its last scheduler lock, so nothing
// is held here by construction.
void rt_task_reclaim_drain(void) {
    // Blocks first, tasks second, and the order is load-bearing: a refused
    // result that lived inline in a task releases that task's pin, which can
    // push it onto the reclaim list this loop then drains.
    while (deferred_block_count > 0) {
        deferred_block_count--;
        deferred_block_release(&deferred_blocks[deferred_block_count]);
    }
    while (task_reclaim_head != NULL) {
        rt_task* task = task_reclaim_head;
        task_reclaim_head = task->reclaim_next;
        task->reclaim_next = NULL;
        reclaim_task(task_reclaim_executor, task);
    }
}

// The drop of a Task<T> value, reached from compiled code: a handle inside a
// frame a cancellation abandoned, an element of a container being torn down,
// a local a shutdown unwinds past. It is the one entry the program has to the
// handle's reference that is not an await, and it must not decide anything
// about the result: an asker that never came is not a value that was taken,
// so the slot keeps its own until the last reference goes and the owner-side
// reclamation in free_task destroys it exactly once (RV2-DEBT-053a).
void rt_task_handle_drop(void* task) {
    // A container's element slot the handle was moved out of holds NULL, and
    // the container's drop glue still visits it: nothing to give back.
    if (task == NULL) {
        return;
    }
    rt_executor* ex = ensure_exec();
    rt_task* target = task_from_handle(task);
    if (ex == NULL || target == NULL) {
        return;
    }
    rt_task_entitlement_drop(ex, target);
    task_release_lane_aware(ex, target);
}

void task_release(rt_executor* ex, rt_task* task) {
    // Caller holds the control lock.
    if (ex == NULL || task == NULL) {
        return;
    }
    if (task_drop_ref_owes_free(ex, task)) {
        free_task_when_unlocked(ex, task);
    }
}

void task_release_lane_aware(rt_executor* ex, rt_task* task) {
    // Free requires the control lane (D3); a control-free caller acquires it
    // only when this drop is the last reference to a DONE task.
    if (ex == NULL || task == NULL) {
        return;
    }
    if (task_drop_ref_owes_free(ex, task)) {
        // A lane that holds nothing reclaims here and now; one that holds a
        // lock defers to the moment it lets go, because destroying the result
        // runs generated code.
        free_task_when_unlocked(ex, task);
    }
}
