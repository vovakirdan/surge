#include "rt.h"

#include <stdalign.h>
#include <stdbool.h>
#include <stdint.h>
#include <string.h>

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

static SurgeArrayViewLink* array_views = NULL;

static void array_panic(const char* msg) {
    rt_panic_numeric((const uint8_t*)msg, (uint64_t)strlen(msg));
}

static uint64_t array_grow_cap(uint64_t current, uint64_t min_cap) {
    if (current < 1) {
        current = 1;
    }
    while (current < min_cap) {
        if (current > UINT64_MAX / 2) {
            array_panic("array capacity out of range");
            return min_cap;
        }
        current *= 2;
    }
    return current;
}

static bool array_is_view(const SurgeArrayHeader* header) {
    return header != NULL && header->cap == SURGE_ARRAY_VIEW_CAP;
}

bool rt_array_is_view(const void* header) {
    return array_is_view((const SurgeArrayHeader*)header);
}

static const SurgeArrayViewLink* array_find_view(const SurgeArrayHeader* header) {
    for (const SurgeArrayViewLink* link = array_views; link != NULL; link = link->next) {
        if (link->view == header) {
            return link;
        }
    }
    return NULL;
}

static SurgeArrayHeader* array_base_for_slice(SurgeArrayHeader* header, uint64_t* base_offset) {
    *base_offset = 0;
    const SurgeArrayViewLink* link = array_find_view(header);
    if (link == NULL) {
        return header;
    }
    *base_offset = link->byte_offset;
    return link->base;
}

static void
array_register_view(SurgeArrayHeader* base, SurgeArrayHeader* view, uint64_t byte_offset) {
    SurgeArrayViewLink* link = (SurgeArrayViewLink*)rt_alloc((uint64_t)sizeof(SurgeArrayViewLink),
                                                             (uint64_t)alignof(SurgeArrayViewLink));
    if (link == NULL) {
        array_panic("array allocation failed");
        return;
    }
    link->base = base;
    link->view = view;
    link->byte_offset = byte_offset;
    link->next = array_views;
    array_views = link;
}

void rt_array_forget_allocation(const void* ptr) {
    if (ptr == NULL) {
        return;
    }
    const SurgeArrayHeader* header = (const SurgeArrayHeader*)ptr;
    SurgeArrayViewLink** cursor = &array_views;
    while (*cursor != NULL) {
        SurgeArrayViewLink* link = *cursor;
        if (link->view == header || link->base == header) {
            *cursor = link->next;
            rt_free((uint8_t*)link,
                    (uint64_t)sizeof(SurgeArrayViewLink),
                    (uint64_t)alignof(SurgeArrayViewLink));
            continue;
        }
        cursor = &link->next;
    }
}

void rt_array_sync_views(void* array_header) {
    SurgeArrayHeader* base = (SurgeArrayHeader*)array_header;
    if (base == NULL) {
        return;
    }
    for (const SurgeArrayViewLink* link = array_views; link != NULL; link = link->next) {
        if (link->base == base && link->view != NULL) {
            link->view->data = base->data == NULL ? NULL : (uint8_t*)base->data + link->byte_offset;
        }
    }
}

static int64_t normalize_range_index(int64_t n, int64_t length) {
    if (n < 0) {
        if (n < -length) {
            return -1;
        }
        return n + length;
    }
    return n;
}

static int64_t range_index_from_value(void* v, int64_t length) {
    int64_t n = 0;
    if (rt_bigint_to_i64(v, &n)) {
        return normalize_range_index(n, length);
    }
    int cmp = rt_bigint_cmp(v, NULL);
    if (cmp < 0) {
        return -1;
    }
    if (length == INT64_MAX) {
        return length;
    }
    return length + 1;
}

static void range_bounds(const SurgeRange* r, int64_t length, int64_t* start, int64_t* end) {
    int64_t start64 = 0;
    int64_t end64 = length;
    if (r != NULL) {
        if (r->has_start) {
            start64 = range_index_from_value(r->start, length);
        }
        if (r->has_end) {
            end64 = range_index_from_value(r->end, length);
        }
        if (r->inclusive && r->has_end && end64 < INT64_MAX) {
            end64++;
        }
    }
    if (start64 < 0) {
        start64 = 0;
    } else if (start64 > length) {
        start64 = length;
    }
    if (end64 < 0) {
        end64 = 0;
    } else if (end64 > length) {
        end64 = length;
    }
    *start = start64;
    *end = end64;
}

