#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_async_internal.h"

#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <unistd.h>

static int net_poll_set_nonblocking_cloexec(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0 || fcntl(fd, F_SETFL, flags | O_NONBLOCK) < 0) {
        return 0;
    }
    flags = fcntl(fd, F_GETFD, 0);
    if (flags < 0 || fcntl(fd, F_SETFD, flags | FD_CLOEXEC) < 0) {
        return 0;
    }
    return 1;
}

int rt_net_poll_wake_init(rt_shard* shard) {
    if (shard == NULL) {
        return 0;
    }
    rt_net_poll_wake* wake = &shard->net_poll_wake;
    if (wake->read_fd >= 0 && wake->write_fd >= 0) {
        return 1;
    }
    int fds[2] = {-1, -1};
    if (pipe(fds) != 0) {
        return 0;
    }
    if (!net_poll_set_nonblocking_cloexec(fds[0]) || !net_poll_set_nonblocking_cloexec(fds[1])) {
        close(fds[0]);
        close(fds[1]);
        return 0;
    }
    wake->read_fd = fds[0];
    wake->write_fd = fds[1];
    return 1;
}

void rt_net_poll_wake_close(rt_net_poll_wake* wake) {
    if (wake == NULL) {
        return;
    }
    if (wake->read_fd >= 0) {
        close(wake->read_fd);
    }
    if (wake->write_fd >= 0) {
        close(wake->write_fd);
    }
    wake->read_fd = -1;
    wake->write_fd = -1;
}

uint64_t rt_net_wake_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    if (shard == NULL || !rt_net_poll_wake_init(shard) || shard->net_poll_wake.write_fd < 0) {
        return 0;
    }
    uint8_t byte = 1;
    ssize_t n = -1;
    do {
        n = write(shard->net_poll_wake.write_fd, &byte, 1);
    } while (n < 0 && errno == EINTR);
    if (n == 1) {
        return 1;
    }
    if (n < 0 && errno == EAGAIN) {
        return 1;
    }
#ifdef EWOULDBLOCK
#if EWOULDBLOCK != EAGAIN
    if (n < 0 && errno == EWOULDBLOCK) {
        return 1;
    }
#endif
#endif
    return 0;
}

uint64_t rt_net_wake_poll_all_shards(rt_executor* ex) {
    uint64_t woken = 0;
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        woken += rt_net_wake_poll_on_shard(ex, (uint32_t)i);
    }
    return woken;
}

uint64_t
rt_net_wake_poll_for_task_wait_keys(rt_executor* ex, const rt_task* task, waker_key fallback_key) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t woken = 0;
    int multi_shard = rt_runtime_shard_count(rt_executor_runtime(ex)) > 1;
    if (task != NULL) {
        for (size_t i = 0; i < task->wait_keys_len; i++) {
            waker_key key = task->wait_keys[i];
            if (!waker_is_net(key)) {
                continue;
            }
            uint32_t owner_shard_id = rt_net_owner_shard_probe_locked(
                ex, (int)key.id, tls_worker_ctx != NULL ? tls_worker_ctx->shard_id : 0);
            if (owner_shard_id == UINT32_MAX) {
                continue;
            }
            uint64_t wrote = rt_net_wake_poll_on_shard(ex, owner_shard_id);
            woken += wrote;
            if (wrote > 0 && multi_shard) {
                rt_sched_wake_signal_shard_n(
                    rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id), 1);
            }
        }
    }
    if (woken == 0 && waker_is_net(fallback_key)) {
        uint32_t owner_shard_id = rt_net_owner_shard_probe_locked(
            ex, (int)fallback_key.id, tls_worker_ctx != NULL ? tls_worker_ctx->shard_id : 0);
        uint64_t wrote =
            owner_shard_id != UINT32_MAX ? rt_net_wake_poll_on_shard(ex, owner_shard_id) : 0;
        woken += wrote;
        if (wrote > 0 && multi_shard) {
            rt_sched_wake_signal_shard_n(rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id),
                                         1);
        }
    }
    return woken;
}

void rt_net_poll_wake_drain(rt_shard* shard) {
    if (shard == NULL || shard->net_poll_wake.read_fd < 0) {
        return;
    }
    uint8_t buf[64];
    for (;;) {
        ssize_t n = read(shard->net_poll_wake.read_fd, buf, sizeof(buf));
        if (n > 0) {
            continue;
        }
        if (n < 0 && errno == EINTR) {
            continue;
        }
        break;
    }
}

int rt_net_has_waiters_on_shard(const rt_executor* ex, uint32_t owner_shard_id) {
    const rt_fd_registry* registry = rt_executor_fd_registry_const_for_shard(ex, owner_shard_id);
    if (registry == NULL) {
        return 0;
    }
    for (size_t i = 0; i < registry->len; i++) {
        const rt_fd_entry* entry = &registry->entries[i];
        if (entry->close_state == RT_FD_CLOSE_STATE_OPEN &&
            (entry->want_accept != 0 || entry->want_read != 0 || entry->want_write != 0)) {
            return 1;
        }
    }
    return 0;
}

int rt_net_begin_poll_on_shard(rt_executor* ex, uint32_t owner_shard_id) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    if (shard == NULL) {
        return 0;
    }
    // net_polling arbitration lives on the shard lane; callers hold either
    // the control lock (io thread, N=1 runner) or nothing (worker turn).
    rt_shard_lock(shard);
    int begun = 0;
    if (!shard->net_polling && rt_net_has_waiters_on_shard(ex, owner_shard_id)) {
        shard->net_polling = 1;
        begun = 1;
    }
    rt_shard_unlock(shard);
    return begun;
}

int rt_net_poll_waiters_owned_on_shard(rt_executor* ex, uint32_t owner_shard_id, int timeout_ms) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    int woke = poll_net_waiters_on_shard(ex, owner_shard_id, timeout_ms);
    if (shard != NULL) {
        rt_shard_lock(shard);
        shard->net_polling = 0;
        pthread_cond_broadcast(&shard->poller_cv);
        rt_shard_unlock(shard);
    }
    pthread_cond_signal(&ex->io_cv);
    return woke;
}
