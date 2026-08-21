#include "slot_control_harness.h"

#include "rt_typed_fifo.h"

#include <stdlib.h>
#include <string.h>

// The queue under test is the storage half of a channel: one descriptor for the
// whole queue, one byte of lifecycle per cell, payloads at the element's own
// stride. These cases pin the three properties the design was chosen for --
// order, exact-once destruction, and a stale ticket being inert -- plus the two
// element shapes that used to be impossible to hold honestly: one wider than a
// machine word, and one with no bytes at all.

// An element deliberately wider than the uint64_t ring it replaces. If anything
// in the queue still squeezed a value through a word, `high` and `tag` would not
// survive the trip.
typedef struct {
    uint64_t low;
    uint64_t high;
    uint32_t tag;
} fifo_wide_value;

#define FIFO_DROP_LOG_CAPACITY 16

static int fifo_drop_calls;
static uint64_t fifo_drop_log[FIFO_DROP_LOG_CAPACITY];

static void fifo_reset_drops(void) {
    fifo_drop_calls = 0;
    memset(fifo_drop_log, 0, sizeof(fifo_drop_log));
}

static void fifo_wide_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(fifo_wide_value));
    ((fifo_wide_value*)source)->tag = 0;
}

static void fifo_wide_drop(void* value) {
    fifo_wide_value* typed = (fifo_wide_value*)value;
    if (fifo_drop_calls < FIFO_DROP_LOG_CAPACITY) {
        fifo_drop_log[fifo_drop_calls] = typed->low;
    }
    fifo_drop_calls++;
    // Poisoning is what turns a second drop into a visible wrong answer rather
    // than a silent repeat of the same one.
    typed->low = 0;
    typed->tag = 0xDEADU;
}

static const rt_value_ops fifo_wide_ops = {
    .layout = {.size = sizeof(fifo_wide_value),
               .align = _Alignof(fifo_wide_value),
               .stride = sizeof(fifo_wide_value),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = fifo_wide_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = fifo_wide_drop,
    .trace = NULL,
    .plan_cross = harness_noop_plan,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

// The owner's first acceptance shape: an opaque runtime resource held as an
// element. It has a layout and a move, and no drop claim at all.
typedef struct {
    void* handle;
} fifo_opaque_value;

static void fifo_opaque_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(fifo_opaque_value));
    ((fifo_opaque_value*)source)->handle = NULL;
}

