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
    if (header->len > (uint64_t)INT64_MAX) {
        array_panic("array length out of range");
        return NULL;
    }

    int64_t start = 0;
    int64_t end = 0;
    range_bounds((const SurgeRange*)r, (int64_t)header->len, &start, &end);
    if (start > end) {
        start = end;
    }

    uint64_t view_len = (uint64_t)(end - start);
    uint64_t offset = 0;
    if (elem_stride != 0 && (uint64_t)start > UINT64_MAX / elem_stride) {
        array_panic("array slice offset out of range");
        return NULL;
    }
    offset = (uint64_t)start * elem_stride;
    if (header->data == NULL && view_len > 0) {
        array_panic("array slice received null data");
        return NULL;
    }

    SurgeArrayHeader* view = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                         (uint64_t)alignof(SurgeArrayHeader));
    if (view == NULL) {
        array_panic("array allocation failed");
        return NULL;
    }
    view->len = view_len;
    // ponytail: cap UINT64_MAX marks a slice view until array headers grow a view flag.
    view->cap = SURGE_ARRAY_VIEW_CAP;
    view->data = header->data == NULL ? NULL : (uint8_t*)header->data + offset;
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
