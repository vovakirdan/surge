#ifndef SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H
#define SURGE_RUNTIME_NATIVE_RT_FD_REGISTRY_H

#include <stddef.h>
#include <stdint.h>

// Shard-local fd registry: the single durable row per live net fd.
// Ownership and lifecycle:
// - Each rt_shard owns one rt_fd_registry by value; it is initialized with the
//   owning shard in rt_runtime_init_n1 and, like the waiter store and poll
//   scratch, guarded by ex->lock. routes registration-side interest
//   writes through the waiter-store bridge in rt_async_waiter.c; makes
//   the registry the only poll input: poll_net_waiters snapshots rows into the
//   shard poll scratch under ex->lock and never scans the waiter store.
// - A row exists while the runtime owns a live registered net fd, or while an
//   interest-only compatibility row has at least one open net-key waiter parked
//   in the waiter store. After prepare_park, net_wait_current_task verifies its
//   interest row exists and otherwise undoes the park, so a parked open net
//   waiter always has a row and every interested fd is polled. Detaching the
//   last interest flag removes only non-registered compatibility rows; row
//   order is not meaningful and find is a linear scan.
// - generation guards fd-reuse stale wakes; close_state guards post-close
//   interest. Remove-plus-recreate preserves stale-wake safety because new rows
//   take a monotonic generation from next_generation instead of resetting to 0.
// - rt_fd_registry_free releases entry storage. No caller exists today because
//   the process has no executor shutdown path (see the dependency map);
//   create that path and wire the free.
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
    uint8_t registered_open;
    uint8_t want_accept;
    uint8_t want_read;
    uint8_t want_write;
} rt_fd_entry;

typedef struct {
    rt_fd_entry* entries;
    size_t len;
    size_t cap;
    uint64_t next_generation;
    // fd -> entries index dense map (-1 = no row), maintained at the two row
    // mutation points (create/remove) under the owner shard lock. Added by
    // so the per-op stale-handle guard is O(1) instead of the
    // linear scan, which would cost O(live fds) on every read/write.
    int32_t* fd_index;
    size_t fd_index_cap;
} rt_fd_registry;

// Poll-interest snapshot row copied into the shard poll scratch under
// ex->lock. The generation is the fd-lifetime stale-wake guard; accept and
// read remain separate so completion after poll can wake only the interests
// present in the snapshot, while the poll layer still folds them into readable
// readiness.
typedef struct {
    int fd;
    uint64_t generation;
    uint32_t owner_shard_id;
    uint8_t want_accept;
    uint8_t want_read;
    uint8_t want_write;
} rt_fd_poll_interest;

typedef struct {
    int fd;
    uint64_t generation;
    uint32_t owner_shard_id;
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
rt_fd_registry* rt_executor_fd_registry_for_shard(rt_executor* ex, size_t shard_index);
const rt_fd_registry* rt_executor_fd_registry_const_for_shard(const rt_executor* ex,
                                                              size_t shard_index);
rt_fd_registry* rt_executor_fd_registry(rt_executor* ex);
const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex);

rt_runtime_status rt_fd_registry_init(rt_fd_registry* registry);
void rt_fd_registry_free(rt_fd_registry* registry);
rt_runtime_status rt_fd_registry_ensure_cap(rt_fd_registry* registry);
size_t rt_fd_registry_len(const rt_fd_registry* registry);
const rt_fd_entry* rt_fd_registry_find_const(const rt_fd_registry* registry, int fd);
rt_runtime_status rt_fd_registry_register_open_fd(rt_fd_registry* registry, int fd);
// recovery: register and report the row's generation so the
// canonical runtime handle can validate its fd lifetime under the owner lock.
rt_runtime_status rt_fd_registry_register_open_fd_generation(rt_fd_registry* registry,
                                                             int fd,
                                                             uint64_t* out_generation);
// Owner-locked fd-lifetime guard predicate. Returns 1 only when fd has an
// OPEN, registered_open row whose full generation matches the canonical
// runtime handle; 0 for missing rows, closed rows, interest-only rows, and
// generation mismatches.
int rt_fd_registry_handle_open(const rt_fd_registry* registry, int fd, uint64_t generation);
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
rt_fd_completion_summary rt_fd_registry_drain_shutdown_net_waiters_locked_on_owner(
    rt_executor* ex, rt_fd_registry* registry, uint32_t owner_shard_id);
rt_fd_completion_summary
rt_fd_registry_wake_closed_net_waiters(rt_executor* ex, const rt_fd_lifecycle_snapshot* snapshot);
size_t rt_fd_registry_snapshot_poll_interest(const rt_fd_registry* registry,
                                             rt_fd_poll_interest* out,
                                             size_t out_cap);

#endif
