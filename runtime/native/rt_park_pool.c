#include "rt_park_pool.h"

#include "rt_value_ops.h"

#include <string.h>

// The pool's shape follows the queue's, and the one place it deliberately
// differs is the generation: per slot rather than per pool, because many parks
// are live at once and a released slot goes straight to the next task. See the
// header for why that is the difference that matters.

#define RT_PARK_POOL_NO_FREE UINT32_MAX

static size_t rt_park_pool_align_up(size_t value, size_t align) {
    if (align <= 1) {
        return value;
    }
    size_t remainder = value % align;
    return remainder == 0 ? value : value + (align - remainder);
}

typedef struct {
    size_t slots_offset;
    size_t headers_offset;
    size_t payloads_offset;
    size_t total;
    // The BLOCK's alignment, which is the wider of the element's and the slot
    // record's, and the ELEMENT's own, which is what the payload area and the
    // control are aligned to. They differ for a zero-sized element, where the
    // block still needs eight and the payload needs one -- handing the control
    // the block's figure there refuses a correctly laid out pool.
    size_t align;
    size_t element_align;
    int valid;
} rt_park_pool_layout;

static rt_park_pool_layout rt_park_pool_plan(const rt_value_ops* operations, uint64_t capacity) {
    rt_park_pool_layout plan;
    memset(&plan, 0, sizeof(plan));
    if (operations == NULL || capacity == 0 || capacity >= (uint64_t)RT_PARK_POOL_NO_FREE) {
        return plan;
    }
    size_t element_align = operations->layout.align;
    if (element_align == 0) {
        element_align = 1;
    }
    // The slot array sits at offset zero so it can be addressed from the caller's
    // void* directly, which is the only spelling that satisfies both the
    // alignment warning and the cast-through-void rule. That makes the block's
    // required alignment the wider of the element's and the slot record's.
    size_t align = element_align < _Alignof(rt_park_slot) ? _Alignof(rt_park_slot) : element_align;
    size_t stride = operations->layout.stride;
    if (stride > 0 && capacity > (uint64_t)(SIZE_MAX / stride)) {
        return plan;
    }
    if (capacity > (uint64_t)(SIZE_MAX / sizeof(rt_park_slot)) / 2) {
        return plan;
    }

    size_t offset = 0;
    plan.slots_offset = 0;
    offset += (size_t)capacity * sizeof(rt_park_slot);
    plan.headers_offset = offset;
    offset += (size_t)capacity;
    offset = rt_park_pool_align_up(offset, element_align);
    plan.payloads_offset = offset;

    size_t payload_bytes = (size_t)capacity * stride;
    // A zero-sized element still needs a legal address for the control to bind
    // to, and one element's alignment is the whole cost of providing it.
    if (payload_bytes == 0) {
        payload_bytes = element_align;
    }
    offset += payload_bytes;

    plan.total = offset;
    plan.align = align;
    plan.element_align = element_align;
    plan.valid = 1;
    return plan;
}

size_t rt_park_pool_alloc_size(const rt_value_ops* operations, uint64_t capacity) {
    rt_park_pool_layout plan = rt_park_pool_plan(operations, capacity);
    return plan.valid ? plan.total : 0;
}

static void* rt_park_pool_cell(const rt_park_pool* pool, uint64_t index) {
    return pool->payloads + (size_t)index * pool->operations->layout.stride;
}

