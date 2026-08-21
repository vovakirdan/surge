#include "rt_typed_fifo.h"

#include "rt_value_ops.h"

#include <string.h>

// WHERE OWNERSHIP LIVES, AND WHY NOT IN THE CONTROL.
//
// rt_slot_control models the life of ONE value in ONE place: it may be rebound
// to new storage only after that value has left (MOVED) or been destroyed
// (DROPPED). A queue holds up to N values at once, so one control cannot state
// which cells are occupied, and giving every cell its own control would spend
// 144 bytes per cell on values that are often smaller than that.
//
// So the two jobs are split along the line the ABI already draws. The per-cell
// lifecycle byte -- rt_slot_header, exactly one byte -- says which cells hold a
// value. The single control is the queue's TYPED TRANSFER: it binds to the one
// cell an operation is about, runs the canonical publish/claim/commit cycle
// against it, and comes back terminal, ready to bind to the next. That is the
// owner-wide admission gate: many cells may be full, but one transfer at a time.
//
// A ticket is what the caller carries across the unlocked window, and it names
// the cell's generation rather than trusting the index. A cell is reused every
// time the queue turns, so an index alone would let a caller that woke up late
// commit into a value that arrived after it.

static size_t rt_typed_fifo_align_up(size_t value, size_t align) {
    if (align <= 1) {
        return value;
    }
    size_t remainder = value % align;
    return remainder == 0 ? value : value + (align - remainder);
}

// The one layout calculation, so the size a caller allocates and the offsets
// init uses can never drift apart.
typedef struct {
    size_t headers_offset;
    size_t payloads_offset;
    size_t total;
    size_t stride;
    size_t align;
    int valid;
} rt_typed_fifo_layout;

static rt_typed_fifo_layout rt_typed_fifo_plan(const rt_value_ops* operations, uint64_t capacity) {
    rt_typed_fifo_layout plan;
    memset(&plan, 0, sizeof(plan));
    if (operations == NULL) {
        return plan;
    }
    size_t align = operations->layout.align;
    if (align == 0) {
        align = 1;
    }
    size_t stride = operations->layout.stride;
    if (capacity > 0 && stride > 0 && capacity > (uint64_t)(SIZE_MAX / stride)) {
        return plan;
    }
    if (capacity > (uint64_t)SIZE_MAX / 2) {
        return plan;
    }

    size_t offset = 0;
    plan.headers_offset = 0;
    offset += (size_t)capacity;
    offset = rt_typed_fifo_align_up(offset, align);
    plan.payloads_offset = offset;

    size_t payload_bytes = (size_t)capacity * stride;
    // A zero-sized element stores nothing, but the control still needs a
    // non-null, correctly aligned address to bind to, so the queue keeps one
    // element's worth of alignment rather than one word per cell. That is the
    // whole saving: a Channel<nothing> of capacity 1024 pays a handful of bytes
    // where the word-wide ring paid eight kilobytes.
    if (payload_bytes == 0) {
        payload_bytes = align;
    }
    offset += payload_bytes;

    plan.total = offset;
    plan.stride = stride;
    plan.align = align;
    plan.valid = 1;
    return plan;
}

size_t rt_typed_fifo_alloc_size(const rt_value_ops* operations, uint64_t capacity) {
    rt_typed_fifo_layout plan = rt_typed_fifo_plan(operations, capacity);
    return plan.valid ? plan.total : 0;
}

static void* rt_typed_fifo_cell(const rt_typed_fifo* fifo, uint64_t index) {
    size_t stride = fifo->operations->layout.stride;
    return fifo->payloads + (size_t)index * stride;
}

