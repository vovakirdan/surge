#include "rt.h"
#include "rt_bignum_internal.h"
#include "rt_heap_accounting.h"

#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Direct native API proof: heap operands bypass the exported fixnum fast path.
// Each process owns every block it creates and starts no executor. Allocation
// counts measure the operation; Valgrind measures physical reclamation. Byte
// accounting is not an oracle because existing arithmetic trims allocation len.
int rt_argc = 0;
char** rt_argv_raw = NULL;

// Linking the runtime requires both dispatch hooks. This stand creates no
// executor or task, so either call means its isolation contract was violated.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    (void)id;
    fputs("biguint-sub: unexpected __surge_poll_call\n", stderr);
    abort();
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    (void)out_dst;
    fputs("biguint-sub: unexpected __surge_blocking_call\n", stderr);
    abort();
}

#define HEAP_VALUE (UINT64_C(1) << 63)

static int failures;

static void check(int condition, const char* message) {
    if (!condition) {
        failures++;
        printf("FAIL %s\n", message);
    }
}

typedef struct HeapMark {
    uint64_t allocs;
    uint64_t frees;
} HeapMark;

static HeapMark heap_mark(void) {
    struct rt_heap_accounting_snapshot snapshot = {0};
    rt_heap_accounting_status status =
        rt_heap_accounting_snapshot(rt_runtime_global_heap_accounting(), &snapshot);
    check(status == RT_HEAP_ACCOUNTING_OK, "heap accounting answered");
    HeapMark mark = {snapshot.alloc_count, snapshot.free_count};
    return mark;
}

static SurgeBigUint* make_held_heap(uint64_t value) {
    void* word = rt_biguint_from_u64(value);
    check(fix_is_heap(word), "operand is a heap value");
    if (!fix_is_heap(word)) {
        return NULL;
    }
    SurgeBigUint* result = word;
    check(result->len == 2 && result->rc == 1, "fresh operand has two limbs and one owner");
    rt_biguint_retain(result);
    return result;
}

static void check_held_heap(const SurgeBigUint* value, uint64_t expected) {
    check(value->len == 2 && value->rc == 2 && value->limbs[0] == (uint32_t)expected &&
              value->limbs[1] == (uint32_t)(expected >> 32),
          "subtraction preserves both input limbs and references");
}

static void release_held_heap(SurgeBigUint* value) {
    if (value != NULL) {
        rt_biguint_release(value);
        rt_biguint_release(value);
    }
}

static void operation_census(const char* row, HeapMark before, uint64_t want_allocs,
                             uint64_t want_frees) {
    HeapMark after = heap_mark();
    uint64_t allocs = after.allocs - before.allocs;
    uint64_t frees = after.frees - before.frees;
    printf("%s: operation_allocs=%" PRIu64 " operation_frees=%" PRIu64 "\n", row, allocs, frees);
    check(allocs == want_allocs, "subtraction has the expected allocation count");
    check(frees == want_frees, "subtraction has the expected free count");
}

static void row_equal(const char* row, int distinct) {
    SurgeBigUint* left = make_held_heap(HEAP_VALUE);
    SurgeBigUint* right = distinct ? make_held_heap(HEAP_VALUE) : left;
    if (left == NULL || right == NULL) {
        release_held_heap(left);
        if (distinct) {
            release_held_heap(right);
        }
        return;
    }
    check(!distinct || left != right, "equal operands occupy distinct blocks");
    HeapMark before = heap_mark();
    void* api_result = rt_biguint_sub(left, right);
    check(api_result == NULL, "exported heap subtraction returns canonical zero");
    check_held_heap(left, HEAP_VALUE);
    check_held_heap(right, HEAP_VALUE);
    bn_err err = BN_ERR_UNDERFLOW;
    SurgeBigUint* raw_result = bu_sub(left, right, &err);
    check(raw_result == NULL && err == BN_OK, "equal subtraction clears an old error to BN_OK");
    check_held_heap(left, HEAP_VALUE);
    check_held_heap(right, HEAP_VALUE);
    rt_biguint_release(api_result);
    rt_biguint_release(raw_result);
    operation_census(row, before, 0, 0);
    release_held_heap(left);
    if (distinct) {
        release_held_heap(right);
    }
}

