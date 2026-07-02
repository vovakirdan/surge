#include "rt_async_internal.h"

// Task 5 skeleton: lifecycle and read-only queries only. No runtime path
// mutates entries yet; Tasks 6-9 add interest/close mutation under ex->lock.

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
