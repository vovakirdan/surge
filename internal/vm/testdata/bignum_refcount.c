#include "rt.h"

#include "rt_bignum_internal.h"
#include "rt_heap_accounting.h"
#include "rt_resident_bytes.h"

#include <inttypes.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

// The heap halves of `int` and `uint` are reference counted, and this stand is
// what reads the count. No compiled program can: the emitter does not call
// these entry points yet, and the rc word is not reachable from Surge source at
// all. So the questions below are asked in C, directly, one row per process.
//
// What each group of rows is for:
//
//   - a value small enough to ride INLINE in the word owns no block. Every
//     entry point has to answer it from the tag, before any load, because an
//     inline word is an integer with the low bit set -- neither aligned nor
//     mapped as an address. NULL, the canonical zero, is the same story. There
//     is one row per entry point per kind, each asserting that nothing was
//     allocated, nothing was freed, and the word came back unchanged;
//   - a heap value starts at one, counts up and down, and gives its block back
//     when the count reaches zero. "Gave it back" is read from the allocator's
//     own alloc/free counters -- never from live_bytes, which drifts upward on
//     every bignum whose `len` was trimmed after allocation, a pre-existing
//     accounting defect that has nothing to do with counting -- and, under the
//     sanitizer build, from ASan's leak and double-free reports;
//   - unshare, the crossing barrier's leaf, hands back the SAME block when the
//     caller holds the only reference and a duplicate when a sibling still
//     holds it. That is the precondition a crossing needs: each side leaves
//     with a block of its own;
//   - and the aliasing row, which exists because the count sits in the tail.
//     bi_as_uint hands out a biguint view of a bigint's tail, so the view's
//     `rc` is the INT'S OWN count word at the same address. The row asserts
//     that alias field by field: it is what makes the layout legal, and it is
//     also the reason a view must never be retained, released or freed.
//
// A row that survives prints a census and exits 0. Every row survives in every
// build this lane ships; the only rows that die are the ten fixnum rows under
// the tag-guard negative control, and the driver reads that death from the exit
// status the way it reads a census from stdout.

// The entry point's argv, which this stand has none of and rt_io.c requires.
int rt_argc = 0;
char** rt_argv_raw = NULL;

// Sixty digits: far outside the inline range and outside int64 as well, so the
// literal cannot be demoted back to a fixnum and stays a multi-limb block. The
// second differs from the first, so a clone that returned its source rather
// than a copy would still be caught by the limb comparison.
#define BIG_DIGITS "123456789012345678901234567890123456789012345678901234567890"
#define OTHER_DIGITS "987654321098765432109876543210987654321098765432109876543210"
// A value that fits inline: (7 << 1) | 1 is a word with the low bit set.
#define SMALL_VALUE 7

static int failures;

static void check(int condition, const char* what) {
    if (!condition) {
        failures++;
        printf("FAIL %s\n", what);
    }
}

// The allocator's own counters, which this stand reads as deltas around a step.
// Counts, never bytes: 16 sites shrink a bignum's `len` after allocating it and
// size the free off the shrunken field, so live_bytes climbs for a reason that
// predates this lane and would make a byte assertion red on arrival.
typedef struct HeapMark {
    uint64_t allocs;
    uint64_t frees;
} HeapMark;

static HeapMark heap_mark(void) {
    struct rt_heap_accounting_snapshot snapshot;
    memset(&snapshot, 0, sizeof(snapshot));
    HeapMark mark = {0, 0};
    rt_heap_accounting_status status =
        rt_heap_accounting_snapshot(rt_runtime_global_heap_accounting(), &snapshot);
    check(status == RT_HEAP_ACCOUNTING_OK, "heap accounting answered");
    mark.allocs = snapshot.alloc_count;
    mark.frees = snapshot.free_count;
    return mark;
}

static uint64_t allocs_since(HeapMark mark) {
    return heap_mark().allocs - mark.allocs;
}

static uint64_t frees_since(HeapMark mark) {
    return heap_mark().frees - mark.frees;
}

static uint64_t unshare_clones_now(void) {
    return rt_resident_bytes_snapshot().unshare_clones;
}

