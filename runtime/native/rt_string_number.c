#include "rt.h"
#include "rt_string_internal.h"

#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Numbers to text and back.
//
// Filed apart from the string operations because these are CONVERSIONS with a
// failure mode -- every parse can refuse, and refusing correctly is most of
// what they do -- while the rest of rt_string.c manipulates text that is
// already text and cannot fail that way.

bool string_span(void* s, const char** out_ptr, size_t* out_len) {
    if (out_ptr == NULL || out_len == NULL) {
        return false;
    }
    *out_ptr = NULL;
    *out_len = 0;
    if (s == NULL) {
        return false;
    }
    const SurgeString* str = *(const SurgeString**)s;
    if (str == NULL) {
        return false;
    }
    *out_ptr = (const char*)str->data;
    *out_len = (size_t)str->len_bytes;
    return true;
}

void trim_span(const char* data, size_t len, size_t* start, size_t* end) {
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
        if ((data[start] | 0x20) == 't' && (data[start + 1] | 0x20) == 'r' &&
            (data[start + 2] | 0x20) == 'u' && (data[start + 3] | 0x20) == 'e') {
            if (out != NULL) {
                *out = 1;
            }
            return true;
        }
    }
    if (n == 5) {
        if ((data[start] | 0x20) == 'f' && (data[start + 1] | 0x20) == 'a' &&
            (data[start + 2] | 0x20) == 'l' && (data[start + 3] | 0x20) == 's' &&
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
