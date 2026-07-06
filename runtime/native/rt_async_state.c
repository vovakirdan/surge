#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"

#include <errno.h>
#include <limits.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
rt_executor exec_state;
_Thread_local jmp_buf* poll_env;
_Thread_local int poll_active = 0;
_Thread_local poll_outcome poll_result;
_Thread_local waker_key pending_key;
_Thread_local uint64_t tls_current_id;
_Thread_local rt_task* tls_current_task;
_Thread_local int tls_worker_id = -1;
_Thread_local rt_worker_ctx* tls_worker_ctx;
static pthread_once_t exec_once = PTHREAD_ONCE_INIT;

static uint8_t channel_wake_force_inject;

int channel_wake_force_inject_enabled(void) {
    return channel_wake_force_inject != 0;
}

static int async_debug_enabled_cached = -1;

int rt_async_debug_enabled(void) {
    if (async_debug_enabled_cached >= 0) {
        return async_debug_enabled_cached;
    }
    const char* value = getenv("SURGE_ASYNC_DEBUG");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        async_debug_enabled_cached = 0;
        return 0;
    }
    async_debug_enabled_cached = 1;
    return 1;
}

void rt_async_debug_printf(const char* fmt, ...) {
    if (!rt_async_debug_enabled() || fmt == NULL) {
        return;
    }
    char buf[512];
    va_list args;
    va_start(args, fmt);
#if defined(__clang__)
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wformat-nonliteral"
#elif defined(__GNUC__)
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wformat-nonliteral"
#endif
    int n = vsnprintf(buf, sizeof(buf), fmt, args);
#if defined(__clang__)
#pragma clang diagnostic pop
#elif defined(__GNUC__)
#pragma GCC diagnostic pop
#endif
    va_end(args);
    if (n <= 0) {
        return;
    }
    uint64_t len = (uint64_t)n;
    if ((size_t)n >= sizeof(buf)) {
        len = (uint64_t)(sizeof(buf) - 1);
    }
    rt_write_stderr((const uint8_t*)buf, len);
}

void panic_msg(const char* msg) {
    if (msg == NULL) {
        return;
    }
    rt_panic((const uint8_t*)msg, (uint64_t)strlen(msg));
}

uint64_t rt_current_task_id(void) {
    if (tls_current_task != NULL) {
        return tls_current_task->id;
    }
    return tls_current_id;
}

rt_task* rt_current_task(void) {
    return tls_current_task;
}

void rt_set_current_task(rt_task* task) {
    tls_current_task = task;
    tls_current_id = task != NULL ? task->id : 0;
}

// Seeded scheduler mode provides deterministic scheduler choices given the same seed and the same
// arrival order of external events; it does not control I/O timing or OS thread interleavings.
static uint8_t rt_env_sched_mode(void) {
    const char* value = getenv("SURGE_SCHED");
    if (value == NULL || value[0] == '\0') {
        return SCHED_PARALLEL;
    }
    if (strcmp(value, "seeded") == 0) {
        return SCHED_SEEDED;
    }
    if (strcmp(value, "parallel") == 0) {
        return SCHED_PARALLEL;
    }
    return SCHED_PARALLEL;
}

static uint64_t rt_env_sched_seed(void) {
    const char* value = getenv("SURGE_SCHED_SEED");
    if (value == NULL || value[0] == '\0') {
        return 0;
    }
    errno = 0;
    char* end = NULL;
    unsigned long long parsed = strtoull(value, &end, 0);
    if (end == value || errno != 0) {
        return 0;
    }
    return (uint64_t)parsed;
}

static uint8_t rt_env_channel_wake_force_inject(void) {
    const char* value = getenv("SURGE_CHANNEL_WAKE_INJECT");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        return 0;
    }
    return 1;
}

static void rt_start_workers(rt_executor* ex);
static int scheduler_runnable_is_empty(const rt_scheduler* scheduler);

static uint32_t rt_config_total_worker_threads(const rt_runtime_start_config* config) {
    if (config == NULL) {
        return 0;
    }
    if (config->shard_count <= 1) {
        return config->legacy_worker_threads;
    }
    if (config->shard_worker_count == 0 ||
        config->shard_count > (size_t)(UINT32_MAX / config->shard_worker_count)) {
        panic_msg("async: worker count overflow");
        return 0;
    }
    return (uint32_t)config->shard_count * config->shard_worker_count;
}

static void exec_init_once(void) {
    rt_executor* ex = &exec_state;
    memset(ex, 0, sizeof(*ex));
    rt_runtime_start_config config;
    const char* config_error = NULL;
    if (rt_runtime_start_config_from_env(&config, &config_error) != RT_RUNTIME_STATUS_OK) {
        rt_runtime_config_exit(config_error);
    }
    if (rt_runtime_init_global(ex, config.shard_count) != RT_RUNTIME_STATUS_OK) {
        panic_msg("async: runtime skeleton initialization failed");
    }
    ex->next_id = 1;
    ex->next_scope_id = 1;
    pthread_mutex_init(&ex->lock, NULL);
    pthread_cond_init(&ex->compat_cv, NULL);
    pthread_cond_init(&ex->done_cv, NULL);
    rt_exec_trace_init();
    rt_sched_trace_init();
    uint32_t threads = config.legacy_worker_threads;
    uint32_t total_worker_threads = rt_config_total_worker_threads(&config);
    uint32_t blocking_threads = config.blocking_threads;
    ex->blocking_count = blocking_threads;
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    if (rt_heap_accounting_prepare_cells(accounting, total_worker_threads, blocking_threads) !=
        RT_HEAP_ACCOUNTING_OK) {
        rt_runtime_destroy_global();
        panic_msg("async: heap accounting cell allocation failed");
    }
    rt_heap_accounting_set_current_cell(rt_heap_accounting_main_cell(accounting));
    rt_runtime_status scheduler_status = rt_runtime_init_shard_schedulers(rt_executor_runtime(ex),
                                                                          config.shard_worker_count,
                                                                          rt_env_sched_mode(),
                                                                          rt_env_sched_seed());
    if (scheduler_status == RT_RUNTIME_STATUS_ALLOCATION_FAILED) {
        rt_runtime_destroy_global();
        panic_msg("async: local queue allocation failed");
    }
    if (scheduler_status != RT_RUNTIME_STATUS_OK) {
        rt_runtime_destroy_global();
        panic_msg("async: scheduler initialization failed");
    }
    channel_wake_force_inject = rt_env_channel_wake_force_inject();
    if (threads > 1 || rt_runtime_shard_count(rt_executor_runtime(ex)) > 1) {
        rt_start_workers(ex);
    }
    rt_blocking_init(ex);
    ex->initialized = 1;
}