rt_slot_control_status rt_park_pool_init(rt_park_pool* pool,
                                         const rt_value_ops* operations,
                                         uint64_t capacity,
                                         void* storage,
                                         size_t storage_size) {
    if (pool == NULL || operations == NULL || storage == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    rt_park_pool_layout plan = rt_park_pool_plan(operations, capacity);
    if (!plan.valid) {
        return RT_SLOT_CONTROL_STORAGE_OVERFLOW;
    }
    if (storage_size < plan.total) {
        return RT_SLOT_CONTROL_STORAGE_OVERFLOW;
    }
    if ((uintptr_t)storage % (uintptr_t)plan.align != 0) {
        return RT_SLOT_CONTROL_STORAGE_MISALIGNED;
    }

    memset(pool, 0, sizeof(*pool));
    uint8_t* base = (uint8_t*)storage;
    pool->operations = operations;
    pool->slots = (rt_park_slot*)storage;
    pool->headers = (rt_slot_header*)(base + plan.headers_offset);
    pool->payloads = base + plan.payloads_offset;
    pool->capacity = capacity;
    pool->live = 0;
    // Generation 0 means "no park", so a zeroed token can never name a live slot.
    pool->next_generation = 1;
    pool->first_free = 0;

    for (uint64_t index = 0; index < capacity; index++) {
        pool->headers[index].state = RT_SLOT_EMPTY;
        pool->slots[index].generation = 0;
        pool->slots[index].live = 0;
        pool->slots[index].reserved = 0;
        pool->slots[index].next_free =
            index + 1 < capacity ? (uint32_t)(index + 1) : RT_PARK_POOL_NO_FREE;
    }

    return rt_slot_control_init(&pool->control,
                                (uint64_t)(uintptr_t)pool,
                                operations,
                                1,
                                (uintptr_t)pool->payloads,
                                operations->layout.size,
                                plan.element_align);
}

uint64_t rt_park_pool_live(const rt_park_pool* pool) {
    return pool == NULL ? 0 : pool->live;
}

int rt_park_pool_token_is_live(const rt_park_pool* pool, const rt_park_token* token) {
    if (pool == NULL || token == NULL || token->owner != pool || token->generation == 0 ||
        token->index >= pool->capacity) {
        return 0;
    }
    const rt_park_slot* slot = &pool->slots[token->index];
    return slot->live != 0 && slot->generation == token->generation;
}

rt_slot_control_status rt_park_pool_acquire_locked(rt_park_pool* pool, rt_park_token* out) {
    if (pool == NULL || out == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    memset(out, 0, sizeof(*out));
    if (pool->first_free == RT_PARK_POOL_NO_FREE) {
        // Every slot is parked on. The caller waits or refuses; it is a
        // condition of the pool, not a defect in it.
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    uint64_t index = pool->first_free;
    rt_park_slot* slot = &pool->slots[index];
    if (slot->live || pool->headers[index].state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVARIANT;
    }

    pool->first_free = slot->next_free;
    slot->next_free = RT_PARK_POOL_NO_FREE;
    slot->generation = pool->next_generation++;
    slot->live = 1;
    slot->reserved = 0;
    pool->live++;

    out->owner = pool;
    out->index = index;
    out->generation = slot->generation;
    return RT_SLOT_CONTROL_OK;
}

// Every transfer entry point asks the same three questions in the same order,
// and the order is what makes the answers meaningful: a stale token is STALE
// even while another transfer is in flight, because the caller's park is over
// either way and retrying would not help it.
static rt_slot_control_status rt_park_pool_check_transfer(const rt_park_pool* pool,
                                                          const rt_park_token* token,
                                                          int expect_reserved) {
    if (pool == NULL || token == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (!rt_park_pool_token_is_live(pool, token)) {
        return RT_SLOT_CONTROL_STALE;
    }
    const rt_park_slot* slot = &pool->slots[token->index];
    if (expect_reserved) {
        if (!slot->reserved) {
            return RT_SLOT_CONTROL_INVALID_STATE;
        }
        return RT_SLOT_CONTROL_OK;
    }
    if (slot->reserved) {
        // This slot is mid-transfer. A slot belongs to one park, so the only
        // caller that can see this is one racing itself.
        return RT_SLOT_CONTROL_BUSY;
    }
    return RT_SLOT_CONTROL_OK;
}

static void rt_park_pool_hold(rt_park_pool* pool, const rt_park_token* token) {
    pool->slots[token->index].reserved = 1;
}

static void rt_park_pool_clear_reservation(rt_park_pool* pool, const rt_park_token* token) {
    pool->slots[token->index].reserved = 0;
}

rt_slot_control_status rt_park_pool_reserve_deliver_locked(rt_park_pool* pool,
                                                           const rt_park_token* token,
                                                           void** out_address) {
    if (out_address == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    *out_address = NULL;
    rt_slot_control_status status = rt_park_pool_check_transfer(pool, token, 0);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    if (pool->headers[token->index].state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    rt_park_pool_hold(pool, token);
    *out_address = rt_park_pool_cell(pool, token->index);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_park_pool_commit_deliver_locked(rt_park_pool* pool,
                                                          const rt_park_token* token) {
    rt_slot_control_status status = rt_park_pool_check_transfer(pool, token, 1);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    if (pool->headers[token->index].state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    pool->headers[token->index].state = RT_SLOT_INITIALIZED;
    rt_park_pool_clear_reservation(pool, token);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_park_pool_abandon_deliver_locked(rt_park_pool* pool,
                                                           const rt_park_token* token) {
    rt_slot_control_status status = rt_park_pool_check_transfer(pool, token, 1);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    if (pool->headers[token->index].state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    rt_park_pool_clear_reservation(pool, token);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_park_pool_reserve_take_locked(rt_park_pool* pool,
                                                        const rt_park_token* token,
                                                        void** out_address) {
    if (out_address == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    *out_address = NULL;
    rt_slot_control_status status = rt_park_pool_check_transfer(pool, token, 0);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    if (pool->headers[token->index].state != RT_SLOT_INITIALIZED) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    pool->headers[token->index].state = RT_SLOT_CLAIMED;
    rt_park_pool_hold(pool, token);
    *out_address = rt_park_pool_cell(pool, token->index);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_park_pool_commit_take_locked(rt_park_pool* pool,
                                                       const rt_park_token* token) {
    rt_slot_control_status status = rt_park_pool_check_transfer(pool, token, 1);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    if (pool->headers[token->index].state != RT_SLOT_CLAIMED) {
        return RT_SLOT_CONTROL_INVARIANT;
    }
    pool->headers[token->index].state = RT_SLOT_EMPTY;
    rt_park_pool_clear_reservation(pool, token);
    return RT_SLOT_CONTROL_OK;
}

// Destroys whatever the slot holds, through the control's cycle, with no owner
// lock held. The header is cleared BEFORE the callback so a re-entrant caller
// cannot find a value to destroy a second time.
static void rt_park_pool_destroy_slot(rt_park_pool* pool, uint64_t index, int first) {
    if (pool->headers[index].state != RT_SLOT_INITIALIZED) {
        return;
    }
    void* cell = rt_park_pool_cell(pool, index);
    size_t size = pool->operations->layout.size;
    size_t align = pool->operations->layout.align;
    if (align == 0) {
        align = 1;
    }
    rt_slot_control_status bound =
        first ? rt_slot_control_init(&pool->control,
                                     (uint64_t)(uintptr_t)pool,
                                     pool->operations,
                                     1,
                                     (uintptr_t)cell,
                                     size,
                                     align)
              : rt_slot_begin_generation_locked(
                    &pool->control, pool->control.generation + 1, (uintptr_t)cell, size, align);
    if (bound != RT_SLOT_CONTROL_OK) {
        return;
    }
    uint64_t generation = pool->control.generation;
    rt_claim_token token;
    if (rt_slot_publish_initial_locked(&pool->control, generation) != RT_SLOT_CONTROL_OK ||
        rt_slot_claim_exclusive_locked(
            &pool->control, NULL, RT_SLOT_CLAIM_DROP, generation, 0, &token) !=
            RT_SLOT_CONTROL_OK) {
        return;
    }
    pool->headers[index].state = RT_SLOT_DROPPED;
    rt_value_drop_in_place_detached(pool->operations, cell);
    rt_slot_commit_drop_locked(&pool->control, &token);
    pool->headers[index].state = RT_SLOT_EMPTY;
}

// A slot returns to the free list only here, and only once: releasing a token
// whose park already ended would hand a successor's slot back to the free list
// while that successor is still parked on it.
static void rt_park_pool_return_slot(rt_park_pool* pool, uint64_t index) {
    rt_park_slot* slot = &pool->slots[index];
    slot->live = 0;
    // The generation is NOT reset. It only ever moves forward, so a token from
    // this park can never match the next one.
    slot->next_free = pool->first_free;
    pool->first_free = (uint32_t)index;
    pool->live--;
}

// The locked half of a destruction: bind the control onto this cell, run the
// whole claim/commit cycle, and hand back the bytes for the caller to destroy.
// Split out of rt_park_pool_destroy_slot so the CALLBACK -- the only part that
// may not run under the owner lock -- can happen outside it.
//
// The header is cleared before the callback for the reason it always was: a
// re-entrant caller must not find the same value to destroy a second time. The
// bytes stay exclusively the caller's meanwhile, because the slot is reserved
// and still live: nothing can deliver into it, take from it, or acquire it.
static void* rt_park_pool_detach_value_locked(rt_park_pool* pool, uint64_t index) {
    if (pool->headers[index].state != RT_SLOT_INITIALIZED) {
        return NULL;
    }
    void* cell = rt_park_pool_cell(pool, index);
    size_t size = pool->operations->layout.size;
    size_t align = pool->operations->layout.align;
    if (align == 0) {
        align = 1;
    }
    if (rt_slot_control_init(&pool->control,
                             (uint64_t)(uintptr_t)pool,
                             pool->operations,
                             1,
                             (uintptr_t)cell,
                             size,
                             align) != RT_SLOT_CONTROL_OK) {
        return NULL;
    }
    uint64_t generation = pool->control.generation;
    rt_claim_token claim;
    if (rt_slot_publish_initial_locked(&pool->control, generation) != RT_SLOT_CONTROL_OK ||
        rt_slot_claim_exclusive_locked(
            &pool->control, NULL, RT_SLOT_CLAIM_DROP, generation, 0, &claim) !=
            RT_SLOT_CONTROL_OK) {
        return NULL;
    }
    pool->headers[index].state = RT_SLOT_DROPPED;
    rt_slot_commit_drop_locked(&pool->control, &claim);
    pool->headers[index].state = RT_SLOT_EMPTY;
    return cell;
}

rt_slot_control_status rt_park_pool_begin_release_locked(rt_park_pool* pool,
                                                         const rt_park_token* token,
                                                         void** out_value) {
    if (out_value != NULL) {
        *out_value = NULL;
    }
    if (pool == NULL || token == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (!rt_park_pool_token_is_live(pool, token)) {
        return RT_SLOT_CONTROL_STALE;
    }
    rt_park_slot* slot = &pool->slots[token->index];
    if (slot->reserved) {
        // A transfer is mid-flight on this very slot; ending the park now would
        // free bytes the other half is still writing.
        return RT_SLOT_CONTROL_BUSY;
    }
    slot->reserved = 1;
    void* value = rt_park_pool_detach_value_locked(pool, token->index);
    if (value != NULL && out_value != NULL) {
        *out_value = value;
    }
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_park_pool_finish_release_locked(rt_park_pool* pool,
                                                          const rt_park_token* token) {
    if (pool == NULL || token == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (!rt_park_pool_token_is_live(pool, token) || !pool->slots[token->index].reserved) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    pool->slots[token->index].reserved = 0;
    rt_park_pool_return_slot(pool, token->index);
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_park_pool_release(rt_park_pool* pool, const rt_park_token* token) {
    if (pool == NULL || token == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (!rt_park_pool_token_is_live(pool, token)) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (pool->slots[token->index].reserved) {
        // A transfer is mid-flight on this very slot; ending the park now would
        // free bytes the other half is still writing.
        return RT_SLOT_CONTROL_BUSY;
    }
    rt_park_pool_destroy_slot(pool, token->index, 1);
    rt_park_pool_return_slot(pool, token->index);
    return RT_SLOT_CONTROL_OK;
}

void rt_park_pool_drain(rt_park_pool* pool) {
    if (pool == NULL || pool->operations == NULL) {
        return;
    }
    int first = 1;
    for (uint64_t index = 0; index < pool->capacity; index++) {
        if (pool->headers[index].state == RT_SLOT_INITIALIZED) {
            rt_park_pool_destroy_slot(pool, index, first);
            first = 0;
        }
        if (pool->slots[index].live) {
            rt_park_pool_return_slot(pool, index);
        }
    }
}

void rt_park_pool_detach_all_locked(rt_park_pool* pool) {
    if (pool == NULL || pool->operations == NULL) {
        return;
    }
    for (uint64_t index = 0; index < pool->capacity; index++) {
        rt_park_slot* slot = &pool->slots[index];
        slot->live = 0;
        slot->reserved = 0;
        slot->next_free = RT_PARK_POOL_NO_FREE;
        // Forward only, as everywhere else: a token from the park that just
        // ended can never match the number this slot carries now.
        slot->generation = pool->next_generation++;
    }
    pool->live = 0;
    // No successor may be handed a slot in a pool that is being destroyed, so
    // the free list is emptied rather than rebuilt.
    pool->first_free = RT_PARK_POOL_NO_FREE;
}

void rt_park_pool_drop_detached(rt_park_pool* pool) {
    if (pool == NULL || pool->operations == NULL) {
        return;
    }
    int first = 1;
    for (uint64_t index = 0; index < pool->capacity; index++) {
        if (pool->headers[index].state == RT_SLOT_INITIALIZED) {
            rt_park_pool_destroy_slot(pool, index, first);
            first = 0;
        }
    }
}
