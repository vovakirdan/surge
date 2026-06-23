#include "rt.h"

#include <stdalign.h>
#include <stdint.h>
#include <string.h>

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
        }
        current *= 2;
    }
    return current;
}

void rt_array_append_raw_bytes(void* array_slot, const uint8_t* src, uint64_t len) {
    if (len == 0) {
        return;
    }
    if (array_slot == NULL || src == NULL) {
        array_panic("array append bytes received null pointer");
    }

    SurgeArrayHeader* header = *(SurgeArrayHeader**)array_slot;
    if (header == NULL) {
        array_panic("array append bytes received null array");
    }
    if (len > UINT64_MAX - header->len) {
        array_panic("array length out of range");
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
        }
        header->data = data;
        header->cap = new_cap;
    }
    if (header->data == NULL) {
        array_panic("array append bytes received null data");
    }

    const uint8_t* copy_src = src;
    if (src_offset != UINTPTR_MAX) {
        copy_src = (const uint8_t*)header->data + src_offset;
    }
    rt_memmove((uint8_t*)header->data + old_len, copy_src, len);
    header->len = new_len;
}