rt_executor* ensure_exec(void) {
    pthread_once(&exec_once, exec_init_once);
    return &exec_state;
}

uint64_t rt_worker_count(void) {
    const rt_executor* ex = ensure_exec();
    const rt_runtime* runtime = ex != NULL ? ex->runtime : NULL;
    return rt_runtime_total_worker_count(runtime);
}

static int scheduler_runnable_is_empty(const rt_scheduler* scheduler) {
    if (scheduler == NULL) {
        return 1;
    }
    if (scheduler->inject.len > 0) {
        return 0;
    }
    if (scheduler->local_queues == NULL || scheduler->worker_count == 0) {
        return 1;
    }
    for (uint32_t i = 0; i < scheduler->worker_count; i++) {
        if (scheduler->local_queues[i].len > 0) {
            return 0;
        }
    }
    return 1;
}

// Locked idle sample for control-lane callers (io thread, N=1 runner):
// reads each shard's queues and running count under that shard's lock, one
// shard at a time. Cross-shard atomicity is not promised (spike D7); for
// SURGE_SHARDS=1 the single-shard sample is exact.
int rt_sched_idle_sample_locked(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL) {
            continue;
        }
        rt_shard_lock(shard);
        const rt_scheduler* scheduler = rt_shard_scheduler_const(shard);
        int busy = scheduler != NULL &&
                   (scheduler->running_count > 0 || !scheduler_runnable_is_empty(scheduler));
        rt_shard_unlock(shard);
        if (busy) {
            return 0;
        }
    }
    return 1;
}

static uint64_t sched_next_u64(rt_worker_ctx* ctx) {
    if (ctx == NULL) {
        return 0;
    }
    uint64_t z = (ctx->sched_rng += UINT64_C(0x9e3779b97f4a7c15));
    z = (z ^ (z >> 30)) * UINT64_C(0xbf58476d1ce4e5b9);
    z = (z ^ (z >> 27)) * UINT64_C(0x94d049bb133111eb);
    return z ^ (z >> 31);
}

static void rt_start_workers(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    if (ex == NULL || runtime == NULL || runtime->shard_count < 1 ||
        runtime->shard_count > RT_RUNTIME_MAX_SHARDS) {
        return;
    }
    uint64_t total_workers64 = rt_runtime_total_worker_count(runtime);
    if (runtime->shard_count <= 1 && total_workers64 <= 1) {
        return;
    }
    if (total_workers64 == 0 || total_workers64 > UINT32_MAX) {
        panic_msg("async: worker count overflow");
        return;
    }
    uint32_t total_workers = (uint32_t)total_workers64;
    size_t thread_count = (size_t)total_workers + 1;
    pthread_t* threads = (pthread_t*)rt_alloc((uint64_t)thread_count * (uint64_t)sizeof(pthread_t),
                                              _Alignof(pthread_t));
    if (threads == NULL) {
        panic_msg("async: worker allocation failed");
        return;
    }
    ex->workers = threads;
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    if (pthread_create(&threads[0], NULL, rt_io_main, ex) != 0) {
        panic_msg("async: io worker start failed");
        return;
    }
    (void)pthread_detach(threads[0]);
    ex->io_started = 1;
    uint32_t flat_index = 0;
    for (size_t shard_index = 0; shard_index < runtime->shard_count; shard_index++) {
        rt_shard* shard = rt_runtime_shard(runtime, shard_index);
        rt_scheduler* scheduler = rt_shard_scheduler(shard);
        if (shard == NULL || scheduler == NULL || scheduler->worker_count == 0) {
            continue;
        }
        uint32_t count = scheduler->worker_count;
        rt_worker_ctx* ctxs = (rt_worker_ctx*)rt_alloc(
            (uint64_t)count * (uint64_t)sizeof(rt_worker_ctx), _Alignof(rt_worker_ctx));
        if (ctxs == NULL) {
            panic_msg("async: worker context allocation failed");
            return;
        }
        memset(ctxs, 0, (size_t)count * sizeof(rt_worker_ctx));
        scheduler->worker_ctxs = ctxs;
        for (uint32_t i = 0; i < count; i++) {
            ctxs[i].ex = ex;
            ctxs[i].shard = shard;
            ctxs[i].scheduler = scheduler;
            ctxs[i].heap_cell = rt_heap_accounting_worker_cell(accounting, flat_index);
            ctxs[i].shard_id = shard->shard_id;
            ctxs[i].worker_id = i;
            ctxs[i].worker_index = flat_index;
            ctxs[i].sched_rng =
                scheduler->sched_seed + UINT64_C(0x9e3779b97f4a7c15) * (uint64_t)(flat_index + 1U);
            if (pthread_create(&threads[(size_t)flat_index + 1], NULL, rt_worker_main, &ctxs[i]) !=
                0) {
                panic_msg("async: worker start failed");
                return;
            }
            (void)pthread_detach(threads[(size_t)flat_index + 1]);
            flat_index++;
        }
    }
}

int rt_debug_validate_worker_ctx(rt_executor* ex,
                                 uint32_t shard_id,
                                 uint32_t worker_id,
                                 uint32_t worker_index) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_id);
    rt_scheduler* scheduler = rt_shard_scheduler(shard);
    if (shard == NULL || scheduler == NULL || scheduler->worker_ctxs == NULL ||
        worker_id >= scheduler->worker_count) {
        return 0;
    }
    rt_worker_ctx* ctx = &scheduler->worker_ctxs[worker_id];
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    return ctx->ex == ex && ctx->shard == shard && ctx->scheduler == scheduler &&
           ctx->shard_id == shard_id && ctx->worker_id == worker_id &&
           ctx->worker_index == worker_index &&
           ctx->heap_cell == rt_heap_accounting_worker_cell(accounting, worker_index);
}

