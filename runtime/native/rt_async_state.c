#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"
#include "rt_far_channel.h"
#include "rt_remote_task.h"

#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
// Like the other executor locks and condition variables, this singleton lives
// until process exit. rt_executor_request_shutdown quiesces it; it is not a
// general reusable executor destructor.
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
    uint32_t threads = config.legacy_worker_threads;
    uint32_t total_worker_threads = rt_config_total_worker_threads(&config);
    rt_sched_trace_init(total_worker_threads);
    uint32_t blocking_threads = config.blocking_threads;
    ex->blocking_count = blocking_threads;
    rt_heap_accounting* accounting = rt_executor_heap_accounting(ex);
    if (rt_heap_accounting_prepare_cells(accounting, total_worker_threads, blocking_threads) !=
        RT_HEAP_ACCOUNTING_OK) {
        rt_runtime_destroy_global();
        panic_msg("async: heap accounting cell allocation failed");
    }
    rt_heap_accounting_set_current_cell(rt_heap_accounting_main_cell(accounting));
    if (rt_remote_task_state_init(ex) != RT_RUNTIME_STATUS_OK) {
        rt_runtime_destroy_global();
        panic_msg("async: remote task state initialization failed");
    }
    if (rt_far_channel_state_init(ex) != RT_RUNTIME_STATUS_OK) {
        (void)rt_remote_task_state_destroy(ex);
        rt_runtime_destroy_global();
        panic_msg("async: far channel state initialization failed");
    }
    rt_runtime_status scheduler_status = rt_runtime_init_shard_schedulers(rt_executor_runtime(ex),
                                                                          config.shard_worker_count,
                                                                          rt_env_sched_mode(),
                                                                          rt_env_sched_seed());
    if (scheduler_status == RT_RUNTIME_STATUS_ALLOCATION_FAILED) {
        rt_far_channel_release_all(ex);
        (void)rt_far_channel_state_destroy(ex);
        (void)rt_remote_task_state_destroy(ex);
        rt_runtime_destroy_global();
        panic_msg("async: local queue allocation failed");
    }
    if (scheduler_status != RT_RUNTIME_STATUS_OK) {
        rt_far_channel_release_all(ex);
        (void)rt_far_channel_state_destroy(ex);
        (void)rt_remote_task_state_destroy(ex);
        rt_runtime_destroy_global();
        panic_msg("async: scheduler initialization failed");
    }
    channel_wake_force_inject = rt_env_channel_wake_force_inject();
    (void)rt_monotonic_now(); // start the wall measurement with the executor
    // The blocking pool must exist before any worker can reach an idle-park
    // edge: the park-edge deadlock scan locks blocking_lock, so the pool's
    // primitives have to be initialized before the worker threads start
    // (pthread_create publishes the initialization).
    rt_blocking_init(ex);
    if (threads > 1 || rt_runtime_shard_count(rt_executor_runtime(ex)) > 1) {
        rt_start_workers(ex);
    }
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
    // ): the segment already exists in both cases (ensure_task_cap /
    // rt_task_table_segment_missing ran first), so this is a pure
    // release-store into a never-moved slot.
    // The two failures below are different defects and must not share a
    // sentence: an id past the directory means the id itself is wrong (a
    // garbage or overflowed id), while a NULL segment means the id is
    // plausible but no creator allocated its segment first. One string for
    // both leaves a field report unable to say which happened.
    size_t seg_idx = (size_t)(id >> RT_TASK_TABLE_SEGMENT_SHIFT);
    char panic_buf[192];
    if (seg_idx >= RT_TASK_TABLE_MAX_SEGMENTS) {
        (void)snprintf(panic_buf,
                       sizeof(panic_buf),
                       "async: task slot id past the table (task=%llu segment=%llu "
                       "max_segments=%llu worker=%d)",
                       (unsigned long long)id,
                       (unsigned long long)seg_idx,
                       (unsigned long long)RT_TASK_TABLE_MAX_SEGMENTS,
                       tls_worker_id);
        panic_msg(panic_buf);
        return;
    }
    rt_task_segment* segment =
        atomic_load_explicit(&ex->tasks_table.segments[seg_idx], memory_order_acquire);
    if (segment == NULL) {
        (void)snprintf(panic_buf,
                       sizeof(panic_buf),
                       "async: task slot segment not allocated (task=%llu segment=%llu "
                       "slot=%llu worker=%d)",
                       (unsigned long long)id,
                       (unsigned long long)seg_idx,
                       (unsigned long long)(id & (RT_TASK_TABLE_SEGMENT_SIZE - 1)),
                       tls_worker_id);
        panic_msg(panic_buf);
        return;
    }
    size_t slot_idx = (size_t)(id & (RT_TASK_TABLE_SEGMENT_SIZE - 1));
    atomic_store_explicit(&segment->slots[slot_idx], task, memory_order_release);
}

