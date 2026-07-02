#include "rt_async_internal.h"

// Lifecycle, read-only queries, and Task 6 registration-side interest
// mutation. All mutation runs under ex->lock through the waiter-store bridge
// in rt_async_waiter.c; nothing reads entries until Task 7 builds the poll
// set from the registry. Close/generation behavior is Task 9.

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

// Registration-side attach: find or create the owning fd row and set the
// interest flag for the key's kind. Idempotent for duplicate same-key waiters
// (flags, not counts: the waiter store decides when the last waiter leaves).
// Creation zeroes the row (generation 0, close_state OPEN); Task 9 owns both
// fields. Allocation can fail only when a new row is needed.
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
        rt_runtime_status status = rt_fd_registry_ensure_cap(registry);
        if (status != RT_RUNTIME_STATUS_OK) {
            return status;
        }
        entry = &registry->entries[registry->len++];
        memset(entry, 0, sizeof(*entry));
        entry->fd = fd;
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
// last interest flag swap-removes the row: a row exists iff some net waiter
// for that fd is parked. Removal resets generation on recreate; Task 9 owns
// generation/close row lifetime.
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