uint32_t rt_debug_current_worker_shard_id(void) {
    return tls_worker_ctx != NULL ? tls_worker_ctx->shard_id : UINT32_MAX;
}

rt_task* get_task(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0) {
        return NULL;
    }
    size_t seg_idx = (size_t)(id >> RT_TASK_TABLE_SEGMENT_SHIFT);
    if (seg_idx >= RT_TASK_TABLE_MAX_SEGMENTS) {
        return NULL;
    }
    rt_task_segment* segment =
        atomic_load_explicit(&ex->tasks_table.segments[seg_idx], memory_order_acquire);
    if (segment == NULL) {
        return NULL;
    }
    size_t slot_idx = (size_t)(id & (RT_TASK_TABLE_SEGMENT_SIZE - 1));
    return atomic_load_explicit(&segment->slots[slot_idx], memory_order_acquire);
}

uint64_t rt_task_table_snapshot(rt_executor* ex) {
    return ex != NULL ? atomic_load_explicit(&ex->next_id, memory_order_acquire) : 0;
}

void rt_task_slot_store(rt_executor* ex, uint64_t id, rt_task* task) {
    // Caller holds either the control lock (growth-adjacent legacy
    // creators) or the task's owner shard lock (steady-state __task_create,
    // Task 6): the segment already exists in both cases (ensure_task_cap /
    // rt_task_table_segment_missing ran first), so this is a pure
    // release-store into a never-moved slot.
    size_t seg_idx = (size_t)(id >> RT_TASK_TABLE_SEGMENT_SHIFT);
    if (seg_idx >= RT_TASK_TABLE_MAX_SEGMENTS) {
        panic_msg("async: task slot out of range");
        return;
    }
    rt_task_segment* segment =
        atomic_load_explicit(&ex->tasks_table.segments[seg_idx], memory_order_acquire);
    if (segment == NULL) {
        panic_msg("async: task slot out of range");
        return;
    }
    size_t slot_idx = (size_t)(id & (RT_TASK_TABLE_SEGMENT_SIZE - 1));
    atomic_store_explicit(&segment->slots[slot_idx], task, memory_order_release);
}

rt_scope* get_scope(rt_executor* ex, uint64_t id) {
    // Lock-free acquire snapshot (Epic 8 Task 9, S5-Q7): segment pointer then
    // slot, both memory_order_acquire, mirroring get_task. A NULL segment or
    // slot means the scope does not exist (never created, or freed by
    // scope_exit and its monotonic id never reused).
    if (ex == NULL || id == 0) {
        return NULL;
    }
    size_t seg_idx = (size_t)(id >> RT_SCOPE_TABLE_SEGMENT_SHIFT);
    if (seg_idx >= RT_SCOPE_TABLE_MAX_SEGMENTS) {
        return NULL;
    }
    rt_scope_segment* segment =
        atomic_load_explicit(&ex->scopes_table.segments[seg_idx], memory_order_acquire);
    if (segment == NULL) {
        return NULL;
    }
    size_t slot_idx = (size_t)(id & (RT_SCOPE_TABLE_SEGMENT_SIZE - 1));
    return atomic_load_explicit(&segment->slots[slot_idx], memory_order_acquire);
}

void rt_scope_slot_store(rt_executor* ex, uint64_t id, rt_scope* scope) {
    // Caller holds the control lock only for the rare segment-growth branch;
    // the steady rt_scope_enter publish and scope_exit clear reach here after
    // ensure_scope_cap / rt_scope_table_segment_missing guaranteed the segment
    // exists, so this is a pure release-store into a never-moved slot.
    size_t seg_idx = (size_t)(id >> RT_SCOPE_TABLE_SEGMENT_SHIFT);
    if (seg_idx >= RT_SCOPE_TABLE_MAX_SEGMENTS) {
        panic_msg("async: scope slot out of range");
        return;
    }
    rt_scope_segment* segment =
        atomic_load_explicit(&ex->scopes_table.segments[seg_idx], memory_order_acquire);
    if (segment == NULL) {
        panic_msg("async: scope slot out of range");
        return;
    }
    size_t slot_idx = (size_t)(id & (RT_SCOPE_TABLE_SEGMENT_SIZE - 1));
    atomic_store_explicit(&segment->slots[slot_idx], scope, memory_order_release);
}

// ensure_task_cap moved to rt_task_table.c (Epic 8 Task 6): the segmented
// table's growth (allocating a segment) is a smaller, self-contained
// operation than the old copy-on-grow table's, so it now lives with the
// rest of the segment-allocation logic rather than here.

// ensure_scope_cap moved to rt_scope_table.c (Epic 8 Task 9): scope table
// growth now allocates a segment, mirroring ensure_task_cap / rt_task_table.c.

void ensure_child_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return;
    }
    if (task->children_cap >= want) {
        return;
    }
    size_t next_cap = task->children_cap == 0 ? 4 : task->children_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = task->children_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)task->children, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: child allocation failed");
        return;
    }
    task->children = next;
    task->children_cap = next_cap;
}

void ensure_scope_child_cap(rt_scope* scope, size_t want) {
    if (scope == NULL) {
        return;
    }
    if (scope->children_cap >= want) {
        return;
    }
    size_t next_cap = scope->children_cap == 0 ? 4 : scope->children_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = scope->children_cap * sizeof(uint64_t);
    size_t new_size = next_cap * sizeof(uint64_t);
    uint64_t* next = (uint64_t*)rt_realloc(
        (uint8_t*)scope->children, (uint64_t)old_size, (uint64_t)new_size, _Alignof(uint64_t));
    if (next == NULL) {
        panic_msg("async: scope child allocation failed");
        return;
    }
    scope->children = next;
    scope->children_cap = next_cap;
}

void clear_select_timers(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || task->select_timers_len == 0) {
        return;
    }
    for (size_t i = 0; i < task->select_timers_len; i++) {
        uint64_t timer_id = task->select_timers[i];
        if (timer_id == 0) {
            continue;
        }
        rt_task* timer = get_task(ex, timer_id);
        if (timer != NULL) {
            cancel_task(ex, timer_id);
            task_release(ex, timer);
        }
        task->select_timers[i] = 0;
    }
    task->select_timers_len = 0;
}