static const rt_value_ops fifo_opaque_ops = {
    .layout = {.size = sizeof(fifo_opaque_value),
               .align = _Alignof(fifo_opaque_value),
               .stride = sizeof(fifo_opaque_value),
               .flags = 0},
    .move_init = fifo_opaque_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = NULL,
    .trace = NULL,
    .plan_cross = harness_noop_plan,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static void fifo_zst_move(void* destination, void* source) {
    (void)destination;
    (void)source;
}

static const rt_value_ops fifo_zst_ops = {
    .layout = {.size = 0, .align = 1, .stride = 0, .flags = 0},
    .move_init = fifo_zst_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = NULL,
    .trace = NULL,
    .plan_cross = harness_noop_plan,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

// Pushing and popping is always reserve -> operate with the lock released ->
// commit, so the helpers spell that shape once and every case inherits it.
static int fifo_push_wide(rt_typed_fifo* fifo, uint64_t low, uint64_t high, uint32_t tag) {
    rt_typed_fifo_ticket ticket;
    if (rt_typed_fifo_reserve_push_locked(fifo, &ticket) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    fifo_wide_value staged = {.low = low, .high = high, .tag = tag};
    fifo_wide_ops.move_init(ticket.address, &staged);
    return rt_typed_fifo_commit_push_locked(fifo, &ticket) == RT_SLOT_CONTROL_OK;
}

static int fifo_pop_wide(rt_typed_fifo* fifo, fifo_wide_value* out) {
    rt_typed_fifo_ticket ticket;
    if (rt_typed_fifo_reserve_pop_locked(fifo, &ticket) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    fifo_wide_ops.move_init(out, ticket.address);
    return rt_typed_fifo_commit_pop_locked(fifo, &ticket) == RT_SLOT_CONTROL_OK;
}

static void* fifo_alloc_storage(const rt_value_ops* operations, uint64_t capacity, size_t* size) {
    *size = rt_typed_fifo_alloc_size(operations, capacity);
    if (*size == 0) {
        return NULL;
    }
    size_t align =
        operations->layout.align < sizeof(void*) ? sizeof(void*) : operations->layout.align;
    // aligned_alloc requires a size that is a multiple of the alignment.
    size_t rounded = (*size + align - 1) / align * align;
    return aligned_alloc(align, rounded);
}

// Order and width: four values in, four values out, unchanged and in sequence.
static int fifo_case_order_and_width(void) {
    size_t size = 0;
    void* storage = fifo_alloc_storage(&fifo_wide_ops, 4, &size);
    REQUIRE(storage != NULL);
    rt_typed_fifo fifo;
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 4, storage, size) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_typed_fifo_capacity(&fifo) == 4);
    REQUIRE(rt_typed_fifo_len(&fifo) == 0);

    for (uint64_t i = 0; i < 4; i++) {
        REQUIRE(fifo_push_wide(&fifo, 100 + i, 0xFFFF0000U + i, (uint32_t)(7 + i)));
    }
    REQUIRE(rt_typed_fifo_len(&fifo) == 4);
    // A fifth push has nowhere to go, and that is a state, not a defect.
    rt_typed_fifo_ticket overflow;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &overflow) == RT_SLOT_CONTROL_INVALID_STATE);

    for (uint64_t i = 0; i < 4; i++) {
        fifo_wide_value out = {0};
        REQUIRE(fifo_pop_wide(&fifo, &out));
        REQUIRE(out.low == 100 + i);
        REQUIRE(out.high == 0xFFFF0000U + i);
        REQUIRE(out.tag == (uint32_t)(7 + i));
    }
    REQUIRE(rt_typed_fifo_len(&fifo) == 0);
    rt_typed_fifo_ticket underflow;
    REQUIRE(rt_typed_fifo_reserve_pop_locked(&fifo, &underflow) == RT_SLOT_CONTROL_INVALID_STATE);

    // Turning the ring past its capacity must keep the same order.
    for (uint64_t round = 0; round < 6; round++) {
        REQUIRE(fifo_push_wide(&fifo, 900 + round, round, 1));
        fifo_wide_value out = {0};
        REQUIRE(fifo_pop_wide(&fifo, &out));
        REQUIRE(out.low == 900 + round);
    }
    free(storage);
    return 0;
}

// One typed transfer at a time: a second reservation is refused with BUSY, which
// the caller retries rather than treating as an error.
static int fifo_case_one_transfer_at_a_time(void) {
    size_t size = 0;
    void* storage = fifo_alloc_storage(&fifo_wide_ops, 2, &size);
    REQUIRE(storage != NULL);
    rt_typed_fifo fifo;
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 2, storage, size) == RT_SLOT_CONTROL_OK);

    rt_typed_fifo_ticket first;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &first) == RT_SLOT_CONTROL_OK);
    rt_typed_fifo_ticket second;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &second) == RT_SLOT_CONTROL_BUSY);
    REQUIRE(second.generation == 0 && second.address == NULL);
    // A refused reservation took nothing, so a pop is refused for the same
    // reason and the queue is still empty.
    rt_typed_fifo_ticket blocked_pop;
    REQUIRE(rt_typed_fifo_reserve_pop_locked(&fifo, &blocked_pop) == RT_SLOT_CONTROL_BUSY);

    // Abandoning leaves the cell empty and frees the transfer for the next
    // caller: the value never arrived, so nothing is published and nothing leaks.
    REQUIRE(rt_typed_fifo_abandon_push_locked(&fifo, &first) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_typed_fifo_len(&fifo) == 0);
    REQUIRE(fifo_push_wide(&fifo, 1, 2, 3));
    REQUIRE(rt_typed_fifo_len(&fifo) == 1);
    free(storage);
    return 0;
}