// A heap bigint from the 60-digit literal, checked to be what the rows assume:
// a block, not an inline word, and more than one limb.
static void* make_big_int(const char* digits) {
    void* value = rt_bigint_from_literal((const uint8_t*)digits, (uint64_t)strlen(digits));
    check(fix_is_heap(value), "the literal produced a heap block, not an inline word");
    if (!fix_is_heap(value)) {
        return NULL;
    }
    check(((const SurgeBigInt*)value)->len > 1, "the literal needed more than one limb");
    return value;
}

static void* make_big_uint(const char* digits) {
    void* value = rt_biguint_from_literal((const uint8_t*)digits, (uint64_t)strlen(digits));
    check(fix_is_heap(value), "the literal produced a heap block, not an inline word");
    if (!fix_is_heap(value)) {
        return NULL;
    }
    check(((const SurgeBigUint*)value)->len > 1, "the literal needed more than one limb");
    return value;
}

static bool same_int_magnitude(const void* a, const void* b) {
    const SurgeBigInt* x = (const SurgeBigInt*)a;
    const SurgeBigInt* y = (const SurgeBigInt*)b;
    if (x->len != y->len || x->neg != y->neg) {
        return false;
    }
    return memcmp(x->limbs, y->limbs, (size_t)x->len * sizeof(uint32_t)) == 0;
}

static bool same_uint_magnitude(const void* a, const void* b) {
    const SurgeBigUint* x = (const SurgeBigUint*)a;
    const SurgeBigUint* y = (const SurgeBigUint*)b;
    if (x->len != y->len) {
        return false;
    }
    return memcmp(x->limbs, y->limbs, (size_t)x->len * sizeof(uint32_t)) == 0;
}

// An inline fixnum and NULL own nothing, so every entry point has to answer
// them from the tag without touching memory. There is ONE ROW PER ENTRY POINT
// rather than one row that calls all five, and the reason is the negative
// control: under the tag guard's control a row dies at its FIRST dereference,
// so a row calling retain and then free would only ever name retain's guard.
// Split, each row's death names the one body whose guard was cut.
static void* small_int_word(void) {
    void* small = rt_bigint_from_i64(SMALL_VALUE);
    check(fix_is_inline(small), "a small int rides inline in the word");
    return small;
}

static void* small_uint_word(void) {
    void* small = rt_biguint_from_u64((uint64_t)SMALL_VALUE);
    check(fix_is_inline(small), "a small uint rides inline in the word");
    return small;
}

static void fixnum_census(const char* name, HeapMark mark) {
    uint64_t allocs = allocs_since(mark);
    uint64_t frees = frees_since(mark);
    printf("%s: allocs=%" PRIu64 " frees=%" PRIu64 "\n", name, allocs, frees);
    check(allocs == 0, "a value with no block allocates nothing");
    check(frees == 0, "a value with no block frees nothing");
}

static void row_int_fixnum_retain_is_uncounted(void) {
    void* small = small_int_word();
    HeapMark mark = heap_mark();
    rt_bigint_retain(small);
    rt_bigint_retain(NULL);
    fixnum_census("int-fixnum-retain-is-uncounted", mark);
}

static void row_int_fixnum_release_is_uncounted(void) {
    void* small = small_int_word();
    HeapMark mark = heap_mark();
    rt_bigint_release(small);
    rt_bigint_release(NULL);
    fixnum_census("int-fixnum-release-is-uncounted", mark);
}

static void row_int_fixnum_free_is_uncounted(void) {
    void* small = small_int_word();
    HeapMark mark = heap_mark();
    rt_bigint_free(small);
    rt_bigint_free(NULL);
    fixnum_census("int-fixnum-free-is-uncounted", mark);
}

static void row_int_fixnum_clone_is_uncounted(void) {
    const void* small = small_int_word();
    HeapMark mark = heap_mark();
    check(rt_bigint_clone(small) == small, "cloning an inline word returns the word");
    check(rt_bigint_clone(NULL) == NULL, "cloning the canonical zero returns it");
    fixnum_census("int-fixnum-clone-is-uncounted", mark);
}