rt_scheduler* current_worker_scheduler(const rt_executor* ex) {
    if (tls_worker_ctx == NULL || tls_worker_ctx->ex != ex) {
        return NULL;
    }
    return tls_worker_ctx->scheduler;
}

static rt_deque* current_local_queue(const rt_executor* ex, rt_scheduler* scheduler) {
    if (scheduler == NULL || scheduler->local_queues == NULL || scheduler->worker_count == 0) {
        return NULL;
    }
    if (current_worker_scheduler(ex) != scheduler || tls_worker_ctx == NULL) {
        return NULL;
    }
    if (tls_worker_ctx->worker_id >= scheduler->worker_count) {
        return NULL;
    }
    return &scheduler->local_queues[tls_worker_ctx->worker_id];
}

static int pop_task_from_deque(rt_executor* ex,
                               rt_deque* dq,
                               int lifo,
                               uint64_t* out_id,
                               rt_trace_sched_source source,
                               uint32_t stealer_shard_id) {
    if (ex == NULL || dq == NULL) {
        return 0;
    }
    while (dq->len > 0) {
        uint64_t id = 0;
        if (lifo) {
            if (!deque_pop_tail(dq, &id)) {
                return 0;
            }
        } else {
            if (!deque_pop_head(dq, &id)) {
                return 0;
            }
        }
        rt_task* task = get_task(ex, id);
        uint8_t status = task_status_load(task);
        if (task == NULL || status == TASK_DONE || status == TASK_RUNNING) {
            if (task != NULL) {
                // Clear stale enqueue flags for discarded entries (e.g., duplicates).
                task_enqueued_store(task, 0);
            }
            continue;
        }
        if (source == RT_TRACE_SCHED_SRC_STEAL &&
            !rt_task_can_steal_from_shard_or_trace_denied(task, stealer_shard_id)) {
            int ok = lifo ? deque_push_tail(dq,
                                            id,
                                            "async: local queue overflow",
                                            "async: local queue allocation failed")
                          : deque_push_head(dq,
                                            id,
                                            "async: local queue overflow",
                                            "async: local queue allocation failed");
            (void)ok;
            return 0;
        }
        task_enqueued_store(task, 0);
        if (task->owner_shard_valid != 0 && task->placement_class == TASK_PLACEMENT_CONNECTION) {
            rt_trace_sched_connection_owner_run(task->owner_shard_id, stealer_shard_id);
        }
        rt_trace_sched_record(source, id);
        if (out_id != NULL) {
            *out_id = id;
        }
        return 1;
    }
    return 0;
}

// Leaf enqueue: caller holds the owner shard's lock and has already
// validated the task (owner == shard, not DONE/RUNNING, not enqueued). The
// wake token for the shard's worker_cv is bumped and signaled under the
// same lock hold, so a sleeping worker cannot miss the push.
int ready_push_task_locked(const rt_executor* ex,
                           rt_shard* owner_shard,
                           rt_task* task,
                           int force_inject,
                           int front,
                           int signal_ready) {
    rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (ex == NULL || task == NULL || scheduler == NULL) {
        return 0;
    }
    // Injection policy:
    // - Worker thread: enqueue locally (LIFO pop) to keep cache locality.
    // - Non-worker thread (main/I/O/external): enqueue on the global injection queue.
    // No last-worker affinity is tracked; wake/spawn follows the current thread.
    rt_deque* local = NULL;
    if (!force_inject) {
        local = current_local_queue(ex, scheduler);
    }
    int signal_ready_now = signal_ready;
    if (local != NULL) {
        // Local queues are popped from the tail, so tail insertion is the local priority path.
        int ok = deque_push_tail(
            local, task->id, "async: local queue overflow", "async: local queue allocation failed");
        if (!ok) {
            return 0;
        }
        // A single local continuation is usually consumed by the current worker on its
        // next scheduler turn; waking another worker often just creates steal/sleep churn.
        signal_ready_now = signal_ready && local->len > 1;
    } else {
        int ok = front ? deque_push_head(&scheduler->inject,
                                         task->id,
                                         "async: inject queue overflow",
                                         "async: inject queue allocation failed")
                       : deque_push_tail(&scheduler->inject,
                                         task->id,
                                         "async: inject queue overflow",
                                         "async: inject queue allocation failed");
        if (!ok) {
            return 0;
        }
    }
    task_enqueued_store(task, 1);
    task_status_store(task, TASK_READY);
    if (signal_ready_now) {
        if (scheduler->wake_pending < UINT32_MAX) {
            scheduler->wake_pending++;
        }
        pthread_cond_signal(&owner_shard->worker_cv);
    }
    return 1;
}

static int ready_push_with_policy(
    rt_executor* ex, uint64_t id, int force_inject, int front, int signal_ready) {
    // Caller holds the control lock and no shard lock; the owner shard lock
    // nests here around the queue mutation (D2 order).
    if (ex == NULL) {
        return 0;
    }
    rt_task* task = get_task(ex, id);
    uint8_t status = task_status_load(task);
    if (task == NULL || status == TASK_DONE) {
        return 0;
    }
    if (status == TASK_RUNNING) {
        return 0;
    }
    if (task_enqueued_load(task) != 0) {
        return 0;
    }
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    if (owner_shard == NULL) {
        return 0;
    }
    rt_shard_lock(owner_shard);
    int pushed = ready_push_task_locked(ex, owner_shard, task, force_inject, front, signal_ready);
    rt_shard_unlock(owner_shard);
    if (!pushed) {
        return 0;
    }
    const rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat_const(ex);
    if (compat != NULL && compat->channel_blocked_workers > 0) {
        maybe_start_compensation_worker_locked(ex);
    }
    return 1;
}

static int ready_push_inner(rt_executor* ex, uint64_t id, int force_inject) {
    return ready_push_with_policy(ex, id, force_inject, 0, 1);
}

void ready_push(rt_executor* ex, uint64_t id) {
    // Caller holds ex->lock.
    (void)ready_push_inner(ex, id, 0);
}

