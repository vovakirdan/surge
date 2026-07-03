#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_net_trace.h"
#include "rt_runtime_config.h"

#include <stdarg.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <unistd.h>

static _Atomic uint64_t net_poll_calls_total;
static _Atomic uint64_t net_poll_timeouts_total;
static _Atomic uint64_t net_poll_wake_fd_total;
static _Atomic uint64_t net_poll_ready_total;
static _Atomic uint64_t net_poll_errors_total;
static _Atomic uint64_t net_poll_timeout_last_ms;
static _Atomic uint64_t net_poll_timeout_max_ms;
static _Atomic uint64_t net_poll_waiters_last;
static _Atomic uint64_t net_poll_waiters_max;
static _Atomic uint64_t net_poll_waiters_total;
static _Atomic uint64_t net_direct_wait_total;
static _Atomic uint64_t net_waiter_scan_entries_total;
static _Atomic uint64_t net_waiter_net_entries_total;
static _Atomic uint64_t net_poll_rebuilds_total;
static _Atomic uint64_t net_poll_allocs_total;
static _Atomic uint64_t net_poll_dedup_checks_total;
static _Atomic uint64_t net_waiter_complete_calls_total;
static _Atomic uint64_t net_waiter_completed_total;
static _Atomic uint64_t net_runtime_shards;
static _Atomic uint64_t net_accept_owner_total;
static _Atomic uint64_t net_accept_owner_mask;
static _Atomic uint64_t net_accept_owner_by_shard[RT_RUNTIME_MAX_SHARDS];
static _Atomic uint64_t net_global_path_fallbacks_total;
static _Atomic uint64_t net_fd_ready_batches_total;
static _Atomic uint64_t net_fd_ready_batch_fds_total;
static _Atomic uint64_t net_fd_ready_batch_fds_max;
static _Atomic uint64_t net_fd_ready_batches_by_shard[RT_RUNTIME_MAX_SHARDS];
static _Atomic uint64_t net_fd_ready_fds_by_shard[RT_RUNTIME_MAX_SHARDS];
static _Atomic uint64_t net_fd_owner_registry_rows_total;
static _Atomic uint64_t net_close_owner_wakeups_total;
static _Atomic uint64_t net_cancel_owner_cleanup_total;
static _Atomic uint64_t net_shutdown_poller_wakeups_total;
static _Atomic uint64_t net_non_owner_conn_denied_total;
static _Atomic uint64_t net_listener_group_members_closed_total;

#define NET_TRACE_DUMP_FORMAT                                                                      \
    "TRACE_NET reason=%s runtime_shards=%llu io_poll_calls=%llu io_poll_timeouts=%llu "            \
    "io_poll_wake_fd=%llu io_poll_net_ready=%llu io_poll_errors=%llu "                             \
    "io_poll_timeout_last_ms=%llu io_poll_timeout_max_ms=%llu "                                    \
    "io_poll_waiters_last=%llu io_poll_waiters_max=%llu "                                          \
    "io_poll_waiters_total=%llu io_direct_waits=%llu "                                             \
    "io_waiter_scan_entries=%llu io_waiter_net_entries=%llu "                                      \
    "io_poll_rebuilds=%llu io_poll_allocs=%llu io_poll_dedup_checks=%llu "                         \
    "io_waiter_complete_calls=%llu io_waiter_completed=%llu "                                      \
    "accept_owner_total=%llu accept_owner_active_shards=%llu accept_owner_min=%llu "               \
    "accept_owner_max=%llu accept_owner_imbalance=%llu global_path_fallbacks=%llu "                \
    "fd_ready_batches=%llu fd_ready_batch_fds_total=%llu fd_ready_batch_fds_max=%llu "             \
    "fd_owner_registry_rows=%llu "                                                                 \
    "close_owner_wakeups=%llu cancel_owner_cleanup=%llu shutdown_poller_wakeups=%llu "             \
    "non_owner_conn_denied=%llu listener_group_members_closed=%llu\n"
