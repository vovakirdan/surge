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

typedef struct SurgeBytesView {
    void* owner;
    uint8_t* ptr;
    uint64_t len;
} SurgeBytesView;

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

static uint32_t decode_utf8_at(const uint8_t* data, uint64_t len, uint64_t idx, uint64_t* advance) {
    if (idx >= len) {
        *advance = 0;
        return 0;
    }
    uint8_t c0 = data[idx];
    if (c0 < 0x80) {
        *advance = 1;
        return (uint32_t)c0;
    }
    if (c0 < 0xC2) {
        *advance = 1;
        return 0xFFFD;
    }
    if (c0 <= 0xDF) {
        if (idx + 1 < len && is_cont(data[idx + 1])) {
            uint32_t cp = ((uint32_t)(c0 & 0x1F) << 6) | (uint32_t)(data[idx + 1] & 0x3F);
            *advance = 2;
            return cp;
        }
        *advance = 1;
        return 0xFFFD;
    }
    if (c0 <= 0xEF) {
        if (idx + 2 < len && is_cont(data[idx + 1]) && is_cont(data[idx + 2])) {
            uint8_t c1 = data[idx + 1];
            if ((c0 == 0xE0 && c1 < 0xA0) || (c0 == 0xED && c1 >= 0xA0)) {
                *advance = 1;
                return 0xFFFD;
            }
            uint32_t cp = ((uint32_t)(c0 & 0x0F) << 12) |
                          ((uint32_t)(data[idx + 1] & 0x3F) << 6) |
                          (uint32_t)(data[idx + 2] & 0x3F);
            *advance = 3;
            return cp;
        }
        *advance = 1;
        return 0xFFFD;
    }
    if (c0 <= 0xF4) {
        if (idx + 3 < len && is_cont(data[idx + 1]) && is_cont(data[idx + 2]) && is_cont(data[idx + 3])) {
            uint8_t c1 = data[idx + 1];
            if ((c0 == 0xF0 && c1 < 0x90) || (c0 == 0xF4 && c1 >= 0x90)) {
                *advance = 1;
                return 0xFFFD;
            }
            uint32_t cp = ((uint32_t)(c0 & 0x07) << 18) |
                          ((uint32_t)(data[idx + 1] & 0x3F) << 12) |
                          ((uint32_t)(data[idx + 2] & 0x3F) << 6) |
                          (uint32_t)(data[idx + 3] & 0x3F);
            *advance = 4;
            return cp;
        }
        *advance = 1;
        return 0xFFFD;
    }
    *advance = 1;
    return 0xFFFD;
}

static uint64_t byte_offset_for_cp(const uint8_t* data, uint64_t len, uint64_t target) {
    uint64_t i = 0;
    uint64_t count = 0;
    while (i < len && count < target) {
        uint64_t advance = 1;
        (void)decode_utf8_at(data, len, i, &advance);
        if (advance == 0) {
            break;
        }
        i += advance;
        count++;
    }
    return i;
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

static void range_bounds(const SurgeRange* r, int64_t length, int64_t* start, int64_t* end) {
    int64_t start64 = 0;
    int64_t end64 = length;
    if (r != NULL) {
        if (r->has_start) {
            start64 = normalize_range_index(r->start, length);
        }
        if (r->has_end) {
            end64 = normalize_range_index(r->end, length);
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

uint32_t rt_string_index(void* s, int64_t index) {
    if (s == NULL) {
        rt_panic_bounds(0, index, 0);
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        rt_panic_bounds(0, index, 0);
    }
    int64_t len = (int64_t)str->len_cp;
    int64_t idx = index;
    if (idx < 0) {
        idx += len;
    }
    if (idx < 0 || idx >= len) {
        rt_panic_bounds(0, idx, len);
    }
    uint64_t i = 0;
    uint64_t count = 0;
    while (i < str->len_bytes) {
        uint64_t advance = 1;
        uint32_t cp = decode_utf8_at(str->data, str->len_bytes, i, &advance);
        if (count == (uint64_t)idx) {
            return cp;
        }
        if (advance == 0) {
            break;
        }
        i += advance;
        count++;
    }
    rt_panic_bounds(0, idx, len);
    return 0;
}

void* rt_string_slice(void* s, void* r) {
    if (s == NULL) {
        return rt_string_from_bytes(NULL, 0);
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        return rt_string_from_bytes(NULL, 0);
    }
    int64_t length = (int64_t)str->len_cp;
    if (length < 0) {
        length = 0;
    }
    int64_t start = 0;
    int64_t end = 0;
    range_bounds((const SurgeRange*)r, length, &start, &end);
    if (start > end) {
        start = end;
    }
    uint64_t byte_start = byte_offset_for_cp(str->data, str->len_bytes, (uint64_t)start);
    uint64_t byte_end = byte_offset_for_cp(str->data, str->len_bytes, (uint64_t)end);
    if (byte_end < byte_start) {
        byte_end = byte_start;
    }
    return rt_string_from_bytes(str->data + byte_start, byte_end - byte_start);
}

void* rt_string_bytes_view(void* s) {
    if (s == NULL) {
        return NULL;
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        return NULL;
    }
    SurgeBytesView* view = (SurgeBytesView*)rt_alloc((uint64_t)sizeof(SurgeBytesView), (uint64_t)alignof(SurgeBytesView));
    if (view == NULL) {
        return NULL;
    }
    view->owner = (void*)str;
    view->ptr = str->data;
    view->len = str->len_bytes;
    return (void*)view;
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
