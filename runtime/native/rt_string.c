#include "rt.h"

#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <stdbool.h>
#include <stdio.h>
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
    void* len;
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
    view->len = rt_biguint_from_u64(str->len_bytes);
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

void* rt_string_repeat(void* s, int64_t count) {
    if (count <= 0) {
        return rt_string_from_bytes(NULL, 0);
    }
    if (s == NULL) {
        return rt_string_from_bytes(NULL, 0);
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        return rt_string_from_bytes(NULL, 0);
    }
    uint64_t unit_bytes = str->len_bytes;
    uint64_t unit_cp = str->len_cp;
    if (unit_bytes == 0 || unit_cp == 0) {
        return rt_string_from_bytes(NULL, 0);
    }
    int64_t max_int = INT64_MAX;
    if (unit_bytes > 0 && count > 0 && (uint64_t)count > (uint64_t)(max_int / (int64_t)unit_bytes)) {
        const char* msg = "string repeat length out of range";
        rt_panic_numeric((const uint8_t*)msg, (uint64_t)strlen(msg));
    }
    if (unit_cp > 0 && count > 0 && (uint64_t)count > (uint64_t)(max_int / (int64_t)unit_cp)) {
        const char* msg = "string repeat length out of range";
        rt_panic_numeric((const uint8_t*)msg, (uint64_t)strlen(msg));
    }
    uint64_t total_bytes = unit_bytes * (uint64_t)count;
    uint64_t total_cp = unit_cp * (uint64_t)count;
    size_t total = sizeof(SurgeString) + (size_t)total_bytes + 1;
    SurgeString* out = (SurgeString*)rt_alloc((uint64_t)total, (uint64_t)alignof(SurgeString));
    if (out == NULL) {
        return NULL;
    }
    out->len_cp = total_cp;
    out->len_bytes = total_bytes;
    for (int64_t i = 0; i < count; i++) {
        uint64_t offset = (uint64_t)i * unit_bytes;
        rt_memcpy(out->data + offset, str->data, unit_bytes);
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

static bool string_span(void* s, const char** out_ptr, size_t* out_len) {
    if (out_ptr == NULL || out_len == NULL) {
        return false;
    }
    *out_ptr = NULL;
    *out_len = 0;
    if (s == NULL) {
        return false;
    }
    SurgeString* str = *(SurgeString**)s;
    if (str == NULL) {
        return false;
    }
    *out_ptr = (const char*)str->data;
    *out_len = (size_t)str->len_bytes;
    return true;
}

static void trim_span(const char* data, size_t len, size_t* start, size_t* end) {
    size_t i = 0;
    size_t j = len;
    while (i < j && isspace((unsigned char)data[i])) {
        i++;
    }
    while (j > i && isspace((unsigned char)data[j - 1])) {
        j--;
    }
    *start = i;
    *end = j;
}

void* rt_string_from_int(int64_t value) {
    char buf[32];
    int n = snprintf(buf, sizeof(buf), "%" PRId64, value);
    if (n < 0) {
        return rt_string_from_bytes(NULL, 0);
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    return rt_string_from_bytes((const uint8_t*)buf, (uint64_t)n);
}

void* rt_string_from_uint(uint64_t value) {
    char buf[32];
    int n = snprintf(buf, sizeof(buf), "%" PRIu64, value);
    if (n < 0) {
        return rt_string_from_bytes(NULL, 0);
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    return rt_string_from_bytes((const uint8_t*)buf, (uint64_t)n);
}

void* rt_string_from_float(double value) {
    char buf[64];
    int n = snprintf(buf, sizeof(buf), "%.17g", value);
    if (n < 0) {
        return rt_string_from_bytes(NULL, 0);
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    return rt_string_from_bytes((const uint8_t*)buf, (uint64_t)n);
}

bool rt_parse_int(void* s, int64_t* out) {
    const char* data = NULL;
    size_t len = 0;
    if (!string_span(s, &data, &len)) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    size_t start = 0;
    size_t end = len;
    trim_span(data, len, &start, &end);
    if (start >= end) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    size_t n = end - start;
    char* buf = (char*)malloc(n + 1);
    if (buf == NULL) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    memcpy(buf, data + start, n);
    buf[n] = 0;
    errno = 0;
    char* endptr = NULL;
    long long val = strtoll(buf, &endptr, 10);
    bool ok = !(errno != 0 || endptr == buf || *endptr != 0);
    if (out != NULL) {
        *out = ok ? (int64_t)val : 0;
    }
    free(buf);
    return ok;
}

bool rt_parse_uint(void* s, uint64_t* out) {
    const char* data = NULL;
    size_t len = 0;
    if (!string_span(s, &data, &len)) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    size_t start = 0;
    size_t end = len;
    trim_span(data, len, &start, &end);
    if (start >= end) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    size_t n = end - start;
    char* buf = (char*)malloc(n + 1);
    if (buf == NULL) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    memcpy(buf, data + start, n);
    buf[n] = 0;
    if (buf[0] == '-') {
        if (out != NULL) {
            *out = 0;
        }
        free(buf);
        return false;
    }
    errno = 0;
    char* endptr = NULL;
    unsigned long long val = strtoull(buf, &endptr, 10);
    bool ok = !(errno != 0 || endptr == buf || *endptr != 0);
    if (out != NULL) {
        *out = ok ? (uint64_t)val : 0;
    }
    free(buf);
    return ok;
}

bool rt_parse_float(void* s, double* out) {
    const char* data = NULL;
    size_t len = 0;
    if (!string_span(s, &data, &len)) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    size_t start = 0;
    size_t end = len;
    trim_span(data, len, &start, &end);
    if (start >= end) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    size_t n = end - start;
    char* buf = (char*)malloc(n + 1);
    if (buf == NULL) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    memcpy(buf, data + start, n);
    buf[n] = 0;
    errno = 0;
    char* endptr = NULL;
    double val = strtod(buf, &endptr);
    bool ok = !(errno != 0 || endptr == buf || *endptr != 0);
    if (out != NULL) {
        *out = ok ? val : 0;
    }
    free(buf);
    return ok;
}

bool rt_parse_bool(void* s, uint8_t* out) {
    const char* data = NULL;
    size_t len = 0;
    if (!string_span(s, &data, &len)) {
        if (out != NULL) {
            *out = 0;
        }
        return false;
    }
    size_t start = 0;
    size_t end = len;
    trim_span(data, len, &start, &end);
    size_t n = end > start ? end - start : 0;
    if (n == 1) {
        if (data[start] == '0') {
            if (out != NULL) {
                *out = 0;
            }
            return true;
        }
        if (data[start] == '1') {
            if (out != NULL) {
                *out = 1;
            }
            return true;
        }
    }
    if (n == 4) {
        if ((data[start] | 0x20) == 't' &&
            (data[start + 1] | 0x20) == 'r' &&
            (data[start + 2] | 0x20) == 'u' &&
            (data[start + 3] | 0x20) == 'e') {
            if (out != NULL) {
                *out = 1;
            }
            return true;
        }
    }
    if (n == 5) {
        if ((data[start] | 0x20) == 'f' &&
            (data[start + 1] | 0x20) == 'a' &&
            (data[start + 2] | 0x20) == 'l' &&
            (data[start + 3] | 0x20) == 's' &&
            (data[start + 4] | 0x20) == 'e') {
            if (out != NULL) {
                *out = 0;
            }
            return true;
        }
    }
    if (out != NULL) {
        *out = 0;
    }
    return false;
}
