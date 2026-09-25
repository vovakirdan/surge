#include "rt.h"

#include "rt_array_internal.h"

#include <stdalign.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

// A null `Range` is the default of a range handle (the emitter's null sentinel
// for every runtime handle), and the owner ruled on 2026-09-25 that USING one is
// a runtime error on both backends, in the VM's words: `panic VM1203: null range
// handle`. Dropping, retaining or unsharing one is nothing. The D2
// return-origin gate refuses every program that reaches a Range default at this
// base, so this stand asks the runtime's entry points directly, one row per
// process: a refusal is a panic the process does not survive, and the driver
// reads a death row from its exit status and the report on stderr.
//
// The entry points a null range reaches natively are exactly these: the two
// slice bounds readers (rt_array_slice, rt_array_slice_fixed, rt_string_slice)
// and rt_range_require, which the emitter calls before it loads the kind byte
// of a range a `for` or a `.next()` is handed. rt_range_free,
// rt_range_bounds_retain and rt_range_unshare are the lifetime paths, and a
// null there is nothing.

// The entry point's argv, which this stand has none of and rt_io.c requires.
int rt_argc = 0;
char** rt_argv_raw = NULL;

#define STRIDE 8u
#define ELEMS 3u

static int failures;

static void check(int condition, const char* what) {
    if (!condition) {
        failures++;
        printf("FAIL %s\n", what);
    }
}

// A three-element int array in its own heap header, the shape the emitter
// hands rt_array_slice through a slot.
static SurgeArrayHeader* make_array(void) {
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        return NULL;
    }
    header->data = rt_alloc((uint64_t)ELEMS * STRIDE, STRIDE);
    if (header->data == NULL) {
        return NULL;
    }
    int64_t* data = (int64_t*)header->data;
    for (uint64_t i = 0; i < ELEMS; i++) {
        data[i] = (int64_t)(i + 1);
    }
    header->len = ELEMS;
    header->cap = ELEMS;
    return header;
}

// A range with neither bound: the literal `..`, a real object that means the
// whole of what it slices. It is what a whole-range slice looks like, and it
// is not null.
static void whole_range(SurgeRange* r) {
    memset(r, 0, sizeof(*r));
}

static void row_array_slice_null(void) {
    SurgeArrayHeader* header = make_array();
    check(header != NULL, "array allocated");
    SurgeArrayHeader* slot = header;
    const SurgeArrayHeader* view =
        (const SurgeArrayHeader*)rt_array_slice((void*)&slot, NULL, STRIDE);
    printf("array-slice-null: len=%llu\n", view == NULL ? 0ULL : (unsigned long long)view->len);
}

static void row_array_slice_fixed_null(void) {
    int64_t elems[ELEMS] = {1, 2, 3};
    const SurgeArrayHeader* view =
        (const SurgeArrayHeader*)rt_array_slice_fixed(elems, NULL, ELEMS, STRIDE);
    printf("array-slice-fixed-null: len=%llu\n",
           view == NULL ? 0ULL : (unsigned long long)view->len);
}

static void row_string_slice_null(void) {
    void* s = rt_string_from_bytes((const uint8_t*)"abcdef", 6);
    void* slot = s;
    void* sliced = rt_string_slice((void*)&slot, NULL);
    printf("string-slice-null: len=%llu\n",
           sliced == NULL ? 0ULL : (unsigned long long)rt_string_len_bytes((void*)&sliced));
}

static void row_require_null(void) {
    rt_range_require(NULL);
    printf("require-null: returned\n");
}

// Controls: a range that is not null is read, and a null on a lifetime path is
// nothing.
static void row_array_slice_whole(void) {
    SurgeArrayHeader* header = make_array();
    check(header != NULL, "array allocated");
    SurgeArrayHeader* slot = header;
    SurgeRange r;
    whole_range(&r);
    const SurgeArrayHeader* view =
        (const SurgeArrayHeader*)rt_array_slice((void*)&slot, &r, STRIDE);
    check(view != NULL && view->len == ELEMS, "a whole range slices every element");
    printf("array-slice-whole: len=%llu\n", view == NULL ? 0ULL : (unsigned long long)view->len);
}

static void row_string_slice_whole(void) {
    void* s = rt_string_from_bytes((const uint8_t*)"abcdef", 6);
    void* slot = s;
    SurgeRange r;
    whole_range(&r);
    void* sliced = rt_string_slice((void*)&slot, &r);
    uint64_t len = sliced == NULL ? 0 : rt_string_len_bytes((void*)&sliced);
    check(len == 6, "a whole range slices every byte");
    printf("string-slice-whole: len=%llu\n", (unsigned long long)len);
}

static void row_require_live(void) {
    SurgeRange r;
    whole_range(&r);
    rt_range_require(&r);
    printf("require-live: returned\n");
}

static void row_null_lifetime_is_nothing(void) {
    void* slot = NULL;
    rt_range_free(NULL);
    rt_range_bounds_retain(NULL);
    rt_range_unshare((void*)&slot);
    rt_range_unshare(NULL);
    check(slot == NULL, "unsharing a null range leaves the slot null");
    printf("null-lifetime-is-nothing: ok\n");
}

// The scheduler's two calls back into compiled code. This stand never starts a
// task, so reaching either one is a defect in the stand rather than a result;
// they exist because the runtime's poll and blocking workers reference them.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    (void)id;
    rt_async_return(NULL, &(uint64_t){0});
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
}

struct row {
    const char* name;
    void (*run)(void);
};

static const struct row rows[] = {
    {"array-slice-null", row_array_slice_null},
    {"array-slice-fixed-null", row_array_slice_fixed_null},
    {"string-slice-null", row_string_slice_null},
    {"require-null", row_require_null},
    {"array-slice-whole", row_array_slice_whole},
    {"string-slice-whole", row_string_slice_whole},
    {"require-live", row_require_live},
    {"null-lifetime-is-nothing", row_null_lifetime_is_nothing},
};

int main(int argc, char** argv) {
    const char* mode = argc > 1 ? argv[1] : "";
    for (size_t i = 0; i < sizeof(rows) / sizeof(rows[0]); i++) {
        if (strcmp(mode, rows[i].name) == 0) {
            rows[i].run();
            printf("range-null-handle: failures=%d\n", failures);
            return failures == 0 ? 0 : 1;
        }
    }
    fprintf(stderr, "range-null-handle: unknown row \"%s\"\n", mode);
    return 2;
}
