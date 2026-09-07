#include "rt.h"

#include "rt_array_internal.h"

#include <stdalign.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

// Before an owned array leaves its shard, the crossing barrier walks its
// buffer: every element slot is handed to a step that makes that element's
// counted leaves private to the destination. rt_array_unshare_walk is the walk,
// and this stand asks it the questions the emitter never can, because the
// emitter only ever hands it an array it already believes is owned.
//
// Why a view is the interesting input. A slice does not copy: the view is a
// second header whose data pointer aims INTO the base's element run, and the
// base keeps reading that run on the origin shard. Rewriting the slots through
// the view rewrites the base's elements; rewriting them through a base some
// view still reads does the same from the other side. Neither can make the
// elements private, so both are refused by name, and the refusal is a panic
// the process does not survive. The rows below drive one call each from a
// fresh process, so a death is a row's whole answer and the driver reads it
// from the exit status and the report on stderr.
//
// Each surviving row prints a census: how many slots the step was handed, and
// how many of them were not the address the buffer layout says they should be.
// The step records every address it is handed for that count. Every base here
// has spare capacity past its length, the way a grown array does, so a walk
// that reads the capacity instead of the length hands out two slots the array
// does not have, and the census says so twice: two calls too many, and two
// addresses past the run.

// The entry point's argv, which this stand has none of and rt_io.c requires.
int rt_argc = 0;
char** rt_argv_raw = NULL;

#define STRIDE 8u
#define ELEMS 3u
#define SPARE 2u
#define RECORD_CAP 8u
// What the spare slots hold: a byte pattern no element was ever written as,
// so a slot past the run is recognisable in a dump.
#define SPARE_POISON 0xA5

static int failures;

static void check(int condition, const char* what) {
    if (!condition) {
        failures++;
        printf("FAIL %s\n", what);
    }
}

// The recording step: every slot it is handed, in the order it was handed.
static void* recorded[RECORD_CAP];
static size_t calls;

// The step also makes the round trip the barrier's real step makes. That step
// allocates a private copy of a counted leaf and releases the shared one, and
// a release re-enters the view registry: rt_free tells the registry to forget
// the block, under the registry lock. So this step allocates and releases a
// scratch block on every slot, and a walk that still held the registry lock
// would stop on its first slot, inside the walk, instead of in some later
// cleanup where the stand's exit would be the only thing to read.
static void record_slot(void* slot) {
    uint8_t* scratch = rt_alloc(STRIDE, STRIDE);
    if (scratch != NULL) {
        rt_free(scratch, STRIDE, STRIDE);
    }
    if (calls < RECORD_CAP) {
        recorded[calls] = slot;
    }
    calls++;
}

static void reset_record(void) {
    for (size_t i = 0; i < RECORD_CAP; i++) {
        recorded[i] = NULL;
    }
    calls = 0;
}

// How many recorded slots are not where the layout puts element i -- data plus
// i strides, in order -- or lie at or past the end of the run, where the
// array's spare capacity is. A slot past the run is counted even when it sits
// exactly where element i WOULD be, because that is precisely what a walk over
// the capacity hands out. Compared as integers so a stale address is a count
// and not a read.
static size_t misplaced_slots(const SurgeArrayHeader* base) {
    size_t misplaced = 0;
    uintptr_t run_end = (uintptr_t)base->data + base->len * STRIDE;
    for (size_t i = 0; i < calls && i < RECORD_CAP; i++) {
        uintptr_t got = (uintptr_t)recorded[i];
        uintptr_t want = (uintptr_t)base->data + i * STRIDE;
        if (got != want || got >= run_end) {
            misplaced++;
        }
    }
    return misplaced;
}

