#include "rt.h"
#include "rt_array_internal.h"

#include <pthread.h>
#include <stdalign.h>
#include <stdbool.h>
#include <string.h>

// Owned-array reclamation (drop emission). Views share their base's data
// through the view registry, and view-ness is NOT part of the static type,
// so the runtime arbitrates every array drop:
//  - dropping a VIEW frees the view header and unlinks it; never the data
//    or its elements (the base owns them);
//  - dropping a BASE with live views defers BOTH the data and the header
//    (the registry links point at the header, and the header is the only
//    holder of the data pointer) until the LAST view unlinks;
//  - dropping a base with no views frees data + header immediately.
// A deferred base lives in the orphan list with its data size/alignment
// AND its element-drop plan (function + count + stride) snapshotted at
// drop time, so the freeing view reclaims elements it never had to know.
//
// Element reclamation: rt_array_free_elems runs a per-element drop
// function (arrays of strings/structs) before freeing the data; the
// leaf rt_array_free (copyable elements) passes drop_elem == NULL. The
// element drops re-enter the runtime (rt_free -> registry lock), so they
// always run AFTER the registry lock is released.
//
// The registry lock also closes a pre-existing gap: the view registry is
// process-global and tasks on different workers mutate it concurrently;
// drop emission turns that latent race hot, so every registry touch now
// serializes here (see rt_array_internal.h).

typedef void (*SurgeArrayDropElem)(void*);

typedef struct SurgeArrayOrphan {
    SurgeArrayHeader* base;
    uint64_t data_size;
    uint64_t data_align;
    SurgeArrayDropElem drop_elem;
    uint64_t elem_count;
    uint64_t elem_stride;
    struct SurgeArrayOrphan* next;
} SurgeArrayOrphan;

static SurgeArrayOrphan* array_orphans = NULL;
static uint64_t array_deferred_base_drops = 0;

// Debug observability for the deferred-reclamation float: tests assert the
// counter moved (or stayed put in cleanly nested rows).
uint64_t rt_array_debug_deferred_base_drops(void) {
    rt_array_registry_lock();
    uint64_t value = array_deferred_base_drops;
    rt_array_registry_unlock();
    return value;
}

static void array_free_header(SurgeArrayHeader* header) {
    rt_free(
        (uint8_t*)header, (uint64_t)sizeof(SurgeArrayHeader), (uint64_t)alignof(SurgeArrayHeader));
}

// Reclaims a base's storage: drops each owning element (if a drop plan is
// present), then frees the data buffer and the header. Runs with NO lock
// held — element drops re-enter the registry.
static void array_free_base_storage(SurgeArrayHeader* base,
                                    uint64_t size,
                                    uint64_t align,
                                    SurgeArrayDropElem drop_elem,
                                    uint64_t elem_count,
                                    uint64_t elem_stride) {
    if (drop_elem != NULL && base->data != NULL) {
        for (uint64_t i = 0; i < elem_count; i++) {
            drop_elem((uint8_t*)base->data + i * elem_stride);
        }
    }
    if (base->data != NULL && size > 0) {
        rt_free((uint8_t*)base->data, size, align);
    }
    array_free_header(base);
}

static bool array_base_has_views_locked(const SurgeArrayHeader* base) {
    for (const SurgeArrayViewLink* link = rt_array_views_head_locked(); link != NULL;
         link = link->next) {
        if (link->base == base) {
            return true;
        }
    }
    return false;
}

static void array_orphan_push_locked(SurgeArrayHeader* base,
                                     uint64_t size,
                                     uint64_t align,
                                     SurgeArrayDropElem drop_elem,
                                     uint64_t elem_count,
                                     uint64_t elem_stride) {
    SurgeArrayOrphan* orphan = (SurgeArrayOrphan*)rt_alloc((uint64_t)sizeof(SurgeArrayOrphan),
                                                           (uint64_t)alignof(SurgeArrayOrphan));
    if (orphan == NULL) {
        // Allocation failure while deferring: keep the base alive (leak)
        // rather than corrupt the registry; the census will name it.
        return;
    }
    orphan->base = base;
    orphan->data_size = size;
    orphan->data_align = align;
    orphan->drop_elem = drop_elem;
    orphan->elem_count = elem_count;
    orphan->elem_stride = elem_stride;
    orphan->next = array_orphans;
    array_orphans = orphan;
    array_deferred_base_drops++;
}

// Detaches the orphan record for base when no views remain. Caller holds
// the registry lock and frees the record AND the deferred storage after
// releasing it: rt_free re-enters the registry lock through
// rt_array_forget_allocation, so nothing may be freed while it is held.
static SurgeArrayOrphan* array_orphan_take_locked(const SurgeArrayHeader* base) {
    SurgeArrayOrphan** cursor = &array_orphans;
    while (*cursor != NULL) {
        SurgeArrayOrphan* orphan = *cursor;
        if (orphan->base == base) {
            *cursor = orphan->next;
            orphan->next = NULL;
            return orphan;
        }
        cursor = &orphan->next;
    }
    return NULL;
}

// Drops one owned array value with an optional per-element drop plan. A
// deferred base's plan is snapshotted at ITS drop, so the view side never
// guesses the element layout.
void rt_array_free_elems(void* array_header,
                         uint64_t elem_stride,
                         uint64_t elem_align,
                         void (*drop_elem)(void*)) {
    SurgeArrayHeader* header = (SurgeArrayHeader*)array_header;
    if (header == NULL) {
        return;
    }
    SurgeArrayViewLink* detached_link = NULL;
    SurgeArrayOrphan* taken_orphan = NULL;
    bool free_own_storage = false;
    uint64_t own_size = 0;
    uint64_t own_len = 0;

    rt_array_registry_lock();
    if (rt_array_header_is_view(header)) {
        const SurgeArrayHeader* base = rt_array_unlink_view_locked(header, &detached_link);
        if (base != NULL && !array_base_has_views_locked(base)) {
            taken_orphan = array_orphan_take_locked(base);
        }
    } else {
        if (array_base_has_views_locked(header)) {
            array_orphan_push_locked(
                header, header->cap * elem_stride, elem_align, drop_elem, header->len, elem_stride);
        } else {
            free_own_storage = true;
            own_size = header->cap * elem_stride;
            own_len = header->len;
        }
    }
    rt_array_registry_unlock();

    if (detached_link != NULL) {
        rt_free((uint8_t*)detached_link,
                (uint64_t)sizeof(SurgeArrayViewLink),
                (uint64_t)alignof(SurgeArrayViewLink));
    }
    if (rt_array_header_is_view(header)) {
        // A view owns only its header; the base owns the elements.
        array_free_header(header);
    } else if (free_own_storage) {
        array_free_base_storage(header, own_size, elem_align, drop_elem, own_len, elem_stride);
    }
    if (taken_orphan != NULL) {
        array_free_base_storage(taken_orphan->base,
                                taken_orphan->data_size,
                                taken_orphan->data_align,
                                taken_orphan->drop_elem,
                                taken_orphan->elem_count,
                                taken_orphan->elem_stride);
        rt_free((uint8_t*)taken_orphan,
                (uint64_t)sizeof(SurgeArrayOrphan),
                (uint64_t)alignof(SurgeArrayOrphan));
    }
}

// Leaf reclamation for arrays of copyable elements: no element drop plan.
void rt_array_free(void* array_header, uint64_t elem_stride, uint64_t elem_align) {
    rt_array_free_elems(array_header, elem_stride, elem_align, NULL);
}
