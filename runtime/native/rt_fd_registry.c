#include "rt_async_internal.h"

// Lifecycle, read-only queries, registration-side interest mutation, close
// transitions, and poll-input reads. All runtime access runs under ex->lock;
// mutation flows through the waiter-store bridge in rt_async_waiter.c and
// poll_net_waiters reads only via the generation-bearing snapshot.

rt_runtime_status rt_fd_registry_init(rt_fd_registry* registry) {
    if (registry == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    memset(registry, 0, sizeof(*registry));
    return RT_RUNTIME_STATUS_OK;
}

void rt_fd_registry_free(rt_fd_registry* registry) {
    if (registry == NULL) {
        return;
    }
    if (registry->entries != NULL) {
        rt_free((uint8_t*)registry->entries,
                (uint64_t)(registry->cap * sizeof(rt_fd_entry)),
                _Alignof(rt_fd_entry));
    }
    memset(registry, 0, sizeof(*registry));
}

// Growth mirrors rt_waiter_store_ensure_cap: start at 16 rows, double with
// overflow guards, rt_realloc moves ownership of the entry array.
rt_runtime_status rt_fd_registry_ensure_cap(rt_fd_registry* registry) {
    if (registry == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    if (registry->len < registry->cap) {
        return RT_RUNTIME_STATUS_OK;
    }
    size_t next_cap = 16;
    if (registry->cap != 0) {
        if (registry->cap > SIZE_MAX / 2U) {
            return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
        }
        next_cap = registry->cap * 2U;
    }
    if (registry->cap > SIZE_MAX / sizeof(rt_fd_entry) ||
        next_cap > SIZE_MAX / sizeof(rt_fd_entry)) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    size_t old_size = registry->cap * sizeof(rt_fd_entry);
    size_t new_size = next_cap * sizeof(rt_fd_entry);
    rt_fd_entry* next = (rt_fd_entry*)rt_realloc(
        (uint8_t*)registry->entries, (uint64_t)old_size, (uint64_t)new_size, _Alignof(rt_fd_entry));
    if (next == NULL) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    registry->entries = next;
    registry->cap = next_cap;
    return RT_RUNTIME_STATUS_OK;
}

size_t rt_fd_registry_len(const rt_fd_registry* registry) {
    return registry != NULL ? registry->len : 0;
}

const rt_fd_entry* rt_fd_registry_find_const(const rt_fd_registry* registry, int fd) {
    if (registry == NULL) {
        return NULL;
    }
    for (size_t i = 0; i < registry->len; i++) {
        if (registry->entries[i].fd == fd) {
            return &registry->entries[i];
        }
    }
    return NULL;
}

static rt_fd_entry* fd_registry_find_mut(rt_fd_registry* registry, int fd) {
    for (size_t i = 0; i < registry->len; i++) {
        if (registry->entries[i].fd == fd) {
            return &registry->entries[i];
        }
    }
    return NULL;
}

static void fd_lifecycle_snapshot_clear(rt_fd_lifecycle_snapshot* out, int fd) {
    if (out == NULL) {
        return;
    }
    memset(out, 0, sizeof(*out));
    out->fd = fd;
}

// Maps a net waker kind to the entry's interest flag. Net keys are the only
// callers; non-net kinds return NULL so both mutators can reject them as
// invalid arguments.
static uint8_t* fd_entry_interest_slot(rt_fd_entry* entry, uint8_t kind) {
    switch ((waker_kind)kind) {
        case WAKER_NET_ACCEPT:
            return &entry->want_accept;
        case WAKER_NET_READ:
            return &entry->want_read;
        case WAKER_NET_WRITE:
            return &entry->want_write;
        default:
            return NULL;
    }
}

// Read-only twin of fd_entry_interest_slot for const queries.
static int fd_entry_interest_value(const rt_fd_entry* entry, uint8_t kind) {
    switch ((waker_kind)kind) {
        case WAKER_NET_ACCEPT:
            return entry->want_accept != 0;
        case WAKER_NET_READ:
            return entry->want_read != 0;
        case WAKER_NET_WRITE:
            return entry->want_write != 0;
        default:
            return 0;
    }
}

static int fd_poll_interest_value(const rt_fd_poll_interest* snapshot, uint8_t kind) {
    if (snapshot == NULL) {
        return 0;
    }
    switch ((waker_kind)kind) {
        case WAKER_NET_ACCEPT:
            return snapshot->want_accept != 0;
        case WAKER_NET_READ:
            return snapshot->want_read != 0;
        case WAKER_NET_WRITE:
            return snapshot->want_write != 0;
        default:
            return 0;
    }
}

// Does the row for key's fd exist and carry key's interest kind? Task 7 uses
// this after prepare_park to resolve the fd-registry-attach-miss bridge: a
// parked net waiter whose attach failed would never be polled once the poll
// set derives from registry rows, so the caller undoes the park instead.
int rt_fd_registry_net_interest_present(const rt_fd_registry* registry, waker_key key) {
    if (registry == NULL || !waker_valid(key) || !waker_is_net(key)) {
        return 0;
    }
    const rt_fd_entry* entry = rt_fd_registry_find_const(registry, (int)key.id);
    if (entry == NULL || entry->close_state != RT_FD_CLOSE_STATE_OPEN) {
        return 0;
    }
    return fd_entry_interest_value(entry, key.kind);
}

// Registration-side attach: find or create the owning fd row and set the
// interest flag for the key's kind. Idempotent for duplicate same-key waiters
// (flags, not counts: the waiter store decides when the last waiter leaves).
// Creation assigns a monotonic generation from the registry. Allocation can
// fail only when a new row is needed; generation exhaustion is reported as
// allocation failure because the generation space is a finite registry
// resource and rt_runtime_status has no narrower code today.
rt_runtime_status rt_fd_registry_attach_net_interest(rt_fd_registry* registry, waker_key key) {
    if (registry == NULL || !waker_valid(key) || !waker_is_net(key)) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    int fd = (int)key.id;
    if (fd < 0 || (uint64_t)fd != key.id) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_fd_entry* entry = fd_registry_find_mut(registry, fd);
    if (entry == NULL) {
        if (registry->next_generation == UINT64_MAX) {
            return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
        }
        rt_runtime_status status = rt_fd_registry_ensure_cap(registry);
        if (status != RT_RUNTIME_STATUS_OK) {
            return status;
        }
        entry = &registry->entries[registry->len++];
        memset(entry, 0, sizeof(*entry));
        entry->fd = fd;
        registry->next_generation++;
        entry->generation = registry->next_generation;
    }
    if (entry->close_state != RT_FD_CLOSE_STATE_OPEN) {
        return RT_RUNTIME_STATUS_OK;
    }
    uint8_t* slot = fd_entry_interest_slot(entry, key.kind);
    if (slot == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    *slot = 1;
    return RT_RUNTIME_STATUS_OK;
}

// Detach after the waiter store proved no waiter of this key remains. A
// missing row is a legal no-op (fd-registry-attach-miss bridge). Clearing the
// last interest flag swap-removes the row. Recreate uses the registry's
// monotonic generation, so stale snapshots from a removed row cannot match the
// next row that reuses the same numeric fd.
void rt_fd_registry_detach_net_interest(rt_fd_registry* registry, waker_key key) {
    if (registry == NULL || !waker_valid(key) || !waker_is_net(key)) {
        return;
    }
    rt_fd_entry* entry = fd_registry_find_mut(registry, (int)key.id);
    if (entry == NULL) {
        return;
    }
    uint8_t* slot = fd_entry_interest_slot(entry, key.kind);
    if (slot == NULL) {
        return;
    }
    *slot = 0;
    if (entry->want_accept == 0 && entry->want_read == 0 && entry->want_write == 0) {
        *entry = registry->entries[registry->len - 1];
        registry->len--;
    }
}

rt_runtime_status
rt_fd_registry_mark_closed(rt_fd_registry* registry, int fd, rt_fd_lifecycle_snapshot* out) {
    if (registry == NULL || out == NULL || fd < 0) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    fd_lifecycle_snapshot_clear(out, fd);
    rt_fd_entry* entry = fd_registry_find_mut(registry, fd);
    if (entry == NULL) {
        return RT_RUNTIME_STATUS_OK;
    }
    out->generation = entry->generation;
    out->want_accept = entry->want_accept;
    out->want_read = entry->want_read;
    out->want_write = entry->want_write;
    entry->close_state = RT_FD_CLOSE_STATE_CLOSED;
    return RT_RUNTIME_STATUS_OK;
}

rt_fd_completion_state rt_fd_registry_completion_state(const rt_fd_registry* registry,
                                                       const rt_fd_poll_interest* snapshot,
                                                       waker_key key) {
    if (registry == NULL || snapshot == NULL || !waker_valid(key) || !waker_is_net(key)) {
        return RT_FD_COMPLETION_STALE;
    }
    if (snapshot->fd < 0 || (uint64_t)snapshot->fd != key.id) {
        return RT_FD_COMPLETION_STALE;
    }
    if (!fd_poll_interest_value(snapshot, key.kind)) {
        return RT_FD_COMPLETION_STALE;
    }
    const rt_fd_entry* entry = rt_fd_registry_find_const(registry, snapshot->fd);
    if (entry == NULL || entry->close_state != RT_FD_CLOSE_STATE_OPEN) {
        return RT_FD_COMPLETION_STALE;
    }
    if (entry->generation != snapshot->generation) {
        return RT_FD_COMPLETION_STALE;
    }
    if (!fd_entry_interest_value(entry, key.kind)) {
        return RT_FD_COMPLETION_STALE;
    }
    return RT_FD_COMPLETION_CURRENT;
}

static void fd_completion_add(rt_fd_completion_summary* summary, rt_waiter_completion completion) {
    if (summary == NULL) {
        return;
    }
    summary->calls++;
    summary->woken += (uint64_t)completion.woken;
}

static void fd_complete_net_key(rt_executor* ex, waker_key key, rt_fd_completion_summary* summary) {
    if (ex == NULL || summary == NULL || !waker_valid(key) || !waker_is_net(key)) {
        return;
    }
    fd_completion_add(summary, rt_executor_wake_net_waiters_for_key(ex, key));
}

static void fd_complete_current_net_key(rt_executor* ex,
                                        const rt_fd_registry* registry,
                                        const rt_fd_poll_interest* snapshot,
                                        waker_key key,
                                        rt_fd_completion_summary* summary) {
    if (rt_fd_registry_completion_state(registry, snapshot, key) != RT_FD_COMPLETION_CURRENT) {
        return;
    }
    fd_complete_net_key(ex, key, summary);
}

rt_fd_completion_summary rt_fd_registry_complete_ready_net_waiters(
    rt_executor* ex, const rt_fd_poll_interest* snapshot, int read_ready, int write_ready) {
    rt_fd_completion_summary summary = {0, 0};
    if (ex == NULL || snapshot == NULL) {
        return summary;
    }
    const rt_fd_registry* registry = rt_executor_fd_registry_const(ex);
    if (read_ready) {
        fd_complete_current_net_key(ex, registry, snapshot, net_read_key(snapshot->fd), &summary);
        fd_complete_current_net_key(ex, registry, snapshot, net_accept_key(snapshot->fd), &summary);
    }
    if (write_ready) {
        fd_complete_current_net_key(ex, registry, snapshot, net_write_key(snapshot->fd), &summary);
    }
    return summary;
}

static int fd_lifecycle_snapshot_has_interest(const rt_fd_lifecycle_snapshot* snapshot) {
    return snapshot != NULL &&
           (snapshot->want_accept != 0 || snapshot->want_read != 0 || snapshot->want_write != 0);
}

rt_fd_completion_summary
rt_fd_registry_wake_closed_net_waiters(rt_executor* ex, const rt_fd_lifecycle_snapshot* snapshot) {
    rt_fd_completion_summary summary = {0, 0};
    if (ex == NULL || !fd_lifecycle_snapshot_has_interest(snapshot)) {
        return summary;
    }
    rt_lock(ex);
    if (snapshot->want_read != 0) {
        fd_complete_net_key(ex, net_read_key(snapshot->fd), &summary);
    }
    if (snapshot->want_accept != 0) {
        fd_complete_net_key(ex, net_accept_key(snapshot->fd), &summary);
    }
    if (snapshot->want_write != 0) {
        fd_complete_net_key(ex, net_write_key(snapshot->fd), &summary);
    }
    if (summary.calls != 0) {
        rt_net_wake_poll();
        pthread_cond_broadcast(&ex->io_cv);
    }
    rt_unlock(ex);
    return summary;
}

// Task 7 poll input: copy one poll-interest row per registry entry into the
// caller's scratch under ex->lock. The copy is the poll snapshot: poll() and
// completion run against it after ex->lock is released, so rows mutated by
// other workers during an in-flight poll cannot change the snapshot. Closed
// rows and zero-interest rows are skipped; rows are unique per fd by
// construction, so there is no dedup pass.
size_t rt_fd_registry_snapshot_poll_interest(const rt_fd_registry* registry,
                                             rt_fd_poll_interest* out,
                                             size_t out_cap) {
    if (registry == NULL || out == NULL) {
        return 0;
    }
    size_t count = 0;
    for (size_t i = 0; i < registry->len && count < out_cap; i++) {
        const rt_fd_entry* entry = &registry->entries[i];
        if (entry->close_state != RT_FD_CLOSE_STATE_OPEN) {
            continue;
        }
        uint8_t want_accept = entry->want_accept != 0 ? 1 : 0;
        uint8_t want_read = entry->want_read != 0 ? 1 : 0;
        uint8_t want_write = entry->want_write != 0 ? 1 : 0;
        if (want_accept == 0 && want_read == 0 && want_write == 0) {
            continue;
        }
        out[count].fd = entry->fd;
        out[count].generation = entry->generation;
        out[count].want_accept = want_accept;
        out[count].want_read = want_read;
        out[count].want_write = want_write;
        count++;
    }
    return count;
}
