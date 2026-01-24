#include "rt.h"

#include <limits.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

typedef struct SurgeMapEntry {
    uint64_t key;
    uint64_t value;
} SurgeMapEntry;

typedef struct SurgeMap {
    uint64_t len;
    uint64_t cap;
    uint64_t key_kind;
    SurgeMapEntry* entries;
} SurgeMap;

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

enum {
    MAP_KEY_STRING = 1,
    MAP_KEY_INT = 2,
    MAP_KEY_UINT = 3,
    MAP_KEY_BIGINT = 4,
    MAP_KEY_BIGUINT = 5,
};

static void map_panic(const char* msg) {
    rt_panic_numeric((const uint8_t*)msg, (uint64_t)strlen(msg));
}

static uint64_t map_round_up(uint64_t size, uint64_t align) {
    if (align <= 1) {
        return size;
    }
    uint64_t rem = size % align;
    if (rem == 0) {
        return size;
    }
    uint64_t add = align - rem;
    if (size > UINT64_MAX - add) {
        return UINT64_MAX;
    }
    return size + add;
}

static bool map_key_eq(const SurgeMap* map, uint64_t key_bits, uint64_t entry_bits) {
    if (map == NULL) {
        return false;
    }
    switch (map->key_kind) {
        case MAP_KEY_STRING: {
            void* left = (void*)(uintptr_t)key_bits;
            void* right = (void*)(uintptr_t)entry_bits;
            return rt_string_eq((void*)&left, (void*)&right);
        }
        case MAP_KEY_INT:
        case MAP_KEY_UINT:
            return key_bits == entry_bits;
        case MAP_KEY_BIGINT: {
            void* left = (void*)(uintptr_t)key_bits;
            void* right = (void*)(uintptr_t)entry_bits;
            return rt_bigint_cmp(left, right) == 0;
        }
        case MAP_KEY_BIGUINT: {
            void* left = (void*)(uintptr_t)key_bits;
            void* right = (void*)(uintptr_t)entry_bits;
            return rt_biguint_cmp(left, right) == 0;
        }
        default:
            map_panic("map: unsupported key kind");
            return false;
    }
}

static bool map_find(const SurgeMap* map, uint64_t key_bits, uint64_t* out_idx) {
    if (map == NULL) {
        return false;
    }
    for (uint64_t i = 0; i < map->len; i++) {
        if (map_key_eq(map, key_bits, map->entries[i].key)) {
            if (out_idx != NULL) {
                *out_idx = i;
            }
            return true;
        }
    }
    return false;
}

static void map_ensure_capacity(SurgeMap* map, uint64_t needed) {
    if (map == NULL) {
        map_panic("map: null handle");
        return;
    }
    if (needed <= map->cap) {
        return;
    }
    uint64_t new_cap = map->cap;
    if (new_cap == 0) {
        new_cap = 4;
    } else if (new_cap > UINT64_MAX / 2) {
        new_cap = needed;
    } else {
        new_cap *= 2;
    }
    if (new_cap < needed) {
        new_cap = needed;
    }
    uint64_t entry_size = (uint64_t)sizeof(SurgeMapEntry);
    if (new_cap > UINT64_MAX / entry_size) {
        map_panic("map capacity overflow");
    }
    uint64_t new_size = new_cap * entry_size;
    uint64_t old_size = map->cap * entry_size;
    void* next = NULL;
    if (map->entries == NULL) {
        next = rt_alloc(new_size, (uint64_t)alignof(SurgeMapEntry));
    } else {
        next = rt_realloc(
            (uint8_t*)map->entries, old_size, new_size, (uint64_t)alignof(SurgeMapEntry));
    }
    if (next == NULL) {
        map_panic("map allocation failed");
        return;
    }
    map->entries = (SurgeMapEntry*)next;
    map->cap = new_cap;
}

void* rt_map_new(uint64_t key_kind) {
    switch (key_kind) {
        case MAP_KEY_STRING:
        case MAP_KEY_INT:
        case MAP_KEY_UINT:
        case MAP_KEY_BIGINT:
        case MAP_KEY_BIGUINT:
            break;
        default:
            map_panic("map: unsupported key kind");
            return NULL;
    }
    SurgeMap* map = (SurgeMap*)rt_alloc((uint64_t)sizeof(SurgeMap), (uint64_t)alignof(SurgeMap));
    if (map == NULL) {
        map_panic("map allocation failed");
        return NULL;
    }
    map->len = 0;
    map->cap = 0;
    map->key_kind = key_kind;
    map->entries = NULL;
    return (void*)map;
}