int ready_take_current_local_tail(rt_executor* ex, uint64_t id) {
    // Serialized by the owner shard lock taken below, NOT the control lock:
    // rt_task_poll (the sole caller, rt_async_task.c) reaches here control-free
    // since Epic 8 Task 7. The take here, the worker's own next_ready pop, and
    // any steal all run under this shard's rt_shard_lock (rt_worker_turn.c), so
    // that lock is the local queue's serializer. Intentionally narrow: it only
    // removes the fresh child __task_create just pushed onto the current
    // worker, so the queue is this shard's own and its lock nests here.
    rt_shard* owner_shard = rt_task_owner_shard(ex, get_task(ex, id));
    rt_scheduler* scheduler = rt_shard_scheduler(owner_shard);
    if (owner_shard == NULL) {
        return 0;
    }
    rt_shard_lock(owner_shard);
    rt_deque* local = current_local_queue(ex, scheduler);
    int taken = 0;
    if (local != NULL && local->len > 0 && local->buf != NULL) {
        size_t idx = local->head + local->len - 1;
        if (local->buf[idx] == id) {
            local->len--;
            if (local->len == 0) {
                local->head = 0;
            }
            taken = 1;
        }
    }
    rt_shard_unlock(owner_shard);
    return taken;
}

static int ready_push_yielded_task(rt_executor* ex, uint64_t id) {
    // A yielding worker immediately re-enters the scheduler loop, so waking another
    // worker here mostly creates condvar churn for task-to-task handoffs.
    return ready_push_with_policy(ex, id, 1, 0, 0);
}

int ready_pop(rt_executor* ex, uint64_t* out_id) {
    // Control-lane single-runner pop (N=1 runner and io drain): the queue is
    // shard 0 state, so the pop nests shard 0's lock under the control lock.
    rt_shard* shard0 = rt_runtime_shard0(rt_executor_runtime(ex));
    rt_scheduler* scheduler = rt_shard_scheduler(shard0);
    if (shard0 == NULL || scheduler == NULL) {
        return 0;
    }
    rt_shard_lock(shard0);
    int popped =
        pop_task_from_deque(ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, 0);
    rt_shard_unlock(shard0);
    return popped;
}

