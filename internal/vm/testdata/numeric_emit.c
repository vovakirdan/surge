#include "rt.h"
#include "rt_bignum_internal.h"
#include "rt_heap_accounting.h"
#include "rt_resident_bytes.h"

#include <inttypes.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// Native builds force-include the checked MIR-to-symbol mapping. These real
// declarations also let changed-C checks type-check the stand on its own;
// incompatible generated signatures fail when both declarations are present.
#define NUMERIC_DECLARATIONS(kind)                                      \
    void* numeric_##kind##_copy(void*);                                 \
    void* numeric_##kind##_explicit_clone(void*);                       \
    void numeric_##kind##_discard(void*);                              \
    void numeric_##kind##_clone(void*, void*);                          \
    void numeric_##kind##_box_clone(void*, void*);                      \
    void numeric_##kind##_clone_elem(void*, void*);                     \
    void numeric_##kind##_drop(void*);                                 \
    void numeric_##kind##_box_drop(void*);                             \
    void numeric_##kind##_drop_elem(void*);                            \
    void numeric_##kind##_unshare(void*);                              \
    void numeric_##kind##_cross_clone(void*, void*);
NUMERIC_DECLARATIONS(int)
NUMERIC_DECLARATIONS(uint)

// The emitted object supplies both dispatch hooks. No executor is started.
int rt_argc = 0;
char** rt_argv_raw = NULL;
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
extern _Thread_local uint32_t __surge_rc_scratch;

enum { RETAIN, RELEASE, CLONE, UNSHARE, CALL_KINDS };
static uint64_t calls[2][CALL_KINDS];
static int measuring;
static int failures;
static const char* current_row;

// These count unresolved calls from generated LLVM, not generated glue calls.
// NOLINTBEGIN(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#define WRAP_VOID(kind, index, op, counter)                         \
    extern void __real_rt_big##kind##_##op(void*);                  \
    void __wrap_rt_big##kind##_##op(void*);                         \
    void __wrap_rt_big##kind##_##op(void* value) {                   \
        if (measuring) calls[index][counter]++;                    \
        __real_rt_big##kind##_##op(value);                          \
    }
#define WRAP_VALUE(kind, index, op, counter, qualifier)             \
    extern void* __real_rt_big##kind##_##op(qualifier void*);        \
    void* __wrap_rt_big##kind##_##op(qualifier void*);               \
    void* __wrap_rt_big##kind##_##op(qualifier void* value) {        \
        if (measuring) calls[index][counter]++;                    \
        return __real_rt_big##kind##_##op(value);                   \
    }
WRAP_VOID(int, 0, retain, RETAIN)
WRAP_VOID(int, 0, release, RELEASE)
WRAP_VALUE(int, 0, clone, CLONE, const)
WRAP_VALUE(int, 0, unshare, UNSHARE, )
WRAP_VOID(uint, 1, retain, RETAIN)
WRAP_VOID(uint, 1, release, RELEASE)
WRAP_VALUE(uint, 1, clone, CLONE, const)
WRAP_VALUE(uint, 1, unshare, UNSHARE, )
// NOLINTEND(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)

typedef struct NumericOps {
    void* (*make)(uint64_t);
    void (*retain)(void*);
    void (*release)(void*);
    void* (*copy)(void*);
    void* (*explicit_clone)(void*);
    void (*discard)(void*);
    void (*clone[3])(void*, void*);
    void (*drop[3])(void*);
    void (*unshare)(void*);
    void (*cross_clone)(void*, void*);
} NumericOps;

