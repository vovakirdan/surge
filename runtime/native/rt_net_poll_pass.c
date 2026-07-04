#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"
#include "rt_net_trace.h"

#include <errno.h>
#include <poll.h>

// One shard's net poll pass: snapshot the owner shard's fd registry into its
// poll scratch under the shard lock, run poll() with no lock held (a
// control-lane caller also releases the control lock, as before the split),
// then complete readiness through the lane-aware waiter primitives.

static size_t net_next_cap(size_t current, size_t want) {
    size_t next = current == 0 ? 8 : current;
    while (next < want) {
        if (next > SIZE_MAX / 2U) {
            return want;
        }
        next *= 2U;
    }
    return next;
}

static int
ensure_net_poll_fds(rt_net_poll_scratch* scratch, size_t want, rt_fd_poll_interest** out) {
    if (scratch == NULL || out == NULL) {
        return 0;
    }
    if (scratch->fds_cap < want) {
        size_t next_cap = net_next_cap(scratch->fds_cap, want);
        rt_fd_poll_interest* next = (rt_fd_poll_interest*)rt_realloc(
            (uint8_t*)scratch->fds,
            (uint64_t)scratch->fds_cap * (uint64_t)sizeof(rt_fd_poll_interest),
            (uint64_t)next_cap * (uint64_t)sizeof(rt_fd_poll_interest),
            _Alignof(rt_fd_poll_interest));
        if (next == NULL) {
            return 0;
        }
        scratch->fds = next;
        scratch->fds_cap = next_cap;
        rt_net_trace_poll_alloc();
    }
    *out = (rt_fd_poll_interest*)scratch->fds;
    return 1;
}

static int ensure_net_poll_pfds(rt_net_poll_scratch* scratch, size_t want, struct pollfd** out) {
    if (scratch == NULL || out == NULL) {
        return 0;
    }
    if (scratch->pfds_cap < want) {
        size_t next_cap = net_next_cap(scratch->pfds_cap, want);
        struct pollfd* next = (struct pollfd*)rt_realloc(
            (uint8_t*)scratch->pfds,
            (uint64_t)scratch->pfds_cap * (uint64_t)sizeof(struct pollfd),
            (uint64_t)next_cap * (uint64_t)sizeof(struct pollfd),
            _Alignof(struct pollfd));
        if (next == NULL) {
            return 0;
        }
        scratch->pfds = next;
        scratch->pfds_cap = next_cap;
        rt_net_trace_poll_alloc();
    }
    *out = (struct pollfd*)scratch->pfds;
    return 1;
}

int poll_net_waiters_on_shard(rt_executor* ex, uint32_t owner_shard_id, int timeout_ms) {
    // Lane-aware poll pass: the snapshot is built under the owner shard's
    // lock (the registry and scratch are shard state); a control-lane caller
    // additionally releases the control lock around the syscall, exactly as
    // before the split. Completion runs with no lock held - the waiter and
    // wake primitives take their own lanes.
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    if (shard == NULL) {
        return 0;
    }
    rt_shard_lock(shard);
    const rt_fd_registry* registry = rt_executor_fd_registry_const_for_shard(ex, owner_shard_id);
    size_t cap = rt_fd_registry_len(registry);
    if (cap == 0) {
        rt_shard_unlock(shard);
        return 0;
    }
    rt_net_poll_scratch* scratch = rt_shard_net_poll_scratch(shard);
    rt_fd_poll_interest* fds = NULL;
    if (!ensure_net_poll_fds(scratch, cap, &fds)) {
        rt_shard_unlock(shard);
        return 0;
    }
    size_t count = rt_fd_registry_snapshot_poll_interest(registry, fds, cap);
    for (size_t i = 0; i < count; i++) {
        fds[i].owner_shard_id = owner_shard_id;
    }
    if (count == 0) {
        rt_shard_unlock(shard);
        return 0;
    }
    int wake_fd = rt_net_poll_wake_init(shard) ? shard->net_poll_wake.read_fd : -1;
    size_t wake_count = wake_fd >= 0 ? 1U : 0U;
    size_t poll_count = count + wake_count;
    if (poll_count < count || poll_count > (size_t)((nfds_t)-1)) {
        rt_shard_unlock(shard);
        return 0;
    }
    struct pollfd* pfds = NULL;
    if (!ensure_net_poll_pfds(scratch, poll_count, &pfds)) {
        rt_shard_unlock(shard);
        return 0;
    }
    rt_net_trace_poll_start(timeout_ms, (uint64_t)count);

    size_t offset = 0;
    if (wake_fd >= 0) {
        pfds[0].fd = wake_fd;
        pfds[0].events = POLLIN;
        pfds[0].revents = 0;
        offset = 1;
    }
    for (size_t i = 0; i < count; i++) {
        size_t poll_idx = offset + i;
        pfds[poll_idx].fd = fds[i].fd;
        pfds[poll_idx].events = 0;
        pfds[poll_idx].revents = 0;
        if (fds[i].want_accept || fds[i].want_read) {
            pfds[poll_idx].events |= POLLIN;
        }
        if (fds[i].want_write) {
            pfds[poll_idx].events |= POLLOUT;
        }
    }

    rt_shard_unlock(shard);
    int caller_held_control = rt_lane_holds_control();
    if (caller_held_control) {
        rt_control_unlock(ex);
    }
    int n = -1;
    nfds_t nfds = (nfds_t)poll_count;
    do {
        n = poll(pfds, nfds, timeout_ms);
    } while (n < 0 && errno == EINTR);
    if (caller_held_control) {
        rt_control_lock(ex);
    }
    if (n < 0) {
        rt_net_trace_poll_error();
        for (size_t i = 0; i < count; i++) {
            rt_fd_completion_summary summary =
                rt_fd_registry_complete_ready_net_waiters(ex, &fds[i], 1, 1);
            rt_net_trace_waiter_completion(summary.calls, summary.woken);
        }
        return 1;
    }
    if (n == 0) {
        rt_net_trace_poll_timeout();
        return 0;
    }

    int woke = 0;
    uint64_t ready_fds = 0;
    if (wake_fd >= 0 && pfds[0].revents != 0) {
        rt_net_poll_wake_drain(shard);
        rt_net_trace_poll_wake_fd();
        woke = 1;
    }
    for (size_t i = 0; i < count; i++) {
        size_t poll_idx = offset + i;
        if (pfds[poll_idx].revents == 0) {
            continue;
        }
        ready_fds++;
        bool read_ready = (pfds[poll_idx].revents & (POLLIN | POLLERR | POLLHUP | POLLNVAL)) != 0;
        bool write_ready = (pfds[poll_idx].revents & (POLLOUT | POLLERR | POLLHUP | POLLNVAL)) != 0;
        rt_fd_completion_summary completion =
            rt_fd_registry_complete_ready_net_waiters(ex, &fds[i], read_ready, write_ready);
        if (read_ready) {
            rt_net_trace_poll_ready();
            woke = 1;
        }
        if (write_ready) {
            rt_net_trace_poll_ready();
            woke = 1;
        }
        rt_net_trace_waiter_completion(completion.calls, completion.woken);
    }
    rt_net_trace_fd_ready_batch(owner_shard_id, ready_fds);

    return woke;
}
