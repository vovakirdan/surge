#include "rt_async_internal.h"
#include "rt_net_trace.h"

#include <signal.h>
#include <stdlib.h>
#include <unistd.h>

static volatile sig_atomic_t trace_exec_enabled_flag = 0;
static volatile sig_atomic_t trace_sched_enabled_flag = 0;
static _Atomic uint64_t trace_wake_called_total;
static _Atomic uint64_t trace_wake_enqueued_total;
static _Atomic uint64_t trace_wake_ignored_completed_total;
static _Atomic uint64_t trace_park_attempt_total;
static _Atomic uint64_t trace_park_committed_total;
static _Atomic uint64_t trace_worker_sleep_total;
static _Atomic uint64_t trace_worker_wake_total;
static _Atomic uint64_t trace_parked_with_work_total;
static _Atomic uint64_t trace_channel_blocking_wait_total;
static _Atomic uint64_t trace_channel_task_blocking_send_total;
static _Atomic uint64_t trace_channel_task_blocking_recv_total;
static _Atomic uint64_t trace_channel_handoff_yield_total;
static _Atomic uint64_t trace_compensation_started_total;
static uint64_t trace_sched_hash;
static uint64_t trace_sched_events;
static uint64_t trace_sched_local_pops;
static uint64_t trace_sched_inject_pops;
static uint64_t trace_sched_steal_pops;
static _Atomic uint64_t trace_sched_tier1_steal_denied_total;
static _Atomic uint64_t trace_sched_conn_owner_placed_total;
static _Atomic uint64_t trace_sched_conn_owner_local_total;
static _Atomic uint64_t trace_sched_conn_owner_mismatch_total;
static _Atomic sig_atomic_t trace_dump_requested_flag;

int rt_exec_trace_enabled(void) {
    return trace_exec_enabled_flag != 0;
}

static int trace_sched_enabled(void) {
    return trace_sched_enabled_flag != 0;
}