// The owner's third acceptance: a ticket from an earlier turn can neither
// deliver into nor destroy the value that took its place.
static int fifo_case_stale_ticket_is_inert(void) {
    size_t size = 0;
    void* storage = fifo_alloc_storage(&fifo_wide_ops, 2, &size);
    REQUIRE(storage != NULL);
    rt_typed_fifo fifo;
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 2, storage, size) == RT_SLOT_CONTROL_OK);

    rt_typed_fifo_ticket stale;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &stale) == RT_SLOT_CONTROL_OK);
    fifo_wide_value staged = {.low = 11, .high = 12, .tag = 13};
    fifo_wide_ops.move_init(stale.address, &staged);
    REQUIRE(rt_typed_fifo_commit_push_locked(&fifo, &stale) == RT_SLOT_CONTROL_OK);

    // Reusing that same cell: pop the value out and push a different one in.
    fifo_wide_value taken = {0};
    REQUIRE(fifo_pop_wide(&fifo, &taken));
    REQUIRE(taken.low == 11);
    REQUIRE(fifo_push_wide(&fifo, 21, 22, 23));
    REQUIRE(fifo_push_wide(&fifo, 31, 32, 33));
    REQUIRE(rt_typed_fifo_len(&fifo) == 2);

    // The late holder now presents its old ticket. Every path refuses it, and
    // refuses it as STALE -- a lost race, not an internal error.
    REQUIRE(rt_typed_fifo_commit_push_locked(&fifo, &stale) == RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_typed_fifo_commit_pop_locked(&fifo, &stale) == RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_typed_fifo_abandon_push_locked(&fifo, &stale) == RT_SLOT_CONTROL_STALE);
    // And the values that took its place are untouched, in order.
    REQUIRE(rt_typed_fifo_len(&fifo) == 2);
    fifo_wide_value out = {0};
    REQUIRE(fifo_pop_wide(&fifo, &out));
    REQUIRE(out.low == 21 && out.high == 22 && out.tag == 23);
    REQUIRE(fifo_pop_wide(&fifo, &out));
    REQUIRE(out.low == 31 && out.high == 32 && out.tag == 33);

    // A ticket that names a real cell but was never issued is refused too.
    rt_typed_fifo_ticket forged = {.index = 0, .generation = 99, .address = storage};
    REQUIRE(rt_typed_fifo_commit_push_locked(&fifo, &forged) == RT_SLOT_CONTROL_STALE);
    free(storage);

    // The case the generation exists for, and the only one where nothing else
    // catches it: the cell has ALREADY BEEN HANDED to the next caller. The
    // index matches, a reservation is outstanding, and the cell is in the state
    // the late holder expects -- so index and occupancy both say yes, and only
    // the generation says this ticket belongs to the previous turn.
    size_t single_size = 0;
    void* single_storage = fifo_alloc_storage(&fifo_wide_ops, 1, &single_size);
    REQUIRE(single_storage != NULL);
    rt_typed_fifo single;
    REQUIRE(rt_typed_fifo_init(&single, &fifo_wide_ops, 1, single_storage, single_size) ==
            RT_SLOT_CONTROL_OK);

    rt_typed_fifo_ticket abandoned;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&single, &abandoned) == RT_SLOT_CONTROL_OK);
    REQUIRE(rt_typed_fifo_abandon_push_locked(&single, &abandoned) == RT_SLOT_CONTROL_OK);

    rt_typed_fifo_ticket successor;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&single, &successor) == RT_SLOT_CONTROL_OK);
    REQUIRE(successor.index == abandoned.index);
    REQUIRE(successor.generation != abandoned.generation);

    // Committing the stale ticket would publish a value that was never written.
    REQUIRE(rt_typed_fifo_commit_push_locked(&single, &abandoned) == RT_SLOT_CONTROL_STALE);
    // Abandoning it would cancel the successor's reservation out from under it.
    REQUIRE(rt_typed_fifo_abandon_push_locked(&single, &abandoned) == RT_SLOT_CONTROL_STALE);
    REQUIRE(rt_typed_fifo_len(&single) == 0);

    // The successor is unharmed and completes normally.
    fifo_wide_value arriving = {.low = 77, .high = 78, .tag = 79};
    fifo_wide_ops.move_init(successor.address, &arriving);
    REQUIRE(rt_typed_fifo_commit_push_locked(&single, &successor) == RT_SLOT_CONTROL_OK);
    fifo_wide_value delivered = {0};
    REQUIRE(fifo_pop_wide(&single, &delivered));
    REQUIRE(delivered.low == 77 && delivered.high == 78 && delivered.tag == 79);
    free(single_storage);
    return 0;
}

