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
    rt_task_slot_store(ex, task->id, NULL);
    rt_free((uint8_t*)task, sizeof(rt_task), _Alignof(rt_task));
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
        free_task(ex, task);
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
}