// A base array shaped the way the runtime shapes one after it has grown: a
// header block and a separate element run of `cap` slots of STRIDE bytes, both
// from rt_alloc, of which the first `len` hold elements and the rest are spare
// capacity filled with the poison pattern. A zero capacity is a header with
// no run at all, which is what an empty owned array looks like before its
// first append.
static SurgeArrayHeader* make_base(uint64_t len, uint64_t cap) {
    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        return NULL;
    }
    header->len = len;
    header->cap = cap;
    header->data = NULL;
    if (cap == 0) {
        return header;
    }
    header->data = rt_alloc(cap * STRIDE, STRIDE);
    if (header->data == NULL) {
        rt_free((uint8_t*)header,
                (uint64_t)sizeof(SurgeArrayHeader),
                (uint64_t)alignof(SurgeArrayHeader));
        return NULL;
    }
    memset(header->data, 0, (size_t)(len * STRIDE));
    memset((uint8_t*)header->data + len * STRIDE, SPARE_POISON, (size_t)((cap - len) * STRIDE));
    return header;
}

// A base of ELEMS elements with SPARE slots of capacity past them.
static SurgeArrayHeader* make_grown_base(void) {
    return make_base(ELEMS, ELEMS + SPARE);
}

static void free_base(SurgeArrayHeader* base) {
    if (base->data != NULL) {
        rt_free((uint8_t*)base->data, base->cap * STRIDE, STRIDE);
    }
    rt_free(
        (uint8_t*)base, (uint64_t)sizeof(SurgeArrayHeader), (uint64_t)alignof(SurgeArrayHeader));
}

// A view over the whole of what `slot` holds: no start and no end reads as
// both bounds absent, which is the full run.
static SurgeArrayHeader* slice_whole(SurgeArrayHeader** slot) {
    SurgeRange range;
    memset(&range, 0, sizeof(range));
    return (SurgeArrayHeader*)rt_array_slice((void*)slot, &range, STRIDE);
}

// A view is dropped the way drop emission drops one: the runtime unlinks it
// and frees its header, and the base keeps its elements.
static void drop_view(SurgeArrayHeader* view) {
    rt_array_free_elems(view, STRIDE, STRIDE, NULL);
}