// The owner's second acceptance: draining frees every payload exactly once.
static int fifo_case_drain_frees_each_payload_once(void) {
    size_t size = 0;
    void* storage = fifo_alloc_storage(&fifo_wide_ops, 4, &size);
    REQUIRE(storage != NULL);
    rt_typed_fifo fifo;
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 4, storage, size) == RT_SLOT_CONTROL_OK);
    fifo_reset_drops();

    // Turn the ring first, so the live values do not start at index zero and a
    // drain that walked the array instead of the queue would be caught.
    REQUIRE(fifo_push_wide(&fifo, 1, 0, 1));
    REQUIRE(fifo_push_wide(&fifo, 2, 0, 1));
    fifo_wide_value taken = {0};
    REQUIRE(fifo_pop_wide(&fifo, &taken));
    REQUIRE(fifo_pop_wide(&fifo, &taken));
    REQUIRE(fifo_drop_calls == 0);

    REQUIRE(fifo_push_wide(&fifo, 41, 0, 1));
    REQUIRE(fifo_push_wide(&fifo, 42, 0, 1));
    REQUIRE(fifo_push_wide(&fifo, 43, 0, 1));
    REQUIRE(rt_typed_fifo_len(&fifo) == 3);

    rt_typed_fifo_drain(&fifo);
    REQUIRE(fifo_drop_calls == 3);
    REQUIRE(fifo_drop_log[0] == 41 && fifo_drop_log[1] == 42 && fifo_drop_log[2] == 43);
    REQUIRE(rt_typed_fifo_len(&fifo) == 0);

    // Draining again must find nothing: exactly once means the second call is
    // silent, not merely non-crashing.
    rt_typed_fifo_drain(&fifo);
    REQUIRE(fifo_drop_calls == 3);

    // The queue stays usable afterwards, and what it hands back is a fresh value
    // rather than a poisoned one.
    REQUIRE(fifo_push_wide(&fifo, 55, 56, 57));
    fifo_wide_value out = {0};
    REQUIRE(fifo_pop_wide(&fifo, &out));
    REQUIRE(out.low == 55 && out.high == 56 && out.tag == 57);
    REQUIRE(fifo_drop_calls == 3);
    free(storage);
    return 0;
}

// The owner's first acceptance: an element that is an opaque resource keeps its
// layout and its move, claims no drop, and is drained without one being invented.
static int fifo_case_opaque_element_has_no_drop(void) {
    size_t size = 0;
    void* storage = fifo_alloc_storage(&fifo_opaque_ops, 2, &size);
    REQUIRE(storage != NULL);
    rt_typed_fifo fifo;
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_opaque_ops, 2, storage, size) == RT_SLOT_CONTROL_OK);
    fifo_reset_drops();

    int first_target = 1;
    int second_target = 2;
    rt_typed_fifo_ticket ticket;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);
    fifo_opaque_value staged = {.handle = &first_target};
    fifo_opaque_ops.move_init(ticket.address, &staged);
    REQUIRE(staged.handle == NULL);
    REQUIRE(rt_typed_fifo_commit_push_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);

    REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);
    fifo_opaque_value second = {.handle = &second_target};
    fifo_opaque_ops.move_init(ticket.address, &second);
    REQUIRE(rt_typed_fifo_commit_push_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);

    // Draining a queue whose element has no drop empties it and calls nothing.
    rt_typed_fifo_drain(&fifo);
    REQUIRE(rt_typed_fifo_len(&fifo) == 0);
    REQUIRE(fifo_drop_calls == 0);
    free(storage);
    return 0;
}

