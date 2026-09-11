#include "rt.h"
#include "rt_bignum_internal.h"
#include "rt_heap_accounting.h"

#include <inttypes.h>
#include <stdalign.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

// This direct Range stand starts no executor. Reaching a dispatch hook is a
// broken isolation contract, never an emulated task result.
// NOLINTBEGIN(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
void __surge_poll_call(uint64_t id) {
    (void)id;
    abort();
}
void __surge_blocking_call(uint64_t id, void* state, void* out);
void __surge_blocking_call(uint64_t id, void* state, void* out) {
    (void)id;
    (void)state;
    (void)out;
    abort();
}

enum { RETAIN, RELEASE, UNSHARE, OP_COUNT };
static uint64_t calls[3][OP_COUNT];
#define WRAP_VOID(kind, index, op, counter)                                                        \
    extern void __real_rt_big##kind##_##op(void*);                                                 \
    void __wrap_rt_big##kind##_##op(void* value);                                                  \
    void __wrap_rt_big##kind##_##op(void* value) {                                                 \
        calls[index][counter]++;                                                                   \
        __real_rt_big##kind##_##op(value);                                                         \
    }
#define WRAP_UNSHARE(kind, index)                                                                  \
    extern void* __real_rt_big##kind##_unshare(void*);                                             \
    void* __wrap_rt_big##kind##_unshare(void* value);                                              \
    void* __wrap_rt_big##kind##_unshare(void* value) {                                             \
        calls[index][UNSHARE]++;                                                                   \
        return __real_rt_big##kind##_unshare(value);                                               \
    }
WRAP_VOID(int, 0, retain, RETAIN)
WRAP_VOID(int, 0, release, RELEASE)
WRAP_UNSHARE(int, 0)
WRAP_VOID(uint, 1, retain, RETAIN)
WRAP_VOID(uint, 1, release, RELEASE)
WRAP_UNSHARE(uint, 1)
WRAP_VOID(float, 2, retain, RETAIN)
WRAP_VOID(float, 2, release, RELEASE)
WRAP_UNSHARE(float, 2)
// NOLINTEND(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)

static int failures;
static const char* current_row;

static void check(int condition, const char* message) {
    if (!condition) {
        failures++;
        printf("FAIL %s: %s\n", current_row, message);
    }
}

static void phase_census(int kind, int op, int counted) {
    uint64_t total = 0;
    for (int k = 0; k < 3; k++) {
        for (int p = 0; p < OP_COUNT; p++) {
            total += calls[k][p];
            if (k != kind)
                check(calls[k][p] == 0, "bound dispatch stays in its numeric kind");
        }
    }
    if (counted)
        check(calls[kind][op] == 2, "both bounds dispatch their owning lifecycle");
    else
        check(total == 0, "inline bounds make zero numeric lifecycle calls");
    printf("range-phase: row=%s op=%d numeric_calls=%" PRIu64 "\n", current_row, op, total);
    memset(calls, 0, sizeof(calls));
}

static void release_word(int kind, void* word) {
    if (kind == 0)
        rt_bigint_release(word);
    else if (kind == 1)
        rt_biguint_release(word);
    else
        rt_bigfloat_release(word);
}

static void check_value(int kind, const void* word, const void* expected) {
    int cmp = kind == 0   ? rt_bigint_cmp(word, expected)
              : kind == 1 ? rt_biguint_cmp(word, expected)
                          : rt_bigfloat_cmp(word, expected);
    check(cmp == 0, "private bound preserves the original value");
}

static void exercise(int kind, void* word, int counted) {
    static const uint8_t bounds[] = {
        SURGE_RANGE_BOUND_INT, SURGE_RANGE_BOUND_UINT, SURGE_RANGE_BOUND_FLOAT};
    memset(calls, 0, sizeof(calls));
    SurgeRange* range = rt_range_bounds_new(word, word, false, bounds[kind]);
    if (range == NULL)
        abort();
    phase_census(kind, RETAIN, counted);
    // Range Copy duplicates its header and retains the two bound obligations.
    SurgeRange* copy = (SurgeRange*)rt_alloc(sizeof(*copy), alignof(SurgeRange));
    if (copy == NULL)
        abort();
    memcpy(copy, range, sizeof(*copy));
    rt_range_bounds_retain(copy);
    phase_census(kind, RETAIN, counted);
    rt_range_unshare((void*)&range);
    phase_census(kind, UNSHARE, counted);
    if (counted && word != NULL) {
        check(range->start != word && range->end != word && range->start != range->end,
              "copied range detaches each bound while caller and sibling remain live");
        check_value(kind, range->start, word);
        check_value(kind, range->end, word);
    } else {
        check(range->start == word && range->end == word, "inline bounds preserve their words");
    }
    check(copy->start == word && copy->end == word, "range copy retains the caller bounds");
    rt_range_free(range);
    phase_census(kind, RELEASE, counted);
    rt_range_free(copy);
    phase_census(kind, RELEASE, counted);
    release_word(kind, word);
}

int main(int argc, char** argv) {
    current_row = argc > 1 ? argv[1] : getenv("SURGE_RANGE_NUMERIC_ROW");
    if (current_row == NULL)
        current_row = "";
    struct rt_heap_accounting_snapshot before = {0}, after = {0};
    check(rt_heap_accounting_snapshot(rt_runtime_global_heap_accounting(), &before) ==
              RT_HEAP_ACCOUNTING_OK,
          "initial allocation census answered");
    if (strcmp(current_row, "int-fixnum") == 0) {
        exercise(0, NULL, 0);
        exercise(0, fixi_box(SURGE_FIXI_MIN, NULL), 0);
        exercise(0, fixi_box(SURGE_FIXI_MAX, NULL), 0);
    } else if (strcmp(current_row, "uint-fixnum") == 0) {
        exercise(1, NULL, 0);
        exercise(1, fixu_box(1, NULL), 0);
        exercise(1, fixu_box(SURGE_FIXU_MAX, NULL), 0);
    } else if (strcmp(current_row, "int-heap") == 0) {
        exercise(0, rt_bigint_from_u64(UINT64_C(1) << 63), 1);
    } else if (strcmp(current_row, "uint-heap") == 0) {
        exercise(1, rt_biguint_from_u64(UINT64_C(1) << 63), 1);
    } else if (strcmp(current_row, "float") == 0) {
        exercise(2, NULL, 1);
        exercise(2, rt_bigfloat_from_i64(7), 1);
    } else {
        fprintf(stderr, "range-numeric: unknown row %s\n", current_row);
        return 2;
    }
    check(rt_heap_accounting_snapshot(rt_runtime_global_heap_accounting(), &after) ==
              RT_HEAP_ACCOUNTING_OK,
          "final allocation census answered");
    uint64_t outstanding =
        after.alloc_count - before.alloc_count - (after.free_count - before.free_count);
    check(outstanding == 0, "all range headers and numeric owners released");
    printf("range-numeric: row=%s failures=%d outstanding_blocks=%" PRIu64 "\n",
           current_row,
           failures,
           outstanding);
    return failures == 0 ? 0 : 1;
}