// Row 1. An owned base is walked slot by slot, in layout order, and the spare
// capacity past its length is not walked.
static void row_owned_base_walks_every_slot(void) {
    SurgeArrayHeader* base = make_grown_base();
    check(base != NULL, "row 1: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* slot = base;
    reset_record();
    rt_array_unshare_walk((void*)&slot, STRIDE, record_slot);
    size_t misplaced = misplaced_slots(base);
    printf("owned-base-walks-every-slot: calls=%zu misplaced=%zu\n", calls, misplaced);
    check(calls == ELEMS, "row 1: one call per element");
    check(misplaced == 0, "row 1: every slot at data + i * stride, inside the run");
    free_base(base);
}

// Row 2. Nothing to walk: an empty base that still has spare capacity, an
// empty base with no run at all, a slot holding no array, and no slot. None
// of them is a refusal, because none of them has a slot some other holder
// reads; and none of them has a slot to walk, whatever its capacity says.
static void row_empty_and_null_walk_nothing(void) {
    SurgeArrayHeader* empty = make_base(0, SPARE);
    check(empty != NULL, "row 2: empty base allocated");
    if (empty == NULL) {
        return;
    }
    SurgeArrayHeader* bare = make_base(0, 0);
    check(bare != NULL, "row 2: bare base allocated");
    if (bare == NULL) {
        free_base(empty);
        return;
    }
    reset_record();
    SurgeArrayHeader* slot = empty;
    rt_array_unshare_walk((void*)&slot, STRIDE, record_slot);
    SurgeArrayHeader* bare_slot = bare;
    rt_array_unshare_walk((void*)&bare_slot, STRIDE, record_slot);
    SurgeArrayHeader* none = NULL;
    rt_array_unshare_walk((void*)&none, STRIDE, record_slot);
    rt_array_unshare_walk(NULL, STRIDE, record_slot);
    printf("empty-and-null-walk-nothing: calls=%zu\n", calls);
    check(calls == 0, "row 2: the step was never handed a slot");
    free_base(bare);
    free_base(empty);
}

// Row 3. A view refuses: its slots are the base's slots. The process dies in
// the call; under the negative control it survives and the census shows the
// base's three slots handed out through the view.
static void row_view_refuses_to_cross(void) {
    SurgeArrayHeader* base = make_grown_base();
    check(base != NULL, "row 3: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* base_slot = base;
    SurgeArrayHeader* view = slice_whole(&base_slot);
    check(view != NULL, "row 3: view allocated");
    if (view == NULL) {
        return;
    }
    SurgeArrayHeader* view_slot = view;
    reset_record();
    rt_array_unshare_walk((void*)&view_slot, STRIDE, record_slot);
    printf("view-refuses-to-cross: calls=%zu misplaced=%zu\n", calls, misplaced_slots(base));
    drop_view(view);
    free_base(base);
}

// Row 4. A base with a live view refuses: a reader of its slots is still on
// this shard.
static void row_base_with_live_view_refuses_to_cross(void) {
    SurgeArrayHeader* base = make_grown_base();
    check(base != NULL, "row 4: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* base_slot = base;
    SurgeArrayHeader* view = slice_whole(&base_slot);
    check(view != NULL, "row 4: view allocated");
    if (view == NULL) {
        return;
    }
    reset_record();
    rt_array_unshare_walk((void*)&base_slot, STRIDE, record_slot);
    printf("base-with-live-view-refuses-to-cross: calls=%zu\n", calls);
    drop_view(view);
    free_base(base);
}

// Row 5. The refusal is about the view that is alive NOW: once it drops, the
// same base walks again.
static void row_base_walks_again_after_the_view_drops(void) {
    SurgeArrayHeader* base = make_grown_base();
    check(base != NULL, "row 5: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* base_slot = base;
    SurgeArrayHeader* view = slice_whole(&base_slot);
    check(view != NULL, "row 5: view allocated");
    if (view == NULL) {
        return;
    }
    drop_view(view);
    reset_record();
    rt_array_unshare_walk((void*)&base_slot, STRIDE, record_slot);
    size_t misplaced = misplaced_slots(base);
    printf("base-walks-again-after-the-view-drops: calls=%zu misplaced=%zu\n", calls, misplaced);
    check(calls == ELEMS, "row 5: one call per element");
    check(misplaced == 0, "row 5: every slot at data + i * stride, inside the run");
    free_base(base);
}

// Row 6. A view of a view is registered against the root base, and its slots
// are the base's slots as much as the first view's were.
static void row_view_of_a_view_refuses_to_cross(void) {
    SurgeArrayHeader* base = make_grown_base();
    check(base != NULL, "row 6: base allocated");
    if (base == NULL) {
        return;
    }
    SurgeArrayHeader* base_slot = base;
    SurgeArrayHeader* view = slice_whole(&base_slot);
    check(view != NULL, "row 6: view allocated");
    if (view == NULL) {
        return;
    }
    SurgeArrayHeader* view_slot = view;
    SurgeArrayHeader* nested = slice_whole(&view_slot);
    check(nested != NULL, "row 6: nested view allocated");
    if (nested == NULL) {
        return;
    }
    SurgeArrayHeader* nested_slot = nested;
    reset_record();
    rt_array_unshare_walk((void*)&nested_slot, STRIDE, record_slot);
    printf("view-of-a-view-refuses-to-cross: calls=%zu\n", calls);
    drop_view(nested);
    drop_view(view);
    free_base(base);
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
    {"owned-base-walks-every-slot", row_owned_base_walks_every_slot},
    {"empty-and-null-walk-nothing", row_empty_and_null_walk_nothing},
    {"view-refuses-to-cross", row_view_refuses_to_cross},
    {"base-with-live-view-refuses-to-cross", row_base_with_live_view_refuses_to_cross},
    {"base-walks-again-after-the-view-drops", row_base_walks_again_after_the_view_drops},
    {"view-of-a-view-refuses-to-cross", row_view_of_a_view_refuses_to_cross},
};

// One row per process, named on the command line. A name this stand does not
// know is its own failure with its own exit status, so a driver's typo cannot
// read as a row that passed.
int main(int argc, char** argv) {
    const char* mode = argc > 1 ? argv[1] : "";
    for (size_t i = 0; i < sizeof(rows) / sizeof(rows[0]); i++) {
        if (strcmp(mode, rows[i].name) == 0) {
            rows[i].run();
            printf("array-unshare-walk: failures=%d\n", failures);
            return failures == 0 ? 0 : 1;
        }
    }
    fprintf(stderr, "array-unshare-walk: unknown row \"%s\"\n", mode);
    return 2;
}