#define NUMERIC_OPS(kind) {                                                   \
    rt_big##kind##_from_u64, rt_big##kind##_retain, rt_big##kind##_release,     \
    numeric_##kind##_copy, numeric_##kind##_explicit_clone,                   \
    numeric_##kind##_discard,                                                \
    {numeric_##kind##_clone, numeric_##kind##_box_clone, numeric_##kind##_clone_elem}, \
    {numeric_##kind##_drop, numeric_##kind##_box_drop, numeric_##kind##_drop_elem},    \
    numeric_##kind##_unshare, numeric_##kind##_cross_clone                     \
}
static const NumericOps numeric_ops[] = {NUMERIC_OPS(int), NUMERIC_OPS(uint)};

static void check(int condition, const char* message) {
    if (!condition) {
        failures++;
        printf("FAIL %s: %s\n", current_row, message);
    }
}

typedef struct HeapMark { uint64_t allocs, frees; } HeapMark;
typedef struct OperationMark {
    HeapMark heap;
    uint64_t unshare_clones;
    uint32_t scratch;
} OperationMark;

static HeapMark heap_mark(void) {
    struct rt_heap_accounting_snapshot snapshot = {0};
    check(rt_heap_accounting_snapshot(rt_runtime_global_heap_accounting(), &snapshot) ==
              RT_HEAP_ACCOUNTING_OK, "heap accounting answered");
    return (HeapMark){snapshot.alloc_count, snapshot.free_count};
}

static OperationMark operation_begin(void) {
    OperationMark mark = {heap_mark(), rt_resident_bytes_snapshot().unshare_clones,
                          __surge_rc_scratch};
    memset(calls, 0, sizeof(calls));
    measuring = 1;
    return mark;
}

static void operation_end(OperationMark before, uint64_t allocs, uint64_t frees,
                          uint64_t clones, int zero_calls) {
    measuring = 0;
    HeapMark after = heap_mark();
    check(after.allocs - before.heap.allocs == allocs, "expected operation allocations");
    check(after.frees - before.heap.frees == frees, "expected operation frees");
    check(rt_resident_bytes_snapshot().unshare_clones - before.unshare_clones == clones,
          "expected unshare clone count");
    check(__surge_rc_scratch == before.scratch, "scratch changed");
    uint64_t total = 0;
    for (int kind = 0; kind < 2; kind++) {
        for (int op = 0; op < CALL_KINDS; op++) total += calls[kind][op];
    }
    if (zero_calls) check(total == 0, "inline numeric lifecycle calls must be zero");
    printf("%s: allocs=%" PRIu64 " frees=%" PRIu64 " numeric_calls=%" PRIu64 "\n",
           current_row, after.allocs - before.heap.allocs, after.frees - before.heap.frees, total);
}

static uint32_t heap_rc(int kind, const void* value) {
    return kind == 0 ? ((const SurgeBigInt*)value)->rc : ((const SurgeBigUint*)value)->rc;
}

static int expected_heap_value(int kind, const void* value) {
    if (!fix_is_heap(value)) return 0;
    if (kind == 0) {
        const SurgeBigInt* v = value;
        return v->neg == 0 && v->len == 2 && v->limbs[0] == 0 && v->limbs[1] == UINT32_C(0x80000000);
    }
    const SurgeBigUint* v = value;
    return v->len == 2 && v->limbs[0] == 0 && v->limbs[1] == UINT32_C(0x80000000);
}

static void* make_heap(int kind) {
    void* value = numeric_ops[kind].make(UINT64_C(1) << 63);
    check(expected_heap_value(kind, value), "fixture is the expected heap value");
    if (!fix_is_heap(value)) abort();
    check(heap_rc(kind, value) == 1, "fixture starts with one reference");
    return value;
}

static void check_copy_cost(int kind) {
    check(calls[kind][RETAIN] == 0, "heap Copy retain is inline");
    check(calls[kind][CLONE] == 0 && calls[kind][UNSHARE] == 0,
          "heap Copy does not deep clone");
}

static void inline_row(int kind, int operation) {
    const NumericOps* ops = &numeric_ops[kind];
    void* words[] = {NULL, fixu_box(7, NULL), fixu_box(SURGE_FIXU_MAX, NULL),
                    fixi_box(SURGE_FIXI_MIN, NULL), fixi_box(SURGE_FIXI_MAX, NULL)};
    for (size_t i = 0; i < sizeof(words) / sizeof(words[0]); i++) {
        void* value = words[i];
        check(!fix_is_heap(value), "inline fixture owns no heap");
        printf("%s: word=%" PRIxPTR "\n", current_row, (uintptr_t)value);
        OperationMark mark = operation_begin();
        if (operation == 0 || operation == 2) {
            const void* copied = operation == 0 ? ops->copy(value) : ops->explicit_clone(value);
            check(copied == value, "inline source Copy preserves the word");
        } else if (operation == 1) {
            ops->discard(value);
        } else if (operation == 3) {
            for (int shape = 0; shape < 3; shape++) {
                void* source = value;
                void* copied = NULL;
                ops->clone[shape]((void*)&copied, (void*)&source);
                check(copied == value && source == value, "inline glue Copy preserves both words");
                ops->drop[shape]((void*)&copied);
            }
        } else if (operation == 4) {
            void* slot = value;
            ops->unshare((void*)&slot);
            check(slot == value, "inline unshare preserves the word");
        } else {
            void* copied = value;
            ops->cross_clone((void*)&copied, (void*)&value);
            check(copied == value, "inline crossing preserves the copied word");
        }
        operation_end(mark, 0, 0, 0, 1);
    }
}

static void heap_copy_row(int kind, int glue) {
    const NumericOps* ops = &numeric_ops[kind];
    for (int shape = 0; shape < (glue ? 3 : 2); shape++) {
        void* source = make_heap(kind);
        void* copied = NULL;
        OperationMark mark = operation_begin();
        if (glue) ops->clone[shape]((void*)&copied, (void*)&source);
        else copied = shape == 0 ? ops->copy(source) : ops->explicit_clone(source);
        check(copied == source && heap_rc(kind, source) == 2,
              "heap Copy retains exactly one shared owner");
        check(expected_heap_value(kind, source), "heap Copy preserves the value");
        if (glue) {
            ops->drop[shape]((void*)&copied);
            copied = NULL;
            check(heap_rc(kind, source) == 1, "dropping glue Copy preserves its caller");
        }
        operation_end(mark, 0, 0, 0, 0);
        check_copy_cost(kind);
        if (!glue) ops->release(copied);
        ops->release(source);
    }
}

static void heap_private_row(int kind, int operation) {
    const NumericOps* ops = &numeric_ops[kind];
    void* source = make_heap(kind);
    void* slot = source;
    if (operation == 10) ops->retain(source); // The sibling stays on this shard.
    OperationMark mark = operation_begin();
    if (operation == 8) {
        ops->drop[0]((void*)&slot);
        // Do not dereference the last owner's freed block. A removed release
        // intentionally leaks it; both the named failure and Valgrind see it.
        source = NULL;
        slot = NULL;
        operation_end(mark, 0, 1, 0, 0);
    } else if (operation == 9) {
        ops->unshare((void*)&slot);
        check(slot == source && heap_rc(kind, source) == 1,
              "unique unshare transfers the existing block");
        operation_end(mark, 0, 0, 0, 0);
        ops->release(slot);
    } else {
        if (operation == 10) ops->unshare((void*)&slot);
        else ops->cross_clone((void*)&slot, (void*)&source);
        check(slot != source, "private sibling not detached");
        check(expected_heap_value(kind, slot), "private copy preserves the value");
        check(heap_rc(kind, source) == 1 && heap_rc(kind, slot) == 1,
              "private copy leaves two independently owned blocks");
        operation_end(mark, 1, 0, operation == 10 ? 1 : 0, 0);
        // The identity mutant leaves rc=2: releasing these two obligations is
        // still safe and lets the child report its named failure normally.
        ops->release(slot);
        ops->release(source);
    }
}

int main(int argc, char** argv) {
    static const char* const rows[] = {
        "inline-null-retain", "inline-null-drop", "inline-null-explicit-clone",
        "inline-null-glue-copy-drop", "inline-null-unshare", "inline-null-cross-clone",
        "heap-scalar-copy", "heap-glue-copy", "heap-release-last-owner",
        "heap-unshare-unique", "heap-unshare-sibling", "heap-cross-clone",
    };
    current_row = argc > 1 ? argv[1] : getenv("SURGE_NUMERIC_EMIT_ROW");
    if (current_row == NULL) current_row = "";
    int kind = -1;
    if (strncmp(current_row, "int/", 4) == 0) kind = 0;
    else if (strncmp(current_row, "uint/", 5) == 0) kind = 1;
    int operation = -1;
    if (kind >= 0) {
        const char* suffix = current_row + (kind == 0 ? 4 : 5);
        for (size_t i = 0; i < sizeof(rows) / sizeof(rows[0]); i++) {
            if (strcmp(suffix, rows[i]) == 0) operation = (int)i;
        }
    }
    if (operation < 0) {
        fprintf(stderr, "numeric-emit: unknown row \"%s\"\n", current_row);
        return 2;
    }
    HeapMark before = heap_mark();
    if (operation < 6) inline_row(kind, operation);
    else if (operation < 8) heap_copy_row(kind, operation == 7);
    else heap_private_row(kind, operation);
    HeapMark after = heap_mark();
    uint64_t outstanding = after.allocs - before.allocs - (after.frees - before.frees);
    check(outstanding == 0, "all heap owners released");
    printf("numeric-emit: row=%s failures=%d outstanding_blocks=%" PRIu64 "\n",
           current_row, failures, outstanding);
    return failures == 0 ? 0 : 1;
}