rt_slot_control_status rt_typed_fifo_init(rt_typed_fifo* fifo,
                                          const rt_value_ops* operations,
                                          uint64_t capacity,
                                          void* storage,
                                          size_t storage_size) {
    if (fifo == NULL || operations == NULL || storage == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    rt_typed_fifo_layout plan = rt_typed_fifo_plan(operations, capacity);
    if (!plan.valid) {
        return RT_SLOT_CONTROL_STORAGE_OVERFLOW;
    }
    if (storage_size < plan.total) {
        return RT_SLOT_CONTROL_STORAGE_OVERFLOW;
    }
    if ((uintptr_t)storage % (uintptr_t)plan.align != 0) {
        return RT_SLOT_CONTROL_STORAGE_MISALIGNED;
    }

    memset(fifo, 0, sizeof(*fifo));
    uint8_t* base = (uint8_t*)storage;
    fifo->operations = operations;
    fifo->headers = (rt_slot_header*)(base + plan.headers_offset);
    fifo->payloads = base + plan.payloads_offset;
    fifo->capacity = capacity;
    fifo->head = 0;
    fifo->len = 0;
    // Generation 0 is the "no reservation" value, so a zeroed ticket -- the one
    // a caller gets from a refused reserve -- can never match.
    fifo->next_generation = 1;
    fifo->reserved = 0;
    fifo->reserved_generation = 0;
    fifo->reserved_index = 0;

    for (uint64_t index = 0; index < capacity; index++) {
        fifo->headers[index].state = RT_SLOT_EMPTY;
    }

    // The control is bound to the first cell only so that it starts valid; every
    // operation rebinds it to the cell it is actually about.
    return rt_slot_control_init(&fifo->control,
                                (uint64_t)(uintptr_t)fifo,
                                operations,
                                1,
                                (uintptr_t)fifo->payloads,
                                operations->layout.size,
                                plan.align);
}

uint64_t rt_typed_fifo_len(const rt_typed_fifo* fifo) {
    return fifo == NULL ? 0 : fifo->len;
}

uint64_t rt_typed_fifo_capacity(const rt_typed_fifo* fifo) {
    return fifo == NULL ? 0 : fifo->capacity;
}

static void rt_typed_fifo_clear_reservation(rt_typed_fifo* fifo) {
    fifo->reserved = 0;
    fifo->reserved_generation = 0;
    fifo->reserved_index = 0;
}

// A ticket matches only while it IS the outstanding reservation. A ticket from
// an earlier turn of the same cell fails here even when that cell is occupied
// again, which is the whole point: a late waker must not be able to deliver into
// or destroy the value that took its place.
static int rt_typed_fifo_ticket_matches(const rt_typed_fifo* fifo,
                                        const rt_typed_fifo_ticket* ticket) {
    return ticket != NULL && fifo->reserved && ticket->generation != 0 &&
           ticket->generation == fifo->reserved_generation &&
           ticket->index == fifo->reserved_index && ticket->index < fifo->capacity;
}

rt_slot_control_status rt_typed_fifo_reserve_push_locked(rt_typed_fifo* fifo,
                                                         rt_typed_fifo_ticket* out) {
    if (fifo == NULL || out == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    memset(out, 0, sizeof(*out));
    if (fifo->reserved) {
        // Losing this race is an outcome, not a defect: one typed transfer at a
        // time is the gate, and the caller retries or gives up.
        return RT_SLOT_CONTROL_BUSY;
    }
    if (fifo->len >= fifo->capacity) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    uint64_t index = (fifo->head + fifo->len) % fifo->capacity;
    if (fifo->headers[index].state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVARIANT;
    }

    uint64_t generation = fifo->next_generation++;
    fifo->reserved = 1;
    fifo->reserved_generation = generation;
    fifo->reserved_index = index;
    out->index = index;
    out->generation = generation;
    out->address = rt_typed_fifo_cell(fifo, index);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_typed_fifo_commit_push_locked(rt_typed_fifo* fifo,
                                                        const rt_typed_fifo_ticket* ticket) {
    if (fifo == NULL || ticket == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (!rt_typed_fifo_ticket_matches(fifo, ticket)) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (fifo->headers[ticket->index].state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    fifo->headers[ticket->index].state = RT_SLOT_INITIALIZED;
    fifo->len++;
    rt_typed_fifo_clear_reservation(fifo);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_typed_fifo_abandon_push_locked(rt_typed_fifo* fifo,
                                                         const rt_typed_fifo_ticket* ticket) {
    if (fifo == NULL || ticket == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (!rt_typed_fifo_ticket_matches(fifo, ticket)) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (fifo->headers[ticket->index].state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    // The value never arrived, so the cell stays empty. The reservation is
    // retired all the same: a caller still holding this ticket is now late.
    rt_typed_fifo_clear_reservation(fifo);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_typed_fifo_reserve_pop_locked(rt_typed_fifo* fifo,
                                                        rt_typed_fifo_ticket* out) {
    if (fifo == NULL || out == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    memset(out, 0, sizeof(*out));
    if (fifo->reserved) {
        return RT_SLOT_CONTROL_BUSY;
    }
    if (fifo->len == 0) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    uint64_t index = fifo->head;
    if (fifo->headers[index].state != RT_SLOT_INITIALIZED) {
        return RT_SLOT_CONTROL_INVARIANT;
    }

    uint64_t generation = fifo->next_generation++;
    fifo->headers[index].state = RT_SLOT_CLAIMED;
    fifo->reserved = 1;
    fifo->reserved_generation = generation;
    fifo->reserved_index = index;
    out->index = index;
    out->generation = generation;
    out->address = rt_typed_fifo_cell(fifo, index);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_typed_fifo_commit_pop_locked(rt_typed_fifo* fifo,
                                                       const rt_typed_fifo_ticket* ticket) {
    if (fifo == NULL || ticket == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (!rt_typed_fifo_ticket_matches(fifo, ticket)) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (fifo->headers[ticket->index].state != RT_SLOT_CLAIMED || ticket->index != fifo->head ||
        fifo->len == 0) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    fifo->headers[ticket->index].state = RT_SLOT_EMPTY;
    fifo->head = (fifo->head + 1) % fifo->capacity;
    fifo->len--;
    rt_typed_fifo_clear_reservation(fifo);
    return RT_SLOT_CONTROL_OK;
}

// Binds the queue's one control to a cell that holds a live value, so the drop
// runs through the same publish/claim/commit cycle every other owner uses. The
// first binding is an init because the control has never held a value; later
// ones are rebindings, which the control accepts only from a terminal state.
static int rt_typed_fifo_bind_control(rt_typed_fifo* fifo, uint64_t index, int first) {
    uintptr_t address = (uintptr_t)rt_typed_fifo_cell(fifo, index);
    size_t size = fifo->operations->layout.size;
    size_t align = fifo->operations->layout.align;
    if (align == 0) {
        align = 1;
    }
    if (first) {
        return rt_slot_control_init(&fifo->control,
                                    (uint64_t)(uintptr_t)fifo,
                                    fifo->operations,
                                    1,
                                    address,
                                    size,
                                    align) == RT_SLOT_CONTROL_OK;
    }
    return rt_slot_begin_generation_locked(
               &fifo->control, fifo->control.generation + 1, address, size, align) ==
           RT_SLOT_CONTROL_OK;
}

void rt_typed_fifo_drain(rt_typed_fifo* fifo) {
    if (fifo == NULL || fifo->operations == NULL) {
        return;
    }
    // The drop dispatch is the helper's, not this file's: it reads the flag, it
    // fails closed if a scheduler lock is held, and it returns silently for an
    // element with no obligation. Reading the slot here would be a second
    // opinion about a question the descriptor already answers.
    int first = 1;
    while (fifo->len > 0) {
        uint64_t index = fifo->head;
        if (fifo->headers[index].state != RT_SLOT_INITIALIZED) {
            break;
        }
        void* cell = rt_typed_fifo_cell(fifo, index);
        if (rt_typed_fifo_bind_control(fifo, index, first)) {
            first = 0;
            uint64_t generation = fifo->control.generation;
            rt_claim_token token;
            if (rt_slot_publish_initial_locked(&fifo->control, generation) == RT_SLOT_CONTROL_OK &&
                rt_slot_claim_exclusive_locked(
                    &fifo->control, NULL, RT_SLOT_CLAIM_DROP, generation, 0, &token) ==
                    RT_SLOT_CONTROL_OK) {
                // The header is cleared BEFORE the callback runs, so a drop that
                // re-enters the queue finds a cell nobody can drop a second time.
                fifo->headers[index].state = RT_SLOT_DROPPED;
                fifo->head = (fifo->head + 1) % fifo->capacity;
                fifo->len--;
                rt_value_drop_in_place_detached(fifo->operations, cell);
                rt_slot_commit_drop_locked(&fifo->control, &token);
                fifo->headers[index].state = RT_SLOT_EMPTY;
                continue;
            }
        }
        // The control refused to take this cell. Retiring the cell anyway would
        // either leak it or drop it without the cycle that proves it happened
        // once, so the drain stops and says so by leaving the queue non-empty.
        break;
    }
    rt_typed_fifo_clear_reservation(fifo);
}