#define NET_TRACE_DUMP_ARGS(reason)                                                                \
    (reason), (unsigned long long)runtime_shards, net_trace_load(&net_poll_calls_total),           \
        net_trace_load(&net_poll_timeouts_total), net_trace_load(&net_poll_wake_fd_total),         \
        net_trace_load(&net_poll_ready_total), net_trace_load(&net_poll_errors_total),             \
        net_trace_load(&net_poll_timeout_last_ms), net_trace_load(&net_poll_timeout_max_ms),       \
        net_trace_load(&net_poll_waiters_last), net_trace_load(&net_poll_waiters_max),             \
        net_trace_load(&net_poll_waiters_total), net_trace_load(&net_direct_wait_total),           \
        net_trace_load(&net_waiter_scan_entries_total),                                            \
        net_trace_load(&net_waiter_net_entries_total), net_trace_load(&net_poll_rebuilds_total),   \
        net_trace_load(&net_poll_allocs_total), net_trace_load(&net_poll_dedup_checks_total),      \
        net_trace_load(&net_waiter_complete_calls_total),                                          \
        net_trace_load(&net_waiter_completed_total), net_trace_load(&net_accept_owner_total),      \
        (unsigned long long)__builtin_popcountll(net_trace_load(&net_accept_owner_mask)),          \
        accept_min, accept_max, accept_imbalance,                                                  \
        net_trace_load(&net_global_path_fallbacks_total),                                          \
        net_trace_load(&net_fd_ready_batches_total),                                               \
        net_trace_load(&net_fd_ready_batch_fds_total),                                             \
        net_trace_load(&net_fd_ready_batch_fds_max),                                               \
        net_trace_load(&net_fd_owner_registry_rows_total),                                         \
        net_trace_load(&net_close_owner_wakeups_total),                                            \
        net_trace_load(&net_cancel_owner_cleanup_total),                                           \
        net_trace_load(&net_shutdown_poller_wakeups_total),                                        \
        net_trace_load(&net_non_owner_conn_denied_total),                                          \
        net_trace_load(&net_listener_group_members_closed_total)

static unsigned long long net_trace_load(const _Atomic uint64_t* counter) {
    return (unsigned long long)atomic_load_explicit(counter, memory_order_relaxed);
}