static void row_positive(const char* row) {
    SurgeBigUint* left = make_held_heap(HEAP_VALUE + 1);
    SurgeBigUint* right = make_held_heap(HEAP_VALUE);
    if (left == NULL || right == NULL) {
        release_held_heap(left);
        release_held_heap(right);
        return;
    }
    HeapMark before = heap_mark();
    void* api_result = rt_biguint_sub(left, right);
    check(fix_is_inline(api_result) && fixu_value(api_result) == 1,
          "positive exported difference demotes to inline one");
    check_held_heap(left, HEAP_VALUE + 1);
    check_held_heap(right, HEAP_VALUE);
    bn_err err = BN_ERR_UNDERFLOW;
    SurgeBigUint* raw_result = bu_sub(left, right, &err);
    check(raw_result != NULL && raw_result->len == 1 && raw_result->rc == 1 &&
              raw_result->limbs[0] == 1 && err == BN_OK,
          "positive raw difference is independently owned one");
    check_held_heap(left, HEAP_VALUE + 1);
    check_held_heap(right, HEAP_VALUE);
    rt_biguint_release(api_result);
    rt_biguint_release(raw_result);
    operation_census(row, before, 2, 2);
    release_held_heap(left);
    release_held_heap(right);
}

static void row_underflow(const char* row) {
    SurgeBigUint* left = make_held_heap(HEAP_VALUE);
    SurgeBigUint* right = make_held_heap(HEAP_VALUE + 1);
    if (left == NULL || right == NULL) {
        release_held_heap(left);
        release_held_heap(right);
        return;
    }
    HeapMark before = heap_mark();
    // The public arithmetic API reports underflow fatally; its internal
    // status-bearing operation is where the unchanged error contract lives.
    bn_err err = BN_OK;
    SurgeBigUint* result = bu_sub(left, right, &err);
    check(result == NULL && err == BN_ERR_UNDERFLOW, "strictly smaller input reports underflow");
    rt_biguint_release(result);
    result = bu_sub(left, right, NULL);
    check(result == NULL, "underflow also accepts an absent error output");
    rt_biguint_release(result);
    check_held_heap(left, HEAP_VALUE);
    check_held_heap(right, HEAP_VALUE + 1);
    operation_census(row, before, 0, 0);
    release_held_heap(left);
    release_held_heap(right);
}

int main(int argc, char** argv) {
    // The existing native row runner supplies argv; the existing Valgrind
    // runner supplies environment, as it does for the channel refcount stand.
    const char* row = argc > 1 ? argv[1] : getenv("SURGE_BIGUINT_SUB_ROW");
    if (row == NULL) {
        row = "";
    }
    HeapMark before = heap_mark();
    if (strcmp(row, "equal-same-heap") == 0) {
        row_equal(row, 0);
    } else if (strcmp(row, "equal-distinct-heaps") == 0) {
        row_equal(row, 1);
    } else if (strcmp(row, "unequal-positive") == 0) {
        row_positive(row);
    } else if (strcmp(row, "unequal-underflow") == 0) {
        row_underflow(row);
    } else {
        fprintf(stderr, "biguint-sub: unknown row \"%s\"\n", row);
        return 2;
    }
    HeapMark after = heap_mark();
    uint64_t allocs = after.allocs - before.allocs;
    uint64_t frees = after.frees - before.frees;
    printf("%s: outstanding_blocks=%" PRIu64 "\n", row, allocs - frees);
    check(allocs == frees, "all input and result blocks are released");
    printf("biguint-sub: failures=%d\n", failures);
    return failures == 0 ? 0 : 1;
}
