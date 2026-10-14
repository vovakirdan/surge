#include "rt_async_internal.h"
#include "rt_far_channel.h"
#include "rt_remote_task.h"

#include <stdlib.h>

// Called only after every runtime thread was joined. Reclaim empty capacity,
// never live task/scope objects: a missed user release must remain observable.
void rt_executor_release_empty_storage(rt_executor* ex) {
    if (ex == NULL || ex->worker_threads_started != 0 || ex->blocking_threads_started != 0 ||
        atomic_load_explicit(&ex->blocking_running, memory_order_acquire) != 0 ||
        ex->blocking_head != NULL) {
        return;
    }
    for (size_t i = 0; i < RT_TASK_TABLE_MAX_SEGMENTS; i++) {
        rt_task_segment* segment =
            atomic_load_explicit(&ex->tasks_table.segments[i], memory_order_acquire);
        if (segment == NULL) {
            continue;
        }
        for (size_t j = 0; j < RT_TASK_TABLE_SEGMENT_SIZE; j++) {
            if (atomic_load_explicit(&segment->slots[j], memory_order_acquire) != NULL) {
                return;
            }
        }
    }
    for (size_t i = 0; i < RT_SCOPE_TABLE_MAX_SEGMENTS; i++) {
        rt_scope_segment* segment =
            atomic_load_explicit(&ex->scopes_table.segments[i], memory_order_acquire);
        if (segment == NULL) {
            continue;
        }
        for (size_t j = 0; j < RT_SCOPE_TABLE_SEGMENT_SIZE; j++) {
            if (atomic_load_explicit(&segment->slots[j], memory_order_acquire) != NULL) {
                return;
            }
        }
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    for (size_t i = 0; i < rt_runtime_shard_count(runtime); i++) {
        const rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard->waiter_store.len != 0) {
            return;
        }
    }
    if (rt_remote_task_state_destroy(ex) != RT_RUNTIME_STATUS_OK ||
        rt_far_channel_state_destroy(ex) != RT_RUNTIME_STATUS_OK) {
        return;
    }
    for (size_t i = 0; i < RT_TASK_TABLE_MAX_SEGMENTS; i++) {
        rt_task_segment* segment =
            atomic_exchange_explicit(&ex->tasks_table.segments[i], NULL, memory_order_acq_rel);
        if (segment != NULL) {
            rt_free((uint8_t*)segment, sizeof(*segment), _Alignof(rt_task_segment));
        }
    }
    for (size_t i = 0; i < RT_SCOPE_TABLE_MAX_SEGMENTS; i++) {
        rt_scope_segment* segment =
            atomic_exchange_explicit(&ex->scopes_table.segments[i], NULL, memory_order_acq_rel);
        if (segment != NULL) {
            rt_free((uint8_t*)segment, sizeof(*segment), _Alignof(rt_scope_segment));
        }
    }
    rt_free((uint8_t*)ex->workers,
            (rt_runtime_total_worker_count(runtime) + 1) * sizeof(pthread_t),
            _Alignof(pthread_t));
    ex->workers = NULL;
    rt_free((uint8_t*)ex->blocking_workers,
            (uint64_t)ex->blocking_count * sizeof(pthread_t),
            _Alignof(pthread_t));
    ex->blocking_workers = NULL;
    free(ex->blocking_worker_ctxs);
    ex->blocking_worker_ctxs = NULL;
    pthread_cond_destroy(&ex->blocking_cv);
    pthread_mutex_destroy(&ex->blocking_lock);
    pthread_cond_destroy(&ex->done_cv);
    pthread_mutex_destroy(&ex->lock);
    rt_heap_accounting_set_current_cell(NULL);
    rt_runtime_destroy_global();
    ex->runtime = NULL;
    ex->initialized = 0;
}
