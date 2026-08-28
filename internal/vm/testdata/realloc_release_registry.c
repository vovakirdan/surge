#include "rt.h"

#include "rt_array_internal.h"
#include "rt_heap_accounting.h"

#include <stdalign.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

// A reallocation RELEASES the old block, and the runtime has exactly one
// release: rt_free, which tells the array view registry to forget the address.
//
// Why the view registry is the instrument. A slice does not copy: the view is a
// second header pointing into the base's element run, and the registry records
// the pair so that growing the base can repoint every view onto the moved run.
// Both sides of that pair are raw ADDRESSES, so a registration outlives the
// block it names unless the release says otherwise. That makes the registry a
// direct read of the question "did this release go through the runtime's one
// release path", answerable without a sanitizer and without depending on
// whether the allocator moved anything.
//
// Why a reallocated header is a fair thing to ask about. rt_realloc is public:
// it is declared in rt.h and spelled as an @intrinsic in core/intrinsics.sg, so
// any caller may hand it any block, and the registry has no way to know which
// blocks are headers. It is also the only release in the tree that could ever
// skip rt_free -- the alignment-conditional fast path handed the block to libc
// realloc, which frees the old block itself and tells nothing else. Every
// registration it named survived, aimed at an address the allocator had taken
// back.
//
// The rows below are the two shapes that stale registration has, plus the
// registry census that sees it before either one fires, plus the release a
// reallocation owes when the block it replaces has a zero old size.

// The entry point's argv, which this stand has none of and rt_io.c requires.
int rt_argc = 0;
char** rt_argv_raw = NULL;

#define BASE_BYTES 8u
#define GROWN_HEADER_BYTES (sizeof(SurgeArrayHeader) * 4u)

static int failures;

static void check(int condition, const char* what) {
    if (!condition) {
        failures++;
        printf("FAIL %s\n", what);
    }
}

// How many registrations still name `address`, on either side of a pair. The
// pointers are compared as integers on purpose: after a release the block is
// gone, and dereferencing it is the defect rather than the measurement.
//
// Read this the instant the release returns and not one allocation later. The
// allocator hands a released address straight back out, so a header allocated
// afterwards can land on it and register there for real -- a count taken then
// says "one" about a registration that is perfectly sound. A reallocation
// allocates before it releases, so the block it returns is never the block it
// released, and the moment between the two is the only unambiguous reading.
static size_t links_naming(uintptr_t address) {
    size_t count = 0;
    rt_array_registry_lock();
    for (const SurgeArrayViewLink* link = rt_array_views_head_locked(); link != NULL;
         link = link->next) {
        if ((uintptr_t)link->view == address || (uintptr_t)link->base == address) {
            count++;
        }
    }
    rt_array_registry_unlock();
    return count;
}

// A base array shaped the way the runtime shapes one: a header block and a
// separate element run, both from rt_alloc.
static SurgeArrayHeader* make_base(uint64_t len) {
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        return NULL;
    }
    header->data = rt_alloc(len, 1);
    if (header->data == NULL) {
        rt_free((uint8_t*)header,
                (uint64_t)sizeof(SurgeArrayHeader),
                (uint64_t)alignof(SurgeArrayHeader));
        return NULL;
    }
    memset(header->data, 0, (size_t)len);
    header->len = len;
    header->cap = len;
    return header;
}

static void free_header(void* header) {
    rt_free(
        (uint8_t*)header, (uint64_t)sizeof(SurgeArrayHeader), (uint64_t)alignof(SurgeArrayHeader));
}

// A whole-array range: no start and no end reads as both bounds absent, which
// is the full run.
static SurgeRange whole_range(void) {
    SurgeRange range;
    memset(&range, 0, sizeof(range));
    return range;
}