static void row_int_fixnum_unshare_is_uncounted(void) {
    void* small = small_int_word();
    HeapMark mark = heap_mark();
    check(rt_bigint_unshare(small) == small, "an inline word is already private");
    check(rt_bigint_unshare(NULL) == NULL, "the canonical zero is already private");
    fixnum_census("int-fixnum-unshare-is-uncounted", mark);
}

static void row_uint_fixnum_retain_is_uncounted(void) {
    void* small = small_uint_word();
    HeapMark mark = heap_mark();
    rt_biguint_retain(small);
    rt_biguint_retain(NULL);
    fixnum_census("uint-fixnum-retain-is-uncounted", mark);
}

static void row_uint_fixnum_release_is_uncounted(void) {
    void* small = small_uint_word();
    HeapMark mark = heap_mark();
    rt_biguint_release(small);
    rt_biguint_release(NULL);
    fixnum_census("uint-fixnum-release-is-uncounted", mark);
}

static void row_uint_fixnum_free_is_uncounted(void) {
    void* small = small_uint_word();
    HeapMark mark = heap_mark();
    rt_biguint_free(small);
    rt_biguint_free(NULL);
    fixnum_census("uint-fixnum-free-is-uncounted", mark);
}

static void row_uint_fixnum_clone_is_uncounted(void) {
    const void* small = small_uint_word();
    HeapMark mark = heap_mark();
    check(rt_biguint_clone(small) == small, "cloning an inline word returns the word");
    check(rt_biguint_clone(NULL) == NULL, "cloning the canonical zero returns it");
    fixnum_census("uint-fixnum-clone-is-uncounted", mark);
}

static void row_uint_fixnum_unshare_is_uncounted(void) {
    void* small = small_uint_word();
    HeapMark mark = heap_mark();
    check(rt_biguint_unshare(small) == small, "an inline word is already private");
    check(rt_biguint_unshare(NULL) == NULL, "the canonical zero is already private");
    fixnum_census("uint-fixnum-unshare-is-uncounted", mark);
}

