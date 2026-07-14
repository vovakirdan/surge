#ifndef SURGE_RUNTIME_NATIVE_RT_ARRAY_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_ARRAY_INTERNAL_H

#include <stdbool.h>
#include <stdint.h>

// Shared array representation (rt_array.c owns the registry; the
// reclamation module consumes it). cap == UINT64_MAX marks a view header;
// typed base arrays count cap in ELEMENTS (data size = cap * stride).
#define SURGE_ARRAY_VIEW_CAP UINT64_MAX

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

typedef struct SurgeArrayViewLink {
    SurgeArrayHeader* base;
    SurgeArrayHeader* view;
    uint64_t byte_offset;
    struct SurgeArrayViewLink* next;
} SurgeArrayViewLink;

// The registry is process-global while tasks mutate it from every worker:
// all reads and writes serialize on this lock (rt_array.c owns it).
void rt_array_registry_lock(void);
void rt_array_registry_unlock(void);

// Registry access for the reclamation module; callers hold the lock.
// CONSTRAINT for every *_locked helper: rt_free re-enters the registry
// (rt_free -> rt_array_forget_allocation -> registry lock), so nothing
// may be rt_free'd while the lock is held — detach under the lock, free
// after releasing it.
const SurgeArrayViewLink* rt_array_views_head_locked(void);
// Detaches the link for `view` (if registered) and returns its base, or
// NULL for an unregistered view (fixed-array slices register no link).
// The detached link lands in *out_link; the caller frees it after
// releasing the registry lock.
SurgeArrayHeader* rt_array_unlink_view_locked(SurgeArrayHeader* view,
                                              SurgeArrayViewLink** out_link);

static inline bool rt_array_header_is_view(const SurgeArrayHeader* header) {
    return header != NULL && header->cap == SURGE_ARRAY_VIEW_CAP;
}

#endif