static bool array_slice_bounds(const SurgeRange* r,
                               uint64_t length,
                               uint64_t elem_stride,
                               uint64_t* len_out,
                               uint64_t* byte_offset_out) {
    if (length > (uint64_t)INT64_MAX) {
        array_panic("array length out of range");
        return false;
    }

    int64_t start = 0;
    int64_t end = 0;
    range_bounds(r, (int64_t)length, &start, &end);
    if (start > end) {
        start = end;
    }

    uint64_t start_u = (uint64_t)start;
    if (elem_stride != 0 && start_u > UINT64_MAX / elem_stride) {
        array_panic("array slice offset out of range");
        return false;
    }
    *len_out = (uint64_t)(end - start);
    *byte_offset_out = start_u * elem_stride;
    return true;
}

static SurgeArrayHeader* array_alloc_view(uint64_t len, void* data) {
    SurgeArrayHeader* view = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                         (uint64_t)alignof(SurgeArrayHeader));
    if (view == NULL) {
        array_panic("array allocation failed");
        return NULL;
    }
    view->len = len;
    // ponytail: cap UINT64_MAX marks a slice view until array headers grow a view flag.
    view->cap = SURGE_ARRAY_VIEW_CAP;
    view->data = data;
    return view;
}

void* rt_array_slice(void* array_slot, void* r, uint64_t elem_stride) {
    if (array_slot == NULL) {
        array_panic("array slice received null pointer");
        return NULL;
    }
    SurgeArrayHeader* header = *(SurgeArrayHeader**)array_slot;
    if (header == NULL) {
        array_panic("array slice received null array");
        return NULL;
    }

    uint64_t view_len = 0;
    uint64_t offset = 0;
    if (!array_slice_bounds((const SurgeRange*)r, header->len, elem_stride, &view_len, &offset)) {
        return NULL;
    }
    if (header->data == NULL && view_len > 0) {
        array_panic("array slice received null data");
        return NULL;
    }

    uint64_t base_offset = 0;
    SurgeArrayHeader* base = array_base_for_slice(header, &base_offset);
    if (base_offset > UINT64_MAX - offset) {
        array_panic("array slice offset out of range");
        return NULL;
    }
    uint64_t total_offset = base_offset + offset;
    void* data = base->data == NULL ? NULL : (uint8_t*)base->data + total_offset;
    SurgeArrayHeader* view = array_alloc_view(view_len, data);
    if (view == NULL) {
        return NULL;
    }
    array_register_view(base, view, total_offset);
    return view;
}

void* rt_array_slice_fixed(void* data_slot, void* r, uint64_t length, uint64_t elem_stride) {
    if (data_slot == NULL) {
        array_panic("array slice received null pointer");
        return NULL;
    }
    void* data = *(void**)data_slot;
    uint64_t view_len = 0;
    uint64_t offset = 0;
    if (!array_slice_bounds((const SurgeRange*)r, length, elem_stride, &view_len, &offset)) {
        return NULL;
    }
    if (data == NULL && view_len > 0) {
        array_panic("array slice received null data");
        return NULL;
    }
    SurgeArrayHeader* view =
        array_alloc_view(view_len, data == NULL ? NULL : (uint8_t*)data + offset);
    return view;
}

void rt_array_append_raw_bytes(void* array_slot, const uint8_t* src, uint64_t len) {
    if (len == 0) {
        return;
    }
    if (array_slot == NULL || src == NULL) {
        array_panic("array append bytes received null pointer");
        return;
    }

    SurgeArrayHeader* header = *(SurgeArrayHeader**)array_slot;
    if (header == NULL) {
        array_panic("array append bytes received null array");
        return;
    }
    if (array_is_view(header)) {
        array_panic("array view is not resizable");
        return;
    }
    if (len > UINT64_MAX - header->len) {
        array_panic("array length out of range");
        return;
    }

    uint64_t old_len = header->len;
    uint64_t new_len = old_len + len;
    uintptr_t src_offset = UINTPTR_MAX;
    if (header->data != NULL && old_len > 0) {
        uintptr_t data_addr = (uintptr_t)header->data;
        uintptr_t src_addr = (uintptr_t)src;
        if (src_addr >= data_addr) {
            uintptr_t candidate = src_addr - data_addr;
            if (candidate < old_len) {
                if (len > old_len - candidate) {
                    array_panic("array append bytes source range out of range");
                    return;
                }
                src_offset = candidate;
            }
        }
    }

    if (new_len > header->cap) {
        uint64_t new_cap = array_grow_cap(header->cap, new_len);
        void* data =
            rt_realloc((uint8_t*)header->data, header->cap, new_cap, (uint64_t)alignof(uint8_t));
        if (data == NULL) {
            array_panic("array allocation failed");
            return;
        }
        header->data = data;
        header->cap = new_cap;
        rt_array_sync_views(header);
    }
    if (header->data == NULL) {
        array_panic("array append bytes received null data");
        return;
    }

    const uint8_t* copy_src = src;
    if (src_offset != UINTPTR_MAX) {
        copy_src = (const uint8_t*)header->data + src_offset;
    }
    rt_memmove((uint8_t*)header->data + old_len, copy_src, len);
    header->len = new_len;
}