static void trace_inc_atomic(_Atomic uint64_t* counter) {
    if (!rt_exec_trace_enabled() || counter == NULL) {
        return;
    }
    (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
}

void rt_trace_wake_called(void) {
    trace_inc_atomic(&trace_wake_called_total);
}

void rt_trace_wake_enqueued(void) {
    trace_inc_atomic(&trace_wake_enqueued_total);
}

void rt_trace_wake_ignored_completed(void) {
    trace_inc_atomic(&trace_wake_ignored_completed_total);
}

void rt_trace_park_attempt(void) {
    trace_inc_atomic(&trace_park_attempt_total);
}

void rt_trace_park_committed(void) {
    trace_inc_atomic(&trace_park_committed_total);
}

void rt_trace_worker_sleep(void) {
    trace_inc_atomic(&trace_worker_sleep_total);
}

void rt_trace_worker_wake(void) {
    trace_inc_atomic(&trace_worker_wake_total);
}

void rt_trace_parked_with_work(void) {
    trace_inc_atomic(&trace_parked_with_work_total);
}

void rt_trace_channel_blocking_wait(void) {
    trace_inc_atomic(&trace_channel_blocking_wait_total);
}

void rt_trace_channel_task_blocking_send(void) {
    trace_inc_atomic(&trace_channel_task_blocking_send_total);
}

void rt_trace_channel_task_blocking_recv(void) {
    trace_inc_atomic(&trace_channel_task_blocking_recv_total);
}

void rt_trace_channel_handoff_yield(void) {
    trace_inc_atomic(&trace_channel_handoff_yield_total);
}

void rt_trace_compensation_started(void) {
    trace_inc_atomic(&trace_compensation_started_total);
}

static size_t trace_append_literal(char* buf, size_t pos, size_t cap, const char* lit) {
    if (buf == NULL || lit == NULL) {
        return pos;
    }
    for (size_t i = 0; lit[i] != '\0' && pos + 1 < cap; i++) {
        buf[pos++] = lit[i];
    }
    return pos;
}

static size_t trace_append_u64(char* buf, size_t pos, size_t cap, uint64_t value) {
    char tmp[32];
    size_t len = 0;
    do {
        tmp[len++] = (char)('0' + (value % 10));
        value /= 10;
    } while (value > 0 && len < sizeof(tmp));
    for (size_t i = 0; i < len && pos + 1 < cap; i++) {
        buf[pos++] = tmp[len - 1 - i];
    }
    return pos;
}

static void
trace_append_kv_u64(char* buf, size_t* pos, size_t cap, const char* name, uint64_t value) {
    if (buf == NULL || pos == NULL || name == NULL) {
        return;
    }
    *pos = trace_append_literal(buf, *pos, cap, " ");
    *pos = trace_append_literal(buf, *pos, cap, name);
    *pos = trace_append_literal(buf, *pos, cap, "=");
    *pos = trace_append_u64(buf, *pos, cap, value);
}

static void trace_collect_scheduler_snapshot(const rt_runtime* runtime,
                                             uint64_t* worker_count,
                                             uint64_t* running,
                                             uint64_t* inject_len,
                                             uint64_t* local_total,
                                             uint64_t* local_max) {
    if (runtime == NULL || runtime->shard_count < 1 ||
        runtime->shard_count > RT_RUNTIME_MAX_SHARDS) {
        return;
    }
    for (size_t i = 0; i < runtime->shard_count; i++) {
        const rt_scheduler* scheduler = rt_shard_scheduler_const(&runtime->shards[i]);
        if (scheduler == NULL) {
            continue;
        }
        *worker_count += scheduler->worker_count;
        *running += scheduler->running_count;
        *inject_len += scheduler->inject.len;
        if (scheduler->local_queues == NULL) {
            continue;
        }
        for (uint32_t worker = 0; worker < scheduler->worker_count; worker++) {
            uint64_t len = (uint64_t)scheduler->local_queues[worker].len;
            *local_total += len;
            if (len > *local_max) {
                *local_max = len;
            }
        }
    }
}

static void trace_exec_dump(const char* reason) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    char buf[768];
    size_t pos = 0;
    pos = trace_append_literal(buf, pos, sizeof(buf), "TRACE_EXEC ");
    if (reason != NULL) {
        pos = trace_append_literal(buf, pos, sizeof(buf), "reason=");
        pos = trace_append_literal(buf, pos, sizeof(buf), reason);
        pos = trace_append_literal(buf, pos, sizeof(buf), " ");
    }
    pos = trace_append_literal(buf, pos, sizeof(buf), "runtime_shards=");
    pos = trace_append_u64(buf, pos, sizeof(buf), rt_runtime_shard_count(exec_state.runtime));
    pos = trace_append_literal(buf, pos, sizeof(buf), " ");
    pos = trace_append_literal(buf, pos, sizeof(buf), "wake_called=");
    pos = trace_append_u64(buf,
                           pos,
                           sizeof(buf),
                           atomic_load_explicit(&trace_wake_called_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " wake_enqueued=");
    pos = trace_append_u64(buf,
                           pos,
                           sizeof(buf),
                           atomic_load_explicit(&trace_wake_enqueued_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " wake_ignored_completed=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_wake_ignored_completed_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " park_attempt=");
    pos = trace_append_u64(buf,
                           pos,
                           sizeof(buf),
                           atomic_load_explicit(&trace_park_attempt_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " park_committed=");
    pos = trace_append_u64(buf,
                           pos,
                           sizeof(buf),
                           atomic_load_explicit(&trace_park_committed_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " worker_sleep=");
    pos = trace_append_u64(buf,
                           pos,
                           sizeof(buf),
                           atomic_load_explicit(&trace_worker_sleep_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " worker_wake=");
    pos = trace_append_u64(buf,
                           pos,
                           sizeof(buf),
                           atomic_load_explicit(&trace_worker_wake_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " parked_with_work=");
    pos =
        trace_append_u64(buf,
                         pos,
                         sizeof(buf),
                         atomic_load_explicit(&trace_parked_with_work_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " channel_blocking_wait=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_channel_blocking_wait_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " channel_task_blocking_send=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_channel_task_blocking_send_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " channel_task_blocking_recv=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_channel_task_blocking_recv_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " channel_handoff_yield=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_channel_handoff_yield_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " compensation_started=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&trace_compensation_started_total, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " blocking_submitted=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&exec_state.blocking_submitted, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " blocking_running=");
    pos =
        trace_append_u64(buf,
                         pos,
                         sizeof(buf),
                         atomic_load_explicit(&exec_state.blocking_running, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " blocking_completed=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&exec_state.blocking_completed, memory_order_relaxed));
    pos = trace_append_literal(buf, pos, sizeof(buf), " blocking_cancel_requested=");
    pos = trace_append_u64(
        buf,
        pos,
        sizeof(buf),
        atomic_load_explicit(&exec_state.blocking_cancel_requested, memory_order_relaxed));
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    (void)write(STDERR_FILENO, buf, pos);
}

static void trace_exec_snapshot_dump(const char* reason) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_executor* ex = &exec_state;
    if (!ex->initialized) {
        return;
    }
    uint64_t worker_count = 0;
    uint64_t running = 0;
    uint64_t inject_len = 0;
    uint64_t local_total = 0;
    uint64_t local_max = 0;
    uint64_t tasks_ready = 0;
    uint64_t tasks_running = 0;
    uint64_t tasks_waiting = 0;
    uint64_t tasks_done = 0;
    uint64_t tasks_ready_user = 0;
    uint64_t tasks_waiting_user = 0;
    uint64_t tasks_done_user = 0;
    uint64_t tasks_user = 0;
    uint64_t tasks_sleep = 0;
    uint64_t tasks_blocking = 0;
    uint64_t tasks_other_kind = 0;
    rt_waiter_trace_counts waiters = {0};

    rt_control_lock(ex);
    trace_collect_scheduler_snapshot(
        ex->runtime, &worker_count, &running, &inject_len, &local_total, &local_max);
    rt_task_table* table = rt_task_table_snapshot(ex);
    for (size_t i = 1; table != NULL && i < table->cap; i++) {
        const rt_task* task = atomic_load_explicit(&table->slots[i], memory_order_acquire);
        if (task == NULL) {
            continue;
        }
        uint8_t status = task_status_load(task);
        if (status == TASK_READY) {
            tasks_ready++;
        } else if (status == TASK_RUNNING) {
            tasks_running++;
        } else if (status == TASK_WAITING) {
            tasks_waiting++;
        } else {
            tasks_done++;
        }
        if (task->kind == TASK_KIND_USER) {
            if (status == TASK_READY) {
                tasks_ready_user++;
            } else if (status == TASK_WAITING) {
                tasks_waiting_user++;
            } else if (status == TASK_DONE) {
                tasks_done_user++;
            }
        }
        switch (task->kind) {
            case TASK_KIND_USER:
                tasks_user++;
                break;
            case TASK_KIND_SLEEP:
                tasks_sleep++;
                break;
            case TASK_KIND_BLOCKING:
                tasks_blocking++;
                break;
            default:
                tasks_other_kind++;
                break;
        }
    }
    rt_trace_collect_waiter_counts(ex, &waiters);

    char buf[1800];
    size_t pos = 0;
    const rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat_const(ex);
    pos = trace_append_literal(buf, pos, sizeof(buf), "TRACE_EXEC_SNAPSHOT");
    if (reason != NULL) {
        pos = trace_append_literal(buf, pos, sizeof(buf), " reason=");
        pos = trace_append_literal(buf, pos, sizeof(buf), reason);
    }
    trace_append_kv_u64(buf, &pos, sizeof(buf), "worker_count", worker_count);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "running", running);
    trace_append_kv_u64(buf,
                        &pos,
                        sizeof(buf),
                        "channel_blocked",
                        compat != NULL ? compat->channel_blocked_workers : 0);
    trace_append_kv_u64(
        buf, &pos, sizeof(buf), "compensation", compat != NULL ? compat->compensation_count : 0);
    trace_append_kv_u64(buf,
                        &pos,
                        sizeof(buf),
                        "compensation_high_water",
                        compat != NULL ? compat->compensation_high_water : 0);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "inject_len", inject_len);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "local_total", local_total);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "local_max", local_max);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "waiters", waiters.total);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "waiters_join", waiters.join);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "waiters_timer", waiters.timer);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "waiters_chan_send", waiters.chan_send);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "waiters_chan_recv", waiters.chan_recv);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "waiters_net", waiters.net);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "waiters_other", waiters.other);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_ready", tasks_ready);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_running", tasks_running);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_waiting", tasks_waiting);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_done", tasks_done);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_ready_user", tasks_ready_user);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_waiting_user", tasks_waiting_user);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_done_user", tasks_done_user);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_user", tasks_user);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_sleep", tasks_sleep);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_blocking", tasks_blocking);
    trace_append_kv_u64(buf, &pos, sizeof(buf), "tasks_other_kind", tasks_other_kind);
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    rt_control_unlock(ex);
    (void)write(STDERR_FILENO, buf, pos);
}