// A zero-sized element stores nothing, and the queue's cost must show that: the
// word-per-cell ring charged eight bytes each for values that have no bytes.
static int fifo_case_zero_sized_element_costs_no_payload(void) {
    const uint64_t capacity = 1024;
    size_t zst_size = rt_typed_fifo_alloc_size(&fifo_zst_ops, capacity);
    size_t word_ring_size = (size_t)capacity * sizeof(uint64_t);
    REQUIRE(zst_size > 0);
    // Headers are one byte per cell, plus one element's alignment so the control
    // has a legal address to bind to. Anything larger means a payload crept in.
    REQUIRE(zst_size <= (size_t)capacity + 8);
    REQUIRE(zst_size < word_ring_size);

    size_t size = 0;
    void* storage = fifo_alloc_storage(&fifo_zst_ops, capacity, &size);
    REQUIRE(storage != NULL);
    rt_typed_fifo fifo;
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_zst_ops, capacity, storage, size) ==
            RT_SLOT_CONTROL_OK);

    // Occupancy is real even though the payload is not: every cell is tracked by
    // its header byte alone.
    for (uint64_t i = 0; i < capacity; i++) {
        rt_typed_fifo_ticket ticket;
        REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);
        REQUIRE(ticket.address != NULL);
        fifo_zst_ops.move_init(ticket.address, ticket.address);
        REQUIRE(rt_typed_fifo_commit_push_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);
    }
    REQUIRE(rt_typed_fifo_len(&fifo) == capacity);
    rt_typed_fifo_ticket overflow;
    REQUIRE(rt_typed_fifo_reserve_push_locked(&fifo, &overflow) == RT_SLOT_CONTROL_INVALID_STATE);
    for (uint64_t i = 0; i < capacity; i++) {
        rt_typed_fifo_ticket ticket;
        REQUIRE(rt_typed_fifo_reserve_pop_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);
        REQUIRE(rt_typed_fifo_commit_pop_locked(&fifo, &ticket) == RT_SLOT_CONTROL_OK);
    }
    REQUIRE(rt_typed_fifo_len(&fifo) == 0);
    free(storage);
    return 0;
}

// Storage the caller did not size or align correctly is refused, rather than
// laid out over memory the queue does not own.
static int fifo_case_storage_is_checked(void) {
    size_t size = rt_typed_fifo_alloc_size(&fifo_wide_ops, 4);
    REQUIRE(size > 0);
    void* storage = aligned_alloc(16, ((size + 16) + 15) / 16 * 16);
    REQUIRE(storage != NULL);
    rt_typed_fifo fifo;
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 4, storage, size - 1) ==
            RT_SLOT_CONTROL_STORAGE_OVERFLOW);
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 4, (uint8_t*)storage + 1, size) ==
            RT_SLOT_CONTROL_STORAGE_MISALIGNED);
    REQUIRE(rt_typed_fifo_init(&fifo, NULL, 4, storage, size) == RT_SLOT_CONTROL_INVALID_ARGUMENT);
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 4, NULL, size) ==
            RT_SLOT_CONTROL_INVALID_ARGUMENT);
    REQUIRE(rt_typed_fifo_init(&fifo, &fifo_wide_ops, 4, storage, size) == RT_SLOT_CONTROL_OK);
    free(storage);
    return 0;
}

int harness_case_fifo(void) {
    int status = fifo_case_order_and_width();
    if (status != 0) {
        return status;
    }
    status = fifo_case_one_transfer_at_a_time();
    if (status != 0) {
        return status;
    }
    status = fifo_case_stale_ticket_is_inert();
    if (status != 0) {
        return status;
    }
    status = fifo_case_drain_frees_each_payload_once();
    if (status != 0) {
        return status;
    }
    status = fifo_case_opaque_element_has_no_drop();
    if (status != 0) {
        return status;
    }
    status = fifo_case_zero_sized_element_costs_no_payload();
    if (status != 0) {
        return status;
    }
    return fifo_case_storage_is_checked();
}
