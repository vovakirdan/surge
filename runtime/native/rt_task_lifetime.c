// Task handle lifetime (RV2-DEBT-003 split): this module
// owns the task handle refcount and the free of a DONE task's memory. Lane
// contract: free_task runs only on the control lane (D3); task_release
// assumes the caller already holds control, task_release_lane_aware acquires
// it only for the last-reference free of a DONE task. Extracted verbatim from
// rt_async_state.c; no behavior change.

#include "rt_async_internal.h"
#include "rt_remote_task.h"

void task_add_ref(rt_task* task) {
    if (task == NULL) {
        return;
    }
    (void)atomic_fetch_add_explicit(&task->handle_refs, 1, memory_order_relaxed);
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
    rt_task_result_dispose(&task->result);
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
    rt_task_result_dispose(&task->result);
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

// Called by the lane the moment it releases its last scheduler lock, so nothing
// is held here by construction.
void rt_task_reclaim_drain(void) {
    while (task_reclaim_head != NULL) {
        rt_task* task = task_reclaim_head;
        task_reclaim_head = task->reclaim_next;
        task->reclaim_next = NULL;
        reclaim_task(task_reclaim_executor, task);
    }
}

void task_release(rt_executor* ex, rt_task* task) {
    // Caller holds the control lock.
    if (ex == NULL || task == NULL) {
        return;
    }
    uint32_t refs = atomic_load_explicit(&task->handle_refs, memory_order_relaxed);
    if (refs == 0) {
        return;
    }
    refs = atomic_fetch_sub_explicit(&task->handle_refs, 1, memory_order_acq_rel);
    if (refs == 1 && task_status_load(task) == TASK_DONE) {
        free_task_when_unlocked(ex, task);
    }
}

void task_release_lane_aware(rt_executor* ex, rt_task* task) {
    // Free requires the control lane (D3); a control-free caller acquires it
    // only when this drop is the last reference to a DONE task.
    if (ex == NULL || task == NULL) {
        return;
    }
    uint32_t refs = atomic_load_explicit(&task->handle_refs, memory_order_relaxed);
    if (refs == 0) {
        return;
    }
    refs = atomic_fetch_sub_explicit(&task->handle_refs, 1, memory_order_acq_rel);
    if (refs == 1 && task_status_load(task) == TASK_DONE) {
        // A lane that holds nothing reclaims here and now; one that holds a
        // lock defers to the moment it lets go, because destroying the result
        // runs generated code.
        free_task_when_unlocked(ex, task);
    }
}
