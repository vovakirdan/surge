#ifndef SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H
#define SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H

#include <stddef.h>
#include <stdint.h>

// Shard-local fd registry: the single durable row per live net fd.
// Ownership and lifecycle:
// - Each rt_shard owns one rt_fd_registry by value; it is initialized with the
//   owning shard in rt_runtime_init_n1 and, like the waiter store and poll
//   scratch, guarded by ex->lock once Tasks 6-9 route net behavior through it.
//   Task 5 wires init only: no runtime path reads or writes entries yet.
// - generation guards fd-reuse stale wakes; close_state guards post-close
//   interest. Behavior lands in Task 9; the fields exist so the row shape is
//   fixed now.
// - rt_fd_registry_free releases entry storage. No caller exists today because
//   the process has no executor shutdown path (see the Epic 4 dependency map);
//   Tasks 10-11 create that path and wire the free.
// This header is included from rt_async_internal.h after rt_runtime_status and
// the rt_shard/rt_executor forward typedefs; translation units include
// rt_async_internal.h, not this header directly.

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

#endif
