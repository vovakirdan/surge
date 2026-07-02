#ifndef SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H
#define SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H

#include <stddef.h>
#include <stdint.h>

// Shard-local fd registry: the single durable row per live net fd.
// Ownership and lifecycle:
// - Each rt_shard owns one rt_fd_registry by value; it is initialized with the
//   owning shard in rt_runtime_init_n1 and, like the waiter store and poll
//   scratch, guarded by ex->lock. Task 6 routes registration-side interest
//   writes through the waiter-store bridge in rt_async_waiter.c; no runtime
//   path reads entries yet (poll input stays waiter-derived until Task 7).
// - A row exists iff at least one net-key waiter for that fd is parked in the
//   waiter store (modulo the fd-registry-attach-miss bridge: on allocation
//   failure the waiter parks without a row; Task 7 resolves that bridge).
//   Detaching the last interest flag swap-removes the row; row order is not
//   meaningful and find is a linear scan.
// - generation guards fd-reuse stale wakes; close_state guards post-close
//   interest. Behavior lands in Task 9; the fields exist so the row shape is
//   fixed now. Remove-plus-recreate resets generation to 0; Task 9 owns
//   re-deciding row lifetime when it adds generation/close semantics.
// - rt_fd_registry_free releases entry storage. No caller exists today because
//   the process has no executor shutdown path (see the Epic 4 dependency map);
//   Tasks 10-11 create that path and wire the free.
// This header is included from rt_async_internal.h after rt_runtime_status,
// waker_key, and the rt_shard/rt_executor forward typedefs; translation units
// include rt_async_internal.h, not this header directly.

typedef enum {
    RT_FD_CLOSE_STATE_OPEN = 0,
    RT_FD_CLOSE_STATE_CLOSED = 1,
} rt_fd_close_state;

typedef struct {
    int fd;
    uint64_t generation;
    uint8_t close_state; // holds rt_fd_close_state values (rt_task.status pattern)
    uint8_t want_accept;
    uint8_t want_read;
    uint8_t want_write;
} rt_fd_entry;

typedef struct {
    rt_fd_entry* entries;
    size_t len;
    size_t cap;
} rt_fd_registry;

rt_fd_registry* rt_shard_fd_registry(rt_shard* shard);
const rt_fd_registry* rt_shard_fd_registry_const(const rt_shard* shard);
rt_fd_registry* rt_executor_fd_registry(rt_executor* ex);
const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex);

rt_runtime_status rt_fd_registry_init(rt_fd_registry* registry);
void rt_fd_registry_free(rt_fd_registry* registry);
rt_runtime_status rt_fd_registry_ensure_cap(rt_fd_registry* registry);
size_t rt_fd_registry_len(const rt_fd_registry* registry);
const rt_fd_entry* rt_fd_registry_find_const(const rt_fd_registry* registry, int fd);
rt_runtime_status rt_fd_registry_attach_net_interest(rt_fd_registry* registry, waker_key key);
void rt_fd_registry_detach_net_interest(rt_fd_registry* registry, waker_key key);

#endif
