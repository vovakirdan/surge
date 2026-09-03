#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"
#include "rt_carrier_bench.h"

#include <ctype.h>
#include <inttypes.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

// Serializes stdout/stderr writes so concurrent async tasks cannot tear each
// other's output mid-line (the multithreaded scheduler runs `print` on several
// threads at once). A single lock across both streams also keeps stdout and
// stderr from interleaving. `write` never re-enters the runtime, so holding
// this across the write loop cannot deadlock.
static pthread_mutex_t rt_io_lock = PTHREAD_MUTEX_INITIALIZER;

#ifndef alignof
#define alignof(t) __alignof__(t)
#endif

extern int rt_argc;
extern char** rt_argv_raw;

uint64_t rt_write_stdout(const uint8_t* ptr, uint64_t length) {
    if (ptr == NULL || length == 0) {
        return 0;
    }
    pthread_mutex_lock(&rt_io_lock);
    uint64_t written = 0;
    while (written < length) {
        ssize_t chunk = write(STDOUT_FILENO, ptr + written, (size_t)(length - written));
        if (chunk <= 0) {
            break;
        }
        written += (uint64_t)chunk;
    }
    pthread_mutex_unlock(&rt_io_lock);
    return written;
}

uint64_t rt_write_stderr(const uint8_t* ptr, uint64_t length) {
    if (ptr == NULL || length == 0) {
        return 0;
    }
    pthread_mutex_lock(&rt_io_lock);
    uint64_t written = 0;
    while (written < length) {
        ssize_t chunk = write(STDERR_FILENO, ptr + written, (size_t)(length - written));
        if (chunk <= 0) {
            break;
        }
        written += (uint64_t)chunk;
    }
    pthread_mutex_unlock(&rt_io_lock);
    return written;
}

void* rt_readline(void) {
    char* buf = NULL;
    size_t cap = 0;
    ssize_t n = getline(&buf, &cap, stdin);
    if (n <= 0) {
        free(buf);
        void* out = rt_string_from_bytes(NULL, 0);
        if (out == NULL) {
            static const uint8_t msg[] = "readline allocation failed";
            rt_fatal_static(RT_OOM, msg, sizeof(msg) - 1);
        }
        return out;
    }
    size_t len = (size_t)n;
    if (buf[len - 1] == '\n') {
        len--;
    }
    if (len > 0 && buf[len - 1] == '\r') {
        len--;
    }
    void* out = rt_string_from_bytes((const uint8_t*)buf, (uint64_t)len);
    free(buf);
    if (out == NULL) {
        static const uint8_t msg[] = "readline allocation failed";
        rt_fatal_static(RT_OOM, msg, sizeof(msg) - 1);
    }
    return out;
}

typedef struct SurgeArrayHeader {
    uint64_t len;
    uint64_t cap;
    void* data;
} SurgeArrayHeader;

void* rt_argv(void) {
    int argc = rt_argc;
    char** argv = rt_argv_raw;
    int count = 0;
    if (argc > 1) {
        count = argc - 1;
    }
    // Generated code stores the array untested (RV2-DEBT-309): a refused
    // block is reported here, never answered as NULL.
    static const uint8_t oom[] = "argv allocation failed";
    void* data = NULL;
    if (count > 0) {
        data = rt_alloc_or_report((uint64_t)count * (uint64_t)sizeof(void*),
                                  (uint64_t)alignof(void*),
                                  oom,
                                  sizeof(oom) - 1);
    }
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc_or_report(
        (uint64_t)sizeof(SurgeArrayHeader), (uint64_t)alignof(SurgeArrayHeader), oom, sizeof(oom) - 1);
    header->len = (uint64_t)count;
    header->cap = (uint64_t)count;
    header->data = data;

    if (data != NULL && argv != NULL) {
        void** slots = (void**)data;
        for (int i = 0; i < count; i++) {
            const char* arg = argv[i + 1];
            if (arg == NULL) {
                slots[i] = rt_string_from_bytes(NULL, 0);
                continue;
            }
            size_t n = strlen(arg);
            slots[i] = rt_string_from_bytes((const uint8_t*)arg, (uint64_t)n);
        }
    }
    return (void*)header;
}