static void net_trace_inc(_Atomic uint64_t* counter) {
    (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
}

static void net_trace_add(_Atomic uint64_t* counter, uint64_t value) {
    (void)atomic_fetch_add_explicit(counter, value, memory_order_relaxed);
}

static void net_trace_store(_Atomic uint64_t* counter, uint64_t value) {
    atomic_store_explicit(counter, value, memory_order_relaxed);
}

static uint64_t net_trace_timeout_ms(int timeout_ms) {
    if (timeout_ms <= 0) {
        return 0;
    }
    return (uint64_t)timeout_ms;
}

static void net_trace_max(_Atomic uint64_t* counter, uint64_t value) {
    uint64_t current = atomic_load_explicit(counter, memory_order_relaxed);
    while (current < value &&
           !atomic_compare_exchange_weak_explicit(
               counter, &current, value, memory_order_relaxed, memory_order_relaxed)) {
    }
}

static uint64_t net_trace_runtime_shards(void) {
    uint64_t runtime_shards = atomic_load_explicit(&net_runtime_shards, memory_order_relaxed);
    if (runtime_shards < 1 || runtime_shards > RT_RUNTIME_MAX_SHARDS) {
        return 1;
    }
    return runtime_shards;
}

static void net_trace_accept_stats(uint64_t runtime_shards,
                                   unsigned long long* out_min,
                                   unsigned long long* out_max,
                                   unsigned long long* out_imbalance) {
    uint64_t min = 0;
    uint64_t max = 0;
    for (uint64_t i = 0; i < runtime_shards; i++) {
        uint64_t value = atomic_load_explicit(&net_accept_owner_by_shard[i], memory_order_relaxed);
        if (i == 0 || value < min) {
            min = value;
        }
        if (value > max) {
            max = value;
        }
    }
    *out_min = (unsigned long long)min;
    *out_max = (unsigned long long)max;
    *out_imbalance = (unsigned long long)(max - min);
}

static size_t net_trace_appendf(char* buf, size_t pos, size_t cap, const char* fmt, ...) {
    if (buf == NULL || fmt == NULL || pos >= cap) {
        return pos;
    }
    va_list args;
    va_start(args, fmt);
    int written = vsnprintf(&buf[pos], cap - pos, fmt, args);
    va_end(args);
    if (written < 0) {
        return pos;
    }
    size_t used = (size_t)written;
    if (used >= cap - pos) {
        return cap - 1;
    }
    return pos + used;
}

static void net_trace_dump_shards(const char* reason, uint64_t runtime_shards) {
    char buf[8192];
    size_t pos = 0;
    pos = net_trace_appendf(buf,
                            pos,
                            sizeof(buf),
                            "TRACE_NET_SHARDS reason=%s runtime_shards=%llu",
                            reason,
                            (unsigned long long)runtime_shards);
    for (uint64_t i = 0; i < runtime_shards; i++) {
        pos = net_trace_appendf(buf,
                                pos,
                                sizeof(buf),
                                " accept_%llu=%llu",
                                (unsigned long long)i,
                                net_trace_load(&net_accept_owner_by_shard[i]));
    }
    for (uint64_t i = 0; i < runtime_shards; i++) {
        pos = net_trace_appendf(buf,
                                pos,
                                sizeof(buf),
                                " fd_ready_batches_%llu=%llu",
                                (unsigned long long)i,
                                net_trace_load(&net_fd_ready_batches_by_shard[i]));
    }
    for (uint64_t i = 0; i < runtime_shards; i++) {
        pos = net_trace_appendf(buf,
                                pos,
                                sizeof(buf),
                                " fd_ready_fds_%llu=%llu",
                                (unsigned long long)i,
                                net_trace_load(&net_fd_ready_fds_by_shard[i]));
    }
    if (pos + 1 < sizeof(buf)) {
        buf[pos++] = '\n';
    }
    (void)write(STDERR_FILENO, buf, pos);
}

void rt_net_trace_dump(const char* reason) {
    if (reason == NULL || reason[0] == '\0') {
        reason = "unknown";
    }
    uint64_t runtime_shards = net_trace_runtime_shards();
    unsigned long long accept_min = 0;
    unsigned long long accept_max = 0;
    unsigned long long accept_imbalance = 0;
    net_trace_accept_stats(runtime_shards, &accept_min, &accept_max, &accept_imbalance);
    (void)dprintf(STDERR_FILENO, NET_TRACE_DUMP_FORMAT, NET_TRACE_DUMP_ARGS(reason));
    net_trace_dump_shards(reason, runtime_shards);
}

void rt_net_trace_direct_wait_enabled(void) {
    net_trace_inc(&net_direct_wait_total);
}

void rt_net_trace_poll_alloc_enabled(void) {
    net_trace_inc(&net_poll_allocs_total);
}

void rt_net_trace_poll_start_enabled(int timeout_ms, uint64_t waiter_count) {
    net_trace_inc(&net_poll_rebuilds_total);
    net_trace_inc(&net_poll_calls_total);
    uint64_t requested_timeout_ms = net_trace_timeout_ms(timeout_ms);
    net_trace_store(&net_poll_timeout_last_ms, requested_timeout_ms);
    net_trace_max(&net_poll_timeout_max_ms, requested_timeout_ms);
    net_trace_store(&net_poll_waiters_last, waiter_count);
    net_trace_max(&net_poll_waiters_max, waiter_count);
    net_trace_add(&net_poll_waiters_total, waiter_count);
}

void rt_net_trace_poll_error_enabled(void) {
    net_trace_inc(&net_poll_errors_total);
}

void rt_net_trace_poll_timeout_enabled(void) {
    net_trace_inc(&net_poll_timeouts_total);
}

void rt_net_trace_poll_wake_fd_enabled(void) {
    net_trace_inc(&net_poll_wake_fd_total);
}

void rt_net_trace_poll_ready_enabled(void) {
    net_trace_inc(&net_poll_ready_total);
}

void rt_net_trace_waiter_completion_enabled(uint64_t calls, uint64_t woken) {
    net_trace_add(&net_waiter_complete_calls_total, calls);
    net_trace_add(&net_waiter_completed_total, woken);
}

void rt_net_trace_accept_owner_enabled(uint32_t owner_shard_id) {
    if (owner_shard_id >= RT_RUNTIME_MAX_SHARDS) {
        return;
    }
    net_trace_inc(&net_accept_owner_total);
    net_trace_inc(&net_accept_owner_by_shard[owner_shard_id]);
    uint64_t bit = UINT64_C(1) << owner_shard_id;
    (void)atomic_fetch_or_explicit(&net_accept_owner_mask, bit, memory_order_relaxed);
}

void rt_net_trace_runtime_shards_enabled(uint64_t count) {
    net_trace_store(&net_runtime_shards, count);
}

void rt_net_trace_global_path_fallback_enabled(void) {
    net_trace_inc(&net_global_path_fallbacks_total);
}

void rt_net_trace_fd_ready_batch_enabled(uint32_t owner_shard_id, uint64_t fd_count) {
    if (fd_count == 0) {
        return;
    }
    net_trace_inc(&net_fd_ready_batches_total);
    net_trace_add(&net_fd_ready_batch_fds_total, fd_count);
    net_trace_max(&net_fd_ready_batch_fds_max, fd_count);
    if (owner_shard_id >= RT_RUNTIME_MAX_SHARDS) {
        return;
    }
    net_trace_inc(&net_fd_ready_batches_by_shard[owner_shard_id]);
    net_trace_add(&net_fd_ready_fds_by_shard[owner_shard_id], fd_count);
}

void rt_net_trace_fd_owner_registry_row_enabled(void) {
    net_trace_inc(&net_fd_owner_registry_rows_total);
}

void rt_net_trace_close_owner_wakeup_enabled(void) {
    net_trace_inc(&net_close_owner_wakeups_total);
}

void rt_net_trace_shutdown_poller_wakeups_enabled(uint64_t count) {
    net_trace_add(&net_shutdown_poller_wakeups_total, count);
}

void rt_net_trace_listener_group_member_closed_enabled(void) {
    net_trace_inc(&net_listener_group_members_closed_total);
}

void rt_net_trace_non_owner_conn_denied_enabled(void) {
    net_trace_inc(&net_non_owner_conn_denied_total);
}
