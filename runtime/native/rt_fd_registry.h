#ifndef SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H
#define SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H

#include <stddef.h>
#include <stdint.h>

// Shard-local fd registry: the single durable row per live net fd.
// Ownership and lifecycle:
// - Each rt_shard owns one rt_fd_registry by value; it is initialized with the
//   owning shard in rt_runtime_init_n1 and, like the waiter store and poll
//   scratch, guarded by ex->lock. Task 6 routes registration-side interest
//   writes through the waiter-store bridge in rt_async_waiter.c; Task 7 makes
//   the registry the only poll input: poll_net_waiters snapshots rows into the
//   shard poll scratch under ex->lock and never scans the waiter store.
// - A row exists iff at least one open net-key waiter for that fd is parked in
//   the waiter store, or a close transition is draining the row's last waiters.
//   The fd-registry-attach-miss bridge is resolved in Task 7: after
//   prepare_park, net_wait_current_task verifies its interest row exists
//   (rt_fd_registry_net_interest_present) and otherwise undoes the park and
//   reports spurious readiness, so a parked open net waiter always has a row
//   and every parked open fd is polled. Detaching the last interest flag
//   swap-removes the row; row order is not meaningful and find is a linear scan.
// - generation guards fd-reuse stale wakes; close_state guards post-close
//   interest. Remove-plus-recreate preserves stale-wake safety because new rows
//   take a monotonic generation from next_generation instead of resetting to 0.
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
    uint64_t next_generation;
} rt_fd_registry;

// Poll-interest snapshot row copied into the shard poll scratch under
// ex->lock. The generation is the fd-lifetime stale-wake guard; accept and
// read remain separate so completion after poll can wake only the interests
// present in the snapshot, while the poll layer still folds them into readable
// readiness.
typedef struct {
    int fd;
    uint64_t generation;
    uint8_t want_accept;
    uint8_t want_read;
    uint8_t want_write;
} rt_fd_poll_interest;

typedef struct {
    int fd;
    uint64_t generation;
    uint8_t want_accept;
    uint8_t want_read;
    uint8_t want_write;
} rt_fd_lifecycle_snapshot;

typedef enum {
    RT_FD_COMPLETION_STALE = 0,
    RT_FD_COMPLETION_CURRENT = 1,
} rt_fd_completion_state;

typedef struct {
    uint64_t calls;
    uint64_t woken;
} rt_fd_completion_summary;

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
int rt_fd_registry_net_interest_present(const rt_fd_registry* registry, waker_key key);
rt_runtime_status
rt_fd_registry_mark_closed(rt_fd_registry* registry, int fd, rt_fd_lifecycle_snapshot* out);
rt_fd_completion_state rt_fd_registry_completion_state(const rt_fd_registry* registry,
                                                       const rt_fd_poll_interest* snapshot,
                                                       waker_key key);
rt_fd_completion_summary rt_fd_registry_complete_ready_net_waiters(
    rt_executor* ex, const rt_fd_poll_interest* snapshot, int read_ready, int write_ready);
rt_fd_completion_summary rt_fd_registry_drain_shutdown_net_waiters_locked(rt_executor* ex,
                                                                          rt_fd_registry* registry);
rt_fd_completion_summary
rt_fd_registry_wake_closed_net_waiters(rt_executor* ex, const rt_fd_lifecycle_snapshot* snapshot);
size_t rt_fd_registry_snapshot_poll_interest(const rt_fd_registry* registry,
                                             rt_fd_poll_interest* out,
                                             size_t out_cap);

#endif