void rt_trace_sched_record(rt_trace_sched_source source, uint64_t id) {
    if (!trace_sched_enabled()) {
        return;
    }
    trace_sched_events++;
    if (source == RT_TRACE_SCHED_SRC_LOCAL) {
        trace_sched_local_pops++;
    } else if (source == RT_TRACE_SCHED_SRC_INJECT) {
        trace_sched_inject_pops++;
    } else if (source == RT_TRACE_SCHED_SRC_STEAL) {
        trace_sched_steal_pops++;
    }
    uint64_t mix = id ^ ((uint64_t)source << 56);
    trace_sched_hash ^= mix;
    trace_sched_hash *= UINT64_C(1099511628211);
}

static void trace_sched_inc_atomic(_Atomic uint64_t* counter) {
    if (!trace_sched_enabled() || counter == NULL) {
        return;
    }
    (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
}

void rt_trace_sched_tier1_steal_denied(void) {
    trace_sched_inc_atomic(&trace_sched_tier1_steal_denied_total);
}

void rt_trace_sched_connection_owner_placed(void) {
    trace_sched_inc_atomic(&trace_sched_conn_owner_placed_total);
}

void rt_trace_sched_connection_owner_run(uint32_t owner_shard_id, uint32_t worker_shard_id) {
    if (owner_shard_id == worker_shard_id) {
        trace_sched_inc_atomic(&trace_sched_conn_owner_local_total);
        return;
    }
    trace_sched_inc_atomic(&trace_sched_conn_owner_mismatch_total);
}

static void trace_exec_signal_handler(int sig) {
    (void)sig;
#ifdef SIGUSR1
    (void)signal(SIGUSR1, trace_exec_signal_handler);
#endif
    atomic_store_explicit(&trace_dump_requested_flag, 1, memory_order_relaxed);
}

static void trace_dump_all(const char* reason) {
    trace_exec_dump(reason);
    if (rt_exec_trace_enabled()) {
        rt_net_trace_runtime_shards(rt_runtime_shard_count(exec_state.runtime));
        rt_net_trace_dump(reason);
    }
    trace_exec_snapshot_dump(reason);
}

void rt_trace_drain_signal_dump(void) {
    if (atomic_exchange_explicit(&trace_dump_requested_flag, 0, memory_order_relaxed) == 0) {
        return;
    }
    trace_dump_all("sigusr1");
    rt_sched_trace_dump();
}

void rt_exec_trace_dump(void) {
    trace_dump_all("exit");
}

void rt_sched_trace_init(void) {
    const char* value = getenv("SURGE_SCHED_TRACE");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        return;
    }
    trace_sched_enabled_flag = 1;
    trace_sched_hash = UINT64_C(1469598103934665603);
    trace_sched_events = 0;
    trace_sched_local_pops = 0;
    trace_sched_inject_pops = 0;
    trace_sched_steal_pops = 0;
    atomic_store_explicit(&trace_sched_tier1_steal_denied_total, 0, memory_order_relaxed);
    atomic_store_explicit(&trace_sched_conn_owner_placed_total, 0, memory_order_relaxed);
    atomic_store_explicit(&trace_sched_conn_owner_local_total, 0, memory_order_relaxed);
    atomic_store_explicit(&trace_sched_conn_owner_mismatch_total, 0, memory_order_relaxed);
}