rt_scope* get_scope(rt_executor* ex, uint64_t id) {
    // Lock-free acquire snapshot (S5-Q7): segment pointer then
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
    // Distinct sentences for the same reason as rt_task_slot_store above: a
    // scope id past the directory is a wrong id, a NULL segment is a missing
    // allocation, and only the message can tell the two apart after the fact.
    size_t seg_idx = (size_t)(id >> RT_SCOPE_TABLE_SEGMENT_SHIFT);
    char panic_buf[192];
    if (seg_idx >= RT_SCOPE_TABLE_MAX_SEGMENTS) {
        (void)snprintf(panic_buf,
                       sizeof(panic_buf),
                       "async: scope slot id past the table (scope=%llu segment=%llu "
                       "max_segments=%llu worker=%d)",
                       (unsigned long long)id,
                       (unsigned long long)seg_idx,
                       (unsigned long long)RT_SCOPE_TABLE_MAX_SEGMENTS,
                       tls_worker_id);
        panic_msg(panic_buf);
        return;
    }
    rt_scope_segment* segment =
        atomic_load_explicit(&ex->scopes_table.segments[seg_idx], memory_order_acquire);
    if (segment == NULL) {
        (void)snprintf(panic_buf,
                       sizeof(panic_buf),
                       "async: scope slot segment not allocated (scope=%llu segment=%llu "
                       "slot=%llu worker=%d)",
                       (unsigned long long)id,
                       (unsigned long long)seg_idx,
                       (unsigned long long)(id & (RT_SCOPE_TABLE_SEGMENT_SIZE - 1)),
                       tls_worker_id);
        panic_msg(panic_buf);
        return;
    }
    size_t slot_idx = (size_t)(id & (RT_SCOPE_TABLE_SEGMENT_SIZE - 1));
    atomic_store_explicit(&segment->slots[slot_idx], scope, memory_order_release);
}

// ensure_task_cap moved to rt_task_table.c: the segmented
// table's growth (allocating a segment) is a smaller, self-contained
// operation than the old copy-on-grow table's, so it now lives with the
// rest of the segment-allocation logic rather than here.

// ensure_scope_cap moved to rt_scope_table.c: scope table
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

// clear_select_timers moved to rt_task_complete.c: select
// timer teardown is completion/cancel-side cleanup, so it lives with
// mark_done and cancel_task.

// Ready-queue cluster (scheduler_runnable_is_empty,
// rt_sched_idle_sample_locked, sched_next_u64, current_worker_scheduler,
// current_local_queue, pop_task_from_deque, ready_push*, ready_pop,
// worker_next_ready) moved to rt_ready_queue.c: shard
// ready-queue mutation and the worker pop policy are one owner surface.
// Yield tick (D7): tick_virtual, rt_next_sleep_deadline, and
// advance_time_to_next_timer moved to rt_virtual_clock.c: the shard-sleep-store
// clock advance is one owner surface, separate from next_ready's poll/park
// scheduling below.

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
// rt_async_scope.c: completion-side scope bookkeeping now runs
// on the scope owner shard lane (scope_on_child_done) with a counted control
// fallback for the rare cancel/failfast walk, replacing the old control-lane
// helpers.

// Handle-lifetime cluster (task_add_ref, free_task, task_release,
// task_release_lane_aware) moved to rt_task_lifetime.c:
// the task handle refcount and control-lane free are one owner surface.

// Completion/cancel cluster (current_task_cancelled, cancel_task,
// mark_done_needs_control, mark_done, apply_poll_outcome) moved to
// rt_task_complete.c: terminal task transitions are one
// owner surface.
