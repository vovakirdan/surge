#include "rt.h"

#include <stdbool.h>
#include <stdlib.h>
#include <string.h>
#include <stdalign.h>

typedef struct SurgeString {
    uint64_t len_cp;
    uint64_t len_bytes;
    uint8_t data[];
} SurgeString;

static int is_cont(uint8_t c) {
    return (c & 0xC0) == 0x80;
}

static uint64_t count_codepoints(const uint8_t* data, uint64_t len) {
    uint64_t count = 0;
    uint64_t i = 0;
    while (i < len) {
        uint8_t c0 = data[i];
        if (c0 < 0x80) {
            i += 1;
            count += 1;
            continue;
        }
        if (c0 < 0xC2) {
            i += 1;
            count += 1;
            continue;
        }
        if (c0 <= 0xDF) {
            if (i + 1 < len && is_cont(data[i + 1])) {
                i += 2;
                count += 1;
                continue;
            }
            i += 1;
            count += 1;
            continue;
        }
        if (c0 <= 0xEF) {
            if (i + 2 < len && is_cont(data[i + 1]) && is_cont(data[i + 2])) {
                uint8_t c1 = data[i + 1];
                if ((c0 == 0xE0 && c1 < 0xA0) || (c0 == 0xED && c1 >= 0xA0)) {
                    i += 1;
                    count += 1;
                    continue;
                }
                i += 3;
                count += 1;
                continue;
            }
            i += 1;
            count += 1;
            continue;
        }
        if (c0 <= 0xF4) {
            if (i + 3 < len && is_cont(data[i + 1]) && is_cont(data[i + 2]) && is_cont(data[i + 3])) {
                uint8_t c1 = data[i + 1];
                if ((c0 == 0xF0 && c1 < 0x90) || (c0 == 0xF4 && c1 >= 0x90)) {
                    i += 1;
                    count += 1;
                    continue;
                }
                i += 4;
                count += 1;
                continue;
            }
            i += 1;
            count += 1;
            continue;
        }
        i += 1;
        count += 1;
    }
    return count;
}

void* rt_string_from_bytes(const uint8_t* ptr, uint64_t len) {
    uint64_t bytes = len;
    uint64_t count = 0;
    if (ptr != NULL && len > 0) {
        count = count_codepoints(ptr, len);
    }
    // TODO: apply NFC normalization once a lightweight C implementation is available.
    size_t total = sizeof(SurgeString) + (size_t)bytes + 1;
    SurgeString* s = (SurgeString*)rt_alloc((uint64_t)total, (uint64_t)alignof(SurgeString));
    if (s == NULL) {
        return NULL;
    }
    s->len_cp = count;
    s->len_bytes = bytes;
    if (bytes > 0 && ptr != NULL) {
        rt_memcpy(s->data, ptr, bytes);
    }
    s->data[bytes] = 0;
    return (void*)s;
}

uint8_t* rt_string_ptr(void* s) {
    if (s == NULL) {
        return NULL;
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        return NULL;
    }
    return str->data;
}

uint64_t rt_string_len(void* s) {
    if (s == NULL) {
        return 0;
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        return 0;
    }
    return str->len_cp;
}

uint64_t rt_string_len_bytes(void* s) {
    if (s == NULL) {
        return 0;
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        return 0;
    }
    return str->len_bytes;
}

void* rt_string_concat(void* a, void* b) {
    SurgeString* left = NULL;
    SurgeString* right = NULL;
    if (a != NULL) {
        left = *(SurgeString**)a;
    }
    if (b != NULL) {
        right = *(SurgeString**)b;
    }
    uint64_t left_bytes = left ? left->len_bytes : 0;
    uint64_t right_bytes = right ? right->len_bytes : 0;
    uint64_t left_cp = left ? left->len_cp : 0;
    uint64_t right_cp = right ? right->len_cp : 0;

    uint64_t total_bytes = left_bytes + right_bytes;
    uint64_t total_cp = left_cp + right_cp;
    size_t total = sizeof(SurgeString) + (size_t)total_bytes + 1;
    SurgeString* out = (SurgeString*)rt_alloc((uint64_t)total, (uint64_t)alignof(SurgeString));
    if (out == NULL) {
        return NULL;
    }
    out->len_cp = total_cp;
    out->len_bytes = total_bytes;
    if (left_bytes > 0 && left != NULL) {
        rt_memcpy(out->data, left->data, left_bytes);
    }
    if (right_bytes > 0 && right != NULL) {
        rt_memcpy(out->data + left_bytes, right->data, right_bytes);
    }
    out->data[total_bytes] = 0;
    return (void*)out;
}

bool rt_string_eq(void* a, void* b) {
    SurgeString* left = NULL;
    SurgeString* right = NULL;
    if (a != NULL) {
        left = *(SurgeString**)a;
    }
    if (b != NULL) {
        right = *(SurgeString**)b;
    }
    if (left == right) {
        return true;
    }
    if (left == NULL || right == NULL) {
        return false;
    }
    if (left->len_bytes != right->len_bytes) {
        return false;
    }
    if (left->len_bytes == 0) {
        return true;
    }
    return memcmp(left->data, right->data, (size_t)left->len_bytes) == 0;
}