// A block off the allocator carries a count of one: the reference being
// returned, and no other.
static void row_fresh_heap_value_starts_at_one(void) {
    void* value = make_big_int(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    uint32_t count = ((const SurgeBigInt*)value)->rc;
    printf("fresh-heap-value-starts-at-one: rc=%" PRIu32 "\n", count);
    check(count == 1, "starts-at-one: a fresh block starts at one");
    rt_bigint_release(value);
}

// The count follows the references, and the block goes back to the
// allocator on the release that reaches zero -- not before, and exactly once.
// The count is deliberately not read after that release: the block is gone.
static void row_heap_value_counts_and_frees_at_zero(void) {
    void* value = make_big_int(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    const SurgeBigInt* block = (const SurgeBigInt*)value;
    HeapMark mark = heap_mark();
    rt_bigint_retain(value);
    check(block->rc == 2, "counts-and-frees: a retain counts up");
    rt_bigint_retain(value);
    check(block->rc == 3, "counts-and-frees: retains accumulate");
    rt_bigint_release(value);
    check(block->rc == 2, "counts-and-frees: a release counts down");
    check(frees_since(mark) == 0, "counts-and-frees: a release above zero frees nothing");
    rt_bigint_release(value);
    check(block->rc == 1, "counts-and-frees: the last sibling leaves one reference");
    check(frees_since(mark) == 0, "counts-and-frees: still nothing freed while a holder remains");
    rt_bigint_release(value);
    uint64_t frees = frees_since(mark);
    uint64_t allocs = allocs_since(mark);
    printf("heap-value-counts-and-frees-at-zero: allocs=%" PRIu64 " frees=%" PRIu64 "\n",
           allocs,
           frees);
    check(allocs == 0, "counts-and-frees: counting a block allocates nothing");
    check(frees == 1, "counts-and-frees: the release that reached zero gave the block back, once");
}

// At count one the caller already holds the only reference, so the
// barrier hands the same block on and allocates nothing.
static void row_unshare_at_one_hands_back_the_same_block(void) {
    void* value = make_big_int(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    check(((const SurgeBigInt*)value)->rc == 1, "unshare-at-one: the value starts at one");
    HeapMark mark = heap_mark();
    uint64_t clones_before = unshare_clones_now();
    void* private_value = rt_bigint_unshare(value);
    uint64_t allocs = allocs_since(mark);
    uint64_t clones = unshare_clones_now() - clones_before;
    printf("unshare-at-one-hands-back-the-same-block: same=%d allocs=%" PRIu64 " clones=%" PRIu64
           "\n",
           private_value == value ? 1 : 0,
           allocs,
           clones);
    check(private_value == value, "unshare-at-one: the same block travels");
    check(allocs == 0, "unshare-at-one: a sole reference needs no duplicate");
    check(clones == 0, "unshare-at-one: nothing was recorded as an unshare clone");
    check(((const SurgeBigInt*)value)->rc == 1, "unshare-at-one: the count did not move");
    rt_bigint_release(private_value);
}

// With a sibling holder the barrier duplicates: the caller leaves with a
// block of its own at count one, the sibling keeps the original at count one,
// and each side can then release independently. Under the bigint unshare
// negative control the clone branch is cut and this row goes red on the very
// first assertion.
static void row_unshare_with_a_sibling_clones_once(void) {
    void* value = make_big_int(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    rt_bigint_retain(value); // A sibling on this shard still holds it.
    check(((const SurgeBigInt*)value)->rc == 2, "unshare-with-a-sibling: two holders");
    HeapMark mark = heap_mark();
    uint64_t clones_before = unshare_clones_now();
    void* private_value = rt_bigint_unshare(value);
    uint64_t allocs = allocs_since(mark);
    uint64_t clones = unshare_clones_now() - clones_before;
    printf("unshare-with-a-sibling-clones-once: different=%d allocs=%" PRIu64 " clones=%" PRIu64
           "\n",
           private_value != value ? 1 : 0,
           allocs,
           clones);
    check(private_value != value, "unshare-with-a-sibling: the caller left with a different block");
    check(allocs == 1, "unshare-with-a-sibling: exactly one new block");
    check(clones == 1, "unshare-with-a-sibling: the duplication was recorded once");
    if (private_value != value) {
        check(((const SurgeBigInt*)private_value)->rc == 1,
              "unshare-with-a-sibling: the duplicate starts at one");
        check(same_int_magnitude(private_value, value),
              "unshare-with-a-sibling: the duplicate has the same value");
    }
    check(((const SurgeBigInt*)value)->rc == 1,
          "unshare-with-a-sibling: the reference the caller held was given up");
    HeapMark release_mark = heap_mark();
    rt_bigint_release(private_value);
    rt_bigint_release(value);
    check(frees_since(release_mark) == 2, "unshare-with-a-sibling: both sides released cleanly");
}

// The count sits in the tail, and this is what that buys and what it
// costs. bi_as_uint hands out a biguint view of a bigint's tail; the view's
// three fields must land on the int's three, byte for byte, or every magnitude
// helper in the runtime reads the wrong words. They do -- and the consequence
// is that the view's `rc` IS the int's count, at one address. Retaining or
// releasing through a view would move the int's count behind its back, and
// freeing one would hand the allocator a pointer four bytes into the block. The
// row asserts the alias; the rule that nothing may own through a view is
// written where bi_as_uint is defined.
static void row_the_uint_view_of_an_int_aliases_its_count(void) {
    void* value = make_big_int(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    SurgeBigInt* block = (SurgeBigInt*)value;
    const SurgeBigUint* view = bi_as_uint(block);
    check(view != NULL, "the-uint-view: the view exists");
    if (view == NULL) {
        rt_bigint_release(value);
        return;
    }
    int len_aliased = (const void*)&view->len == (const void*)&block->len;
    int rc_aliased = (const void*)&view->rc == (const void*)&block->rc;
    int limbs_aliased = (const void*)&view->limbs[0] == (const void*)&block->limbs[0];
    printf("the-uint-view-of-an-int-aliases-its-count: len=%d rc=%d limbs=%d\n",
           len_aliased,
           rc_aliased,
           limbs_aliased);
    check(len_aliased, "the-uint-view: the view's length is the int's length");
    check(rc_aliased, "the-uint-view: the view's count is the int's own count word");
    check(limbs_aliased, "the-uint-view: the view's limbs are the int's limbs");
    check(view->len == block->len, "the-uint-view: and it reads the same length back");
    check(view->rc == block->rc, "the-uint-view: and the same count");
    rt_bigint_release(value);
}

// The uint half of the counting story.
static void row_biguint_counts_and_frees_at_zero(void) {
    void* value = make_big_uint(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    const SurgeBigUint* block = (const SurgeBigUint*)value;
    check(block->rc == 1, "biguint-counts-and-frees: a fresh block starts at one");
    HeapMark mark = heap_mark();
    rt_biguint_retain(value);
    check(block->rc == 2, "biguint-counts-and-frees: a retain counts up");
    rt_biguint_release(value);
    check(block->rc == 1, "biguint-counts-and-frees: a release counts down");
    check(frees_since(mark) == 0, "biguint-counts-and-frees: a release above zero frees nothing");
    rt_biguint_release(value);
    uint64_t frees = frees_since(mark);
    uint64_t allocs = allocs_since(mark);
    printf(
        "biguint-counts-and-frees-at-zero: allocs=%" PRIu64 " frees=%" PRIu64 "\n", allocs, frees);
    check(allocs == 0, "biguint-counts-and-frees: counting a block allocates nothing");
    check(frees == 1,
          "biguint-counts-and-frees: the release that reached zero gave the block back, once");
}

// The uint half of the sole-reference barrier.
static void row_biguint_unshare_at_one_hands_back_the_same_block(void) {
    void* value = make_big_uint(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    HeapMark mark = heap_mark();
    void* private_value = rt_biguint_unshare(value);
    uint64_t allocs = allocs_since(mark);
    printf("biguint-unshare-at-one-hands-back-the-same-block: same=%d allocs=%" PRIu64 "\n",
           private_value == value ? 1 : 0,
           allocs);
    check(private_value == value, "biguint-unshare-at-one: the same block travels");
    check(allocs == 0, "biguint-unshare-at-one: a sole reference needs no duplicate");
    rt_biguint_release(private_value);
}

// The uint half of the duplicating barrier.
static void row_biguint_unshare_with_a_sibling_clones_once(void) {
    void* value = make_big_uint(BIG_DIGITS);
    if (value == NULL) {
        return;
    }
    rt_biguint_retain(value);
    HeapMark mark = heap_mark();
    uint64_t clones_before = unshare_clones_now();
    void* private_value = rt_biguint_unshare(value);
    uint64_t allocs = allocs_since(mark);
    uint64_t clones = unshare_clones_now() - clones_before;
    printf("biguint-unshare-with-a-sibling-clones-once: different=%d allocs=%" PRIu64
           " clones=%" PRIu64 "\n",
           private_value != value ? 1 : 0,
           allocs,
           clones);
    check(private_value != value,
          "biguint-unshare-with-a-sibling: the caller left with a different block");
    check(allocs == 1, "biguint-unshare-with-a-sibling: exactly one new block");
    check(clones == 1, "biguint-unshare-with-a-sibling: the duplication was recorded once");
    if (private_value != value) {
        check(((const SurgeBigUint*)private_value)->rc == 1,
              "biguint-unshare-with-a-sibling: the duplicate starts at one");
        check(same_uint_magnitude(private_value, value),
              "biguint-unshare-with-a-sibling: the duplicate has the same value");
    }
    check(((const SurgeBigUint*)value)->rc == 1,
          "biguint-unshare-with-a-sibling: the caller gave up its reference");
    HeapMark release_mark = heap_mark();
    rt_biguint_release(private_value);
    rt_biguint_release(value);
    check(frees_since(release_mark) == 2,
          "biguint-unshare-with-a-sibling: both sides released cleanly");
}

// A clone is a block of its own at count one, with the source's value
// and none of its count. This is the duplicate a deep copy installs at a shard
// boundary, and it is the one entry point that must NOT demote a value back to
// an inline fixnum: the caller needs a leaf it can hold, retain and release.
static void row_clone_is_an_independent_block(void) {
    void* value = make_big_int(BIG_DIGITS);
    void* other = make_big_int(OTHER_DIGITS);
    if (value == NULL || other == NULL) {
        return;
    }
    rt_bigint_retain(value); // The count is the source's and is not copied.
    HeapMark mark = heap_mark();
    void* copy = rt_bigint_clone(value);
    uint64_t allocs = allocs_since(mark);
    printf("clone-is-an-independent-block: different=%d allocs=%" PRIu64 "\n",
           copy != value ? 1 : 0,
           allocs);
    check(copy != value, "clone-is-independent: a clone is a different block");
    check(allocs == 1, "clone-is-independent: exactly one new block");
    check(fix_is_heap(copy), "clone-is-independent: a clone stays a block and is not demoted");
    if (fix_is_heap(copy)) {
        check(((const SurgeBigInt*)copy)->rc == 1, "clone-is-independent: the clone starts at one");
        check(same_int_magnitude(copy, value),
              "clone-is-independent: the clone carries the source's value");
        check(!same_int_magnitude(copy, other), "clone-is-independent: and is not just any block");
    }
    check(((const SurgeBigInt*)value)->rc == 2,
          "clone-is-independent: cloning does not move the source's count");
    rt_bigint_release(copy);
    rt_bigint_release(value);
    rt_bigint_release(value);
    rt_bigint_release(other);
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

typedef struct StandRow {
    const char* name;
    void (*run)(void);
} StandRow;

static const StandRow rows[] = {
    {"int-fixnum-retain-is-uncounted", row_int_fixnum_retain_is_uncounted},
    {"int-fixnum-release-is-uncounted", row_int_fixnum_release_is_uncounted},
    {"int-fixnum-free-is-uncounted", row_int_fixnum_free_is_uncounted},
    {"int-fixnum-clone-is-uncounted", row_int_fixnum_clone_is_uncounted},
    {"int-fixnum-unshare-is-uncounted", row_int_fixnum_unshare_is_uncounted},
    {"uint-fixnum-retain-is-uncounted", row_uint_fixnum_retain_is_uncounted},
    {"uint-fixnum-release-is-uncounted", row_uint_fixnum_release_is_uncounted},
    {"uint-fixnum-free-is-uncounted", row_uint_fixnum_free_is_uncounted},
    {"uint-fixnum-clone-is-uncounted", row_uint_fixnum_clone_is_uncounted},
    {"uint-fixnum-unshare-is-uncounted", row_uint_fixnum_unshare_is_uncounted},
    {"fresh-heap-value-starts-at-one", row_fresh_heap_value_starts_at_one},
    {"heap-value-counts-and-frees-at-zero", row_heap_value_counts_and_frees_at_zero},
    {"unshare-at-one-hands-back-the-same-block", row_unshare_at_one_hands_back_the_same_block},
    {"unshare-with-a-sibling-clones-once", row_unshare_with_a_sibling_clones_once},
    {"the-uint-view-of-an-int-aliases-its-count", row_the_uint_view_of_an_int_aliases_its_count},
    {"biguint-counts-and-frees-at-zero", row_biguint_counts_and_frees_at_zero},
    {"biguint-unshare-at-one-hands-back-the-same-block",
     row_biguint_unshare_at_one_hands_back_the_same_block},
    {"biguint-unshare-with-a-sibling-clones-once", row_biguint_unshare_with_a_sibling_clones_once},
    {"clone-is-an-independent-block", row_clone_is_an_independent_block},
};

// One row per process, named on the command line. A name this stand does not
// know is its own failure with its own exit status, so a driver's typo cannot
// read as a row that passed.
int main(int argc, char** argv) {
    const char* mode = argc > 1 ? argv[1] : "";
    for (size_t i = 0; i < sizeof(rows) / sizeof(rows[0]); i++) {
        if (strcmp(mode, rows[i].name) == 0) {
            rows[i].run();
            printf("bignum-refcount: failures=%d\n", failures);
            return failures == 0 ? 0 : 1;
        }
    }
    fprintf(stderr, "bignum-refcount: unknown row \"%s\"\n", mode);
    return 2;
}