void rt_exec_trace_init(void) {
    const char* value = getenv("SURGE_TRACE_EXEC");
    if (value == NULL || value[0] == '\0' || (value[0] == '0' && value[1] == '\0')) {
        return;
    }
    trace_exec_enabled_flag = 1;
#ifdef SIGUSR1
    (void)signal(SIGUSR1, trace_exec_signal_handler);
#endif
}

int rt_trace_dump_requested(void) {
    return atomic_load_explicit(&trace_dump_requested_flag, memory_order_relaxed) != 0;
}

void rt_sched_trace_dump(void) {
    if (!trace_sched_enabled()) {
        return;
    }
    if (!exec_state.initialized) {
        return;
    }
    rt_control_lock(&exec_state);
    const rt_scheduler* scheduler = rt_executor_scheduler_const(&exec_state);
    uint64_t local = trace_sched_local_pops;
    uint64_t inject = trace_sched_inject_pops;
    uint64_t steal = trace_sched_steal_pops;
    uint64_t events = trace_sched_events;
    uint64_t hash = trace_sched_hash;
    uint64_t tier1_steal_denied =
        atomic_load_explicit(&trace_sched_tier1_steal_denied_total, memory_order_relaxed);
    uint64_t conn_owner_placed =
        atomic_load_explicit(&trace_sched_conn_owner_placed_total, memory_order_relaxed);
    uint64_t conn_owner_local =
        atomic_load_explicit(&trace_sched_conn_owner_local_total, memory_order_relaxed);
    uint64_t conn_owner_mismatch =
        atomic_load_explicit(&trace_sched_conn_owner_mismatch_total, memory_order_relaxed);
    uint64_t seed = scheduler != NULL ? scheduler->sched_seed : 0;
    uint8_t mode = scheduler != NULL ? scheduler->sched_mode : SCHED_PARALLEL;
    rt_control_unlock(&exec_state);

    char buf[512];
    size_t pos = 0;
    pos = trace_append_literal(buf, pos, sizeof(buf), "SCHED_TRACE mode=");
    pos = trace_append_literal(buf, pos, sizeof(buf), mode == SCHED_SEEDED ? "seeded" : "parallel");
    pos = trace_append_literal(buf, pos, sizeof(buf), " seed=");
    pos = trace_append_u64(buf, pos, sizeof(buf), seed);
    pos = trace_append_literal(buf, pos, sizeof(buf), " local=");
    pos = trace_append_u64(buf, pos, sizeof(buf), local);
    pos = trace_append_literal(buf, pos, sizeof(buf), " inject=");
    pos = trace_append_u64(buf, pos, sizeof(buf), inject);
    pos = trace_append_literal(buf, pos, sizeof(buf), " steal=");
    pos = trace_append_u64(buf, pos, sizeof(buf), steal);
    pos = trace_append_literal(buf, pos, sizeof(buf), " tier1_steal_denied=");
    pos = trace_append_u64(buf, pos, sizeof(buf), tier1_steal_denied);
    pos = trace_append_literal(buf, pos, sizeof(buf), " conn_owner_placed=");
    pos = trace_append_u64(buf, pos, sizeof(buf), conn_owner_placed);
    pos = trace_append_literal(buf, pos, sizeof(buf), " conn_owner_local=");
    pos = trace_append_u64(buf, pos, sizeof(buf), conn_owner_local);
    pos = trace_append_literal(buf, pos, sizeof(buf), " conn_owner_mismatch=");
    pos = trace_append_u64(buf, pos, sizeof(buf), conn_owner_mismatch);
    pos = trace_append_literal(buf, pos, sizeof(buf), " events=");
    pos = trace_append_u64(buf, pos, sizeof(buf), events);
    pos = trace_append_literal(buf, pos, sizeof(buf), " hash=");
    pos = trace_append_u64(buf, pos, sizeof(buf), hash);
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    (void)write(STDERR_FILENO, buf, pos);
}