int worker_next_ready(rt_worker_ctx* ctx, uint64_t* out_id) {
    rt_executor* ex = ctx != NULL ? ctx->ex : NULL;
    rt_scheduler* scheduler = ctx != NULL ? ctx->scheduler : NULL;
    uint32_t worker_id = ctx != NULL ? ctx->worker_id : 0;
    uint32_t shard_id = ctx != NULL ? ctx->shard_id : 0;
    if (ex == NULL || scheduler == NULL) {
        return 0;
    }
    if (scheduler->sched_mode == SCHED_SEEDED) {
        rt_deque* local = scheduler->local_queues != NULL && worker_id < scheduler->worker_count
                              ? &scheduler->local_queues[worker_id]
                              : NULL;
        int local_has = local != NULL && local->len > 0;
        int inject_has = scheduler->inject.len > 0;
        int others_have = 0;
        if (scheduler->local_queues != NULL && scheduler->worker_count > 1) {
            for (uint32_t i = 0; i < scheduler->worker_count; i++) {
                if (i == worker_id) {
                    continue;
                }
                if (scheduler->local_queues[i].len > 0) {
                    others_have = 1;
                    break;
                }
            }
        }
        if (local_has && inject_has) {
            if ((sched_next_u64(ctx) & 1U) == 0U) {
                if (pop_task_from_deque(ex, local, 1, out_id, RT_TRACE_SCHED_SRC_LOCAL, shard_id)) {
                    return 1;
                }
                if (pop_task_from_deque(
                        ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
                    return 1;
                }
            } else {
                if (pop_task_from_deque(
                        ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
                    return 1;
                }
                if (pop_task_from_deque(ex, local, 1, out_id, RT_TRACE_SCHED_SRC_LOCAL, shard_id)) {
                    return 1;
                }
            }
        } else if (local_has) {
            if (pop_task_from_deque(ex, local, 1, out_id, RT_TRACE_SCHED_SRC_LOCAL, shard_id)) {
                return 1;
            }
        } else if (inject_has) {
            if (others_have && (sched_next_u64(ctx) & 1U) != 0U) {
                if (scheduler->worker_count > 1) {
                    uint32_t span = scheduler->worker_count - 1;
                    uint32_t start = (worker_id + 1 + (uint32_t)(sched_next_u64(ctx) % span)) %
                                     scheduler->worker_count;
                    for (uint32_t offset = 0; offset < span; offset++) {
                        uint32_t victim = start + offset;
                        if (victim >= scheduler->worker_count) {
                            victim -= scheduler->worker_count;
                        }
                        if (victim == worker_id) {
                            continue;
                        }
                        if (pop_task_from_deque(ex,
                                                &scheduler->local_queues[victim],
                                                0,
                                                out_id,
                                                RT_TRACE_SCHED_SRC_STEAL,
                                                shard_id)) {
                            return 1;
                        }
                    }
                }
            }
            if (pop_task_from_deque(
                    ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
                return 1;
            }
        }
        if (scheduler->local_queues == NULL || scheduler->worker_count <= 1) {
            return 0;
        }
        uint32_t span = scheduler->worker_count - 1;
        uint32_t start =
            (worker_id + 1 + (uint32_t)(sched_next_u64(ctx) % span)) % scheduler->worker_count;
        for (uint32_t offset = 0; offset < span; offset++) {
            uint32_t victim = start + offset;
            if (victim >= scheduler->worker_count) {
                victim -= scheduler->worker_count;
            }
            if (victim == worker_id) {
                continue;
            }
            if (pop_task_from_deque(ex,
                                    &scheduler->local_queues[victim],
                                    0,
                                    out_id,
                                    RT_TRACE_SCHED_SRC_STEAL,
                                    shard_id)) {
                return 1;
            }
        }
        return 0;
    }
    if (ctx != NULL && ++ctx->pop_tick % 61U == 0U &&
        pop_task_from_deque(
            ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
        return 1;
    }
    if (scheduler->local_queues != NULL && worker_id < scheduler->worker_count) {
        if (pop_task_from_deque(ex,
                                &scheduler->local_queues[worker_id],
                                1,
                                out_id,
                                RT_TRACE_SCHED_SRC_LOCAL,
                                shard_id)) {
            return 1;
        }
    }
    if (pop_task_from_deque(
            ex, &scheduler->inject, 0, out_id, RT_TRACE_SCHED_SRC_INJECT, shard_id)) {
        return 1;
    }
    if (scheduler->local_queues == NULL || scheduler->worker_count <= 1) {
        return 0;
    }
    for (uint32_t offset = 1; offset < scheduler->worker_count; offset++) {
        uint32_t victim = (worker_id + offset) % scheduler->worker_count;
        if (victim == worker_id) {
            continue;
        }
        if (pop_task_from_deque(ex,
                                &scheduler->local_queues[victim],
                                0,
                                out_id,
                                RT_TRACE_SCHED_SRC_STEAL,
                                shard_id)) {
            return 1;
        }
    }
    return 0;
}

// Yield tick (D7): advance the clock, fire the ticking shard's own due
// sleepers inline, and hand other shards a wake token when their atomic
// min-deadline mirror says they have due work — the owner pops its own
// store on its next scheduler turn.
void tick_virtual(rt_executor* ex) {
    if (ex == NULL) {
        return;
    }
    uint64_t now = rt_clock_tick(ex);
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* own = tls_worker_ctx != NULL && tls_worker_ctx->ex == ex ? tls_worker_ctx->shard
                                                                       : rt_runtime_shard0(runtime);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL || rt_sleep_store_min(&shard->sleep_store) > now) {
            continue;
        }
        if (shard == own) {
            (void)rt_sleep_fire_due_on_shard(ex, shard, now);
        } else {
            rt_sched_wake_signal_shard_n(shard, 1);
        }
    }
}

int rt_next_sleep_deadline(const rt_executor* ex, uint64_t* out_deadline) {
    const rt_runtime* runtime = ex != NULL ? ex->runtime : NULL;
    size_t shard_count = rt_runtime_shard_count(runtime);
    uint64_t next_deadline = UINT64_MAX;
    for (size_t i = 0; i < shard_count; i++) {
        const rt_shard* shard = rt_runtime_shard_const(runtime, i);
        uint64_t min = shard != NULL ? rt_sleep_store_min(&shard->sleep_store) : UINT64_MAX;
        if (min < next_deadline) {
            next_deadline = min;
        }
    }
    if (next_deadline == UINT64_MAX) {
        return 0;
    }
    if (out_deadline != NULL) {
        *out_deadline = next_deadline;
    }
    return 1;
}

int advance_time_to_next_timer(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t next_deadline = 0;
    if (!rt_next_sleep_deadline(ex, &next_deadline)) {
        return 0;
    }
    (void)rt_clock_advance_to(ex, next_deadline);
    uint64_t now = rt_clock_now(ex);
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        (void)rt_sleep_fire_due_on_shard(ex, rt_runtime_shard(runtime, i), now);
    }
    return 1;
}

int next_ready(rt_executor* ex, uint64_t* out_id) {
    if (ex == NULL) {
        return 0;
    }
    while (!ready_pop(ex, out_id)) {
        if (rt_net_begin_poll_on_shard(ex, 0) && rt_net_poll_waiters_owned_on_shard(ex, 0, 0)) {
            continue;
        }
        uint64_t next_deadline = 0;
        int have_timer = rt_next_sleep_deadline(ex, &next_deadline);
        if (have_timer) {
            if (rt_net_has_waiters_on_shard(ex, 0)) {
                uint64_t now = ex->now_ms;
                uint64_t diff = next_deadline > now ? next_deadline - now : 0;
                int timeout_ms = diff > (uint64_t)INT_MAX ? INT_MAX : (int)diff;
                if (timeout_ms > 0) {
                    if (rt_net_begin_poll_on_shard(ex, 0) &&
                        rt_net_poll_waiters_owned_on_shard(ex, 0, timeout_ms)) {
                        continue;
                    }
                    const rt_shard* shard = rt_runtime_shard_const(rt_executor_runtime(ex), 0);
                    if (shard != NULL && shard->net_polling && !ex->shutdown) {
                        rt_io_wait_slice(ex);
                        continue;
                    }
                }
                if (advance_time_to_next_timer(ex)) {
                    continue;
                }
            } else if (advance_time_to_next_timer(ex)) {
                continue;
            }
        } else {
            if (rt_net_begin_poll_on_shard(ex, 0) &&
                rt_net_poll_waiters_owned_on_shard(ex, 0, -1)) {
                continue;
            }
            const rt_shard* shard = rt_runtime_shard_const(rt_executor_runtime(ex), 0);
            if (shard != NULL && shard->net_polling && rt_net_has_waiters_on_shard(ex, 0) &&
                !ex->shutdown) {
                rt_io_wait_slice(ex);
                continue;
            }
            return 0;
        }
    }
    return 1;
}

rt_task* task_from_handle(void* handle) {
    if (handle == NULL) {
        panic_msg("invalid task handle");
        return NULL;
    }
    return (rt_task*)handle;
}

uint64_t task_id_from_handle(void* handle) {
    const rt_task* task = task_from_handle(handle);
    if (task == NULL) {
        return 0;
    }
    return task->id;
}

void task_add_child(rt_task* parent, uint64_t child_id) {
    if (parent == NULL || child_id == 0) {
        return;
    }
    ensure_child_cap(parent, parent->children_len + 1);
    parent->children[parent->children_len++] = child_id;
}

void scope_add_child(rt_scope* scope, uint64_t child_id) {
    if (scope == NULL || child_id == 0) {
        return;
    }
    ensure_scope_child_cap(scope, scope->children_len + 1);
    scope->children[scope->children_len++] = child_id;
}

int scope_remove_child(rt_scope* scope, uint64_t child_id) {
    if (scope == NULL || child_id == 0 || scope->children_len == 0) {
        return 0;
    }
    for (size_t i = 0; i < scope->children_len; i++) {
        if (scope->children[i] != child_id) {
            continue;
        }
        size_t last = scope->children_len - 1;
        if (i != last) {
            scope->children[i] = scope->children[last];
        }
        scope->children[last] = 0;
        scope->children_len--;
        return 1;
    }
    return 0;
}

// scope_cancel_children_locked / scope_child_done_locked moved to
// rt_async_scope.c (Epic 8 Task 9): completion-side scope bookkeeping now runs
// on the scope owner shard lane (scope_on_child_done) with a counted control
// fallback for the rare cancel/failfast walk, replacing the old control-lane
// helpers.

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

int current_task_cancelled(rt_executor* ex) {
    (void)ex;
    const rt_task* task = rt_current_task();
    return task != NULL && task_cancelled_load(task) != 0;
}

void cancel_task(rt_executor* ex, uint64_t id) {
    if (ex == NULL || id == 0) {
        return;
    }
    rt_task* task = get_task(ex, id);
    if (task == NULL || task_status_load(task) == TASK_DONE) {
        return;
    }
    if (task_cancelled_load(task) != 0) {
        return;
    }
    task_cancelled_store(task, 1);
    if (task->kind == TASK_KIND_BLOCKING) {
        rt_blocking_request_cancel(ex, task);
    }
    if (task_status_load(task) == TASK_WAITING) {
        wake_task(ex, task->id, 1);
    }
    // Since Epic 8 Task 6, task_add_child appends into this task's
    // children[] under the task's own owner shard lock, not control (the
    // steady-state __task_create path takes no control lock at all). This
    // control-held walk can no longer read task->children[]/children_len
    // directly - a concurrent append on a RUNNING task (this walk's caller
    // already holds control, but the appending thread does not) could
    // realloc the array mid-read. Collect-then-recurse (mirrors
    // wake_key_all's collect-then-wake and
    // rt_executor_wake_net_waiters_for_key_on_owner's inline-batch pattern):
    // snapshot the ids under the task's owner shard lock, release it, then
    // recurse. Legal lane order (control held, then at most one shard lock,
    // released before any further lock); never two shard locks, since each
    // recursion level locks/copies/unlocks before descending.
    //
    // Locking THIS task's CURRENT owner shard (not whatever shard protected
    // some earlier append) is sufficient even if this task's own owner
    // changed since an earlier append: every append and every self-replace
    // of this task's own owner_shard_id happen on this task's own executing
    // thread (see the invariant at __task_create's matching lock site,
    // rt_async_task.c), so program order plus the same-thread "lock
    // handoff" (a release sequenced-before a later acquire of a different
    // lock, both by the same thread, transitively carries prior writes
    // forward) publishes every earlier append to whoever locks the current
    // owner shard. This depends on that invariant staying true - re-check
    // it before adding any new rt_task_replace_owner call site.
    rt_shard* owner_shard = rt_task_owner_shard(ex, task);
    rt_shard_lock(owner_shard);
    size_t child_count = task->children_len;
    uint64_t inline_children[8];
    uint64_t* children = inline_children;
    if (child_count > 8) {
        children = (uint64_t*)rt_alloc(child_count * sizeof(uint64_t), _Alignof(uint64_t));
        if (children == NULL) {
            rt_shard_unlock(owner_shard);
            panic_msg("async: cancel snapshot allocation failed");
            return;
        }
    }
    if (child_count > 0) {
        memcpy(children, task->children, child_count * sizeof(uint64_t));
    }
    rt_shard_unlock(owner_shard);
    for (size_t i = 0; i < child_count; i++) {
        cancel_task(ex, children[i]);
    }
    if (children != inline_children) {
        rt_free((uint8_t*)children, child_count * sizeof(uint64_t), _Alignof(uint64_t));
    }
}

// Lane-aware completion (peel B1b): a control-lane caller runs the whole
// body as before; a worker (control-free) takes the control lock only when
// the exit actually owns control-lane work. The pure shard-local exit — no
// residual multi-key registrations, no scope, no main awaiter, refs held —
// never touches the control lane.
//
// park_needs_control is derived by mark_done from the park_key it snapshotted
// under the task's own owner shard lock (RV2-DEBT-019 race 2): reading park_key
// here unlocked raced wake_task_on_shard_locked's write. Only a net park_key
// still forces control here (cross-shard registry removal). S6-Q1 is now
// complete: the WAKER_JOIN reason went in Task 8, and the scope reason
// (parent_scope_id/scope_registered) and the WAKER_SCOPE park_key reason are
// gone in Task 9 - scope completion bookkeeping runs on the scope owner shard
// lane (scope_on_child_done) and the scope_key waiter store moved to the scope
// owner shard, so both are owner-local. mark_done_needs_control's final form is
// net-key + done_waiters (plus the select/multi-key compat residual).
// completion_reason_out reports whether a genuine completion reason (net key /
// wait_keys / select timers) forced control, as the exact complement of the
// done_waiters-only case: the AWAIT_COMPAT tag in mark_done keys off it, so the
// tag split cannot drift from the control decision (they share this one
// evaluation; Epic 8 Task 10 review finding).
static int mark_done_needs_control(const rt_executor* ex,
                                   const rt_task* task,
                                   int park_needs_control,
                                   int* completion_reason_out) {
    int completion_reason =
        task->wait_keys_len > 0 || task->select_timers_len > 0 || park_needs_control;
    if (completion_reason_out != NULL) {
        *completion_reason_out = completion_reason;
    }
    if (completion_reason) {
        return 1;
    }
    if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) > 0) {
        return 1;
    }
    return 0;
}