// Row 1. The census, which needs no sanitizer and does not care whether the
// allocator moved the block: a release ran, so the registration is gone.
static void row_release_forgets_registration(void) {
    SurgeArrayHeader* base = make_base(BASE_BYTES);
    check(base != NULL, "row 1: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* slot = base;
    SurgeRange range = whole_range();
    SurgeArrayHeader* view = (SurgeArrayHeader*)rt_array_slice((void*)&slot, &range, 1);
    check(view != NULL, "row 1: view allocated");
    if (view == NULL) {
        return;
    }
    uintptr_t released = (uintptr_t)view;
    check(links_naming(released) == 1, "row 1: the slice registered the view");

    void* moved = rt_realloc((uint8_t*)view,
                             (uint64_t)sizeof(SurgeArrayHeader),
                             (uint64_t)GROWN_HEADER_BYTES,
                             (uint64_t)alignof(SurgeArrayHeader));
    check(moved != NULL, "row 1: the reallocation succeeded");

    size_t stale = links_naming(released);
    printf("release-forgets-registration: stale=%zu\n", stale);
    check(stale == 0, "row 1: the released header holds no registration");

    rt_free((uint8_t*)moved, (uint64_t)GROWN_HEADER_BYTES, (uint64_t)alignof(SurgeArrayHeader));
    rt_free((uint8_t*)base->data, BASE_BYTES, 1);
    free_header(base);
}

// Row 2. What a stale VIEW registration does when the base grows: syncing the
// views writes the moved element run into every registered view, which is a
// write through a released header.
static void row_released_view_then_base_grows(void) {
    SurgeArrayHeader* base = make_base(BASE_BYTES);
    check(base != NULL, "row 2: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* slot = base;
    SurgeRange range = whole_range();
    SurgeArrayHeader* view = (SurgeArrayHeader*)rt_array_slice((void*)&slot, &range, 1);
    check(view != NULL, "row 2: view allocated");
    if (view == NULL) {
        return;
    }
    uintptr_t released = (uintptr_t)view;
    void* moved = rt_realloc((uint8_t*)view,
                             (uint64_t)sizeof(SurgeArrayHeader),
                             (uint64_t)GROWN_HEADER_BYTES,
                             (uint64_t)alignof(SurgeArrayHeader));
    check(moved != NULL, "row 2: the reallocation succeeded");
    size_t stale = links_naming(released);

    uint8_t payload[64];
    memset(payload, 0xAB, sizeof(payload));
    // Past the base's capacity, so the element run is reallocated and the view
    // sync runs over whatever the registry still holds.
    rt_array_append_raw_bytes((void*)&slot, payload, (uint64_t)sizeof(payload));

    printf("released-view-then-base-grows: stale=%zu len=%llu\n",
           stale,
           (unsigned long long)base->len);
    check(stale == 0, "row 2: the released view holds no registration");
    check(base->len == BASE_BYTES + sizeof(payload), "row 2: the append landed");

    rt_free((uint8_t*)moved, (uint64_t)GROWN_HEADER_BYTES, (uint64_t)alignof(SurgeArrayHeader));
    rt_free((uint8_t*)base->data, base->cap, 1);
    free_header(base);
}

// Row 3. What a stale BASE registration does when the view is sliced again:
// resolving the view's base hands back the registered address and the slice
// reads its element run, which is a read through a released header.
static void row_released_base_then_view_slices(void) {
    SurgeArrayHeader* base = make_base(BASE_BYTES);
    check(base != NULL, "row 3: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* slot = base;
    SurgeRange range = whole_range();
    SurgeArrayHeader* view = (SurgeArrayHeader*)rt_array_slice((void*)&slot, &range, 1);
    check(view != NULL, "row 3: view allocated");
    if (view == NULL) {
        return;
    }
    uintptr_t released = (uintptr_t)base;
    SurgeArrayHeader* moved = (SurgeArrayHeader*)rt_realloc((uint8_t*)base,
                                                            (uint64_t)sizeof(SurgeArrayHeader),
                                                            (uint64_t)GROWN_HEADER_BYTES,
                                                            (uint64_t)alignof(SurgeArrayHeader));
    check(moved != NULL, "row 3: the reallocation succeeded");
    if (moved == NULL) {
        return;
    }
    size_t stale = links_naming(released);

    SurgeArrayHeader* view_slot = view;
    SurgeArrayHeader* nested = (SurgeArrayHeader*)rt_array_slice((void*)&view_slot, &range, 1);
    check(nested != NULL, "row 3: the nested slice returned a view");

    printf("released-base-then-view-slices: stale=%zu nested_len=%llu\n",
           stale,
           nested == NULL ? 0ULL : (unsigned long long)nested->len);
    check(stale == 0, "row 3: the released base holds no registration");

    if (nested != NULL) {
        free_header(nested);
    }
    free_header(view);
    rt_free((uint8_t*)moved->data, BASE_BYTES, 1);
    rt_free((uint8_t*)moved, (uint64_t)GROWN_HEADER_BYTES, (uint64_t)alignof(SurgeArrayHeader));
}

// Row 4. A reallocation whose old size is zero still owes the release: the
// block behind the pointer is real whatever the caller said its size was, and
// the counters are where a block nobody released shows up.
static void row_zero_old_size_still_releases(void) {
    struct rt_heap_accounting_snapshot before;
    struct rt_heap_accounting_snapshot after;
    rt_heap_accounting* accounting = rt_runtime_global_heap_accounting();
    check(rt_heap_accounting_snapshot(accounting, &before) == RT_HEAP_ACCOUNTING_OK,
          "row 4: snapshot before");

    void* block = rt_alloc(48, 64);
    check(block != NULL, "row 4: over-aligned block allocated");
    void* moved = rt_realloc((uint8_t*)block, 0, 96, 64);
    check(moved != NULL, "row 4: the reallocation succeeded");
    rt_free((uint8_t*)moved, 96, 64);

    check(rt_heap_accounting_snapshot(accounting, &after) == RT_HEAP_ACCOUNTING_OK,
          "row 4: snapshot after");
    uint64_t live_delta = after.live_blocks - before.live_blocks;
    printf("zero-old-size-still-releases: live_delta=%llu\n", (unsigned long long)live_delta);
    check(live_delta == 0, "row 4: nothing stayed live");
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

int main(int argc, char** argv) {
    const char* mode = argc > 1 ? argv[1] : "all";
    if (strcmp(mode, "all") == 0 || strcmp(mode, "release-forgets-registration") == 0) {
        row_release_forgets_registration();
    }
    if (strcmp(mode, "all") == 0 || strcmp(mode, "released-view-then-base-grows") == 0) {
        row_released_view_then_base_grows();
    }
    if (strcmp(mode, "all") == 0 || strcmp(mode, "released-base-then-view-slices") == 0) {
        row_released_base_then_view_slices();
    }
    if (strcmp(mode, "all") == 0 || strcmp(mode, "zero-old-size-still-releases") == 0) {
        row_zero_old_size_still_releases();
    }
    printf("realloc-release-registry: failures=%d\n", failures);
    return failures == 0 ? 0 : 1;
}