void* rt_stdin_read_all(void) {
    uint8_t* buf = NULL;
    size_t len = 0;
    size_t cap = 0;

    for (;;) {
        if (cap - len < 1024) {
            size_t next = cap == 0 ? 4096 : cap * 2;
            uint8_t* tmp = (uint8_t*)realloc(buf, next);
            if (tmp == NULL) {
                free(buf);
                return rt_string_from_bytes(NULL, 0);
            }
            buf = tmp;
            cap = next;
        }
        ssize_t n = read(STDIN_FILENO, buf + len, cap - len);
        if (n <= 0) {
            break;
        }
        len += (size_t)n;
    }

    size_t start = 0;
    size_t end = len;
    while (start < end && isspace((unsigned char)buf[start])) {
        start++;
    }
    while (end > start && isspace((unsigned char)buf[end - 1])) {
        end--;
    }

    void* out = rt_string_from_bytes(buf + start, (uint64_t)(end - start));
    free(buf);
    return out;
}

void rt_exit(int64_t code) {
    rt_exec_trace_dump();
    rt_sched_trace_dump();
    if (rt_carrier_bench_finish() != 0) {
        exit(1);
    }
    exit((int)code);
}

void rt_panic(const uint8_t* ptr, uint64_t length) {
    static const uint8_t prefix[] = "panic: ";
    rt_write_stderr(prefix, (uint64_t)(sizeof(prefix) - 1));
    if (ptr != NULL && length > 0) {
        rt_write_stderr(ptr, length);
        if (ptr[length - 1] != '\n') {
            rt_write_stderr((const uint8_t*)"\n", 1);
        }
    } else {
        rt_write_stderr((const uint8_t*)"\n", 1);
    }
    _exit(1);
}

// Reports a panic under a code the CALLER names.
//
// The code belongs to the condition, not to the reporter. rt_panic_numeric
// below hardcodes VM3202 because that is what a failed numeric CONVERSION is,
// and for a while every other emitter-side panic borrowed it: arithmetic
// overflow, division by zero and a resize of an array view all announced
// themselves as invalid numeric conversions, where the VM answers VM1101,
// VM3203 and VM1003. Two backends disagreeing on the code for one condition is
// the kind of difference a reader trusts and should not have to.
void rt_panic_code(const uint8_t* code,
                   uint64_t code_length,
                   const uint8_t* ptr,
                   uint64_t length,
                   const uint8_t* span,
                   uint64_t span_length) {
    static const uint8_t prefix[] = "panic ";
    static const uint8_t separator[] = ": ";
    static const uint8_t fallback[] = "invalid numeric conversion";
    rt_write_stderr(prefix, (uint64_t)(sizeof(prefix) - 1));
    if (code != NULL && code_length > 0) {
        rt_write_stderr(code, code_length);
    }
    rt_write_stderr(separator, (uint64_t)(sizeof(separator) - 1));
    if (ptr != NULL && length > 0) {
        rt_write_stderr(ptr, length);
        if (ptr[length - 1] != '\n') {
            rt_write_stderr((const uint8_t*)"\n", 1);
        }
    } else {
        rt_write_stderr(fallback, (uint64_t)(sizeof(fallback) - 1));
        rt_write_stderr((const uint8_t*)"\n", 1);
    }
    rt_panic_write_where(span, span_length);
    _exit(1);
}

void rt_panic_numeric(const uint8_t* ptr,
                      uint64_t length,
                      const uint8_t* span,
                      uint64_t span_length) {
    static const uint8_t code[] = "VM3202";
    rt_panic_code(code, (uint64_t)(sizeof(code) - 1), ptr, length, span, span_length);
}

void rt_panic_bounds(
    uint64_t kind, int64_t index, int64_t length, const uint8_t* span, uint64_t span_length) {
    const char* code = "VM1004";
    if (kind == 1) {
        code = "VM2105";
    }
    char buf[128];
    int n = 0;
    if (kind == 1) {
        n = snprintf(buf,
                     sizeof(buf),
                     "panic %s: array index %" PRId64 " out of range for length %" PRId64 "\n",
                     code,
                     index,
                     length);
    } else {
        n = snprintf(buf,
                     sizeof(buf),
                     "panic %s: index %" PRId64 " out of bounds for length %" PRId64 "\n",
                     code,
                     index,
                     length);
    }
    if (n < 0) {
        const uint8_t fallback[] = "panic VM1004: bounds check failed\n";
        rt_write_stderr(fallback, (uint64_t)(sizeof(fallback) - 1));
        rt_panic_write_where(span, span_length);
        _exit(1);
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    rt_write_stderr((const uint8_t*)buf, (uint64_t)n);
    rt_panic_write_where(span, span_length);
    _exit(1);
}