void mark_done(rt_executor* ex, rt_task* task, uint8_t result_kind, uint64_t result_bits) {
    if (ex == NULL || task == NULL) {
        return;
    }
    // Pin the task across completion: a joiner woken mid-body may consume
    // the result and drop the last handle on another shard before the body
    // finishes touching the task.
    task_add_ref(task);
    // The task is RUNNING here (just polled to completion, TASK_DONE is not
    // stored until below), and the wake path only reads or clears a task's
    // park_key while that task is parked (WAITING, not enqueued) under its
    // owner shard lock (wake_task_on_shard_locked's WAITING gate). So no other
    // thread writes this RUNNING task's park_key, and this plain read/clear is
    // race-free without a lock (RV2-DEBT-019: the wake-side gate is the
    // load-bearing half; the old unlocked read here raced only because the
    // wake path used to clear park_key on RUNNING tasks too).
    waker_key park = task->park_key;
    // A net park_key needs the cross-shard registry removal. Join keys (Task 7)
    // and scope keys (Task 9, scope_key store moved to the scope owner shard)
    // are owner-local, so they are removed control-free below (S6-Q1 complete).
    int park_needs_control = waker_valid(park) && waker_is_net(park);
    // Attribute honestly (Epic 8 Task 10, rule 5): a completion forced onto
    // the control lane SOLELY because a non-worker awaiter is parked on
    // done_cv (done_waiters>0, with no net park_key and no residual
    // multi-key work) is external-await compatibility, counted separately
    // from worker steady-state completion. Net-key / wait_keys / select-
    // timer removals are genuine completion control work and stay COMPLETION.
    // completion_reason comes out of the same evaluation that decides
    // need_control, so the tag split cannot drift from the control decision.
    int completion_reason = 0;
    int need_control = !rt_lane_holds_control() &&
                       mark_done_needs_control(ex, task, park_needs_control, &completion_reason);
    if (need_control) {
        rt_control_lock(ex);
        if (completion_reason) {
            rt_trace_control_lock_site(RT_CTRL_SITE_COMPLETION);
        } else {
            rt_trace_control_lock_site(RT_CTRL_SITE_AWAIT_COMPAT);
        }
    }
    if (task->wait_keys_len > 0) {
        clear_wait_keys(ex, task);
    }
    if (task->select_timers_len > 0) {
        clear_select_timers(ex, task);
    }
    if (waker_valid(park)) {
        remove_waiter(ex, park, task->id);
    }
    task->park_key = waker_none();
    task->park_prepared = 0;
    if (task->kind == TASK_KIND_SLEEP && task->sleep_armed) {
        // Cancelled sleepers leave the deadline index here; fired sleepers
        // were already popped, so the remove is a no-op for them.
        rt_shard* sleep_shard = rt_task_owner_shard(ex, task);
        rt_shard_lock(sleep_shard);
        (void)rt_sleep_store_remove(&sleep_shard->sleep_store, task->id);
        rt_shard_unlock(sleep_shard);
        task->sleep_armed = 0;
    }
    // Epic 8 Task 7 enabling change (rule 1): the result fields must be
    // written before the TASK_DONE release store, so a joiner's acquire-load
    // of TASK_DONE (rt_task_poll, now control-free) publishes them without
    // needing the control lock. Nothing else in this function reads either
    // field, so the reorder is behavior-preserving.
    task->result_kind = result_kind;
    task->result_bits = result_bits;
    task_status_store(task, TASK_DONE);
    task_enqueued_store(task, 0);
    task->state = NULL;
    // Scope completion bookkeeping (Epic 8 Task 9, S5-Q8): runs on the scope
    // owner shard lane. The steady same-owner non-failfast child-done is
    // control-free under the pinned shard lock; only a cross-owner (re-placed
    // child) or a failfast-triggering completion takes the counted control
    // fallback inside scope_on_child_done, so it no longer forces
    // mark_done_needs_control on the request hot path.
    scope_on_child_done(ex, task, result_kind);
    wake_key_all_with_policy(ex, join_key(task->id), 0);
    if (atomic_load_explicit(&ex->done_waiters, memory_order_acquire) > 0) {
        pthread_cond_broadcast(&ex->done_cv);
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
    // Drop the completion pin; the release frees under the control lane when
    // this was the last reference.
    task_release_lane_aware(ex, task);
}

void apply_poll_outcome(rt_executor* ex, rt_task* task, poll_outcome outcome) {
    if (ex == NULL || task == NULL) {
        return;
    }
    switch (outcome.kind) {
        case POLL_DONE_SUCCESS:
            mark_done(ex, task, TASK_RESULT_SUCCESS, outcome.value_bits);
            break;
        case POLL_DONE_CANCELLED:
            if (task->scope_id != 0) {
                // Owner-cancelled scope teardown (S5-Q14, Epic 8 Task 9): the
                // child cancel walk needs the control lane (re-derivation:
                // cancel_task reads sibling owner_shard_ids that F2 self-replace
                // writes under control), so this rare branch takes control. But
                // same-owner child-done now runs control-free on the pinned
                // shard lane, so control no longer excludes it - the re-park on
                // scope_key uses register-then-verify (scope_key routes to the
                // pinned store) to avoid losing a child-done wake.
                int need_control = !rt_lane_holds_control();
                if (need_control) {
                    rt_control_lock(ex);
                    rt_trace_control_lock_site(RT_CTRL_SITE_SCOPE);
                }
                rt_scope* scope = get_scope(ex, task->scope_id);
                rt_shard* pinned = scope != NULL ? rt_scope_owner_shard(ex, scope) : NULL;
                size_t active = 0;
                if (scope != NULL) {
                    rt_shard_lock(pinned);
                    active = scope->active_children;
                    rt_shard_unlock(pinned);
                }
                if (scope != NULL && active > 0) {
                    task->cancel_pending = 1;
                    scope_cancel_children_controlled(ex, scope);
                    task->state = outcome.state;
                    waker_key key = scope_key(scope->id);
                    prepare_park(ex, task, key, 0);
                    rt_shard_lock(pinned);
                    size_t active_after = scope->active_children;
                    rt_shard_unlock(pinned);
                    if (active_after != 0) {
                        park_current(ex, key);
                        if (need_control) {
                            rt_control_unlock(ex);
                        }
                        break;
                    }
                    // All children drained during the walk/registration: undo
                    // the park and fall through to exit + mark_done.
                    remove_waiter(ex, key, task->id);
                    task->park_prepared = 0;
                    task->park_key = waker_none();
                    pending_key = waker_none();
                }
                if (scope != NULL) {
                    rt_shard_lock(pinned);
                    scope_exit_locked(ex, scope);
                    rt_shard_unlock(pinned);
                }
                if (need_control) {
                    rt_control_unlock(ex);
                }
            }
            mark_done(ex, task, TASK_RESULT_CANCELLED, 0);
            break;
        case POLL_YIELDED:
            task->state = outcome.state;
            task_status_store(task, TASK_READY);
            // Yielded tasks go through the inject queue to avoid local LIFO starvation.
            (void)ready_push_yielded_task(ex, task->id);
            tick_virtual(ex);
            break;
        case POLL_PARKED:
            task->state = outcome.state;
            park_current(ex, outcome.park_key);
            break;
        default:
            panic_msg("async: unknown poll outcome");
            break;
    }
}