uint64_t rt_map_len(const void* map_ptr) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return 0;
    }
    const SurgeMap* map = (const SurgeMap*)map_ptr;
    return map->len;
}

bool rt_map_contains(const void* map_ptr, uint64_t key_bits) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    const SurgeMap* map = (const SurgeMap*)map_ptr;
    return map_find(map, key_bits, NULL);
}

bool rt_map_get_ref(void* map_ptr, uint64_t key_bits, uint64_t* out_bits) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    SurgeMap* map = (SurgeMap*)map_ptr;
    uint64_t idx = 0;
    if (!map_find(map, key_bits, &idx)) {
        return false;
    }
    if (out_bits != NULL) {
        *out_bits = (uint64_t)(uintptr_t)&map->entries[idx].value;
    }
    return true;
}

bool rt_map_get_mut(void* map_ptr, uint64_t key_bits, uint64_t* out_bits) {
    return rt_map_get_ref(map_ptr, key_bits, out_bits);
}

bool rt_map_insert(void* map_ptr, uint64_t key_bits, uint64_t value_bits, uint64_t* out_prev) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    SurgeMap* map = (SurgeMap*)map_ptr;
    uint64_t idx = 0;
    if (map_find(map, key_bits, &idx)) {
        if (out_prev != NULL) {
            *out_prev = map->entries[idx].value;
        }
        map->entries[idx].value = value_bits;
        return true;
    }
    if (map->len == UINT64_MAX) {
        map_panic("map length overflow");
    }
    map_ensure_capacity(map, map->len + 1);
    map->entries[map->len].key = key_bits;
    map->entries[map->len].value = value_bits;
    map->len += 1;
    return false;
}

bool rt_map_remove(void* map_ptr, uint64_t key_bits, uint64_t* out_prev) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return false;
    }
    SurgeMap* map = (SurgeMap*)map_ptr;
    uint64_t idx = 0;
    if (!map_find(map, key_bits, &idx)) {
        return false;
    }
    if (out_prev != NULL) {
        *out_prev = map->entries[idx].value;
    }
    uint64_t last = map->len - 1;
    if (idx != last) {
        map->entries[idx] = map->entries[last];
    }
    map->entries[last].key = 0;
    map->entries[last].value = 0;
    map->len = last;
    return true;
}

void* rt_map_keys(const void* map_ptr, uint64_t elem_size, uint64_t elem_align) {
    if (map_ptr == NULL) {
        map_panic("map: null handle");
        return NULL;
    }
    const SurgeMap* map = (const SurgeMap*)map_ptr;
    if (elem_size == 0) {
        elem_size = 1;
    }
    if (elem_align == 0) {
        elem_align = 1;
    }
    if (elem_size > 8) {
        map_panic("map keys element too large");
    }
    uint64_t stride = map_round_up(elem_size, elem_align);
    if (stride == 0 || stride == UINT64_MAX) {
        map_panic("map keys stride overflow");
    }
    if (map->len > 0 && stride > UINT64_MAX / map->len) {
        map_panic("map keys size overflow");
    }
    uint64_t data_size = stride * map->len;
    void* data = NULL;
    if (data_size > 0) {
        if (data_size > (uint64_t)SIZE_MAX) {
            map_panic("map keys size overflow");
        }
        data = rt_alloc(data_size, elem_align);
        if (data == NULL) {
            map_panic("map keys allocation failed");
        }
    }
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        map_panic("map keys allocation failed");
        return NULL;
    }
    header->len = map->len;
    header->cap = map->len;
    header->data = data;
    if (data != NULL && map->len > 0) {
        uint8_t* bytes = (uint8_t*)data;
        size_t stride_size = (size_t)stride;
        for (uint64_t i = 0; i < map->len; i++) {
            uint64_t key_bits = map->entries[i].key;
            size_t offset = (size_t)i * stride_size;
            uint8_t* slot = bytes + offset;
            if (elem_size == 8) {
                memcpy(slot, &key_bits, sizeof(key_bits));
            } else {
                memcpy(slot, &key_bits, (size_t)elem_size);
            }
        }
    }
    return (void*)header;
}
